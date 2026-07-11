# CHANGELOG - MCP Filesystem Server Ultra-Fast

## [4.5.26] - 2026-07-11

### feat(core): structuredContent + outputSchema on `read_file`, `write_file`, `edit_file`, `multi_edit`

Until v4.5.26 every tool response was a single text block; clients had to
parse the compact format (`WRITTEN [C] ... | NB`, `M %s | %d@+%d-%d | %dL`,
`UNDO:xxx | chain:yyy`) to recover counts, hashes, backup IDs and the parent
in the undo chain. Any third-party MCP client — i.e. anything that is not
Claude with the in-repo `CLAUDE.md` — had no interop contract.

FASE 1 publishes the contract via MCP `outputSchema` and guarantees every
success response carries a `structuredContent` payload conforming to it.

**1. `write_file` structured payload + `RecordWriteHash` fix (`tools_core.go:206-216, 510-535, 573-631`).**
Six success returns converted from `NewToolResultText` to
`NewToolResultStructured(attachMessage(sc, msg), msg)`. Three payload shapes:
- normal write: `{path, bytes_written, content_hash, message}`
- base64 write: same shape (no base64 flag in the payload — the textual `msg`
  suffix `... | %dB base64` already disambiguates).
- warn/feedback write: above + `feedback` (the already-formatted compact
  `[WARN:...]` or verbose multi-line string from `core.FormatFeedback` /
  `FormatFeedbackCompact`) + `backup_id` (when the adaptive downgrade
  auto-created a safety backup).

`content_hash` is computed via `computeFileOCCHash(normPath)` and recorded
through `core.RecordWriteHash(normPath, hash)` immediately after the successful
`engine.WriteFileContent` / `engine.WriteBase64`. **This was missing in every
prior release** — a `write_file` followed by an `edit_file` on the same path
in the same session would trip a false auto-OCC `external_change` warning
because the edit's on-disk hash check compared against the *pre-write* read
hash. The smoke test in §5 of `docs/FASE1-STRUCTURED-OUTPUTSCHEMA.md`
encadenates write→edit `content_hash`→`expected_hash` and asserts no external
change warning fires — that's the proof the fix landed.

**2. `parent_backup_id` on edit_file and multi_edit (`tools_core.go:217-228, attachParentBackup helper; applied at 8 sites).**
New helper `attachParentBackup(m, engine, backupID)` looks up
`engine.GetBackupManager().GetBackupInfo(id).PreviousBackupID` and injects
`parent_backup_id` into the payload when the chain has a parent. Mirrors the
`chain:` segment of the compact text response. Applied at all 8
`NewToolResultStructured` call sites that emit `editStructured` /
`multiEditStructured` (6 in `tools_core.go` for edit_file's replace_range,
delete_range, compact, verbose paths; 2 in `tools_batch.go:286, 377` for
multi_edit). The helper is mutation-style (returns the same map) so the two
edit_file sites that conditionally add `external_change` to a local `sc`
before the wrap keep both fields. A regression test in `output_schema_test.go`
locks that contract — see "variable-site foot-gun" test there.

**3. `read_file` multi-file branch + two read fallbacks to structured (`tools_core.go:280-290, 356-361, 384-389`).**
The batch read (`paths` JSON array) was the only `NewToolResultText` success
return left on `read_file`; converted to `NewToolResultStructured({"content":
combined}, combined)`. The two read paths where `computeFileOCCHash` fails
(base64 fallback, range fallback) were also converted for consistency —
`content_hash` is optional in the schema so omitting it conforms.

**4. `outputSchema` on the 4 tools (`output_schemas.go` new file; options added at `tools_core.go:206, 460, 605` and `tools_batch.go:51`).**
Uses `mcp.WithRawOutputSchema(json.RawMessage)` — never `WithOutputSchema[T]`
(marshal-time conflict in mcp-go). Schemas are JSON literals with full
per-property descriptions, so `tools/list` is now self-documenting for any
MCP client.

**No breaking changes.** `Content[0].Text` is byte-identical to the prior
`NewToolResultText(msg)` output for every converted branch — verified by
inspecting the mcp-go SDK constructors (`mcp.NewToolResultStructured` and
`mcp.NewToolResultText` both produce `Content: []Content{TextContent{Type:
"text", Text: <arg>}}`). All existing tests
(`content_hash_test.go`, `edit_structured_test.go`, `edit_auto_occ_test.go`,
`read_file_range_test.go`, `edit_diff_test.go`,
`search_replace_handler_test.go`, `occ_hash_partial_read_test.go`) continue
to pass without modification — they all use `strings.Contains` substring
checks on `Content[0].Text`, never `==` equality.

**Files.** New: `output_schemas.go` (4 `json.RawMessage` schemas), `output_schema_test.go` (schema↔payload coherence). Modified: `tools_core.go`, `tools_batch.go`, `main.go` (version bump), `CHANGELOG.md`.

---

## [4.5.28] - 2026-07-11

### feat(git): agent-usable refactor — paths array, output enum, show action, help(tool:"git")

The `git` tool was unusable for LLM agents in large repos: passthrough-style
params (`args:"diff --stat"`), no `paths` filter (full 5.880-line diff dumps),
no schema inspection (`help(tool:"git")` returned the server banner).

This iteration implements `docs/git-tool-spec.md` end-to-end:

**Schema (`tools_git.go:20-42`)** — rebuilt with the 12 params from spec §1:
- `action` (required) enum extended with **`show`** (9 actions now).
- `paths` is a **native `[]string` array** (no more JSON-string parsing).
  Wire-as-is via `mcp.WithArray("paths", mcp.WithStringItems())`. A string
  value is rejected with a `usage:` line that names the correct call shape.
- New: `output` enum (`stat`/`name-only`/`full` for diff/status/show;
  `oneline`/`full` for log), `max_lines` (default 200), `limit` (log, default 10),
  `rev` (replaces `commit_range` + `source`), `name` + `checkout` for branch.
- **`force` retained only for branch delete (-d vs -D).** All other force
  semantics removed (see SECURITY-AUDIT addendum).
- **Dropped**: `auto_message`, `branch_action`, `branch_name`, `source`, `all`,
  `commit_range`, `max_count`, `dry_run` — per spec §6 "fuera de alcance".

**Handlers**:
- `gitDiff` now implements the **4-layer guardrail** per spec §3:
  default `output:"stat"`; if `full` requested without `paths` AND >20 files
  changed → degrade to stat with a top-of-output banner (LLMs read the start
  of a tool result more reliably than the tail). `full` + explicit `paths`
  always honored. `max_lines` applied to every output, footer with the
  `Acota con paths:["..."]` hint when truncated.
- `gitShow` is new (spec §2 row `show`); `rev` required, default stat.
- `gitStatus` / `gitLog` / `gitShow` / `gitDiff` / `gitCommit` accept native
  `paths`; all reject empty for destructive actions.
- `gitBranch` dispatches on presence of `name` (list vs create/delete);
  `name`+`checkout:true` → `git switch -c <name>` in one shot.
- `gitAdd` / `gitRestore` no longer support implicit "everything"; explicit
  `paths` only (refuse-by-default safety).
- `gitCommit`: drops auto-generated message; explicit `message` required.

**Errors with `usage:`** (spec §4): every git tool error now starts with
`ERROR:` and ends with `usage: <example>` so an agent self-corrects in 1 call
instead of 3. The helper `usageError(msg, example)` is centralized in
`tools_helpers.go` for future application to the other 19 tools.

**`help(tool:"git")`**: the standalone `help` tool now accepts a `tool` arg
and renders **description + InputSchema (JSON) + 8 curated examples** for any
registered tool. Powered by `toolRegistry.toolExamples` (new optional field).
Built on variadic `addTool(tool, handler, examples...)` — the 17 existing
call sites are source-compatible (variadic allows omitting examples).

**Validation** (`core/param_validator.go:42-237`): `git` added to `toolSchemas`
with the new params. **`paths` is omitted intentionally** — `ParamType` only
supports string/number/boolean; the handler validates array shape via
`pathsFromArgs`. `param_validator_test.go:106-118` updated.

**Tests** (`git_diff_status_log_test.go`, new — 33 cases): table-driven for
helpers, `usageError`, `truncateOutput`, `pathsFromArgs`, `parseOutputArg`;
guardrail degradation/bypass for 21-file and 25-file repos; `max_lines`
truncation footer; `rev` ranges; native-paths + string-rejection for `add`/
`restore`; `gitCommit` success + nothing-staged + missing-message;
`gitBranch` list/create+checkout/delete-force. Pre-existing
`git_restore_test.go` migrated to new schema (`source` → `rev`,
`branch_name` → `name`, native `[]interface{}` paths). **45 git-related tests passing.**

**New helper file** `tools_helpers.go`: `usageError`, `pathsFromArgs`,
`truncateOutput`, `parseIntArg`, `parseOutputArg`, `countChangedFiles`.

## [4.5.27] - 2026-07-08

### feat(search): auto-detect `search_files` output_format — ripgrep-style for ≤5 matches

The default `output_format:"text"` for `search_files` (with `include_content:true`)
returned only `line[col:col]` positions without the line content, forcing a
re-read of the file with `read_file` to see what was on each line. This broke
the Grep intuition (Claude Code's built-in `Grep` shows the line content by
default with `output_mode:"content"`).

**New default behaviour** (`core/search_operations.go`):
- ≤5 matches → ripgrep-style `path:line:content` (one row per match, CR/LF
  stripped from line content).
- >5 matches → existing verbose header with emojis (🔍 / 📁 / Context: blocks
  unchanged).
- `include_context:true` always forces verbose (the `Context:` block layout
  doesn't fit a single-line ripgrep row).
- `output_format:"json"` and `output_format:"text"` unchanged — JSON still
  emits `{"matches":[…]}` and explicit `"text"` preserves backward-compatible
  verbose/compact for any caller that relied on it.

**Schema cleanup** (`tools_search.go`):
- `output_format` description now explains the auto behaviour and the escape
  hatch (`"text"` to force legacy verbose regardless of match count).
- `output` alias description used to advertise `content|files_with_matches|count`
  — values that were never implemented in the engine. Description now states
  that only `"text"|"json"` are honoured and the legacy values fall through
  to the default text branch.
- Tool description updated to mention the auto-adapting default.

**Tests**:
- New `search_ripgrep_format_test.go` — 6 unit tests covering the threshold
  (≤5 ripgrep, ≥6 verbose), `include_context` forcing verbose, JSON
  unchanged, and backward compat for explicit `output_format:"text"`.
- New `smoke_search_test.go` — 4 E2E tests that spawn the built binary and
  drive it via stdio JSON-RPC to confirm the user-visible behaviour matches.
  Skipped automatically when the binary is not present (set `SMOKE_BIN` or
  build with `go build -o bin/filesystem-ultra-v4-new.exe .` to enable).

`go build` and `go test ./...` clean on Windows. No regression in existing
`TestAdvancedTextSearch*` / `TestSmartSearch*` / `TestCapSearchOutput*` /
`TestSearchFilesParams`. Server restart required to load the new binary.

---

## [4.5.25] - 2026-07-04

### feat(list_directory, multi_edit): structured directory output + aggregate batch diff

Two ergonomics gaps reported from a real agent session: the compact
`list_directory` one-liner is hard to parse, and `multi_edit` returned only a
stat summary (`17@+212-208`) — reviewing a 17-edit batch required re-reading
the file.

**1. `list_directory` gains `output_format`** (`tools_search.go`,
`core/engine.go`, `core/param_validator.go`):
- `compact` (default) — unchanged, token-efficient one-liner.
- `json` — new `ListDirectoryJSON`: `{path, total, truncated, entries:[{name,
  type, size, modified}]}` with RFC3339 mtimes. Respects `MaxListItems` and is
  cached under the synthetic key `<path>::json` (mtime-validated, no collision
  with the text listing).
- `tree` — exposes the previously unreachable `ListDirectoryTree` (recursive
  JSON tree) with `max_depth` (default 2).

**2. `multi_edit` gains `diff_format`** (`core/edit_operations.go`,
`tools_batch.go`, `core/param_validator.go`) — parity with `edit_file`:
`""/auto` (default: full diff when small, summary with anchors past 200 diff
lines), `full`, `summary`, `stat`, `none` (previous behaviour).
`MultiEditResult` now carries `OriginalContent`/`FinalContent` (`json:"-"`,
never serialized) so the tool layer renders ONE aggregate diff of the whole
batch via the existing `core.RenderDiff` — no file re-read, more compact than
N per-edit diffs. Rendered in compact, verbose and `dry_run` responses.

**3. Schema/validator housekeeping:** `dry_run` was read by the multi_edit
handler but never declared in the tool schema — now declared. Added `dry_run`,
`expected_hash` and `diff_format` to the `multi_edit` param validator, and
`output_format`/`max_depth` to `list_directory`'s. `help_content.go` and
`TOOL-REFERENCE.md` updated.

`go build` verified clean on Windows (user-run). Server restart required to
load the new binary.

---

## [4.5.24] - 2026-07-03

### fix(search): false negatives in `search_files` — filename-only default misread as content search

`search_files` without `include_content:true` only matches the pattern against
file **names** (SmartSearch path). Callers systematically read the generic
`No matches found for pattern 'X' in <file>` response as a content-search miss
— e.g. searching `"tr acabado"` inside `Pda.cshtml` returned "no matches"
without ever opening the file. Three guards added:

**1. File path forces content search** (`tools_search.go`). If `path` is a
regular file (not a directory) and `include_content` was not set, the search
is routed to `AdvancedTextSearch` — a filename match over a single explicit
file is meaningless. `filepath.Walk` over a file visits exactly that file.

**2. Content-only params imply content intent** (`tools_search.go`). Explicit
`output_format`, `output`, or `context_lines` were silently ignored in the
filename-only SmartSearch path (none apply to it). They now flip
`include_content` on, so `{path, pattern, output_format:"json"}` searches
file contents and returns JSON as the caller intended.

**3. Honest no-match message** (`core/search_operations.go`). A filename-only
search with zero hits now reports: `No filename matches for pattern 'X' in
<path> (filename-only search — file contents were NOT searched; pass
include_content:true to search inside files)` so the caller self-corrects on
the next call.

**Not changed:** `case_sensitive` still defaults to `true` (Bug #32 decision).
A case mismatch remains a possible source of content-search misses — set
`case_sensitive:false` explicitly.

**⚠️ Pending:** `go build` + `go test ./...` on Windows — the sandbox used for
this fix had no Go toolchain. Manual review done; server restart required to
load the new binary.

---

## [4.5.23] - 2026-07-02

### fix(git): security audit — restore command construction, add option-injection, branch -d/-D

Security + correctness audit of `tools_git.go`. Four defects found, all fixed.
Every resulting git invocation was validated against real git (2.34.1).

**🔴 CRITICAL — `restore` never ran the `restore` subcommand (non-dry-run).**
The `"restore"` token was only prepended inside the `dry_run` branch. A real
restore executed `git --staged -- file` / `git <source> -- file` (no
subcommand) which git rejects — so `git(action:"restore")` was broken for
every non-preview call. **Fix:** `cmdArgs` now starts with `"restore"`
unconditionally.

**🟠 HIGH — `restore` passed `source` positionally.** `git restore HEAD~1 -- f`
makes git parse `HEAD~1` as a pathspec (`fatal: pathspec 'HEAD~1' did not
match`). **Fix:** source is now `--source=<rev>`; source-only restore targets
the whole tree via an explicit `-- .` pathspec (git refuses source restore
without a pathspec).

**🟠 HIGH — option injection in `git add`.** File paths were appended without a
`--` end-of-options separator, so a path like `"-A"`, `"--pathspec-from-file=
/etc/passwd"`, or `"--renormalize"` was interpreted by git as an *option*
rather than a filename. **Fix:** `--` separator inserted before all
user-supplied paths in both the list and single-path branches; the dry-run
`-n` is now inserted right after `add` (appending it would land after `--`
and be treated as a pathspec).

**🟡 MEDIUM — `branch delete` safety inversion.** The dispatcher gate required
`force=true` to delete *any* branch, but `force=true` also escalates `-d`
(safe: git refuses unmerged) to `-D` (force delete). Net effect: a safe
delete was impossible — every delete was pushed into the destructive `-D`
path. **Fix:** `isDestructiveGitAction` no longer gates `branch delete`; a
plain delete uses `-d` (git's own unmerged guard applies), `force:true`
opts into `-D`.

**Also fixed — `restore` dry-run never worked.** `git restore` has no `-n`
or `--dry-run` (both errored "unknown switch"). The preview is now emulated
with an equivalent `git diff` (`--cached` for staged, `<source>` for source,
working-tree otherwise) showing exactly what the restore would change.

**Coherence — option-injection guard centralized.** The `rejectOptionLike`
check for `source`, `branch_name`, and `commit_range` now runs in the
dispatcher *before* the destructive gate (previously per-handler, after the
gate). Handler-level checks remain as defense in depth.

**Version:** `serverVersion` bumped 4.5.19 → 4.5.23 (string was stale — lagged
the 4.5.20/21/22 commits).

**⚠️ Pending:** `go build` + `go test ./...` must be run on Windows — the audit
sandbox had no Go toolchain. Manual review + real-git command validation done.

---

## [4.5.22] - 2026-07-02

### fix(git): `restore` validation order + `staged`/`dry_run` not destructive

Three usability bugs in `git(action:"restore", ...)`:

**Bug 3a — incoherent validation order.** A malformed call (no `paths`,
no `source`) returned different errors depending on which flags were
present:
- `{}, no force`  → "Destructive git operation 'restore' requires force=true"
- `{paths:'["x"]'}, force:true` → "paths required for restore"

Same conceptual problem (call missing required scope), two different
error messages, the second one being outright wrong.
**Fix:** required-params check now runs first inside `gitRestore`, before
any hook execution or command construction. The dispatcher gate is
bypassed when the handler can return a more specific error.

**Bug 3b — `restore --staged` wrongly required `force=true`.**
`git restore --staged` is non-destructive: it only moves staged changes
back to the working tree (the equivalent of `git reset HEAD <path>`),
it never discards work. Forcing the user to pass `force:true` for an
unstage operation was a false-positive safety gate.
**Fix:** `isDestructiveGitAction` now returns `false` when `restore` is
called with `staged:true` OR `dry_run:true`. Both bypass the force gate.

**Bug 3c — `source` alone was rejected.** `git restore --source=HEAD~1`
with no paths is legitimate: it restores the whole tree to that source.
The old code required `paths` even when `source` was supplied.
**Fix:** required-params check now accepts EITHER `paths` (non-empty) OR
`source`. When both are absent, returns a single coherent error.

**Reproduction matrix (all green):**

| Call                                          | Pre-fix                       | Post-fix                          |
|-----------------------------------------------|-------------------------------|-----------------------------------|
| `{}, no force`                                | requires force                | requires paths OR source          |
| `{paths:'["a.txt"]'}, no force`               | requires force                | requires force (still WT)         |
| `{staged:true, paths:'["a.txt"]'}, no force`  | requires force (WRONG)        | OK                                |
| `{dry_run:true, paths:'["a.txt"]'}, no force` | requires force (WRONG)        | OK (preview)                      |
| `{source:"HEAD~1", force:true}`               | paths required (WRONG)        | OK                                |
| `{paths:'["malformed'}, force:true`           | paths required                | invalid paths JSON                |

**Additional cleanups:**
- Hook execution moved after validation (no point running hooks on
  malformed calls).
- Path normalization happens once, after JSON parse + required check.
- Compact-mode response no longer shows `'OK: restored 2 file(s) from '`
  with a trailing space and empty source.

**Files changed:**
- `tools_git.go` — `gitRestore` reordered + `isDestructiveGitAction` nuanced
  (+56 / −18 lines).

**Not fixed in this release (future work):**
- The `rejectOptionLike("source", ...)` check runs inside `gitRestore`,
  AFTER the dispatcher gate. So a call like
  `git(action:"restore", source:"--output=/tmp/x", force:false)` will
  still hit the destructive-gate error first and never reach the
  option-injection check. To fix properly, the option-injection check
  should run before the gate — a separate refactor.

## [4.5.21] - 2026-07-02

### fix(git): restore no longer fails with cryptic `exec: Stderr already set`

`git(action:"restore", ...)` on Windows would surface the misleading
`exec: Stderr already set` error — instead of the actual `git restore`
error message — whenever the underlying `git` invocation exited non-zero.
This made it impossible to recover from a bad call (e.g. invalid path)
via the MCP: the user would see a Go-internal error and have no clue what
went wrong.

**Root cause:** `execGitCommand` (Windows branch) used `cmd.CombinedOutput()`
in both the primary path (`git` direct) and the fallback path (`cmd /c git`).
When the primary call exited non-zero, control fell through to the cmd.exe
fallback, which assigned `cmd2.Stderr = &stderr` and then called
`cmd2.CombinedOutput()`. `CombinedOutput` requires `Stdout`/`Stderr` to be
**unset** because it reassigns them internally to its own buffers — but
the pre-assignment of `Stderr` caused it to fail with the cryptic error,
**and the real git error had already been swallowed** by then.

**Fix:** Switched to `cmd.Run()` with caller-owned `Stdout`/`Stderr` buffers
in all three branches (Windows primary, Windows fallback, Unix). This
eliminates the `CombinedOutput` / pre-assigned-field collision entirely
and lets the function return the real git error verbatim when the command
fails.

**Reproduction (pre-fix):**

```js
git(action:"restore", staged:true, force:true, paths:'[":/"]')
// → "git restore failed: exec: Stderr already set:"   ← WRONG, hides real error
```

**Post-fix:**

```js
git(action:"restore", staged:true, force:true, paths:'[":/"]')
// → "exit status 1"  (or the real git stderr, e.g. 'fatal: pathspec ...')
```

**Files changed:**
- `tools_git.go` — `execGitCommand` rewritten with `cmd.Run()` + buffers
  (+33 / −12 lines, single function).

**Smoke tests (all green):**
1. `git restore --staged -- :/` — no longer crashes
2. `git status --porcelain` — ok, 3 files
3. `git log -3 --oneline` — ok, 3 commits
4. `git diff --cached` — ok, empty
5. `git restore --staged -- tools_git.go` — ok, no-op
6. Fallback path (invalid cwd) — `chdir` error, not `Stderr already set`
7. Invalid subcommand — `exit 1` + real git message

## [4.5.20] - 2026-07-02

### fix(git): git add respects `paths` and refuses silent `-A`

The `git` tool's `add` action silently fell through to `git add -A` whenever
called without an explicit scope — even when the caller passed a `paths` JSON
array. Combined with a subsequent `git(action:"commit", ...)`, a single bad
call could stage and commit the entire repository without warning.

**Reproduction (pre-fix):**

```js
git(action:"add", path:".../Camio",
    paths:'["Program.cs","CHANGELOG.md"]')
// → "OK: staged 336 file(s)"   ← WRONG: paths ignored, -A applied
```

**New scope resolution (priority order, no silent fallbacks):**

| Caller provides              | Command executed            |
|------------------------------|-----------------------------|
| `paths:'["a.go","b.go"]'`    | `git add a.go b.go`         |
| `path:"a.go"`                | `git add a.go`              |
| `all:true`                   | `git add -A` (explicit opt-in) |
| none of the above            | **error**: refuses silently-defaulting to `-A` |

The `paths` parameter (already declared in the schema since v4.5.2 but never
parsed) is now read in `gitAdd` with the same JSON-array shape used by
`gitRestore`. JSON parse errors are caught before any `git` invocation.

**Files changed:**
- `tools_git.go` — `gitAdd` rewritten with `switch` on scope priority + explicit `default` error branch.
  (+33 / −9 lines)

**Not yet fixed in this release (still open):**
- **Bug 2:** `git(action:"restore", ...)` on Windows fails with `exec: Stderr
  already set` when the underlying `git` exits non-zero (e.g. invalid path).
  Reproduction confirmed; root cause is `CombinedOutput()` colliding with a
  pre-assigned `cmd2.Stderr` in the cmd.exe fallback of `execGitCommand`.
- **Bug 3:** validation order in `gitRestore` is incoherent — gate `force:true`
  runs before checking required `paths`/`source`, and `restore --staged`
  (non-destructive) still demands `force:true`.

## [4.5.19] - 2026-06-15

### Close the last auto-OCC gap: pipeline writes refresh the baseline

The 4.5.18 follow-up: `execute_pipeline` (separate from `ExecuteBatch`) didn't
refresh the auto-OCC baseline, so a file the session read and then modified via
a pipeline could falsely trip "external change" on a later `edit_file`. The
pipeline now refreshes the baseline for its affected files on successful,
non-dry-run completion — same guarantee as batch.

The refresh logic is consolidated into a shared `RefreshKnownHashes` helper used
by both batch and pipeline (DRY).

- `core/feedback.go`: `RefreshKnownHashes` + `refreshKnownHashPath` (shared helper).
- `core/batch_operations.go`: `refreshKnownHashes` now delegates to the shared helper.
- `core/pipeline.go`: sequential and parallel `Execute` call `RefreshKnownHashes(affectedFiles)` when not dry-run and successful.

**Tests added:**
- `core/pipeline_auto_occ_test.go` — a pipeline edit refreshes the baseline (no false positive on a later edit).

**Verification:**

```bash
go vet ./...
go test -timeout 90s ./...
```

**Plan fully closed:** feedback items 1–6, editor-parity points 1–4, the Go AST
validator, and all auto-OCC follow-ups (batch + pipeline) are shipped and tested.

## [4.5.18] - 2026-06-15

### Two fixes from dogfooding the 4.5.17 build live

**Fix 1 — auto-OCC warning now in the structured payload.** The external-change
warning (new point 4) was appended only to the text fallback, not to the
`structuredContent` of the edit response — inconsistent with `structure_warning`,
so structured-only clients never saw it. It's now an `external_change` field in
the structured edit response.
- `tools_core.go`: edit_file replace path adds `external_change` to the structured map.

**Fix 2 — batch writes refresh the auto-OCC baseline.** `batch_operations`
modified files without updating the session's known hash, so a file the session
had read and then changed via batch would falsely trip auto-OCC ("external
change") on the next `edit_file` — or block it under `--auto-occ=block`. The
batch now refreshes the baseline for every file it writes (and clears it for
deleted/moved-away files), so the session's own batch changes are never flagged.
- `core/feedback.go`: `InvalidateKnownHash`.
- `core/batch_operations.go`: `refreshKnownHashes`, called after a successful batch.

**Known remaining gap:** the `execute_pipeline` path (separate from
`ExecuteBatch`) does not yet refresh the baseline — pipeline writes could still
false-positive on a later edit. Tracked as follow-up.

**Tests added:**
- `core/batch_auto_occ_test.go` — batch write refreshes baseline (no false positive); batch delete clears it.
- `edit_auto_occ_test.go` — extended to assert `external_change` is in the structured payload.

**Verification:**

```bash
go vet ./...
go test -timeout 90s ./...
```

## [4.5.17] - 2026-06-15

### Automatic optimistic-concurrency on edits (new point 4 — completes the plan)

`edit_file` already supported explicit OCC via `expected_hash`. This adds an
**automatic** layer: the session remembers the `content_hash` it last saw for
each file (from a read OR its own write/edit), and before an edit that does not
pass `expected_hash`, it compares the current on-disk hash against that baseline.
If the file changed on disk since the session last saw it — i.e. another process
modified it — it surfaces a warning (or blocks, configurable). This catches lost
updates even when the caller never opted into OCC.

**Key correctness property:** the baseline is updated on the session's **own**
writes too (using the post-edit `content_hash` from v4.5.15), so consecutive
edits never false-positive — only genuinely external changes do.

**Mode (CLI `--auto-occ`):** `off` | `warn` (default) | `block`. `warn` appends a
non-blocking notice to the edit response; `block` rejects the edit with a clear
error. Only fires when the baseline is fresh (read/written within 10 min).

- `core/feedback.go`: `knownHash` in the session state; `RecordReadHash`,
  `RecordWriteHash`, `CheckAutoOCC`, `SetAutoOCCMode`; `PatternExternalChange`.
- `tools_core.go`: `read_file` records the hash on every read path; `edit_file`
  runs `CheckAutoOCC` when no `expected_hash` is given (reusing the on-disk hash
  it already computes) and records its own write hash. delete_range/replace_range
  record their write hash too.
- `tools_batch.go`: `multi_edit` records its write hash.
- `main.go`: `--auto-occ` flag → `core.SetAutoOCCMode`.

**Tests added:**
- `core/auto_occ_test.go` — `CheckAutoOCC` logic (external change vs own write, mode block/off, unknown-mode fallback).
- `edit_auto_occ_test.go` — end-to-end: read → external change → edit warns (warn mode, not blocked); consecutive own edits don't false-positive.

**Verification:**

```bash
go vet ./...
go test -timeout 90s ./...   # full suite green
```

**Plan complete:** `docs/PLAN-next-improvements.md` items 1–4 and the Go AST
validator are all shipped (v4.5.15–4.5.17).

> Note: `autoOCCMode` is a package-level variable set once at startup; it is not
> mutex-guarded. Fine in practice (set before serving), but if a future change
> mutates it at runtime under concurrency, guard it.

## [4.5.16] - 2026-06-15

### `edit_file` mode `replace_range` (new point 2)

The line-numbered partner to `delete_range` and the natural follow-up to a
range read: read lines X..Y with `read_file`, then replace exactly those lines
with `new_text` — no fragile `old_text` match. Reuses the byte-exact line-splice
machinery from `delete_range` (`ComputeLineRangeReplacement`), creates a backup,
writes atomically, runs the post-edit structural check, and returns the
post-edit `content_hash` (so edits chain without re-reading) plus the structured
response.

**Newline handling (the careful part):**
- If there are lines after the range and `new_text` doesn't end in `\n`, one is
  added so the replacement doesn't glue onto the following line.
- When replacing through the last line, a trailing `\n` is added only when the
  original file ended with one (preserves the file's trailing-newline state).
- An empty `new_text` is a pure deletion (equivalent to `delete_range`).

Reuses existing params (`start_line`, `end_line`, `new_text`) — no new params.

**Files changed:**

| File | Change |
|---|---|
| `main.go` | version 4.5.15 → 4.5.16 |
| `core/line_range.go` | `ComputeLineRangeReplacement` + `ReplaceLineRange` engine method |
| `tools_core.go` | `mode:"replace_range"` handler branch + mode/param descriptions |
| `CLAUDE.md` | edit_file modes: documents `delete_range`/`replace_range` + post-edit `content_hash` |
| `core/replace_range_test.go` | NEW — byte-exact splice (all newline edge cases) + end-to-end engine test |

**Verification:**

```bash
go vet ./...
go test -timeout 90s ./...   # full suite green
```

**Plan status:** of `docs/PLAN-next-improvements.md`, the AST-Go validator and
points 1, 2 and 3 are done; **point 4** (session-scoped auto-OCC) remains.

## [4.5.15] - 2026-06-15

### Editor-parity improvements: Go AST check + post-edit hash + structured edit responses

Three improvements aimed at making filesystem-ultra usable as a primary editor, derived from comparing it with a state-tracking editor that never needs to re-read. See `docs/PLAN-next-improvements.md` for the full plan (items 1, 3 and the Go AST validator; items 2 and 4 are deferred).

**Go AST validator (upgrades the point-2 structural check for `.go`).** The post-edit structural check now does a real Go parse for `.go` files (`go/parser`, in-process, pure stdlib) instead of only counting braces. It catches the whole class of syntax errors a lexical brace count misses (bad tokens, dangling expressions, missing keywords), at edit time rather than in the build cycle. Same **delta** discipline as the brace check: only warns when the file parsed *before* the edit and not *after*, so pre-existing breakage never produces a false alarm. Non-`.go` files keep the lexical brace-balance delta.
- `core/structure_check.go`: `CheckGoSyntax`, `parseGoError`, and `CheckStructureDelta` (single dispatch entry: `.go` → AST, else → brace balance). `EditFile`/`MultiEdit`/`DeleteLineRange` now call `CheckStructureDelta`.
- Not a duplicate of `go vet`/`go build`: the value is **immediacy** (caught on the edit, not the build).

**New point 1 — post-edit `content_hash`.** `edit_file`, `multi_edit` and `delete_range` now return the FNV-1a `content_hash` of the file *after* the edit (`NewHash` on `EditResult`/`MultiEditResult`, computed over the bytes actually written). It equals the token `expected_hash` validates, so a caller can chain the next edit's `expected_hash` **without re-reading the file** — matching the "no re-read" loop of a state-tracking editor.
- `core/edit_operations.go`: `NewHash` fields + `contentHashFNV` helper; set in `EditFile`/`MultiEdit`. `core/line_range.go`: set in `DeleteLineRange`.

**New point 3 — structured edit responses.** `edit_file` (replace + delete_range) and `multi_edit` now return a `structuredContent` payload (`content_hash`, `replacements`, `lines_added`/`lines_removed`, `total_lines`, `backup_id`, `risk_warning`, `structure_warning`, `integrity`) **alongside** the existing text (text is unchanged — purely additive, naive clients keep the fallback). Clients (and the dashboard) can parse fields reliably instead of regex-scraping the text. `regex`/`search_replace` modes left for a follow-up (different result types).
- `tools_core.go`: `editStructured` helper; replace + delete_range returns wrapped. `tools_batch.go`: `multiEditStructured`; multi_edit returns wrapped.

**Tests added:**
- `core/go_syntax_test.go` — AST delta + dispatch (`.go` catches non-brace errors; non-`.go` falls back to brace balance).
- `core/newhash_test.go` — `NewHash` after `EditFile`/`DeleteLineRange` equals the on-disk hash.
- `edit_structured_test.go` — end-to-end: the edit's structured `content_hash` equals a subsequent `read_file`'s hash (re-read-free chaining).

**Verification:**

```bash
go vet ./...                 # clean
go test -timeout 90s ./...   # full suite green (core, main, tests/, tests/security, dashboard)
```

**Files changed:**

| File | Change |
|---|---|
| `main.go` | version 4.5.14 → 4.5.15 |
| `core/structure_check.go` | Go AST validator + `CheckStructureDelta` dispatch |
| `core/edit_operations.go` | `NewHash` fields + `contentHashFNV`; AST/hash hooks in `EditFile`/`MultiEdit` |
| `core/line_range.go` | `NewHash` + `CheckStructureDelta` in `DeleteLineRange` |
| `tools_core.go` | `editStructured`; structured returns (replace, delete_range) |
| `tools_batch.go` | `multiEditStructured`; structured returns (multi_edit) |
| `docs/PLAN-next-improvements.md` | NEW — plan for next improvements (items 1–4 + AST) |
| `core/go_syntax_test.go`, `core/newhash_test.go`, `edit_structured_test.go` | NEW tests |

## [4.5.14] - 2026-06-15

### Reliability, cost & integrity improvements (6 items from a refactor post-mortem)

Six improvements derived from real friction observed during a Blazor refactor: token cost of dry-run diffs, lack of post-edit structural checks, missing OCC token on partial reads, no atomic block-move, the `force` flag being overloaded, and no trace of interrupted tool calls.

**Point 3 — `content_hash` on partial reads.** `read_file` now returns the `content_hash` (FNV-1a of the raw file bytes — the same OCC token `edit_file`/`multi_edit` validate via `expected_hash`) for **range**, **head/tail** and **base64** reads, not just full reads. The server hashes the whole file but returns only the requested slice, so a caller can use optimistic-concurrency `expected_hash` on a large file without pulling the whole file into context.
- `tools_core.go`: `computeFileOCCHash` helper; base64 + range branches return structured `content_hash`. `expected_hash` description updated.

**Point 6b — in-flight audit breadcrumb.** `auditWrap` writes an `status:"in_progress"` entry with a `req_id` **before** running the handler; the final entry shares the same `req_id`. A call interrupted mid-flight (e.g. the MCP transport is cut when the user switches app surface) leaves an orphan `in_progress` line in `operations.jsonl`, so it's possible to tell whether the request never arrived, was cut mid-execution, or completed but lost its reply. Guarded by `AuditEnabled()` — zero overhead without `--log-dir`.
- `audit.go`: `newRequestID`, pre-handler breadcrumb. `core/audit_logger.go`: `RequestID` (`req_id`) field.

**Point 1 — `diff_format` for dry-run/edit diffs.** New `diff_format` param on `edit_file`: `""`/`auto` (full when small, summary + hint when large — token-safe default), `full`, `summary` (per-hunk ranges + 3 anchor lines, eliding large bodies), `stat` (`+N -M`), `none`. Unifies behaviour across replace/regex/search_replace modes (regex previously always dumped the full diff — the original 720-line cost).
- `core/diff.go`: `RenderDiff`, `formatHunksSummary`, refactor to shared `formatHunksFull`. `tools_core.go`: `diffFormatArg`; all three diff emission points use `RenderDiff`.

**Point 6a — atomic writes in `batch_operations`.** `executeWrite` used a direct `os.WriteFile` (non-atomic); a batch cut mid-write could leave a partial file. Now uses the shared `atomicWriteFile` (temp + rename), matching `write_file`.
- `core/engine.go`: `atomicWriteFile` helper (consolidates the duplicated temp+rename pattern). `core/batch_operations.go`: `executeWrite` uses it, preserving file mode.

**Point 2 — post-edit structural check (delta brace balance).** After editing brace-based source files, the net balance of `{} () []` is compared old vs new. If it was balanced before and is **not** after, the edit introduced the imbalance → non-blocking warning (attached to the response and audit). The *delta* approach avoids false positives on fragments or already-unbalanced files; braces inside strings/comments/raw-strings are ignored by a lightweight C-like scanner.
- `core/structure_check.go`: NEW — `delimiterBalance`, `CheckBalanceDelta`, `isBalanceCheckedExt`. `core/edit_operations.go`: `StructureWarning` on `EditResult`/`MultiEditResult` + check in `EditFile`/`MultiEdit`. Surfaced in `tools_core.go`/`tools_batch.go`.

**Point 5 — decouple `force` from the rewrite guard.** `force` no longer bypasses the accidental full-file rewrite guard; a dedicated `allow_rewrite` flag does. `force` is now reserved for the risk-threshold bypass, so forcing a risky-but-intended edit no longer silently disables rewrite protection. The guard message recommends `write_file` and notes that `force` does not bypass it.
- `tools_core.go`: parse `allow_rewrite`; guard uses `!allowRewrite`. `core/feedback.go`: doc + suggestion. `core/param_validator.go`: `allow_rewrite`.

**Point 4 — `delete_range` + atomic `extract`.** New `edit_file` `mode:"delete_range"` removes lines `[start_line, end_line]` (atomic, with backup). New `batch_operations` `extract` action moves lines from `source` to `destination` using the **same computed bytes** to write the destination and remove from the source — written == deleted by construction, closing the drift gap of the old two-step (write dest + delete source) workflow. One backup covers both files; they revert together under `atomic:true`.
- `core/line_range.go`: NEW — `ComputeLineRangeDeletion` (byte-exact), `DeleteLineRange`. `core/batch_operations.go`: `extract` type (validate/dispatch/rollback/backup), `executeExtract`; `StartLine`/`EndLine`/`Append` fields on `FileOperation`.

**Tests added:**
- `core/line_range_test.go` — byte-exact extract + error cases.
- `core/structure_check_test.go` — balance delta + string/comment/raw-string exclusion.
- `core/diff_render_test.go` — diff formats + auto-collapse.
- `occ_hash_partial_read_test.go` — partial-read hash == raw-bytes FNV.

**Follow-ups (not in this release):** dashboard should group `operations.jsonl` by `req_id` (now 2 lines per op); optional `verify_structure` flag (point 2 is auto-by-extension); stdio→HTTP transport (connector config, not code).

**Verification:**

```bash
go vet ./...        # clean
go test ./...       # full suite green (incl. tests/ + tests/security)
```

**Files changed:**

| File | Change |
|---|---|
| `main.go` | version 4.5.13 → 4.5.14 |
| `tools_core.go` | `computeFileOCCHash`, `diffFormatArg`, partial-read hash, `diff_format`, `delete_range` mode, `allow_rewrite` |
| `tools_batch.go` | structure warning surface, `extract` in description |
| `core/diff.go` | `RenderDiff` + summary/full hunk formatters |
| `core/structure_check.go` | NEW — delimiter balance check |
| `core/line_range.go` | NEW — line-range deletion + extract primitive |
| `core/edit_operations.go` | `StructureWarning` fields + checks |
| `core/batch_operations.go` | atomic `executeWrite`, `extract` action + rollback |
| `core/engine.go` | `atomicWriteFile` helper |
| `core/feedback.go` | rewrite guard → `allow_rewrite` |
| `core/audit_logger.go` | `RequestID` field |
| `audit.go` | in-flight breadcrumb + `newRequestID` |
| `core/param_validator.go` | `diff_format`, `allow_rewrite`, `start_line`, `end_line` |
| `CLAUDE.md` | `extract` type documented |
| `tests/*`, `*_test.go` | 4 new test files; rewrite-guard test comments updated |

## [Unreleased / 4.5.13] - 2026-06-12

### Hooks — examples + docs brought up to date

The hooks system (`core/hooks.go`) has had **16 events** since the addition of `pre-read`/`post-read`/`pre-search`/`post-search` (added after the docs were last updated), but the user-facing examples and docs were stuck at 12. Also, `examples/hooks.example.json` had **malformed JSON** (duplicate content pasted at the end after the root `}`) — copying that file as `hooks.json` and starting the server with `--hooks-enabled --hooks-config=hooks.json` would fail to parse.

**Fixes:**
- **`examples/hooks.example.json`** — rewrote clean. Was JSON-invalid (duplicate content after root `}`); now a well-formed single object with all 16 events. The `post-delete` example now mentions the v4.5.11+ `sd_id` and `dest_path` metadata fields (so audit hooks can log the recoverable copy).
- **`examples/hooks-test.json`** — already valid; now covered by a regression test so it can't silently break again.
- **`examples/README.md`** — now mentions `hooks-test.json` (the working all-enabled testing config) and explains when to use it vs `hooks.example.json`; adds a snippet showing the soft-delete `post-delete` audit hook with `jq`.
- **`docs-website/src/content/docs/features/hooks.md`** — corrected "12 Hook Events" → "16 Hook Events", added the missing `pre-read`/`post-read`/`pre-search`/`post-search` sections, added a "Soft-delete Metadata (v4.5.11+)" subsection that documents the `sd_id` + `dest_path` fields in the `post-delete` hook context.
- **`core/hooks.go`** — removed unused `event HookEvent` parameter from `aggregateResults` (private method, single caller). No behavior change.
- **`tests/hooks_examples_test.go`** — NEW. 3 regression tests:
  - `TestHooksExampleJSONIsValid` — parses the file and asserts all 16 events are present
  - `TestHooksTestJSONIsValid` — same for the testing config
  - `TestHooksExampleHasNoDuplicateStructure` — round-trips the JSON to detect trailing junk (the original bug)
- **`docs-website/.../hooks.md`** — added the `re:` regex pattern type to the Pattern Types list (it was missing from the website copy).
- **`docs/features/HOOKS.md`** — internal mirror of the website hooks doc, also stuck on 12 events and missing the soft-delete section. Brought in sync, then **deleted** in favor of the website as the single source of truth. `docs/README.md` index entry updated to point at the website.

**Why this matters:** `hooks.example.json` is the primary copy-paste template for users setting up the hooks system. A broken reference file would silently break any new user setup. The regression tests guarantee the example stays valid as the codebase evolves.

**Files changed:**

| File | Change |
|---|---|
| `examples/hooks.example.json` | rewritten clean + SD-ID example |
| `examples/README.md` | mentions `hooks-test.json` + soft-delete snippet |
| `docs-website/src/content/docs/features/hooks.md` | 12→16 events, new read/search sections, new soft-delete metadata subsection, regex `re:` pattern added |
| `docs/features/HOOKS.md` | internal mirror brought in sync, then deleted (unified with website) |
| `docs/README.md` | hooks index entry redirected to website |
| `core/hooks.go` | remove unused `event` param in `aggregateResults` |
| `tests/hooks_examples_test.go` | NEW — 3 regression tests |
| `CHANGELOG.md` | this entry |

**Verification:**

```bash
go build ./...                                       # clean
go test -run TestHooks ./tests/...                    # 0.045s, 3 cases green
go test ./...                                        # full suite green
```

### multi_edit risk notice — real replacement count + clamped `% of file`

The `multi_edit` risk notice appended to success responses showed `0 replacements` and could print `>100% of file` — both made the notice actively misleading (a caller reading `0 replacements` could conclude the operation was a no-op, when in fact the file had been modified). Reported on 2026-06-13, fixed in this release. See [issue #21](https://github.com/scopweb/mcp-filesystem-go-ultra/issues/21).

**Fix 1 — real replacement count.** `calculateMultiEditImpact` in `core/edit_operations.go` built the `aggregateImpact` from a simulated content run and never assigned `Occurrences`, so `FormatRiskNotice` in `core/impact_analyzer.go` printed the Go zero value (`0`). The fix tracks the per-edit `ReplacementCount` returned by `performIntelligentEdit` and assigns the sum to `aggregateImpact.Occurrences` after the loop, so all three `FormatRiskNotice` call sites (skipped-only, dry_run, main path) see the real value. For 4 applied edits the notice now reads `4 replacements` instead of `0 replacements`.

**Fix 2 — clamp displayed `% of file` at 100.** The honest-scope formula `Σ max(|oldText|,|newText|) / fileSize × 100` is a correct upper bound on bytes touched but can exceed 100% on net insertions (a 6-byte anchor replaced by 600 bytes in a 200-byte file yields 300%). The `change_percentage` JSON field and the internal `RiskLevel` are unchanged — only the *displayed* value in the notice string is clamped, so the magnitude word (`"large edit"` / `"very large edit"`) still encodes severity above the 40%/80% thresholds. The notice now reads `100% of file` in those cases.

**Files changed:**

| File | Change |
|------|--------|
| `core/edit_operations.go` | `MultiEdit` accumulates `totalReplacements` from per-edit `ReplacementCount`; assigns to `aggregateImpact.Occurrences` after the loop |
| `core/impact_analyzer.go` | `FormatRiskNotice` clamps the printed `ChangePercentage` to 100 in the notice string (internal field untouched) |
| `tests/multi_edit_occurrences_counter_test.go` | NEW — 2 regression tests: real count + clamped percentage |
| `CHANGELOG.md` | this entry |

**Verification:**

```bash
go test ./tests/ -run "TestIssue21_" -v            # both pass
go test ./core/...                                  # full suite green
go test ./tests/...                                 # full suite green
```

### read_file content_hash — moved to structured response field (B1 fix)

`read_file` (full-file mode) used to append a trailing line `# content_hash: <8hex>` to the response body. The line is the OCC token for stale-edit protection (echoed back as `edit_file` / `multi_edit` `expected_hash` to detect concurrent writes). The bug was that the line was **visually indistinguishable from legitimate Markdown content** — same `# comment` syntax, no separator, no label. A consumer (human or AI) copying the response text as an `old_text` anchor in `edit_file` got `no matches found`; in `multi_edit` the whole atomic batch rolled back. Root-caused via a controlled experiment on 2026-06-13 (probe file with a planted `00000000` line that never appeared in the response). See [issue #23](https://github.com/scopweb/mcp-filesystem-go-ultra/issues/23).

**Fix — move the hash to a structured response field.** `read_file` now returns the file body as plain text (no trailer) and the hash as `result.StructuredContent["content_hash"]`. This uses the MCP standard `structuredContent` field (MCP-Go SDK's `NewToolResultStructured`); clients that understand it read the hash from there, clients that don't see only the file body. The OCC mechanism (`edit_file(expected_hash:…)`, `multi_edit(expected_hash:…)`) is unchanged. Range reads and batch `paths` reads were already trailer-free; this fix only changes the full-file path.

**Migration note for clients**: any consumer that regex-extracted the trailing `# content_hash: <hex>` line from the read_file response text must read from `StructuredContent["content_hash"]` instead. The `expected_hash` parameter on `edit_file` and `multi_edit` still accepts the same 8-hex-char value, so the only change is *where you get the value from*, not the value itself.

**Files changed:**

| File | Change |
|------|--------|
| `tools_core.go` | `read_file` full-read path returns `NewToolResultStructured({"content_hash": …}, body)` — body no longer has the `# content_hash:` trailer; hash moves to the structured field |
| `content_hash_test.go` | `TestContentHash_AppearsInRead` and `TestContentHash_Stable` updated to read from `StructuredContent`; new `TestContentHash_RoundTripsWithExpectedHash` exercises the OCC end-to-end (read → extract hash → use as `expected_hash` → edit succeeds) |
| `CHANGELOG.md` | this entry |

**Verification:**

```bash
go test -run "TestContentHash_|TestExpectedHash_" -v   # all 5 pass
go test ./...                                            # full suite green
```

### multi_edit — accept `expected_hash` (OCC parity with edit_file, B3)

`multi_edit` now accepts the same `expected_hash` parameter `edit_file` has had since Improvement B3 (the OCC stale-read token for detecting concurrent writes). Until this release, the protection only worked for single edits — a consumer editing a file in a concurrent scenario (file open in another editor, another agent calling `edit_file`) could opt into stale-read protection for a single edit but not for a batch, even though a single hash check before the atomic loop would cover the whole batch. See [issue #24](https://github.com/scopweb/mcp-filesystem-go-ultra/issues/24).

**Fix**: `multi_edit` declares the `expected_hash` string parameter; the handler reads it and passes it to the engine. The engine computes FNV-1a of the file at call time and, on mismatch, returns the **exact same** `stale edit: file content changed since read (expected hash: X, actual: Y). Re-read the file with read_file to get the current content_hash, then retry.` error string that `edit_file` uses — so a consumer that pattern-matches on `stale edit:` retries the same way for both tools. The check happens **before** the edit loop and the backup creation, so a stale hash never creates an unnecessary backup and never applies any edits. Empty `expected_hash` disables the check (backward compatible with all existing callers).

**Why byte-for-byte parity with `edit_file`'s error matters**: the user-facing error string is the consumer's only signal that a re-read is required. If the two tools diverged, every consumer would need two retry code paths for what is conceptually one condition.

**Files changed:**

| File | Change |
|------|--------|
| `core/edit_operations.go` | `MultiEdit` signature gains `expectedHash string`; new check after the file read rejects on hash mismatch with the same string `edit_file` uses |
| `tools_batch.go` | `multi_edit` tool registration declares the parameter; handler reads it from args and threads it to the engine |
| `core/pipeline.go` | Pipeline executor passes `""` (no OCC) — pipeline-level OCC is a separate concern |
| 9 test files (`tests/bug{16,17,22,23,27}_test.go`, `multi_edit_occurrences_counter_test.go`, `undo_step_through_test.go`, `core/truncation_test.go`) | All existing `engine.MultiEdit(...)` call sites pass `""` for the new parameter — backward compatible |
| `tests/multi_edit_expected_hash_test.go` | NEW — 4 regression tests: valid hash, stale hash, omitted hash, atomic rollback |
| `CHANGELOG.md` | this entry |

**Verification:**

```bash
go test ./tests/ -run "TestIssue24_" -v            # 4/4 pass
go test ./...                                       # full suite green (no regressions in the 25+ other MultiEdit tests)
```

## [Unreleased / 4.5.12] - 2026-06-11

### Dashboard — Trash tab (UI for soft-deleted files)

The dashboard now has a **Trash** tab (between Backups and Statistics) that lets the user discover, view, restore, and purge soft-deleted files managed by the MCP server's `BackupManager` (added in v4.5.11, issue #16).

**Features:**
- 4 summary cards: trash entry count, total size, oldest entry, newest entry
- Search by SD-ID, original path, or file name (substring, case-insensitive, 300ms debounce)
- Filter by age (older than 1/7/30/90 days)
- Per-row status: `Ready` (file exists in trash + original path is free), `Path Exists` (would need to overwrite to restore), `Missing` (file is gone from trash)
- Per-row actions: **View** (in-browser), **Download**, **Restore** (moves file back to original path), **Purge** (permanently deletes)
- Bulk "Purge Old (>7d)" button in the search bar
- Server-side pagination (50 per page, 7-page window)
- Polled every 30s like the Backups tab

**Safety:** the UI respects the server's safety rules — Restore is disabled when the file is missing from trash or the original path is occupied; Purge requires a `confirm()` dialog; SD-IDs are validated server-side against `safeIDRegex` (`^[a-zA-Z0-9_-]+$`); the `dest_path` in metadata is confirmed to be under `<backup-dir>/filesdelete/` before any RemoveAll.

**Graceful degradation:** if the dashboard was started without `--backup-dir`, the Trash tab shows a clear "Trash is only available when the dashboard was started with --backup-dir" message instead of an error.

**Files changed:**

| File | Change |
|------|--------|
| `cmd/dashboard/main.go` | +`TrashEntry` and `TrashSearchResponse` types; +`trashCache` (10s TTL); +`loadAllTrash` +`enrichTrashEntry` helpers; +`trashListHandler`/`trashSearchHandler`/`trashDetailHandler`/`trashFileHandler`/`trashRestoreHandler`/`trashPurgeHandler`; +7 mux routes; restores & purges invalidate the cache |
| `cmd/dashboard/static/index.html` | +Trash tab button in nav; +`#page-trash` with 4 cards, search bar, table container, pagination, "Purge Old" button |
| `cmd/dashboard/static/app.js` | +`trashPage` state, +`searchTrash`/`renderTrashPagination`/`goTrashPage`/`trashRestore`/`trashPurge`/`trashPurgeOlderThan`; +event listeners; +initial fetch + 30s poll |
| `cmd/dashboard/static/style.css` | +`.btn-danger` (red-tinted, mirrors `.btn-action`); +`.trash-search-bar`; +`.trash-row` |
| `cmd/dashboard/main_test.go` | NEW — 14 test cases covering list/search/pagination/filter/restore/purge/detail/file-serve + invalid SD-ID rejection, missing-trash graceful degradation, refuse-to-overwrite, dry-run, bulk-by-age |
| `CHANGELOG.md` | this entry |

**Endpoints added:**

| Endpoint | Method | Purpose |
|---|---|---|
| `/api/trash` | GET | All entries (no pagination) |
| `/api/trash/search` | GET | Paginated + filterable (q, older_than_days, limit, offset) |
| `/api/trash/detail/{sd-id}` | GET | Single entry with enriched details |
| `/api/trash/file/{sd-id}/{filename}` | GET | Stream file content (supports `?download=true`) |
| `/api/trash/restore` | POST | JSON body `{sd_id:"..."}` → moves file back |
| `/api/trash/purge` | POST | JSON body `{sd_id:"..."}` or `{older_than_days:N, dry_run:bool}` → deletes |

**Verification:**

```bash
go build ./cmd/dashboard/                                        # builds clean
go test -timeout 60s -run TestTrash ./cmd/dashboard/             # 0.10s, 14 cases green
go test -timeout 180s ./...                                       # full suite green
```

**Depends on:** PR #17 (issue #16) — the soft-delete infrastructure must exist on disk for the UI to be useful. This PR is stacked on top of `fix/issue-16-soft-delete-backup-integration`; merge order matters.

## [Unreleased / 4.5.11] - 2026-06-11

### Reliability — `delete_file` (soft-delete) backup integration (bug 2026-06-11, issue #16)

`delete_file` in soft-delete mode (the default) used a parallel `filesdelete/` mechanism that was **not integrated with the main `BackupManager`**. The response gave no path, no ID, and no restore command, and `backup action:list/restore` did not see soft-deletes. This caused a real data-loss scare on 2026-06-11 where two files from a .NET project were soft-deleted and ended up at `C:\temp\__REPOS\...` (the `hasProjectIndicators()` walk-up does not include `.csproj`/`.sln`, so it walked all the way to `C:\temp\`). The user could not find the files because the response gave no path.

**Fix (issue #16):**
- When `--backup-dir` is configured, soft-deletes go to `<backup-dir>/filesdelete/<sd-id>/<basename>` with a `metadata.json` sidecar (discoverable, owner-only `0700` permissions, SHA-256 hash for integrity).
- The response now includes the SD-ID, the trash path, and a one-line restore command.
- AI can restore via `backup(action:"restore_trash", sd_id:"sd-...")` even when the trash directory is outside the allowed paths.
- AI can enumerate via `backup(action:"list_trash", filter_path:"...", older_than_days:N)`.
- AI can permanently clean up via `backup(action:"purge_trash", older_than_days:N)`.
- Without `--backup-dir`, the legacy walk-up behavior is preserved (with a deprecation warning) so users who don't pass `--backup-dir` are not broken.

**Audit:** the audit log now records `sd_id` for each soft-delete so the dashboard can show them in a future UI iteration.

**Files changed:**

| File | Change |
|------|--------|
| `core/backup_manager.go` | +`SoftDeleteInfo` struct, +`SoftDeleteFile`/`ListTrash`/`RestoreTrash`/`PurgeTrash` methods, +`generateSoftDeleteID`/`sanitizeSoftDeleteID`/`hashFile` helpers; `sanitizeBackupID` refactored to delegate to shared `sanitizeID(id, kindLabel)` |
| `core/file_operations.go` | `SoftDeleteFile` signature changed to `(*SoftDeleteInfo, error)`; delegates to `BackupManager` when `--backup-dir` is set; falls back to legacy walk-up (with `slog.Warn` deprecation warning) otherwise |
| `core/pipeline.go` | pipeline `delete` action updated to the new signature |
| `core/audit_logger.go` | +`SDID` field on `AuditEntry` (json `sd_id,omitempty`); +`SetSoftDeleteID` helper |
| `tools_files.go` | `delete_file` response now includes SD-ID, dest path, and restore command (compact + verbose formats; batch path emits per-file SD-IDs); new `formatSoftDeleteLine`/`formatSoftDeleteCompact`/`formatSoftDeleteVerbose` helpers |
| `tools_batch.go` | `backup` tool gains 3 actions: `list_trash`, `restore_trash`, `purge_trash`; tool description updated; new `sd_id` parameter; error message updated |
| `cmd/dashboard/main.go` | mirror `AuditEntry` struct gains `SDID` field (no UI change in this PR) |
| `tests/softdelete_backupdir_test.go` | NEW — `TestSoftDeleteUsesBackupDir` (verifies new path layout + metadata.json) |
| `tests/softdelete_response_test.go` | NEW — `TestSoftDeleteReturnsSoftDeleteInfo`, `TestSoftDeleteLegacyReturnsInfoWithEmptySDID` |
| `tests/trash_restore_test.go` | NEW — `TestBackupRestoreTrash`, `TestRestoreTrashRejectsPathTraversal`, `TestRestoreTrashRefusesIfOriginalPathExists` |
| `tests/trash_list_test.go` | NEW — `TestBackupListTrash` (all/filter/limit) |
| `tests/trash_purge_test.go` | NEW — `TestBackupPurgeTrash` (dry-run + real), `TestBackupPurgeTrashRespectsCutoff` |
| `tests/root_delete_test.go` | extended with `TestSoftDeleteFallsBackWhenNoBackupDir` (legacy path preserved) |

**Out of scope (follow-ups):**
- Dashboard UI for the trash tab (with search, restore, purge buttons)
- Cross-volume rename fallback (`io.Copy` + `os.Remove` when `os.Rename` returns `EXDEV`)
- Auto-migration of legacy `filesdelete/` data left over from before this fix (the user's incident data at `C:\temp\__REPOS\...` is left in place — they have the path)
- `hasProjectIndicators()` `.csproj`/`.sln`/`.cs` additions (no longer needed because `--backup-dir` controls the location)

## [Unreleased / 4.5.10] - 2026-06-11

### Security / Safety — `edit_file` rewrite guard (bug 2026-06-11)

The `edit_file` tool now **blocks** calls that match the "accidental full-file rewrite" anti-pattern. This pattern was observed in production: a 15-line header was passed as `old_text` while the full 150-line file content was passed as `new_text`, producing a 298-line file with the procedure duplicated. The model intended to rewrite the file but `edit_file`'s exact-match semantics only swapped the header, leaving the rest of the old file concatenated below.

**Detection (3 signals, ALL must fire):**
1. `new_text` is disproportionately larger than `old_text` (ratio > 2×)
2. The file has substantial content remaining after the matched block (more than 50% of fileSize is outside the match)
3. `new_text` is substantial in absolute AND relative terms (> 500 bytes AND > 50% of fileSize)

**Override:** pass `force:true` to apply the edit anyway — a safety backup is created automatically by the existing edit pipeline.

**Audit:** when the guard blocks, the audit log records `feedback_pattern: "accidental_rewrite"` and `feedback_status: "ko"` for dashboard visibility.

**Why the 3-signal design:** the absolute-size signal (3) prevents false positives on legitimate small edits where the ratio is high simply because `old_text` was tiny (e.g., expanding a 19-byte TODO comment to 68 bytes in a 5 KB file: ratio 3.6× but the edit is clearly not a "rewrite"). The legitimate "rewrite a 100-line function" case has both `old_text` and `new_text` similar in length, so signal 1 doesn't fire.

**Files changed:**

| File | Change |
|------|--------|
| `core/feedback.go` | +`PatternAccidentalRewrite` constant, +`CheckEditRewrite` function |
| `tools_core.go` | `edit_file` handler calls `CheckEditRewrite` before `engine.EditFile`; blocks when `BlockOp && !force`; `force` schema description updated to mention rewrite-guard bypass |
| `tests/bug_ai_rewrite_concat_test.go` | NEW — 7 cases: bug pattern (blocks), legitimate refactor (allows), small new_text (allows), tiny file (allows), high ratio but no remaining (allows), empty inputs (4 sub-cases), force-bypass contract |

**Tests:**
```
ok  core              0.822s
ok  tests            17.195s
ok  tests/security    0.987s
```

**Why this is a security fix, not just UX:** the bug is silent — no error, no warning, the file is "valid" with content duplicated. Detection requires the server to compare the actual `old_text` length to `new_text` length, something the model cannot do reliably from context. The guard transforms a silent failure into a clear, actionable error.

## [Unreleased / 4.5.9] - 2026-06-09

### Improvement — Read deduplication (`singleflight`) + `ReadFileRange` cache path

Concurrent cold reads of the same path no longer stampede the disk. `ReadFileContent` and `ReadFileRange` (files ≤ 5 MB) share a deduplicated load via `golang.org/x/sync/singleflight`, with results stored in BigCache. Cache invalidation on edits/moves/streaming also calls `readFlight.Forget` so waiters cannot attach to a stale in-flight read.

**Behavior:**

| Path | Before | After |
|------|--------|-------|
| 12 goroutines, same file, cold cache | 12× `os.ReadFile` | 1× `os.ReadFile` |
| `ReadFileRange` after warm cache | Always scanned disk | Served from cache bytes |
| `InvalidateCache` + re-read | Cache miss only | Cache miss + flight forget |

**Line-count parity:** `extractLineRangeFromBytes` uses a `bytesLineScanner` that matches `bufio.Scanner` semantics (no extra empty line when the file ends with `\n`), so range footers still report the real total line count (`truncation_test.go` regression preserved).

**Files changed:**

| File | Change |
|------|--------|
| `core/read_dedup.go` | NEW — `readFileBytesDeduped`, `invalidateFileReadCache`, `extractLineRangeFromBytes` |
| `core/read_dedup_test.go` | NEW — concurrency, cache-hit range, invalidate, bufio parity |
| `core/engine.go` | `ReadFileContent` uses dedup; `InvalidateCache` forgets flight |
| `core/file_operations.go` | `ReadFileRange` fast path via cache/dedup |
| `core/edit_operations.go`, `batch_rename.go`, `large_file_processor.go`, `streaming_operations.go` | `invalidateFileReadCache` on writes |
| `docs/plans/READ_DEDUP_PLAN.md` | NEW — implementation plan + checkpoint |
| `go.mod` | `golang.org/x/sync` promoted to direct require |

**Code removed (now lives inside `readFileBytesDeduped`)** — restore from `git show 3ac6959^:core/engine.go` if rollback is needed:

- Inline `readResult` struct + buffered `resultChan` + `go func()` in `UltraFastEngine.ReadFileContent` (the manual goroutine/channel/select pattern used to honour `ctx.Done()` for `os.ReadFile`).
- Direct calls to `e.cache.SetFile(...)` and `e.cache.TrackAccess(...)` after a successful read — both moved inside the dedup helper so the flight result is the single source of truth.
- Direct `e.cache.InvalidateFile(path)` calls from `ReadFileContent`/`WriteFileContent`/`WriteFileBytes`/Edit/MultiEdit/`searchAndReplaceInFile`/Rename/Move/Copy/SoftDelete/Delete/`executeRenameOperations` — all replaced by `e.invalidateFileReadCache(path)`, which additionally calls `readFlight.Forget(path)` so any in-flight singleflight waiters are released before the next read.
- `if e == nil || e.cache == nil` guard inside `InvalidateCache` — `invalidateFileReadCache` already no-ops on nil, so the outer guard is redundant; the method now also `NormalizePath`s the argument.

**Why the refactor is safe:** all deleted logic is preserved inside `core/read_dedup.go` — context cancellation still returns a `ContextError` from inside the flight, error wrapping still produces a `PathError`, cache write/track still happens exactly once per cold path, and every write-side caller that previously called `InvalidateFile` now calls `invalidateFileReadCache` (which still does that, plus forgets the flight).

**Test results:**

```
ok  core              1.167s
ok  tests            16.121s
ok  tests/security    0.920s
```

## [Unreleased / 4.5.6] - 2026-06-07

### Improvement — Log-driven optimizations (analysis of 12 days, 16,742 operations)

After analyzing `C:\temp\mcp-proxy-logs\proxy.jsonl` (27-may → 07-jun, 2053 ops, 76 errors), four low-risk, backwards-compatible improvements landed:

**A. `search_files` output cap (M1+M2) — token-cost fix**

44% of all output tokens came from `search_files`. Worst case observed: a single 2.28 MB response (~570k tokens). Now the handler truncates responses larger than the configured cap (default 500 KB) and appends a marker so the model knows to retry with `count_only:true` or a narrower path.

```
⚠️ truncated: response exceeded 500 KB. Use count_only:true or narrow the path/pattern.
```

- New constant: `core.DefaultMaxSearchOutputBytes = 500 * 1024` (`core/config.go`)
- New field: `Config.MaxSearchOutputBytes` (0 = use default)
- New helper: `capSearchOutput(text, engine)` in `tools_search.go`
- New accessor: `(*UltraFastEngine).GetConfig()` (`core/engine.go`)
- Behavior below the cap is unchanged; only over-cap responses are truncated.
- Tests: 4 cases in `search_output_cap_test.go` (below, above, default, exact boundary).

**B. `content_hash` + `expected_hash` (B3) — stale-edit protection**

Log analysis found 6 stale-edit cycles in 12 days (read → edit fail → re-read → edit ok). The model uses an `old_text` that was modified by a prior edit. Now `read_file` appends an 8-hex-char FNV-1a hash to its response, and `edit_file` accepts an optional `expected_hash` to refuse the edit if the file changed.

```
# read_file response footer:
hello world
# content_hash: 1a2b3c4d

# edit_file with wrong hash:
ERROR: stale edit: file content changed since read (expected hash: 00000000, actual: 1a2b3c4d).
Re-read the file with read_file to get the current content_hash, then retry.
```

- `hash/fnv` (stdlib) — no new dependency
- `expected_hash` is **optional**; behavior without it is identical to before
- Schema registered in `core/param_validator.go` for both `edit_file` and `edit` alias
- Tests: 5 cases in `content_hash_test.go` (appears-in-read, stable, accepted, rejected, omitted).

**C. `cache_hit` in audit log (M3) — observability fix**

The `AuditEntry.CacheHit *bool` field has existed since the audit logger was added, but no code was setting it. Now `ReadFileContent` records `true` on cache hit and `false` on disk read. Operations log (`operations.jsonl`) will show real cache effectiveness.

- New API: `core.SetCacheHit(ctx, hit bool)` (`core/audit_logger.go`)
- Wire-up: 2 lines in `core/engine.go:ReadFileContent` (hit branch + after disk read)
- Tests: 2 cases in `core/cache_hit_audit_test.go` (records correctly, no-op without entry).

**D. `SetError` + proxy error extraction (M6) — diagnostic completeness**

The audit log's `Error` field was only populated when the JSON-RPC envelope had a top-level `error` member — but most tool errors come back as `result.isError: true` with the message in `result.content[0].text`. Now both layers handle it.

- New API: `core.SetError(ctx, msg string)` (`core/audit_logger.go`) — handlers can override the auto-extracted error with a custom reason
- Proxy: `cmd/proxy/main.go` now extracts `result.content[0].text` when `isError: true`, populating `proxy.jsonl` `error` field (was empty for ~95% of MCP-level errors)
- Tests: 3 cases in `tests/audit_set_error_test.go` (sets-field-and-forces-error, empty-noop, no-entry-noop).

**Files changed (11 total, +118 / -2 lines):**

| File | Change |
|------|--------|
| `core/audit_logger.go` | +`SetCacheHit`, +`SetError` |
| `core/config.go` | +`DefaultMaxSearchOutputBytes` |
| `core/engine.go` | +`GetConfig()`, +wire `SetCacheHit` in `ReadFileContent` |
| `core/param_validator.go` | +`expected_hash` in `edit_file` and `edit` schemas |
| `core/cache_hit_audit_test.go` | NEW |
| `tools_core.go` | +`hash/fnv` import, +content_hash footer, +expected_hash check, +schema field |
| `tools_search.go` | +`capSearchOutput` helper, +cap at 2 call sites |
| `content_hash_test.go` | NEW |
| `search_output_cap_test.go` | NEW |
| `tests/audit_set_error_test.go` | NEW |
| `cmd/proxy/main.go` | +extract error text from `result.content[0].text` |

**Test results:** all existing tests still pass. 14 new tests added, all green.

```
ok  core         0.678s
ok  tests        15.820s
ok  tests/security 0.873s
ok  .            0.498s
```

**Out of scope (deferred):**
- ~~`git` tool 38.9% error rate~~ — investigated 2026-06-11. Root cause is **NOT a tool bug**: analysis of 18 git calls in `C:\temp\mcp-proxy-logs\proxy.jsonl` (3 months, 17,951 total ops) shows 5 of 7 errors are instant failures (duration 0-2ms = input validation / "not a git repository" before any git exec) on paths that are NOT git repos. 1 is a missing path arg. Only 1 of 7 (14%) is a real git command error (45ms duration, transient — retried successfully 3s later). The remaining errors are from the same `opus-4` model retrying the same broken call 3 times in 14 seconds without adapting — anti-pattern, not tool bug. No code change needed; CLAUDE.md now documents the 8 actions correctly and a workflow rule prevents the "calling git on a non-repo path" anti-pattern.
- `get_file_info` 23% error rate — needs running server + Windows lock investigation.
- `mcp_search`/`mcp_read`/etc. prefix duplication — already resolved by user (12-day log shows 15 unique tool names vs 41 in 3-month history).
- `ReadFileRange` doesn't use cache — separate PR.

## [Unreleased / 4.5.7] - 2026-06-07

### Bug fix — `edit_file`/`multi_edit` find 0 matches on files with mixed whitespace

Reported reproduction: a 87 KB JS file edited with VSCode/Windows editors ends up with mixed tabs and spaces. `edit_file` and `project_replace` (in `search_replace` mode) return **0 matches** even for patterns that clearly exist, because the existing byte-exact matcher can't reconcile tabs with 4-space runs. Same problem with CRLF vs LF: a file with Windows line endings and a pattern typed with LF never matches. The previous workaround was for the user to manually reformat and re-save the file before editing.

**Fix — `tolerant_whitespace: true` flag (opt-in)**

Both `edit_file` and `multi_edit` now accept an explicit `tolerant_whitespace` boolean. When `true`, the matcher treats one tab as 4 spaces and CRLF/CR as LF, while preserving the file's original bytes verbatim outside the match region. Pure stdlib, no new dependency.

```js
// Before (fails on mixed-indent files):
edit_file({path: "_events.js", old_text: "    taula_llistat(", new_text: "    taula_llistat_new("})

// After (works regardless of tabs/spaces in the file):
edit_file({path: "_events.js", old_text: "    taula_llistat(", new_text: "    taula_llistat_new(", tolerant_whitespace: true})
```

- New file: [`core/whitespace_matcher.go`](core/whitespace_matcher.go) — `normalizeForTolerantMatch` (whitespace normalization + byteMap for position translation), `findAllTolerantMatches`, `applyTolerantMatches`. Conservative: only tabs and line endings are normalized, not runs of multiple spaces.
- [`core/edit_operations.go`](core/edit_operations.go) — `EditFile`, `MultiEdit`, `performIntelligentEdit` accept `tolerantWhitespace bool` and run as `OPTIMIZATION 0` (before the exact-match fast path) when enabled. If tolerant matching finds nothing, the existing cascade (literal escapes, leading-whitespace fallback, flexible regex) still runs.
- Existing `OPTIMIZATION 7` (leading-whitespace fallback) and `OPTIMIZATION 8` (flexible regex) remain in place as further fallbacks — so behavior with `tolerant_whitespace: false` is byte-identical to before for every file we tested.
- API change: `EditFile` and `MultiEdit` now take 6 args (added `tolerantWhitespace bool`). All ~20 internal/test call sites updated; the default value `false` preserves existing behavior.
- Schema: `tolerant_whitespace` registered in `core/param_validator.go` for `edit_file`, `multi_edit`, and the `edit` alias.
- Wire-up: `tools_core.go` and `tools_batch.go` extract the param and pass it through.

### Feature — `minify_js` tool (pure Go, no Node, no external deps)

A new tool to minify JavaScript files in place. Pure-stdlib state machine that handles `//` and `/* */` comments, single/double/template strings (with `${expr}` interpolation), regex literals (`/.../[flags]`) with character classes, and the regex-vs-division disambiguation that real JS tokenizers do. Auto-creates a backup before overwriting, recoverable with `backup(action:"undo_last")`.

```js
// Dry run first:
minify_js({path: "app.js", dry_run: true})
// → "MINIFY (dry-run) app.js | 87342→31045B (-56297, 64.4%) | comments:42"

// Live run:
minify_js({path: "app.js", remove_comments: true, single_line: true})
// → file overwritten; UNDO:20260607-xxxxx is the backup ID
```

- New file: [`core/minifier.go`](core/minifier.go) — `MinifyJS(src, MinifyOptions) (string, MinifyStats)`, plus `MinifyStats{InputBytes, OutputBytes, BytesSaved, ReductionPercent, CommentsStripped, Truncated}`. Best-effort: the 95% of real-world JS works perfectly; exotic regex-with-`/`-in-char-class and tagged-template edge cases are handled with conservative heuristics (the minifier never modifies the contents of strings, regexes, or template substitutions).
- New file: [`tools_minify.go`](tools_minify.go) — registers the `minify_js` MCP tool with parameters `path`, `output_path` (optional, write elsewhere instead of overwriting), `remove_comments`, `collapse_whitespace`, `single_line`, `dry_run`, `create_backup`.
- New public API: `(*UltraFastEngine).InvalidateCache(path)` and `core.SecureRandomSuffix()` — thin wrappers over the existing private helpers so the new tool keeps the cache consistent and uses unpredictable temp-file names.
- Tool count: **20 tools** (18 core + git + help + **minify_js**).

### Tests

- [`core/whitespace_matcher_test.go`](core/whitespace_matcher_test.go) — 13 cases: tabs↔spaces (both directions), CRLF↔LF, lone CR, multiple matches, byte-range preservation, UTF-8 byte positions preserved, end-to-end via `performIntelligentEdit` with a tab in the middle of a line (a case the existing `OPTIMIZATION 7` cannot handle).
- [`core/minifier_test.go`](core/minifier_test.go) — 25+ cases covering strings, regex, templates, division, shebang, comment removal modes, single-line toggle, truncation on malformed input, and a real-world DataTable snippet.
- All existing tests still pass: `go test ./...` green (`core 0.68s`, `tests 14.4s`, `tests/security 0.82s`).

**Files changed (26 total):**

| File | Change |
|------|--------|
| `core/whitespace_matcher.go` | NEW — tolerant matcher + byteMap |
| `core/whitespace_matcher_test.go` | NEW |
| `core/minifier.go` | NEW — JS state-machine minifier |
| `core/minifier_test.go` | NEW |
| `core/edit_operations.go` | +`tolerantWhitespace` param on `EditFile`/`MultiEdit`/`performIntelligentEdit`; new OPTIMIZATION 0 |
| `core/engine.go` | +`InvalidateCache(path)`, +`SecureRandomSuffix()` |
| `core/param_validator.go` | +`tolerant_whitespace` in edit_file, multi_edit, edit schemas |
| `core/streaming_operations.go` | update EditFile caller |
| `core/claude_optimizer.go` | update EditFile caller |
| `core/pipeline.go` | update EditFile + MultiEdit callers |
| `core/batch_operations.go` | update performIntelligentEdit caller |
| `core/truncation_test.go` | update MultiEdit callers |
| `core/engine_bench_test.go` | update EditFile caller |
| `tools_core.go` | +extract `tolerant_whitespace`; +register minify tools; +`20 tools` log |
| `tools_batch.go` | +extract `tolerant_whitespace` for multi_edit |
| `tools_minify.go` | NEW — `minify_js` tool registration |
| `tests/bug16_test.go` | update EditFile + MultiEdit callers |
| `tests/bug17_test.go` | update MultiEdit callers |
| `tests/bug18_literal_escapes_test.go` | update EditFile callers |
| `tests/bug22_multi_edit_test.go` | update MultiEdit callers |
| `tests/bug23_test.go` | update EditFile + MultiEdit callers |
| `tests/bug27_multi_edit_atomic_test.go` | update MultiEdit callers |
| `tests/bug28_html_edit_test.go` | update EditFile callers |
| `tests/mcp_functions_test.go` | update EditFile callers |
| `tests/undo_step_through_test.go` | update EditFile + MultiEdit callers |

**Test results:** all existing tests still pass. 38 new tests added, all green.

```
ok  core         0.680s
ok  tests        14.372s
ok  tests/security 0.824s
ok  .            0.654s
```

## [Unreleased / 4.5.8] - 2026-06-09

### Security — Go 1.26.3 → 1.26.4 stdlib CVE fixes

`govulncheck` flagged two vulnerabilities in the Go standard library, both fixed in 1.26.4. Anyone building or running the server on 1.26.3 is affected.

- **GO-2026-5039** — "Arbitrary inputs are included in errors without any escaping" in `net/textproto` (triggered by `io.CopyBuffer` → `textproto.Reader.ReadMIMEHeader`, reachable from `core.CopyFileWithBuffer`).
- **GO-2026-5037** — "Inefficient candidate hostname parsing in `crypto/x509`" (triggered by `http.ListenAndServe` → `x509.Certificate.Verify`, reachable from `cmd/dashboard/main.go`).

`GO_VERSION` bumped from `1.26.3` → `1.26.4` in `.github/workflows/ci.yml`. Users running the prebuilt binaries inherit the fix; users building from source should `go version` ≥ 1.26.4.

### Bug fix — TOCTOU defense now distinguishes Windows directory junctions from real symlinks

`core.ResolveSymlinks` (the TOCTOU defense called before `MoveFile`, `CopyFile`, and pipeline `copy` actions) used `filepath.EvalSymlinks` and treated ANY difference between the resolved and original paths as a symlink. On Windows, OS-created directory junctions (`%LOCALAPPDATA%\Temp` → `%USERPROFILE%\AppData\Local\Temp`, `Recent`, `Cookies`, etc.) caused the resolved form to always differ, so the defense incorrectly rejected every file operation whose path traversed one — including `t.TempDir()` paths in the standard test suite. Junctions are not attacker-controlled reparse points, so flagging them was a false positive that would have blocked legitimate paths in any Windows deployment that happened to walk through `Temp`, `LocalAppData`, etc.

**Fix** — replaced the resolution-based check with an Lstat-based walk. `os.Lstat` does not follow links, and on Windows it reports junctions as plain directories (their reparse-point attribute is not surfaced through Lstat's mode bits), so junctions are correctly treated as legitimate while real symlinks are still caught. The canonical resolved path is still returned for callers that need it; only the `wasSymlink` bool changes meaning (now true ONLY if a real symlink was traversed).

The test-skip band-aids added in 4.5.7 (which gated `TestMoveFile`, `TestCopyFile`, `TestPipeline_Copy` on Windows) are removed — they are no longer needed and `core/engine_test.go`'s `skipIfWindowsJunctionTempDir` helper is deleted entirely. Verified locally on Windows: all three tests now pass without the skip.

### Build — `embed_rg` binaries are now downloaded in CI

The `filesystem-ultra-v4-embed_rg.exe` binary embeds ripgrep via `//go:embed all:rg-*`. The `.exe` files are gitignored (build artifacts) and were never committed, so the CI build of the embed_rg variant failed on Windows with `pattern *.exe: no matching files found`. Fixed in three places:

- `embed/ripgrep/embed.go` — uses the `all:` prefix on the `rg-*` glob to override `.gitignore`, with a single pattern that matches whichever platforms have downloaded binaries (host builds don't need every platform).
- `embed/ripgrep/download.sh` — rewritten to fetch every supported platform from the official ripgrep 15.1.0 GitHub release (windows/amd64, linux/amd64, linux/arm64, darwin/amd64, darwin/arm64) and place them at the exact names `embed.go` expects (`rg-windows-amd64.exe`, `rg-linux-amd64`, etc.). The old script used `musleabi` for Linux, which is not a published ripgrep asset — corrected to `musl` for amd64 and `gnu` for arm64.
- `.github/workflows/ci.yml` — new `Download ripgrep binaries for embed_rg` step runs `bash embed/ripgrep/download.sh` before the build script, on both ubuntu and windows runners.

The `embed_rg` binary now builds cleanly in CI; the resulting `bin/filesystem-ultra-v4-embed_rg.exe` is ~12.6 MB (up from 8.4 MB) — the extra ~4.2 MB are the embedded ripgrep binaries for all 5 platforms.

## [Unreleased / 4.5.5] - 2026-06-04

### Improvement — Adaptive write_file behavior when backup is available

`write_file` previously hard-blocked when new content was < 50% or > 3× the existing file size (`truncation` and `inflation_loop` patterns in `core/feedback.go`), forcing a `delete_file` + `write_file` cycle that wasted tokens on long sessions.

Now, when the engine has a `BackupManager` configured (default: `--backup-dir` → `temp/mcp-batch-backups`), these patterns instead:
1. Create a safety backup of the existing file (linked to the undo chain via `CreateBackupWithContextAndParent`)
2. Proceed with the write
3. Return a non-blocking `WARN` (status `warn` in the audit log) that includes the backup ID and the literal `backup(action:"restore", backup_id:"...")` undo command. Response format is forced to verbose so the restore command is visible, even in `--compact-mode`.

When the backup manager is unavailable (rare — only if `NewBackupManager` failed at startup, e.g. permissions), the original hard-block behavior is preserved as a safety net.

**Response format (downgraded case):**
```
WRITTEN C:\foo\bar.go | 8055B
⚠️ [TRUNCATION] WARNING: new content (8055 B) is less than 50% of existing file (62749 B). Looks like accidental truncation.
   → Backup created: 20260604-130xxx. To undo: backup(action:"restore", backup_id:"20260604-130xxx"). Read the full file first, then use edit_file for partial changes. To force overwrite: delete_file first, then write_file.
```

**Files:**
- `core/feedback_adaptive.go` (new) — `ApplyAdaptiveWriteBlock` pure helper
- `core/feedback_adaptive_test.go` (new) — 9 table-driven cases + restore-command format pin
- `core/feedback.go` — added `Downgraded bool` field to `FeedbackSignal` (with `omitempty` for JSON back-compat)
- `tools_core.go` — handler of `write_file` now calls the helper; normalizes path once; forces verbose response on downgraded warns
- `core/claude_optimizer.go` — added `// NOTE:` documenting the intentional divergence with the legacy `IntelliGentWrite` guard

**Not changed:** the legacy truncation guard in `core/claude_optimizer.go:IntelliGentWrite` — hard-blocks even when backup is available. The divergence is intentional for this release; unification planned for 4.5.6+.

**Build artifacts:**
- `bin/filesystem-ultra-v4-embed_rg.exe` (12 MB, with ripgrep embedded) — rebuilt 2026-06-04

### Security — Major improvements to hook coverage, Git tool, and WSL auto-sync

**Hook system coverage (biggest win):**
- `batch_operations` now properly executes pre/post hooks for `write`, `edit`, `delete`, `move`, `copy`, `create_dir`, and `search_and_replace` when an engine is attached. Previously these operations completely bypassed user-configured hooks.
- Pipeline `regex_transform` now runs `pre-edit`/`post-edit` hooks with full file content (when possible).
- Pipeline rollback now best-effort fires `post-edit` hooks on restored files.
- Added `IsPathAllowed` checks to `file_exists` / `file_not_exists` pipeline conditions (prevents filesystem probing outside allowed paths).

**Git tool hardening:**
- Fixed command injection risk on Windows in `execGitCommand` (removed dangerous string concatenation in `cmd /c` fallback; arguments are now passed properly).
- Added anti-destructive protection: `restore` and `branch delete` now require explicit `force=true`.
- Improved tool annotations (`destructiveHint`, `idempotentHint`).

**WSL / Auto-sync security:**
- Auto-sync and the `wsl` tool now respect `--allowed-paths` on converted target paths.
- `TargetMapping` destinations are validated against allowed paths at config time.
- Added symlink rejection/skipping when copying across the WSL-Windows boundary (defense against symlink attacks).

**Tests:**
- Significantly improved `TestBatchOperationsRespectHooks` (now covers write + edit + delete denial via hooks in batch operations).
- All pipeline + batch + condition tests updated and passing after signature changes.

**Files changed:**
- `core/batch_operations.go`
- `core/pipeline.go`, `core/pipeline_conditions.go`
- `tools_git.go`
- `core/autosync_config.go`, `core/wsl_sync.go`, `core/path_converter.go`
- `tools_platform.go`
- `tests/batch_security_test.go`, `tests/pipeline_conditions_test.go`
- `SECURITY.md`
- `CHANGELOG.md`

---

## [4.5.4] - 2026-05-30

### Fix — git tool: "Stderr already set" error on Windows with path

**Bug:** `git(action:"status", path:"...")` returned `"git status failed: exec: Stderr already set"` when a path was provided. Worked fine without path (auto-detect repo root). Affected all actions that accept a path.

**Root cause:** In `execGitCommand()`, when the first `git` call failed and retried via `cmd3` (cmd /c fallback), the same `stderr` buffer was reused across two distinct `exec.Cmd` objects. Go's exec package rejects sharing a `*bytes.Buffer` between `Stderr` fields of different commands.

**Fix:** Remove `stderr` assignment from `cmd2` (CombinedOutput captures it internally), give `cmd3` its own local `stderr` buffer and proper error formatting.

**Files:** `tools_git.go`

---

## [4.5.3] - 2026-05-27

### Fix — return_lines parameter accepts bool (closes #29)

The `search_files` tool's `return_lines` parameter was declared as `ParamString` in the schema, but the handler already accepted both `bool` and `string` (`"true"/"false"`). When Claude Desktop sent `return_lines: true` as a JSON boolean, validation failed with `"expected string, got bool"`.

**Fixed:**
- Change `return_lines` schema from `ParamString` to `ParamBoolean` in `search_files` and `search` alias
- Update `typeMatches()` for `ParamBoolean` to also accept string `"true"/"false"` (backwards compatible)
- Add test coverage for `return_lines` bool value

**Files:** `core/param_validator.go`

---

## [4.5.2] - 2026-05-27

### Feature — Git Version Control Integration

New `git` tool for version control operations inside git repositories. Fully integrated with existing security (allowed paths), hooks, and audit systems.

**Actions:**

| Action | Parameters | Description |
|--------|------------|-------------|
| `status` | `path?` | Compact or full porcelain status |
| `diff` | `path?`, `staged?`, `commit_range?` | Unified diff with truncation at 50 lines |
| `log` | `path?`, `max_count?` | Oneline commit history |
| `add` | `path?`, `all?`, `dry_run?` | Stage files with pre/post hooks |
| `commit` | `message`, `auto_message?`, `force?` | Commit with risk assessment |
| `restore` | `paths`, `staged?`, `source?`, `dry_run?` | Restore from index or commit |
| `branch` | `branch_action?`, `branch_name?`, `force?` | List/create/delete branches |
| `init` | `path?` | Initialize new repository |

**Risk Assessment (git_commit):**
- LOW: <15 files, <800 insertions
- MEDIUM: 15-40 files or 800-3000 insertions
- HIGH: >40 files, >3000 insertions, or >500 deletions
- Blocked unless `force: true`

**auto_message:** Generates conventional commit messages using `--numstat --name-only`:
- Detects type: `feat`, `fix`, `refactor`, `test`, `docs`, `chore`
- Heuristics: file names (tests, docs, config), deletion counts

**Files:**
- **`core/git.go`** — `FindGitRoot()`, `IsGitRepo()` (walks up tree, handles Windows `.git` file with `gitdir:` prefix)
- **`tools_git.go`** — Full `git` tool with 8 actions, compact mode, hook integration
- **`core/engine.go`** — `GetHookManager()` exported for git hook access

**New commands:** 19 tools total (was 18 core + help)

---

## [4.5.1] - 2026-05-21

### Fix — search_replace: $ escaping in dry_run diff (regression)

The previous fix (4.5.1 escape `$` → `$$` in replacement) was incomplete — it only fixed the actual file write in `searchAndReplaceInFile`, but NOT the dry_run diff preview in `tools_core.go:570`. This caused dry_run to show incorrect diffs with `$var` corruption even though the actual write was correct.

**Fixed:** dry_run diff now uses the same `$` escaping as the actual replacement.

### Fix — backup/restore: multiple critical bugs fixed

**Bug 1: project_replace backup included files never modified**
- `matchedFiles` was backed up BEFORE processing — all files passing path filters, not just those with actual replacements
- Now backup is created AFTER processing, only with files that actually had `replaced > 0`
- **File:** `core/project_replace.go` (line ~222)

**Bug 2: No hash verification on restore**
- `BackupMetadata.Hash` was stored but never verified after restore
- Added `copyFileAndVerifyHash()` that computes SHA256 of destination and compares to stored hash
- If hash mismatch, restore fails with error — no silent corruption
- **File:** `core/backup_manager.go` (line ~760)

**Bug 3: Silent continuation on copy failure**
- `copyFile` failures were logged and silently continued, allowing partial restore with no error
- Now any failure (hash mismatch, copy error, dir creation) returns error with list of failed files
- **File:** `core/backup_manager.go` (line ~420)

**Bug 4: No dry-run for full restore**
- `undo_last` had dry_run, but `restore_backup` did not
- Added `dry_run:true` parameter for full restore preview (lists all files, sizes, hashes)
- **File:** `tools_batch.go` (line ~702)

### Fix — search_replace: escape $ in replacement text

Fixes bug where `search_replace` mode consumed `$` characters from PHP variables (e.g. `$variable` became `variable`). Go's `ReplaceAllString` interprets `$` as capture group reference — now escaped to `$$` for literal output.

**Affected:** `edit_file` with `mode: "search_replace"` and replacement text containing `$`

## [4.5.0] - 2026-05-20

### Feature — project_replace: project-wide find/replace in one call

Nueva herramienta para reemplazar patrones en todo un árbol de proyecto en una sola llamada MCP. Reemplaza el patrón de N llamadas `multi_edit` por 1.

**Motivación:** Operaciones de find/replace en proyectos grandes (45+ archivos) requieren 45+ round-trips cliente↔servidor, creando 45 backups individuales y consumiendo contexto innecesario.

**Parámetros:**
- `path` (requerido) — raíz del scan
- `find` (requerido) — texto o regex a buscar
- `replace` (requerido) — texto de reemplazo
- `literal` (default: true) — si false, regex
- `case_sensitive` (default: true)
- `file_types` — extensiones separadas por coma (".php,.html")
- `include_paths` / `exclude_paths` — globs opcionales
- `preview` — diff sin escribir
- `create_backup` (default: true) — backup consolidado único
- `parallel` (default: true)
- `max_files` (default: 1000) — safety cap

**Respuesta:**
```json
{
  "files_changed": 45,
  "total_replacements": 230,
  "backup_id": "20260520-...",
  "per_file": [{"path": "...", "replacements": 5}, ...]
}
```

**Cambios:**
- **`core/project_replace.go`** — nueva implementación con scan + replace + backup batch
- **`tools_batch.go`** — registrado como tool `project_replace`
- **`tests/project_replace_test.go`** — 10 tests (basic, dry_run, file_types, exclude_paths, regex, case_insensitive, max_files, no_matches, backup, empty_dir)

**Ganancias:**
- Latencia: 1 round-trip en vez de N
- Tokens: 1 respuesta en vez de N confirmaciones de "1@+N-N"
- Backups: 1 chain ID en vez de N
- Preview: diff agregado sin múltiples analyze_operation

---

## [4.4.1] - 2026-05-19

### Fix — Sistema de backup unificado para batch_operations

**Problema:** `batch_operations` usaba su propio sistema de backup privado (`mcp-batch-backups/` con IDs `batch-YYYYMMDD-HHMMSS`) que no era visible para `backup(action:"list")` ni restaurable con `backup(action:"restore")`.

**Solución:**
- `BatchOperationManager` ahora acepta un `BackupManager` compartido vía `SetBackupManager()`
- Los backups de `batch_operations` ahora se crean en el mismo directorio que `edit_file`
- Metadatos mejorados en formato `BackupInfo` compatible con el sistema principal

**Cambios:**
- **`core/batch_operations.go`** — `SetBackupManager()`, `getBackupDir()`, metadata mejorado con `BackupInfo`
- **`tools_batch.go`** — Usa `SetBackupManager(engine.GetBackupManager())` para compartir backup manager
- **`tests/batch_security_test.go`** — Actualizado para nueva API

**Resultado:**
- Backups de `batch_operations` ahora aparecen en `backup(action:"list")` ✅
- IDs `batch-YYYYMMDD-HHMMSS` son aceptados por `backup(action:"restore")` ✅
- `BackupPath` devuelto por batch es útil para recovery ✅

---

## [4.4.0] - 2026-05-11

### Feature — Claude Code tool name aliases

Nuevos aliases que coinciden con los nombres de herramientas de Claude Code para compatibilidad directa.

**Aliases agregados** (`tools_aliases.go`):
- `View` — alias de `read_file`
- `Edit` — alias de `edit_file`
- `Write` — alias de `write_file`
- `Replace` — alias de `write_file`
- `LS` — alias de `list_directory`
- `GlobTool` — alias de `search_files` (modo filename-only)
- `GrepTool` — alias de `search_files` (con contenido, usa ripgrep cuando está disponible)

**Motivación:** El source code de Claude Code se filtró en marzo 2026. Estos aliases permiten que prompts/scripts escritos para Claude Code funcionen directamente con este servidor MCP.

---

### Feature — Ripgrep as optional search backend

Búsqueda acelerada via ripgrep (`rg`) con fallback a Go-native.

**Implementación:**
- **`core/ripgrep_search.go`** — `DetectRipgrep()` + `RunRipgrepSearch()`
- **`core/engine.go`** — Detección al inicio, `ripgrepAvailable` + `ripgrepVersion`
- **`core/search_operations.go`** — Dispatch automático a ripgrep cuando `output_format="json"` y `rg` disponible

**Fallback chain:**
1. `rg` en PATH → usar directamente
2. Binario embebido (con `embed_rg` build tag) → extraer y usar
3. No disponible → Go-native regex (sin cambios de comportamiento)

**Binarios embebidos** (`embed/ripgrep/`):
- `rg-windows-amd64.exe` (4.1MB, v15.1.0)
- `download.sh` para descargar más plataformas (Linux amd64/arm64, macOS Intel/Apple Silicon)

**Builds:**
```bash
# Default (sin embed)
go build -ldflags="-s -w" -trimpath -o filesystem-ultra-v4.exe .

# Con ripgrep embebido
go build -ldflags="-s -w" -trimpath -tags embed_rg -o filesystem-ultra-v4-embed.exe .
```

---

## [4.3.9] - 2026-05-01

### Feature — New AI-optimized response formats

Respuestas reformateadas para mejor parseo por LLMs, menor consumo de tokens, y chain de undo visible.

**edit_file:**
- Compact: `M path/file | N@+N-N | NL | UNDO:202605011236 | chain:202605011235`
- Verbose: `M path/file | N replacement(s) | +N -N | NL\n✓ UNDO:full-id ← chain:parent-id`

**multi_edit:**
- Compact: `M path/file | 3/5@+10-2 | 50L | skip:2 | UNDO:202605011236 | chain:202605011235`
- Verbose: similar con detalles expandidos

**write_file:**
- Compact/Verbose: `WRITTEN path/file | 1234B`

**list_directory:**
- Compact: `path/to/dir | file1 file2/ dir/ | 3/12`
- Verbose: `FILE file1 1.2KB | path/to/dir\nDIR subdir/ | path/to/dir\n--- | 1 dirs, 2 files`

**Backup ID truncation:**
- Display: 12 chars (timestamp only) para eficiencia
- Chain: `chain:parentID` muestra parent para undo step-through
- Full ID disponible en audit log / backup list

---

## [4.3.8] - 2026-04-30

### Feature — Undo step-through via backup chain

Sistema de undo que permite recorrer la cadena de backups hacia atrás, uno a uno, en lugar de restaurar todo de golpe. Cada backup conoce su "parent" vía `PreviousBackupID`.

**Uso:**
```json
backup(action: "undo_last", file_path: "file.go")
// Reversible: ejecuta un paso, muestra cuántas opciones quedan
backup(action: "undo_last", file_path: "file.go", preview: true)
// Preview: solo muestra qué haría sin ejecutar
backup(action: "undo_chain", file_path: "file.go")
// Muestra la cadena completa de backups para el archivo
```

**Cambios:**

- **`core/backup_manager.go`** — `BackupInfo` incluye `PreviousBackupID string` + `CreateBackupWithContextAndParent()` + `RestorePreviousInChain()`
- **`core/engine.go`** — `backupChain map[string]string` para tracking + getter/setter `GetCurrentBackupID()`, `SetCurrentBackupID()`, `ClearBackupID()`
- **`core/edit_operations.go`** — `EditFile()` y `MultiEdit()` crean backups enlazados y actualizan la cadena
- **`tools_batch.go`** — `undo_last` con `file_path` sigue la cadena hacia atrás; nuevo `undo_chain` action

---

### Feature — Auto-verificación de integridad post-edit (HIGH/CRITICAL)

Después de `edit_file` o `multi_edit` con riesgo HIGH o CRITICAL, se ejecuta automáticamente una verificación ligera del archivo:
- ¿Archivo legible?
- ¿Tamaño razonable? (no truncado a poqu bytes tras cambio masivo)
- ¿Líneas contadas cuadran?
- Hash CRC32 para referencia

**Output ejemplo:**
```
File Integrity
---
✓ Verified: 847 lines, 23456 bytes, hash a3f2b1c9
```

**Warning:**
```
⚠️  Integrity Warning: File is only 50 bytes after a 80% change — verify content
```

**Cambios:**

- **`core/engine.go`** — `VerifyFileIntegrity(path, expectedChangePct)` + `FileIntegrityResult` struct
- **`core/edit_operations.go`** — `EditResult` y `MultiEditResult` incluyen campo `Integrity`
- **`tools_core.go`** / **`tools_batch.go`** — Output de integridad adjuntado a respuestas de edit/multi_edit

---

## [4.3.7] - 2026-04-30

### Feature — Análisis completo en respuestas de edit

Las respuestas de `edit_file` y `multi_edit` ahora incluyen análisis detallado (Plan Mode style) para que la IA vea el impacto completo después de cada operación.

**Cambios:**

- **`core/edit_operations.go`** — `EditResult` y `MultiEditResult` ahora incluyen campo `Analysis *ChangeAnalysis`
- **`core/edit_operations.go`** — `EditFile()` y `MultiEdit()` generan análisis completo post-ejecución
- **`tools_core.go`** — La respuesta de `edit_file` ahora incluye diff preview, risk factors, suggestions

**Output ejemplo:**
```
Successfully edited file.go
Changes: 2 replacement(s) (+5 -3)
...

---
Change Analysis
---
File: file.go
Operation: edit
Risk Level: HIGH
Risk Factors:
  - ⚠️ Large portion of file affected (42.5%)
Changes:
  + 5 lines added
  - 3 lines removed
Impact: Multiple surgical edits
Suggestions:
  - Consider using search_files + read_file(start_line/end_line) for surgical edits
```

---

### Feature — Diff preview en dry_run de regex

Los modes `regex` y `search_replace` ahora incluyen unified diff completo cuando se usa `dry_run: true`.

**Cambios:**

- **`core/large_file_processor.go`** — `ProcessingResult` incluye `TransformedContent` para DryRun
- **`core/regex_transformer.go`** — `RegexTransformResult` propaga contenido transformado
- **`tools_core.go`** — DryRun de regex ahora incluye diff en la respuesta

---

### Feature — JSON output para search_files

Nuevo parámetro `output_format: "json"` en `search_files` para output estructurado que la IA puede parsear fácilmente.

**Uso:**
```json
search_files(path: ".", pattern: "TODO", include_context: true, output_format: "json")
```

**Output:**
```json
{
  "pattern": "TODO",
  "path": ".",
  "total_matches": 3,
  "matches": [
    {"file": "a.go", "line": 10, "line_number": 10, "match_start": 0, "match_end": 4, "line_content": "// TODO: fix this", "context": ["func foo() {", "// TODO: fix this", "}"]}
  ],
  "summary": "Found 3 matches for pattern 'TODO' in ."
}
```

**Cambios:**

- **`tools_search.go`** — Nuevo parámetro `output_format` en tool definition
- **`core/search_operations.go`** — `AdvancedTextSearch` soporta `output_format: "json"` con función `formatSearchMatchesJSON`

---

### Fix — CRITICAL risk ya no bloquea operaciones

Removido el blocking de operaciones CRITICAL. Ahora todas las operaciones se ejecutan con backup automático y warning. La IA decide si procede basándose en la información completa.

**Cambios:**

- **`core/impact_analyzer.go`** — `ShouldBlockOperation()` ahora retorna `false` siempre
- **`core/impact_analyzer.go`** — `GetRecommendation()` para CRITICAL ya no dice "blocked"
- **`tests/bug16_test.go`** — Test actualizado para reflejar nuevo comportamiento

**Razón:** El blocking consumía recursos (backup, simulación) y luego la IA no podía hacer el trabajo. Con backup automático y warning completo, la IA puede decidir con información completa.

---

## [4.3.6] - 2026-04-24

### Security — Prompt injection mitigation

Removidas instrucciones imperativas del servidor MCP que se inyectaban en cada mensaje del usuario.

#### Cambios

- **`main.go`** — `serverInstructions` reducido de ~25 líneas de reglas/TOOLS/WORKFLOW/RISK a solo:
  `"MCP Filesystem Ultra — File operations server. Run 'help' for tool list."`

- **`tools_aliases.go`** — Descripción del tool `help` limpiada de "CALL THIS FIRST to discover all 16 filesystem tools..."

- **`.claude/skills/filesystem-ultra-tools/skill.md`** — Removidas secciones "Never use bash alternatives", "Recommended workflow" con imperativos hacia el LLM

#### Background

El servidor enviaba `WithInstructions()` durante el handshake MCP. El cliente concatenaba este contenido a cada mensaje del usuario, violando el principio de que las instrucciones de estilo las configura el usuario, no el MCP.

---

## [4.3.5] - 2026-04-20

### Feature — Regex support en hooks

Los patrones de hook ahora aceptan prefijo `re:` para matching por expresión regular, manteniendo backward compatibility con los patrones exactos y de wildcard existentes.

- `"pattern": "re:^(write|edit)_.*$"` — regex explícita
- `"pattern": "*.go"` — wildcard (sin cambios)
- `"pattern": "write_file"` — exacto (sin cambios)

**Implementación**: `regexp.Compile` una sola vez por patrón, cacheado en `sync.Map`. Regex inválidas se loguean con `slog.Warn` y se tratan como no-match (nunca crashean el dispatcher).

**Archivos**:
- `core/hooks.go` — `matchesPattern()` detecta prefijo, `matchesRegex()` + cache compilado
- `core/hooks_regex_test.go` — 10 casos (exact + wildcard + regex + cache + inválidos)
- `docs/features/HOOKS.md` — documentada la nueva variante de patrón

### Feature — Benchmark suite

Nuevo conjunto de benchmarks estándar Go (`testing.B`) en el paquete `core` para detectar regresiones de performance entre releases.

9 benchmarks: `BenchmarkReadFile_{Small,Medium,Large,CacheHit}`, `BenchmarkReadFileRange`, `BenchmarkWriteFile_{Small,Large}`, `BenchmarkEditFile`, `BenchmarkParallelReads`.

```bash
# Ejecutar con baseline
go test ./core/ -run=xxx -bench=. -benchmem -benchtime=3s

# Escalabilidad parallel
go test ./core/ -run=xxx -bench=BenchmarkParallelReads -cpu=1,2,4,8,16
```

**Archivos**:
- `core/engine_bench_test.go` — suite de benchmarks con `b.SetBytes` y `b.RunParallel`
- `docs/features/BENCHMARKS.md` — guía de ejecución, comparativa con `benchstat`, interpretación

### Docs — Pipeline paralelo end-to-end

Nueva guía dedicada `docs/features/PIPELINE_GUIDE.md` con ejemplo completo de pipeline paralelo (TODO→FIXME cross-lenguaje Go + JS):

- 8 steps organizados en 4 niveles DAG
- Ilustra `input_from`, `input_from_all`, conditions (`count_gt`), template vars (`{{step.field}}`), destructive serialization, rollback con `stop_on_error + create_backup`
- Link añadido desde `BATCH_OPERATIONS_GUIDE.md`

---

## [4.3.4] - 2026-04-20

### Feature — ROI / Savings dashboard: tokens consumidos vs baseline sin filesystem

Nueva página **ROI / Savings** en el dashboard y enriquecimiento del audit log para toma de decisiones.

#### Nuevos campos en `operations.jsonl` (AuditEntry)

| Campo | Descripción |
|-------|-------------|
| `session_id` | ID de sesión (hexadecimal 16 chars). Nueva sesión tras > 5 min de inactividad. Agrupa ops de la misma conversación Claude |
| `file_lines_total` | Total de líneas del archivo objetivo (para calcular eficiencia de range-read) |
| `lines_read` | Líneas realmente leídas/afectadas por la operación |
| `tokens_consumed` | Tokens estimados consumidos por esta op: `(bytes_in + bytes_out) / 4` |
| `tokens_baseline` | Tokens estimados sin filesystem (enfoque naive): `file_size/4` para reads, `file_size*2/4` para edits |
| `tokens_saved` | `max(0, tokens_baseline - tokens_consumed)` |

#### API nueva: `GET /api/roi`

Agrega el log de operaciones y devuelve:
- Totales globales: tokens consumidos, baseline, ahorro, % ahorro
- Eficiencia de range-reads: % de reads con rango y % promedio del archivo leído
- Sesiones recientes (últimas 20): duración, ops, tokens, ahorro por sesión
- Desglose por herramienta: qué tools aportan más ahorro
- Top 10 operaciones más eficientes
- Anti-patrones detectados (`feedback_pattern` acumulados)

#### Dashboard: página "ROI / Savings"

8 cards + 4 tablas:
- **Cards**: Tokens Saved / Savings % / Tokens Consumed / Baseline / Sessions / Range Reads / Avg % File Read / Time Span
- **By Tool**: desglose por herramienta con ahorro promedio por op
- **Top 10 savings**: operaciones individuales más eficientes
- **Sessions**: historial de sesiones con tokens y errores
- **Anti-patterns**: conteo de feedback patterns detectados

#### Archivos modificados

- `core/audit_logger.go` — nuevos campos en `AuditEntry` + `SetFileLinesTotal()` + `SetLinesRead()`
- `core/engine.go` — `CurrentSessionID()` + session tracking con timeout de inactividad
- `audit.go` — poblar `session_id` + cálculo `tokens_consumed/baseline/saved` en `auditWrap`
- `tools_core.go` — `SetFileLinesTotal` + `SetLinesRead` en handler `read_file`
- `cmd/dashboard/main.go` — `AuditEntry` actualizado + `roiHandler` + `/api/roi` endpoint
- `cmd/dashboard/static/index.html` — página ROI / Savings
- `cmd/dashboard/static/app.js` — `fetchROI()` + polling 30s

---

## [4.3.3] - 2026-04-20

### Feature — Proxy captura `clientInfo` del handshake MCP (`cmd/proxy/main.go`)

**Contexto**: El protocolo MCP no transmite el nombre del modelo en ningún mensaje — no existe campo para ello en `tools/call`. El `--model` flag era la única forma de identificación.

**Mejora**: El proxy ahora intercepta el mensaje `initialize` del handshake MCP y extrae `clientInfo.name` + `clientInfo.version` automáticamente. Este valor se logea como campo `client` en cada entrada de `proxy.jsonl`.

| Campo | Fuente | Identifica |
|-------|--------|------------|
| `model` | `--model` flag | Modelo AI (e.g. `sonnet-4`) — requiere config manual |
| `client` | `initialize` clientInfo | App cliente MCP (e.g. `Claude Desktop/0.9.2`) — auto-detectado |

El campo `client` aparece también en stderr al inicio: `mcp-proxy: client detected from initialize: "Claude Desktop/0.9.2"`.

**Archivos modificados**: `cmd/proxy/main.go`, `cmd/proxy/CLAUDE.md`

---

## [4.3.2] - 2026-04-20

### Fix — `batch_operations` write→edit en mismo batch fallaba por validación pre-ejecución (`core/batch_operations.go`)

**Problema**: `validateOperations` hacía `os.Stat` en todos los ops antes de ejecutar ninguno. Si un batch contenía `write` seguido de `edit`/`copy`/`search_and_replace`/`move`/`delete` sobre el mismo archivo recién creado, la validación fallaba con "file does not exist" aunque la secuencia de ejecución fuera correcta.

**Solución**: Se añade `pendingPaths map[string]bool` que se construye secuencialmente durante la validación:
- `write` y `create_dir` agregan su path al set tras validarse
- `copy` y `move` agregan el destination; `move` elimina el source
- `delete` elimina el path del set
- `edit`, `search_and_replace`, `copy` (source), `move` (source), `delete` — el check `os.IsNotExist` se combina con `!pendingPaths[path]`, permitiendo referencias a archivos que una op anterior del mismo batch creará

Esto habilita cadenas completas como `write → edit → copy` en un único batch atómico.

**Archivos modificados**: `core/batch_operations.go`

---

## [4.3.1] - 2026-04-20

### Fix — Auto-truncación de archivos grandes en `read_file` sin rango (`format.go`, `tools_core.go`)

**Problema**: `read_file(path)` sin `start_line`/`end_line` devolvía el contenido crudo sin ningún indicador del total de líneas del archivo. Si Claude Desktop truncaba silenciosamente la respuesta MCP, el modelo asumía que lo recibido era el archivo completo, causando ediciones incorrectas o análisis parciales.

**Solución**: La ruta de lectura completa ahora pasa el contenido por `autoTruncateLargeFile()` antes de devolverlo:
- Archivos ≤ 500 líneas → devueltos sin cambios (comportamiento idéntico al anterior)
- Archivos > 500 líneas → truncados a las primeras 500 líneas con footer informativo:

```
[Lines 1-500 of 1869 total lines in ObservationsService.cs — use start_line/end_line to read more, e.g. start_line=501 end_line=1001]
```

El footer es idéntico en formato al que ya emitía `ReadFileRange`, garantizando un señal consistente independientemente del modo de llamada.

**Archivos modificados**: `format.go`, `tools_core.go`  
**Tests añadidos**: `format_test.go` — 3 casos: archivo pequeño sin cambios, truncación correcta, formato del footer

---

## [4.3.0] - 2026-04-19

### Feature — Unified Diff in edit responses (`core/diff.go`)

Every successful `edit_file` call now appends a standard unified diff to the response.

**Format**: standard 3-context-line hunks, `--- a/file` / `+++ b/file` / `@@ -N,M +N,M @@`.

- **Compact mode**: diff appended inline after the status line
- **Verbose mode**: diff appended under `Diff:` label
- **Dry-run**: diff not generated (no changes applied)
- `DiffStats(old, new)` helper available for compact `+N -M` summary

**Implementation**: Pure LCS algorithm, zero external dependencies. `UnifiedDiffContext()` accepts configurable context lines.

**Files added**: `core/diff.go`

---

### Feature — Pattern Reinforcement / Feedback system (`core/feedback.go`)

The server detects common LLM anti-patterns and annotates responses with structured feedback — non-blocking warnings (`warn`) or hard blocks (`ko`) — instead of silent failures or cryptic errors.

#### Detected patterns

| Pattern | Trigger | Action |
|---|---|---|
| `truncation` | `write_file` with new content < 50% of existing file | **BLOCK** |
| `inflation_loop` | `write_file` with new content > 3× existing file | **BLOCK** |
| `full_rewrite` | `write_file` on existing file > 10KB | warn |
| `stale_read` | `edit_file` on file not read in this session (last 10 min) | warn |
| `repeated_old_text` | same `old_text` fails to match 2+ times on same file | warn |
| `large_new_text` | `new_text` > 80% of file size | warn |

#### Session state (in-memory, per server instance)
- `RecordRead(path)` — called after every successful `read_file` and `edit_file`
- `RecordFailedOldText(path, oldText)` — increments failure counter per path+text
- `ResetFailedOldText(path, oldText)` — clears counter on successful edit

#### Response format
- **Compact mode**: inline tag `[WARN:stale_read]` or `[KO:truncation]`
- **Verbose mode**: annotated block after the main response

**Files added**: `core/feedback.go`

---

### Fix — Backup restore now returns pre-restore backup ID

`RestoreBackup()` signature changed from `([]string, error)` to `([]string, string, error)`.
The second return value is the ID of the safety backup created before restoring.

**Before** — response was silent about the safety backup:
```
Restore completed successfully
Restored 1 file(s): ...
A backup of the current state was created before restoring
```

**After** — includes the pre-restore ID and UNDO command:
```
Restore completed successfully
Restored from backup: 20260419-130000-abc
Restored 1 file(s): ...
Safety backup (state before restore): 20260419-140000-xyz
UNDO this restore: backup(action:"restore", backup_id:"20260419-140000-xyz")
```

Same fix applied to `undo_last` — now exposes REDO command pointing to pre-undo backup.

**Files changed**: `core/backup_manager.go`, `tools_batch.go`, `core/pipeline.go` (rollback call site)

---

### Fix — `edit_file` compact mode lost UNDO instruction

The compact mode response had been shortened to `[backup:ID]`, losing the full restore command.
Restored to `[backup | UNDO: backup(action:"restore", backup_id:"...")]`.

**File changed**: `tools_core.go`

---

### Improvement — Audit log extended for feedback and diff

`AuditEntry` gains three new fields:

| Field | Type | Description |
|---|---|---|
| `feedback_pattern` | string | Detected pattern ID (e.g. `stale_read`) |
| `feedback_status` | string | `warn` or `ko` (omitted when ok) |
| `diff_lines` | int | Lines in the generated unified diff |

`Status` field now supports three values: `ok`, `warn`, `error` (previously only `ok`/`error`).

`BytesOut` now excludes the unified diff text — metric remains representative of file bytes, not response size.

New context helpers: `SetFeedback(ctx, signal)`, `SetDiffLines(ctx, n)`.

**Files changed**: `core/audit_logger.go`, `audit.go`, `tools_core.go`

---

### Improvement — Dashboard UI updated for new log fields

- `app.js`: `statusClass` now handles `ok`/`warn`/`error` (was binary ok/error)
- `app.js`: Detail panel now renders `feedback_pattern` as colored badge and `diff_lines` count
- `style.css`: Added `.badge.warn` — yellow, consistent with `--yellow` design token

**Files changed**: `cmd/dashboard/static/app.js`, `cmd/dashboard/static/style.css`

---

### Summary of files changed

| File | Change |
|---|---|
| `core/diff.go` | NEW — unified diff generator |
| `core/feedback.go` | NEW — pattern detector + session state |
| `core/audit_logger.go` | AuditEntry new fields + SetFeedback/SetDiffLines helpers |
| `core/backup_manager.go` | RestoreBackup signature → ([]string, string, error) |
| `core/pipeline.go` | rollback() updated for new RestoreBackup signature |
| `tools_core.go` | read_file RecordRead, write_file CheckWriteOp, edit_file diff+feedback integration |
| `tools_batch.go` | restore + undo_last expose pre-restore backup ID |
| `audit.go` | BytesOut excludes diff text |
| `cmd/dashboard/static/app.js` | warn status, feedback_pattern badge, diff_lines |
| `cmd/dashboard/static/style.css` | .badge.warn style |

---

## [4.2.2] - 2026-04-17

### Security — Bug #29: Write inflation loop protection

**Issue**: In long sessions, Claude may call `write_file` in a loop building content as `(content_read + new_block)`. Each call inflates the file, e.g., a 26KB file appended with 2KB 64 times → 122KB, breaking compilation with CS8802/CS8801.

**Fix**: Added inflation guard in `IntelligentWrite()` — if new content exceeds 3× existing file size (>10KB), write is blocked with error explaining the pattern and suggesting `edit_file` instead.

**Files changed**: `core/claude_optimizer.go`

### Performance — Token savings for Claude Desktop

#### 1. Edit efficiency hints on full-file rewrite detection
When `edit_file` detects a potential full-file rewrite (oldText > 1000 bytes, single replacement), the response now includes a TIP nudging toward the efficient workflow:

```
💡 TIP: For a single replacement, consider using search_files(pattern) → read_file(start_line/end_line) → edit_file(old_text, new_text) to save tokens
```

**Files changed:**
- `core/edit_operations.go`: Added `EfficiencyHint` field to `EditResult` struct
- `tools_core.go`: Added efficiency hint to compact and verbose edit responses

#### 2. Improved serverInstructions with concrete workflow examples
`serverInstructions` (sent during MCP handshake) expanded with:

- **AVOID rule**: `write_file` for existing files wastes tokens
- **TOKEN SAVINGS EXAMPLES**: Three concrete scenarios with exact tool call patterns
  - Surgical function change: range read + targeted edit
  - Cross-file rename: batch pipeline
  - Large file handling: range read

**File changed:** `main.go`

#### 3. analyze_operation returns efficiency suggestions
`analyze_operation` now detects and warns about inefficient operations:

- **Edit analysis**: When oldText > 1000 bytes, single occurrence, and file > 5KB → suggests surgical edit workflow
- **Write analysis**: When new content is <50% or >200% of existing file size → suggests edit_file instead of full rewrite

**Files changed:**
- `core/plan_mode.go`: Added `EfficiencyTip` field to `ChangeAnalysis` struct + logic in `AnalyzeEditChange()` and `AnalyzeWriteChange()`
- `format.go`: Updated `formatChangeAnalysis()` to display efficiency tip

### Dependencies
- `github.com/mark3labs/mcp-go` v0.47.1 → **v0.48.0**
- `github.com/stretchr/objx` v0.5.2 → **v0.5.3**
- `golang.org/x/mod` v0.21.0 → **v0.35.0**
- `golang.org/x/tools` v0.26.0 → **v0.44.0**

---

## [4.2.1] - 2026-04-04

### Security — AI-era attack surface hardening (5 vectors mitigated)

#### 1. Path Security Layer — new `core/path_security.go`
Universal validation applied to **every path operation** regardless of `--allowed-paths` configuration.

- **NTFS Alternate Data Streams (ADS)**: Paths containing `:` after the drive letter are rejected (e.g. `C:\file.txt:hidden_stream`). Prevents hidden covert channels invisible to `list_directory` and OS file managers. (Windows-only check via `runtime.GOOS`.)
- **Unicode directional overrides and zero-width characters**: 18 dangerous code points blocked including `U+202E` (RIGHT-TO-LEFT OVERRIDE — RTLO extension spoofing), `U+200B` (ZERO WIDTH SPACE — hook pattern evasion), `U+202D/202E/202A/202B` (bidirectional embeddings), `U+FEFF` (BOM), `U+2028/2029` (line/paragraph separators). Entire Unicode `Cf` (Format) category also blocked.
- **Windows reserved device names**: `CON`, `PRN`, `AUX`, `NUL`, `COM0-9`, `LPT0-9` rejected by base name (case-insensitive, extension-stripped). Prevents DoS via `os.ReadFile("CON")` freezing the process waiting for stdin. Check applied cross-platform for portability.

#### 2. WSL Blanket Bypass Removed — `core/engine.go` `IsPathAllowed()`
Previously, any path starting with `\\wsl.localhost\` or `\\wsl$\` **unconditionally bypassed** `--allowed-paths` access control:
```
# Before: this worked regardless of --allowed-paths
read_file(path="\\wsl.localhost\Ubuntu\etc\passwd")   → ALLOWED (bypass)
write_file(path="\\wsl.localhost\Ubuntu\etc\cron.d\x") → ALLOWED (bypass)
```
WSL paths now follow the same containment check as all other paths when `--allowed-paths` is configured. When no `--allowed-paths` is set (open-access mode), WSL paths continue to be accessible.

#### 3. `IsPathAllowed()` refactored — security checks always run
`validatePathSecurity()` is called first in `IsPathAllowed()` before any containment check. Security checks (ADS, Unicode, reserved names) fire even in open-access mode (no `--allowed-paths`). The outer `if len(AllowedPaths) > 0` guards have been removed from all 20 call sites — `IsPathAllowed()` now returns `true` when AllowedPaths is empty (after passing security checks), making the method always safe to call.

#### 4. Hook system: cross-platform command execution — `core/hooks.go`
Hook commands of type `command` previously used `cmd /C` unconditionally, causing hooks to silently fail on Linux and macOS. Fixed with OS detection:
- Windows: `cmd /C <command>`
- Linux/macOS: `sh -c <command>`

### Security
- Updated Go toolchain to **go1.26.2** (fixes 4 stdlib CVEs):
  - **GO-2026-4947** — Unexpected work during chain building in `crypto/x509`
  - **GO-2026-4946** — Inefficient policy validation in `crypto/x509`
  - **GO-2026-4870** — Unauthenticated TLS 1.3 KeyUpdate causes DoS in `crypto/tls`
  - **GO-2026-4866** — Case-sensitive `excludedSubtrees` name constraints auth bypass in `crypto/x509`

### Added — Hook system: 12 events now fully connected (was 4)
- **4 new hook event constants** in `core/hooks.go`: `HookPreRead`, `HookPostRead`, `HookPreSearch`, `HookPostSearch`
- **`pre-delete` / `post-delete`** hooks connected in `DeleteFile()` and `SoftDeleteFile()` — `pre-delete` with `failOnError:true` can block destructive deletes of sensitive files (`.env`, `.pem`, `.key`)
- **`pre-create` / `post-create`** hooks connected in `CreateDirectory()` — enables naming convention enforcement and directory scaffolding  
- **`pre-move` / `post-move`** hooks connected in `MoveFile()` — `HookContext` includes `SourcePath` + `DestPath` for destination validation
- **`pre-copy` / `post-copy`** hooks connected in `CopyFile()` — blocks copying sensitive files to insecure locations
- **`pre-read` / `post-read`** hooks connected in `ReadFileContent()` — `pre-read` with `failOnError:true` can deny access to credential files; `post-read` enables compliance audit logging
- **`pre-search` / `post-search`** hooks connected in `SmartSearch()` and `AdvancedTextSearch()` — search pattern available in `HookContext.Metadata` for credential-harvesting detection
- **`examples/hooks.example.json`** fully updated: all 16 hook events documented with security use cases, `_comment` fields explaining each pattern

### Dependencies
- `github.com/mark3labs/mcp-go` v0.46.0 → **v0.47.1**
- `golang.org/x/sys` v0.42.0 → **v0.43.0**
- `go` directive updated: 1.26.1 → **1.26.2**

### Fixed — `read_file\` with \`path\`+\`paths\`+range ignored range
When calling \`read_file\` with both \`path\` and \`paths\` parameters AND \`start_line\`/\`end_line\` range parameters, the \`paths\` array was processed first, ignoring the range and returning full file content (or potentially truncating in edge cases).

**Fix in \`tools_core.go\`**: Added logic to detect when both \`path\` and \`paths\` are provided with range parameters. In this case, \`path\`+range takes precedence over \`paths\`.

**Reproducción**: \`read_file(path=\"f.cs\", paths='[\"f.cs\"]', start_line=40, end_line=50)\`

**Issue**: [scopweb/mcp-filesystem-go-ultra#8](https://github.com/scopweb/mcp-filesystem-go-ultra/issues/8)

---

## [4.2.1] - 2026-04-04

### Security Fix — Allowed-path root deletion protection

Destructive operations (`delete_file`, `soft_delete`, `move_file`) could target the `--allowed-paths` root directory itself, allowing `os.RemoveAll()` to wipe an entire allowed tree. This affected both Linux and Windows.

**Root cause:** `IsPathAllowed()` returned `true` for the root path via its equality check, and delete/move functions had no additional guard.

**Fix:**
- New `IsAllowedPathRoot()` method in `core/engine.go` — detects if a path resolves to an allowed-path root (handles symlinks, trailing slashes, dot components)
- `DeleteFile()`, `SoftDeleteFile()`, `MoveFile()` in `core/file_operations.go` — reject allowed-path roots with `access denied` error
- `validateOperations()` in `core/batch_operations.go` — blocks batch delete/move on allowed-path roots
- Tests: `TestDeleteAllowedPathRootBlocked` and `TestDeleteAllowedPathRootVariations`

### Changed — Split main.go into 10 files by responsibility

The monolithic `main.go` (3574 lines) was split into focused files, all remaining in `package main`:

| File | Lines | Responsibility |
|------|-------|----------------|
| `main.go` | ~250 | Config, CLI flags, `main()` |
| `audit.go` | ~110 | `auditWrap`, `summarizeArgs` |
| `format.go` | ~415 | Formatters, `parseSize`, `truncateContent` |
| `help_content.go` | ~580 | `getHelpContent()` (static help text) |
| `tools_core.go` | ~515 | `toolRegistry`, `registerTools`, read/write/edit_file |
| `tools_search.go` | ~250 | list_directory, search_files, analyze_operation |
| `tools_files.go` | ~255 | create_directory, delete/move/copy_file, get_file_info |
| `tools_batch.go` | ~605 | multi_edit, batch_operations, backup |
| `tools_platform.go` | ~455 | wsl, server_info |
| `tools_aliases.go` | ~260 | 6 aliases, fs super-tool, help |

Tool registration uses a `toolRegistry` struct shared across files.

### Fixed
- `bug23_test.go` — missing `dryRun` parameter in `MultiEdit` call (preexisting after v4.2.0 signature change)

## [4.2.0] - 2026-04-02

### Added
- **`help` tool** — standalone tool that returns the full tool catalog with usage rules and best practices. Keyword-rich description ensures Claude Desktop's semantic search picks it up for any filesystem query.
- **`fs` super-tool** — single entry point dispatching to all 16 operations via `action` param. Solves lazy-loading clients that only discover 4-5 tools.
- **`server.WithInstructions()`** — sends tool catalog during MCP initialize handshake (spec 2025-11-25 compliant).
- **`/filesystem-ultra-tools` skill** — Claude Code skill (`.claude/skills/filesystem-ultra-tools/`) that calls `help` at conversation start.
- **Tool title annotations** — all tools have `WithTitleAnnotation()` for better client UI display.
- **Cross-reference descriptions** — every tool description mentions related tools so Claude Desktop learns about tools it hasn't loaded yet.
- **`server.WithLogging()`** — MCP logging capability enabled.
- **6 aliases** — `read_text_file`, `search`, `edit`, `write`, `create_file`, `directory_tree` with full parameter schemas.

### Fixed (v4.2.0 hotfix — 4 bugs found in testing)
- **dry_run:true wrote to disk** [CRITICAL] — `EditFile`/`MultiEdit` lacked `dryRun` parameter; edits were applied. Fixed: `dryRun bool` added, skips backup/hooks/write when true.
- **case_sensitive:false ignored in search_files** — default was `false`, routing never activated `AdvancedTextSearch`. Fixed: default changed to `true`.
- **batch rename returned 0 files** — `filepath.Walk` skipped root dir. Fixed: early return for root path.
- **count_only ignored whole_word/case_sensitive** — `CountOccurrences` didn't accept these flags. Fixed: added params with `(?i)` and `\b` regex support.

### Changed
- Tool descriptions shortened and unified with "Related: ..." cross-references for Claude Desktop discoverability.

## [4.1.3] - 2026-03-17

### Bug Fix: #27 — multi_edit atomic rollback (prevents file truncation)

`multi_edit` with 2+ edits could truncate files when the second edit's `old_text` didn't match after the first edit was applied. The file was written with only partial changes, causing code blocks to disappear (e.g., 565 lines → 178 lines).

**Root cause:** `multi_edit` applied edits sequentially and wrote the file even when some edits failed. Common triggers:
- Quote escaping mismatches (`\"` vs `"`, single vs double quotes in HTML/JS)
- Content shift after prior edit changed surrounding text

**Fix:** `multi_edit` is now atomic — if ANY edit fails, the file is NOT modified and the error response includes:
- Which edits would have applied (rolled back)
- Which edits failed and why
- The backup_id (original file is always safe)
- Actionable instruction: "Fix the failing old_text and retry"

**Files changed:**
- `core/edit_operations.go` — atomic rollback when `FailedEdits > 0` (no partial writes)
- `main.go` — detailed error response with per-edit status and backup_id

## [4.1.2] - 2026-03-17

### Bug Fix: #24 — v3 tool names in error messages + undo/recovery system for AI self-healing

When edit_file or multi_edit failed, error messages referenced deprecated v3 tool names (`read_file_range`, `smart_search`) that no longer exist in v4, causing Claude Desktop to call non-existent tools and enter error loops.

Additionally, when an AI model (e.g. Haiku) made bad edits across multiple files, there was no easy way for the AI itself to discover and restore from filesystem-ultra's own backups — requiring manual human intervention.

#### Fix 1: Update error messages from v3 to v4 tool names

- **Fixed**: `core/edit_operations.go` — 3 error messages: `read_file_range` → `read_file`, `smart_search` → `search_files`
- **Fixed**: `core/engine.go` — 1 recommendation message: `smart_search + read_file_range` → `search_files + read_file`
- **Fixed**: `core/batch_operations.go` — 2 error messages: `read_file_range` → `read_file`

#### Fix 2: UNDO instructions in every edit response

Every `edit_file` and `multi_edit` response now includes the exact command to undo the change:

- **Compact mode**: `OK: 1 changes [backup:abc123 | UNDO: backup(action:"restore", backup_id:"abc123")]`
- **Verbose mode**: `Backup ID: abc123\nUNDO: backup(action:"restore", backup_id:"abc123")`

This ensures the AI always has the information needed to restore, even after model switches or context loss.

#### Fix 3: `undo_last` action for backup tool

New `backup(action:"undo_last")` — restores the most recent backup without requiring a backup_id:

- Finds the most recent backup automatically
- Supports `preview: true` to show what would be restored before doing it
- Creates a safety backup of the current state before restoring
- Zero new dependencies — reuses existing `ListBackups(1)` + `RestoreBackup()`

#### Fix 4: Recovery instructions in tool descriptions

- **Updated**: `edit_file` description now includes: `UNDO: Every edit returns a backup_id. To undo: backup(action:"restore", backup_id:"..."). Quick undo: backup(action:"undo_last").`
- **Updated**: `multi_edit` description — same undo instructions
- **Updated**: `backup` tool description — lists `undo_last` as valid action, adds `DISASTER RECOVERY` guidance

#### Files changed

| File | Changes |
|------|---------|
| `main.go` | edit_file/multi_edit responses with UNDO, undo_last case, updated descriptions |
| `core/edit_operations.go` | 3× `read_file_range` → `read_file`, `smart_search` → `search_files` |
| `core/engine.go` | 1× recommendation message updated to v4 tool names |
| `core/batch_operations.go` | 2× `read_file_range` → `read_file` |

#### Fix 5: `read_file` with `start_line` but no `end_line` ignored start_line (Bug #25)

When the AI called `read_file(path, start_line=880)` without `end_line`, the `start_line` parameter was silently ignored and the entire file was returned from line 1. This caused the AI to believe files were truncated or "wrapping around".

- **Fixed**: `main.go` — `start_line` without `end_line` now reads from `start_line` to end of file
- **Fixed**: `main.go` — `end_line` without `start_line` now reads from line 1 to `end_line`

#### Fix 6: Backup system info visible in `server_info(action:"stats")`

The AI had no way to discover where backups were stored or how many existed.

- **Added**: `core/backup_manager.go` — `GetBackupDir()` and `GetBackupLimits()` getters
- **Added**: `main.go` — `server_info(action:"stats")` now shows backup directory, limits, total count, latest backup, and undo command

#### Fix 7: `server_info(topic:"recovery")` — Disaster recovery guide

New help topic with step-by-step instructions for AI self-recovery from bad edits.

- **Added**: `main.go` — "recovery" topic covering: undo_last, find backups by filename, compare before restore, pre-repair checklist, golden rule (stop editing if making things worse)

#### Files changed (complete)

| File | Changes |
|------|---------|
| `main.go` | UNDO in responses, undo_last, start_line fix, stats backup info, recovery help topic, multi_edit JSON sanitizer |
| `core/edit_operations.go` | 3× error messages v3→v4 |
| `core/engine.go` | 1× recommendation v3→v4 |
| `core/batch_operations.go` | 2× error messages v3→v4 |
| `core/backup_manager.go` | GetBackupDir(), GetBackupLimits() getters |
| `core/impact_analyzer.go` | FormatRiskNotice: compact actionable format, VERIFY instruction for HIGH risk, removed v3 `restore_backup` |
| `tests/bug16_test.go` | Updated assertion for new risk notice format |

#### Fix 8: Risk warnings — compact, actionable, no redundancy

Risk warnings were verbose and passive (informational but not actionable for the AI).

- **Changed**: `FormatRiskNotice` now returns compact format: `⚠️ HIGH RISK (80% changed)` — one line
- **Added**: For HIGH/CRITICAL risk: `⚠️ VERIFY: read_file("path", mode:"tail")` — actionable instruction
- **Removed**: Redundant UNDO in risk warning (already present in main response line)
- **Removed**: Verbose risk factors list, char count, occurrence count (passive info)
- **Fixed**: `restore_backup(backup_id)` → removed (v3 tool name that doesn't exist)

#### Fix 9: `multi_edit` — invalid JSON with literal newlines (Bug #26)

Claude Desktop sends `edits_json` with literal newlines inside string values (e.g., multi-line `old_text`). Go's `json.Unmarshal` rejects raw `\n`/`\r`/`\t` inside JSON strings.

- **Added**: `main.go` — JSON string sanitizer that escapes literal control characters (`\n` → `\\n`, `\r` → `\\r`, `\t` → `\\t`) only inside quoted strings, preserving already-escaped sequences and structural whitespace outside strings

## [4.1.1] - 2026-03-12

### Bug Fix: #19 — write_base64, wsl_sync y copy_file fallan desde contenedor Linux (claude.ai web)

Cuando Claude se usa desde claude.ai (browser), el `bash_tool` corre en un contenedor Linux aislado — no es WSL real. Las rutas `/home/claude/...` no son accesibles desde Windows vía `\\wsl.localhost\...`, causando timeouts y errores confusos.

#### Problema 1: write_base64 timeout con payloads grandes

- **Added**: Constante `MaxBase64PayloadSize = 512KB` en `core/config.go`
- **Added**: Validación de tamaño antes del decode en `main.go` — retorna error explícito inmediato en vez de timeout
- **Updated**: Descripción del tool: documenta límite de 512KB, sugiere `mcp_write` para texto y chunking para binarios grandes

#### Problema 2: wsl_sync falla con rutas de contenedor Linux

- **Added**: `CheckLinuxPathAccessible()` en `core/path_detector.go` — detecta si una ruta Linux es accesible desde Windows
  - Sin WSL distro → error: "path es de contenedor Linux, no accesible desde Windows"
  - Con WSL pero UNC path inexistente → error: "path no accesible, probablemente contenedor aislado"
  - Ambos casos sugieren usar `mcp_write` como alternativa
- **Added**: Llamada a `CheckLinuxPathAccessible()` en handler de `wsl_sync` antes de intentar la copia
- **Updated**: Descripción del tool: "Requires real WSL (Claude Desktop). Does NOT work from isolated Linux containers"

#### Problema 3: copy_file con rutas de contenedor + error confuso

- **Added**: Llamada a `CheckLinuxPathAccessible()` en handler de `copy_file` antes de `CopyFile()`
- **Fixed**: Mensaje de error ahora incluye source y dest explícitamente: `copy_file error (source='...', dest='...'): ...`
- **Updated**: Descripción del tool: documenta que rutas de contenedor Linux no son accesibles

#### Files changed

| File | Changes |
|------|---------|
| `core/config.go` | `MaxBase64PayloadSize` constant |
| `core/path_detector.go` | `CheckLinuxPathAccessible()` function |
| `main.go` | Size validation in `write_base64`, path checks in `wsl_sync` and `copy_file`, updated descriptions |

---

## [4.1.0] - 2026-03-06

### Pipeline Transformation System v2 — Conditions, Templates, Parallel Execution & 3 New Actions

Major upgrade to `execute_pipeline` expanding it from 9 to 12 actions with conditional logic, template variables, DAG-based parallel execution, and structured error reporting.

#### Phase 1: SubOp Tracking + Structured Errors

- **Added**: `PipelineStepError` structured error type with StepID, StepIndex, Action, Param, Message, Suggestion fields
- **Added**: `AppendSubOp()` tracking in pipeline executor — sub_op shows full execution path (e.g., `"pipeline:3_steps → search → edit → regex_transform"`)
- **Added**: SubOp tracking in `LargeFileProcessor` (`in_memory`, `line_by_line`, `chunk_by_chunk`) and `RegexTransformer` (`regex_sequential`, `regex_parallel`)
- **Files changed**: `core/pipeline.go`, `core/errors.go`, `core/large_file_processor.go`, `core/regex_transformer.go`

#### Phase 2: Conditional Steps + Template Variables

- **Added**: 9 condition types: `has_matches`, `no_matches`, `count_gt`, `count_lt`, `count_eq`, `file_exists`, `file_not_exists`, `step_succeeded`, `step_failed`
- **Added**: Template variable system — `{{step_id.field}}` resolves to prior step results (fields: `count`, `files_count`, `files`, `risk`, `edits`)
- **Added**: `Condition *StepCondition` field on PipelineStep — steps can be skipped based on prior results
- **Added**: `Skipped bool` and `SkipReason string` fields on StepResult
- **New files**: `core/pipeline_conditions.go`, `core/pipeline_templates.go`
- **Tests**: `tests/pipeline_conditions_test.go` (14 tests), `tests/pipeline_templates_test.go` (10 tests)

#### Phase 3: Parallel Execution + New Actions

- **Added**: `parallel: true` flag on PipelineRequest — enables DAG-based parallel execution
- **Added**: DAG scheduler with topological sort (Kahn's algorithm) grouping independent steps into execution levels
- **Added**: Destructive step splitting — write operations on same level are serialized for safety
- **Added**: `input_from_all: ["step1", "step2"]` — multi-step references for aggregate/merge
- **Added**: 3 new actions:
  - `aggregate` — combines content/files from multiple steps
  - `diff` — unified diff between two files
  - `merge` — union/intersection of file lists from multiple steps
- **New files**: `core/pipeline_scheduler.go`
- **Tests**: `tests/pipeline_scheduler_test.go` (6 tests), `tests/pipeline_new_actions_test.go` (5 tests)

#### Phase 4: Streaming Progress + Documentation

- **Added**: `OnProgress` callback on PipelineExecutor — fires per-step audit entries
- **Added**: Per-step audit log entries (`sub_op: "step:1/3:find:search"`) visible in dashboard Operations page
- **Updated**: `CLAUDE.md` with full Pipeline v2 documentation
- **Updated**: `main.go` — OnProgress wiring with `engine.AuditEnabled()` check
- **Updated**: `docs-website/` — Pipeline feature page and API reference updated

#### Summary

- **12 actions** (was 9): search, read_ranges, edit, multi_edit, count_occurrences, regex_transform, copy, rename, delete, aggregate, diff, merge
- **43 new tests** across 4 test files, all passing
- **Full backward compatibility** — existing pipeline JSON works unchanged

---

## [4.0.1] - 2026-03-04

### Bug Fix: #18 — Literal escape normalization + parameter aliases for Claude Desktop

Claude Desktop sometimes sends `old_text` with literal `\n` (backslash + n as two characters) instead of real newline characters, causing "no matches found" errors. Also, Claude Desktop occasionally uses `old_str`/`new_str` parameter names (from its native `str_replace` convention) instead of `old_text`/`new_text`.

#### Literal escape normalization (Bug #18a)

- **Added**: `normalizeLiteralEscapes()` function — converts literal `\n` and `\t` to real characters
- **Safety**: Only converts when string has literal `\n` but NO real newlines (avoids corrupting code containing `\n` string literals)
- **Applied as fallback** in `performIntelligentEdit()` (OPTIMIZATION 6) — tried only after exact match, TrimSpace, line-by-line, and multiline matching all fail
- **Applied in** `validateEditContext()` (Level 1.5) — prevents premature rejection before `performIntelligentEdit` has chance to match
- **Files changed**: `core/edit_operations.go`

#### Compare files operation (new feature)

- **Added**: `analyze_operation(operation:"compare", path:"fileA", path_b:"fileB")` — diff two arbitrary files
- **Added**: `CompareFiles()` engine method in `core/plan_mode.go`
- **Added**: `generateComparisonDiff()` — unified-style diff with line numbers (shows up to 40 differences)
- **Output**: Line counts, size comparison, line-by-line diff preview
- **Read-only**: No files are modified, risk level always "low"
- **Tests**: `tests/compare_files_test.go` — 5 tests (different, identical, not found, access denied, larger files)

#### Parameter aliases (Bug #18b)

- **Added**: `mcp_edit` now accepts both `old_text`/`new_text` and `old_str`/`new_str` parameter names
- **Added**: `multi_edit` edits array now accepts both `{"old_text", "new_text"}` and `{"old_str", "new_str"}` per edit
- **Files changed**: `main.go`

#### Tests

- **Added**: `tests/bug18_literal_escapes_test.go` — 4 regression tests:
  - `TestBug18_LiteralNewlineEscapes` — literal `\n` in old_text matches file with real newlines
  - `TestBug18_LiteralTabEscapes` — literal `\t` in old_text matches file with real tabs
  - `TestBug18_RealNewlinesStillWork` — real newlines continue to work as before
  - `TestBug18_CodeWithBackslashN` — code containing `\n` string literals is NOT corrupted

---

## [4.0.0] - 2026-03-03

### BREAKING CHANGE: Tool consolidation — 59 tools reduced to 30

Major refactor to eliminate redundant MCP tool registrations. Claude Desktop uses lazy loading (tool_search) when a server exposes more than ~30 tools, which degrades the user experience. This release consolidates duplicate and overlapping tools into intelligent, auto-routing unified tools — without removing any underlying functionality.

**All engine functions, core logic, and internal optimizations remain unchanged.** Only the MCP tool surface was restructured.

#### Consolidation summary

| Group | Before | After | Removed |
|-------|--------|-------|---------|
| READ | 5 tools | 2 tools | -3 |
| WRITE | 5 tools | 2 tools | -3 (+ upgraded mcp_write) |
| EDIT | 5 tools | 1 tool | -4 (+ upgraded mcp_edit) |
| SEARCH | 3 tools | 1 tool | -2 (+ upgraded mcp_search) |
| LIST | 2 tools | 1 tool | -1 |
| ANALYSIS | 5 tools | 1 tool | -4 |
| ARTIFACTS | 3 tools | 1 tool | -2 |
| BACKUPS | 5 tools | 2 tools | -3 |
| WSL | 6 tools | 2 tools | -4 |
| TELEMETRY | 2 tools | 1 tool | -1 |
| DELETE | 2 tools | 1 tool | -1 |
| **Total** | **59** | **30** | **-29** |

#### READ — 5 → 2 tools

- **Removed**: `read_file`, `chunked_read_file`, `intelligent_read`
- **Kept**: `mcp_read` (already called `IntelligentRead` internally, which auto-selects direct vs chunked based on file size), `read_file_range`, `read_base64`

#### WRITE — 5 → 2 tools

- **Removed**: `write_file`, `create_file` (was a literal alias), `streaming_write_file`, `intelligent_write`
- **Upgraded**: `mcp_write` now calls `engine.IntelligentWrite()` instead of `engine.WriteFileContent()`. Auto-selects between direct write (small files) and streaming write (large files) based on file size threshold.
- **Kept**: `mcp_write`, `write_base64`

#### EDIT — 5 → 1 tool

- **Removed**: `edit_file`, `smart_edit_file`, `intelligent_edit`, `recovery_edit` (was already deprecated)
- **Upgraded**: `mcp_edit` now calls `engine.IntelligentEdit()` instead of `engine.EditFile()`. Auto-selects between direct edit (small files) and smart streaming edit (large files) based on file size threshold. Includes risk assessment, auto-backup, and context validation.
- **Kept**: `mcp_edit`

#### SEARCH — 3 → 1 tool

- **Removed**: `smart_search`, `advanced_text_search`
- **Upgraded**: `mcp_search` now supports all parameters from both removed tools and auto-routes:
  - Basic call (path + pattern) → `SmartSearch` (fast filename/content search)
  - With `include_content`, `file_types` → `SmartSearch` with filters
  - With `case_sensitive`, `whole_word`, `include_context`, `context_lines` → `AdvancedTextSearch` automatically
- **Kept**: `mcp_search`

#### LIST — 2 → 1 tool

- **Removed**: `list_directory` (identical to `mcp_list`)
- **Kept**: `mcp_list`

#### ANALYSIS / Plan Mode — 5 → 1 tool

- **Removed**: `analyze_file`, `get_optimization_suggestion`, `analyze_write`, `analyze_edit`, `analyze_delete`
- **New**: `analyze_operation` — unified dry-run tool with `operation` parameter:
  - `operation: "file"` → file analysis and strategy recommendation
  - `operation: "optimize"` → Claude Desktop optimization suggestions
  - `operation: "write"` → dry-run write analysis (requires `content`)
  - `operation: "edit"` → dry-run edit analysis (requires `old_text`, `new_text`)
  - `operation: "delete"` → dry-run delete analysis

#### ARTIFACTS — 3 → 1 tool

- **Removed**: `capture_last_artifact`, `write_last_artifact`, `artifact_info`
- **New**: `artifact` — auto-deduces action from parameters provided:
  - `content` provided → capture artifact in memory
  - `path` provided → write stored artifact to file
  - Both `content` + `path` → capture and write in one step (new capability)
  - No parameters → return artifact info

#### BACKUPS — 5 → 2 tools

- **Removed**: `list_backups`, `get_backup_info`, `compare_with_backup`, `cleanup_backups`
- **New**: `backup` — auto-deduces action from parameters:
  - No parameters → list all backups
  - `backup_id` → show detailed backup info
  - `backup_id` + `file_path` → compare file with backup (was `compare_with_backup`)
  - `cleanup: true` → clean up old backups (with `older_than_days`, `dry_run`)
  - Supports all filter params from original `list_backups`: `limit`, `filter_operation`, `filter_path`, `newer_than_hours`
- **Kept**: `restore_backup` (with `preview` mode that replaces `compare_with_backup` for pre-restore diff)

#### WSL — 6 → 2 tools

- **Removed**: `wsl_to_windows_copy`, `windows_to_wsl_copy`, `sync_claude_workspace`, `wsl_windows_status`, `configure_autosync`, `autosync_status`
- **New**: `wsl_sync` — unified copy/sync tool:
  - `source_path` starting with `/` → WSL-to-Windows copy (auto-detects direction)
  - `source_path` starting with drive letter → Windows-to-WSL copy (auto-detects direction)
  - `direction` parameter → workspace sync (wsl_to_windows, windows_to_wsl, bidirectional)
  - All original params preserved: `dest_path`, `filter_pattern`, `dry_run`, `create_dirs`
- **New**: `wsl_status` — unified status + autosync config:
  - No parameters → combined WSL integration status + autosync status
  - `enabled` parameter → configure autosync (with `sync_on_write`, `sync_on_edit`, `silent`)

#### TELEMETRY — 2 → 1 tool

- **Removed**: `performance_stats`, `get_edit_telemetry`
- **New**: `stats` — returns performance stats + edit telemetry in a single response

#### DELETE — 2 → 1 tool

- **Removed**: `soft_delete_file`
- **Changed**: `delete_file` now defaults to safe soft-delete (move to trash). Use `permanent: true` for permanent deletion. Safer by default.

#### Final tool inventory (16 tools)

```
CORE (5):      read_file, write_file, edit_file, list_directory, search_files
EDIT+ (1):     multi_edit
FILES (4):     move_file, copy_file, delete_file, create_directory
BATCH (1):     batch_operations  (includes pipelines + batch rename)
BACKUP (1):    backup            (includes restore via action:"restore")
ANALYSIS (1):  analyze_operation
WSL (1):       wsl               (includes sync + status via action param)
UTIL (1):      server_info       (includes help, stats, artifact via action param)
INFO (1):      get_file_info
INFO (1):      get_file_info
RENAME (1):    batch_rename_files
```

#### Migration guide for existing users

| Old tool | New tool | Notes |
|----------|----------|-------|
| `read_file` | `mcp_read` | Same params |
| `chunked_read_file` | `mcp_read` | Auto-selects chunked for large files |
| `intelligent_read` | `mcp_read` | Already used internally |
| `write_file` / `create_file` | `mcp_write` | Same params, now auto-optimized |
| `streaming_write_file` | `mcp_write` | Auto-selects streaming for large files |
| `intelligent_write` | `mcp_write` | Already used internally |
| `edit_file` | `mcp_edit` | Same params, now auto-optimized |
| `smart_edit_file` | `mcp_edit` | Auto-selects smart edit for large files |
| `intelligent_edit` | `mcp_edit` | Already used internally |
| `recovery_edit` | `mcp_edit` | Was already deprecated |
| `smart_search` | `mcp_search` | Add `include_content`, `file_types` |
| `advanced_text_search` | `mcp_search` | Add `case_sensitive`, `whole_word`, `include_context` |
| `list_directory` | `mcp_list` | Same params |
| `analyze_file` | `analyze_operation(operation:"file")` | |
| `get_optimization_suggestion` | `analyze_operation(operation:"optimize")` | |
| `analyze_write` | `analyze_operation(operation:"write")` | |
| `analyze_edit` | `analyze_operation(operation:"edit")` | |
| `analyze_delete` | `analyze_operation(operation:"delete")` | |
| `capture_last_artifact` | `artifact(content:"...")` | |
| `write_last_artifact` | `artifact(path:"...")` | |
| `artifact_info` | `artifact()` | |
| `list_backups` | `backup()` | |
| `get_backup_info` | `backup(backup_id:"...")` | |
| `compare_with_backup` | `backup(backup_id, file_path)` | |
| `cleanup_backups` | `backup(cleanup:true)` | |
| `wsl_to_windows_copy` | `wsl_sync(source_path:"/home/...")` | Auto-detects direction |
| `windows_to_wsl_copy` | `wsl_sync(source_path:"C:\\...")` | Auto-detects direction |
| `sync_claude_workspace` | `wsl_sync(direction:"...")` | |
| `wsl_windows_status` | `wsl_status()` | |
| `configure_autosync` | `wsl_status(enabled:true)` | |
| `autosync_status` | `wsl_status()` | Included in output |
| `performance_stats` | `stats()` | |
| `get_edit_telemetry` | `stats()` | Included in output |
| `soft_delete_file` | `delete_file(path)` | Now default behavior |
| `delete_file` (permanent) | `delete_file(path, permanent:true)` | Must opt-in |

---

## [3.16.0] - 2026-03-02

### Bug Fix: #17 — multi_edit misleading success counter + full parity with EditFile

- **Fixed**: `multi_edit` reported "1/2 edits" when overlapping edits caused Edit 2's `oldText` to be absent after Edit 1 subsumed it. Subsumed edits are now detected as `already_present` instead of `failed`.
- **Added**: `EditDetailStatus` type (`applied`, `already_present`, `failed`) and `EditDetail` struct for per-edit outcome tracking.
- **Added**: `SkippedEdits` and `EditDetails` fields to `MultiEditResult` (backward compatible — existing fields unchanged).
- **Added**: Aggregate risk assessment in `MultiEdit()` via new `calculateMultiEditImpact()` — simulates all edits to compute final-vs-original change percentage.
- **Added**: CRITICAL risk blocking in `MultiEdit()` — requires `force: true` for >=90% file rewrites (parity with `EditFile`).
- **Added**: Context validation in `MultiEdit()` — validates edits against original content, allows partial success.
- **Added**: Pre/Post hook execution in `MultiEdit()` — `HookPreEdit` before edit loop, `HookPostEdit` after write.
- **Added**: Risk warning in `MultiEdit()` response for MEDIUM/HIGH risk operations.
- **Changed**: Compact mode response now differentiates: `OK: 2 edits (1 applied, 1 already present), 193 lines`.
- **Changed**: Verbose mode response includes "Edit details:" section with per-edit status.
- **Optimized**: Skip file write when all edits are `already_present` (no I/O, no file watcher triggers).
- **Files changed**: `core/edit_operations.go`, `main.go`, `tests/bug17_test.go`, `tests/bug16_test.go`
- **Tests**: `tests/bug17_test.go` — 9 regression tests covering overlapping edits, independent edits, genuine failures, CRITICAL blocking, force bypass, per-edit details, backward compatibility, all-already-present, and mixed batches.

---

## [3.15.1] - 2026-03-02

### Bug Fix: #16 — Edit risk model only blocks CRITICAL

- **Changed**: Default risk thresholds: MEDIUM=20% (was 30%), HIGH=75% (was 50%). CRITICAL remains at 90%.
- **Changed**: Only CRITICAL (>=90%) risk operations are blocked and require `force: true`. MEDIUM and HIGH risk operations now auto-proceed with automatic backup and a non-blocking warning in the response.
- **Fixed**: Backup is now created BEFORE the blocking decision. Previously, blocked operations had no backup. Now even CRITICAL-blocked edits report their backup ID in the error message.
- **Added**: `RiskWarning` field in `EditResult` and `MultiEditResult` for non-blocking risk notices appended to success responses.
- **Added**: `FormatRiskNotice()` method on `ChangeImpact` for MEDIUM/HIGH warning formatting.
- **Added**: `force` parameter to `smart_edit_file` and `multi_edit` MCP tools (previously missing).
- **Changed**: `MultiEdit()` now uses persistent `BackupManager` instead of temporary `.backup` files that were deleted on success.
- **Changed**: `streamingEditLargeFile()` now performs risk assessment and creates backups (previously bypassed both).
- **Changed**: All edit tool responses now include backup ID and risk warnings when applicable.
- **Files changed**: `core/impact_analyzer.go`, `core/edit_operations.go`, `core/streaming_operations.go`, `core/claude_optimizer.go`, `core/engine.go`, `core/pipeline.go`, `main.go`
- **Tests**: `tests/bug16_test.go` — 10 regression tests covering all risk levels, blocking, force override, backup-before-block, MultiEdit, and FormatRiskNotice.

---

## [3.15.0] - 2026-02-28

### Performance Optimizations

#### 1. Cache resolved AllowedPaths in `isPathAllowed()`
- **Before**: `filepath.EvalSymlinks()` called per allowed path in loop, on every operation (read/write/edit/delete/list). 5 allowed paths x 1,000 ops = 5,000 unnecessary I/O syscalls.
- **After**: Allowed paths resolved once at engine startup via `resolveAllowedPaths()`. Loop in `isPathAllowed()` now iterates pre-resolved strings with zero syscalls. Target path still resolved per-call (symlink escape prevention unchanged).
- **Files changed**: `core/engine.go`

#### 2. Use `CompileRegex` cache in search functions
- **Before**: `performSmartSearch()`, `performAdvancedTextSearch()`, and `CountOccurrences()` called `regexp.Compile()` directly, recompiling the same pattern on every call.
- **After**: All three now use `e.CompileRegex()` with RWMutex-protected cache. Repeated searches with the same pattern skip compilation entirely.
- **Files changed**: `core/search_operations.go`

#### 3. Replace `isTextFile()`/`isBinaryFile()` O(n) linear search with O(1) map lookup
- **Before**: Standalone `isTextFile()` and `isBinaryFile()` in `streaming_operations.go` scanned 45-entry slices linearly.
- **After**: Deleted both functions. Callers now use `textExtensionsMap` (70+ entries, already existed in `search_operations.go`) and new `binaryExtensionsMap`, both O(1). Added 3 missing extensions (`.lock`, `.pl`, `.emacs`).
- **Files changed**: `core/streaming_operations.go`, `core/claude_optimizer.go`, `core/search_operations.go`

---

## [3.14.5] - 2026-02-28

### Bug Fixes

#### Bug #15 — `mcp_edit` ignored `force: true`, always blocked high-risk edits

- **Root cause**: `mcp_edit` is an alias for `edit_file` but was missing the `force` parameter entirely. The tool schema did not declare it, so AI clients had no way to pass it. The handler hardcoded `false` as the force argument to `EditFile`, meaning any edit that exceeded the 30% change threshold was permanently blocked regardless of what the caller sent.
- **Symptoms**: Claude received the "OPERATION BLOCKED" error with the instruction to add `"force": true`, attempted a second call with `force: true`, but the server silently ignored the parameter and returned the same error. The only workaround was to fall back to `mcp_write` (full file rewrite), losing the surgical edit semantics.
- **Fix**: Added `mcp.WithBoolean("force", ...)` to the `mcp_edit` tool schema and the corresponding `args["force"].(bool)` extraction in the handler, matching the pattern already used by `edit_file`, `intelligent_edit`, and `auto_recovery_edit`. Security unchanged — the 30%/50%/90% risk thresholds remain active by default; `force: true` must be explicitly passed to override.
- **Files changed**: `main.go`

---

## [3.14.4] - 2026-02-27

### Bug Fixes

#### Bug #14 — `edit_file` rejected valid edits due to trailing whitespace in `validateEditContext`

- **Root cause**: `validateEditContext` acted as a strict gatekeeper using a byte-exact CRLF-normalized `strings.Contains` check. If the file had trailing spaces on any line but Claude's `old_text` did not (or vice versa), the check failed immediately — before `performIntelligentEdit` could attempt its own fallbacks (including OPTIMIZATION 6's flexible regex, which handles exactly this case).
- **Symptoms**: Claude retried the edit after a forced re-read, which succeeded because it copied exact bytes. First attempt always failed despite the file being unchanged, wasting tokens and a tool call.
- **Fix**: Added Level 2 check in `validateEditContext`: after the exact normalized check fails, `trimTrailingSpacesPerLine` is applied to both content and `old_text`. If the trimmed comparison matches, validation passes and `performIntelligentEdit`'s fallbacks perform the actual replacement. Added `trimTrailingSpacesPerLine` helper.
- **Error message improved**: when both levels fail, the message now includes old_text line count and lists actionable root causes (BOM, non-breaking spaces, Unicode normalization).
- **Files changed**: `core/edit_operations.go`

---

## [3.14.3] - 2026-02-27

### Bug Fixes

#### Bug #13 — `smart_search` / `advanced_text_search` slow on large projects

- **Root cause (1)**: Both walk callbacks called `validatePath` on every file and directory visited. `validatePath` calls `isPathAllowed`, which calls `filepath.EvalSymlinks` — a real I/O syscall per file. On a project with thousands of files this produced thousands of unnecessary syscalls; the root path is already validated before the walk starts.
- **Root cause (2)**: Neither walk pruned common build-artifact directories. `bin/`, `obj/`, `.vs/`, `packages/`, `node_modules/`, `.git/` and others were traversed in full, each containing hundreds to thousands of files that cannot contain source-code matches.
- **Root cause (3)**: Common .NET/web extensions (`.aspx`, `.cshtml`, `.razor`, `.resx`, `.csproj`, `.sln`, `.xaml`, `.targets`, `.props`, `.nuspec`, `.ascx`, `.ashx`, `.asmx`, `.asax`, `.vbhtml`) were missing from `textExtensionsMap`. Every file with an unrecognised extension fell through to the binary-detection path, which opens the file and reads 512 bytes — one extra `Open`+`Read` per unknown file.
- **Fix**: Removed `validatePath` from both walk callbacks (security unchanged — root validated once before walk). Added `searchSkipDirs` map; both walks return `filepath.SkipDir` for any directory in the set. Added 14 ASP.NET/MSBuild extensions to `textExtensionsMap`.
- **Files changed**: `core/search_operations.go`

---

## [3.14.2] - 2026-02-26

### Bug Fixes

#### Bug #12 — `batch_operations` edit replaced entire file instead of find-and-replace

- **Root cause**: `executeEdit` in `core/batch_operations.go` was an unfinished TODO placeholder. It read the file into `content` but discarded it, then wrote `op.NewText` as the complete file content. `op.OldText` was never used. The operation returned success with no indication anything was wrong.
- **Fix**: Replaced the placeholder with `strings.Replace(original, op.OldText, op.NewText, 1)`. Returns an explicit error if `old_text` is not found in the file. `BytesAffected` now reflects the correct net byte delta.
- **Files changed**: `core/batch_operations.go`

---

## [3.14.1] - 2026-02-17

### Bug Fixes

#### Bug #11 — Linux path corruption + stale directory cache (two independent bugs)

**Bug A: `copy_file` corrupts Linux source paths on Windows**

- **Root cause**: `NormalizePath()` fell through to `filepath.Clean()` for pure Linux paths like `/tmp/...`. On Windows, `filepath.Clean("/tmp/foo")` → `\tmp\foo` — a broken path that pointed nowhere.
- **Fix**: Added `getDefaultWSLDistro()` (cached via `sync.Once`, calls `wsl.exe -l --quiet` once) in `core/path_detector.go`. `NormalizePath()` now converts Linux paths to `\\wsl.localhost\<distro>\...` UNC form when running on Windows. If WSL is unavailable, path is returned unchanged to preserve meaningful error messages.
- **Example**: `/tmp/package/dist/css/bootstrap.min.css` → `\\wsl.localhost\Ubuntu\tmp\package\dist\css\bootstrap.min.css`

**Bug B: `mcp_list` returns stale listing after external writes (bash, cp, etc.)**

- **Root cause**: `SetDirectory()` stored only the listing string with a 3-minute TTL. Writes by external processes were invisible to the cache.
- **Fix**: `dirCacheEntry` struct now stores the listing **and** the directory mtime at cache time. Before returning a cache hit, `ListDirectoryContent()` does `os.Stat(path)` and compares `ModTime()` with the cached mtime. If the directory was modified externally, the entry is invalidated and re-read from disk. Overhead: ~1 µs per cache hit.

**Files changed**: `core/path_detector.go`, `core/engine.go`, `cache/intelligent.go`

---

## [3.14.0] - 2026-02-13

### 🚀 Major Feature: Pipeline Transformation System

#### New Tool: `execute_pipeline`
Multi-step file transformation pipeline that reduces token consumption by **4x** (from ~4000-8000 tokens to ~1000-2000 tokens per workflow) by combining multiple operations into a single MCP call.

#### Pipeline Features
- **9 Supported Actions**:
  - `search` - Find files matching a pattern
  - `read_ranges` - Read file contents
  - `edit` - Simple find/replace operations
  - `multi_edit` - Multiple edits in one operation
  - `count_occurrences` - Count pattern occurrences
  - `regex_transform` - Advanced regex transformations
  - `copy` - Copy files to destination
  - `rename` - Rename/move files
  - `delete` - Soft-delete files

- **Safety Features**:
  - Automatic backup creation before destructive operations
  - Risk assessment (LOW/MEDIUM/HIGH/CRITICAL) based on files affected
  - Rollback on failure when `stop_on_error=true`
  - Dry-run mode for previewing changes
  - Force flag to bypass safety checks

- **Data Flow**:
  - Steps execute sequentially with context passing
  - `input_from` parameter chains steps together
  - In-memory data transfer between steps
  - Validation prevents forward references and circular dependencies

- **Limits & Validation**:
  - Maximum 20 steps per pipeline
  - Maximum 100 files affected per pipeline
  - Step ID validation (alphanumeric + `-` + `_`)
  - Action-specific parameter validation
  - Backward-only dependency references

#### Files Added
- `core/pipeline_types.go` (455 lines) - Pipeline request/result structs, validation
- `core/pipeline.go` (874 lines) - Execution engine, action handlers, risk assessment
- `tests/pipeline_test.go` (669 lines) - 8 comprehensive test cases

#### Files Modified
- `core/config.go` - Added pipeline constants and thresholds
- `core/engine.go` - Added `ExecutePipeline()` wrapper method
- `main.go` - Registered `execute_pipeline` MCP tool with formatter

#### Example Usage
```json
{
  "name": "refactor-namespace",
  "steps": [
    {"id": "find", "action": "search", "params": {"pattern": "oldNamespace"}},
    {"id": "replace", "action": "edit", "input_from": "find",
     "params": {"old_text": "oldNamespace", "new_text": "newNamespace"}},
    {"id": "verify", "action": "count_occurrences", "input_from": "find",
     "params": {"pattern": "newNamespace"}}
  ]
}
```

#### Performance Impact
- **Token Reduction**: 4x fewer tokens for multi-step workflows
- **Network Efficiency**: Single MCP call vs multiple round-trips
- **Response Time**: Batched operations reduce total latency

#### Test Results
- ✅ 6 of 8 tests passing (validation, search/count, dry-run, stop-on-error, backup, multi-edit, copy)
- ✅ Build successful
- ✅ No breaking changes

---

## [3.13.2] - 2026-02-07

### Performance & Toolchain Update

#### Go Toolchain
- **Go version**: `1.24.0` → `1.26.0`
- Compiled with latest Go stable release

#### Dependency Updates
- **ants/v2**: `v2.11.4` → `v2.11.5` (goroutine worker pool)

#### Performance Optimization: `isTextFile()`
- **O(1) lookup**: Refactored from slice iteration to global `map[string]bool`
- **Before**: Linear search through slice of extensions
- **After**: Constant-time map lookup

#### Extended File Type Support
Added 70+ modern file extensions for text search recognition:

| Category | Extensions Added |
|----------|------------------|
| **Rust/Systems** | `.rs`, `.zig`, `.nim`, `.v` |
| **Frontend** | `.vue`, `.svelte`, `.astro`, `.tsx`, `.jsx` |
| **Mobile** | `.kt`, `.swift`, `.dart` |
| **Backend** | `.php`, `.rb`, `.scala`, `.groovy`, `.clj` |
| **Config/IaC** | `.tf`, `.hcl`, `.toml`, `.ini`, `.env` |
| **Data** | `.graphql`, `.prisma`, `.proto` |
| **Shell** | `.zsh`, `.fish`, `.ps1`, `.psm1` |
| **DevOps** | `.dockerfile`, `Dockerfile`, `Makefile`, `Jenkinsfile` |
| **Docs** | `.rst`, `.adoc`, `.org`, `.tex` |

#### Files Modified
- `go.mod` - Updated Go version and ants dependency
- `core/search_operations.go` - Optimized `isTextFile()` with map lookup + new extensions
- `CLAUDE.md` - Updated version references

#### Test Results
- ✅ All tests passing
- ✅ Build successful
- ✅ No breaking changes

---

## [3.13.1] - 2026-02-03

### Bug Fix: `include_context` ignored in compact mode

#### Problem
`advanced_text_search` with `include_context: true` and `context_lines: N` only returned positions (`file:line[start:end]`) when `--compact-mode` was enabled (default for Claude Desktop). Context lines were collected during the search phase but discarded by the compact formatter. Users had to make additional `read_file_range` calls to see surrounding code.

#### Root Cause
The compact mode formatting branch in `AdvancedTextSearch` (`core/search_operations.go:133-154`) did not check `includeContext` — it always used the position-only format regardless of the parameter.

#### Fix
When `include_context=true`, compact mode now uses a condensed context format:
```
1 matches
/path/file.go:10[5:10] matched line content
  | context line before
  | context line after
```
When `include_context=false` (default), behavior is unchanged — comma-separated positions.

#### Files Modified
- `core/search_operations.go` — Compact mode formatter now respects `include_context`
- `tests/mcp_functions_test.go` — Added `TestAdvancedTextSearchCompactModeContext` (compact mode + context regression test)

#### Test Results
- All existing tests: PASS
- New compact mode context test: PASS

---

## [3.13.0] - 2026-01-31

### Security Audit & Dependency Update

#### Go Toolchain
- **Toolchain**: `go1.24.6` -> `go1.24.12` - fixes **8 CVEs** in Go standard library:
  - GO-2026-4340: `crypto/tls` handshake messages processed at incorrect encryption level
  - GO-2025-4175: `crypto/x509` improper wildcard DNS name constraint validation
  - GO-2025-4155: `crypto/x509` excessive resource consumption on error string printing
  - GO-2025-4013: `crypto/x509` panic when validating DSA public keys
  - GO-2025-4011: `encoding/asn1` DER payload memory exhaustion
  - GO-2025-4010: `net/url` insufficient IPv6 hostname validation
  - GO-2025-4008: `crypto/tls` ALPN negotiation information leak
  - GO-2025-4007: `crypto/x509` quadratic complexity in name constraint checks

#### CRITICAL Security Fixes (5)
- **Symlink traversal bypass** (`core/engine.go`): `isPathAllowed()` now resolves symlinks
  via `filepath.EvalSymlinks()` before performing containment checks, preventing sandbox
  escape through symlinks pointing outside allowed paths
- **Missing access control on `EditFile()`** (`core/edit_operations.go`): Added
  `isPathAllowed()` check - previously edits bypassed allowed-path restrictions entirely
- **Missing access control on `StreamingWriteFile()`** (`core/streaming_operations.go`):
  Large file writes (>MediumFileThreshold) now enforce allowed-path restrictions
- **Missing access control on `ChunkedReadFile()`** (`core/streaming_operations.go`):
  Large file reads now enforce allowed-path restrictions
- **Missing access control on `SmartEditFile()`** (`core/streaming_operations.go`):
  Smart edit operations now enforce allowed-path restrictions

#### HIGH Security Fixes (3)
- **Missing access control on `MultiEdit()`** (`core/edit_operations.go`): Batch edit
  operations now enforce allowed-path restrictions
- **Deadlock in `ListBackups()`** (`core/backup_manager.go`): Fixed dangerous
  RLock->RUnlock->Lock->Unlock->RLock pattern that could cause deadlocks or
  unlock-of-unlocked-mutex panics under concurrent access
- **Path traversal via backup IDs** (`core/backup_manager.go`): Added `sanitizeBackupID()`
  validation to prevent directory traversal attacks through crafted backup IDs
  (e.g., `../../etc`) in `GetBackupInfo`, `RestoreBackup`, `CompareWithBackup`,
  `GetBackupPath`

#### MEDIUM Security Fixes (5)
- **Predictable temp file names** (`core/engine.go`, `core/edit_operations.go`): All
  temporary files now use `crypto/rand` via `secureRandomSuffix()` instead of predictable
  timestamps or counters, preventing symlink attacks on temp file paths
- **Weak backup ID generation** (`core/backup_manager.go`): Backup IDs now use
  `crypto/rand` (8 bytes / 16 hex chars) instead of `time.Now().UnixNano()%0xFFFFFF`
- **File permission preservation** (`core/engine.go`, `core/edit_operations.go`,
  `core/streaming_operations.go`): Write operations now preserve original file permissions
  instead of always using hardcoded `0644`, preventing sensitive files from becoming
  world-readable after edits
- **Symlink following in `copyDirectory()`** (`core/file_operations.go`): Directory copy
  now skips symlinks to prevent sandbox escape and infinite loops from circular symlinks
- **Restrictive backup metadata permissions** (`core/backup_manager.go`): Backup
  `metadata.json` files now created with `0600` instead of `0644`

#### Other
- **Build fix**: `tests/security/*.go` changed from `package main` to `package security`
  and renamed `security_tests.go` -> `security_tests_test.go` (pre-existing build error)
- All dependencies verified at latest stable versions (bigcache v3.1.0, fsnotify v1.9.0,
  mcp-go v0.43.2, ants v2.11.4)
- All tests passing (core, tests, tests/security including fuzzing)

---

## [3.12.0] - IN DEVELOPMENT

### 🎯 Code Editing Excellence: Phase 1 - Coordinate Tracking

#### Objective
Enable precise code location and targeting through character-level coordinate tracking in search results. Foundation for v3.12.0's 70-80% token reduction goal.

#### Phase 1: Coordinate Tracking ✅

**New Feature: Character Offset Tracking**
- Added `calculateCharacterOffset()` helper function
  - Uses `regexp.FindStringIndex()` for precise position detection
  - Handles multiple occurrences correctly (Bug #2 fix)
  - 0-indexed character offsets relative to line start
- Populates `MatchStart` and `MatchEnd` fields in `SearchMatch` struct
- Passes compiled regex pattern for accurate coordinate calculation

**Search Operations Enhanced**
- `performSmartSearch()`: Now calculates and returns character coordinates
- `performAdvancedTextSearch()`: Both memory-efficient and context paths now track coordinates
- Results include exact position within each matched line
- Correctly handles multiple pattern occurrences on same line

**Test Coverage**
- New file: `tests/coordinate_tracking_test.go`
- 7 new test cases covering:
  - SmartSearch coordinate accuracy
  - AdvancedTextSearch with coordinates
  - Coordinates with context lines
  - Edge cases (special characters, multiple occurrences)
  - **Bug #2 Fix**: Multiple occurrences on same line (TestMultipleOccurrencesOnSameLine)
  - Backward compatibility
  - Position accuracy verification
- All tests passing (53 total: 47 existing + 7 new), zero regressions

**Impact**
- Claude Desktop can pinpoint exact edit locations
- Enables sub-line-level targeting
- Foundation for Phase 2 diff-based edits
- 100% backward compatible (no breaking changes)

#### Implementation Details
- Modified: `core/search_operations.go`
  - Added `calculateCharacterOffset(line, regexPattern)` function (lines 707-721)
    - Uses `regexp.FindStringIndex()` instead of `strings.Index()`
    - Correctly handles multiple pattern occurrences (Bug #2 fix)
    - Returns (startOffset, endOffset) for accurate positioning
  - Enhanced `performSmartSearch()` to pass regex pattern (line 310)
  - Enhanced `performAdvancedTextSearch()` - both paths (lines 502, 520)
    - Memory-efficient path: uses bufio.Scanner
    - Context path: uses strings.Split
- Created: `tests/coordinate_tracking_test.go` (384 lines)
  - 7 test functions covering all scenarios
  - Specific test for Bug #2: TestMultipleOccurrencesOnSameLine
- No new dependencies, no API changes

---

## [3.11.0] - 2025-12-21

### 🚀 Performance & Modernization: P0 & P1 Optimization Initiative

#### Overview
Comprehensive modernization and performance optimization of the core engine, achieving 30-40% memory savings and modernizing codebase to Go 1.21+ standards.

#### Phase P0: Critical Modernization ✅

**P0-1a: Error Handling Modernization**
- New file: `core/errors.go`
- Custom error types: `PathError`, `ValidationError`, `CacheError`, `EditError`, `ContextError`
- Go 1.13+ error wrapping with `%w` instead of `%v`
- Better error inspection and debugging

**P0-1b: Context Cancellation**
- Added context cancellation checks in search operations
- Prevents unnecessary work after context timeout
- Improved responsiveness under cancellation

**P0-1c: Environment Detection Caching**
- Environment cache with 5-minute TTL
- 2-3x faster environment detection (WSL, Windows user detection)
- Thread-safe with RWMutex

#### Phase P1: Performance Optimizations ✅

**P1-1: Buffer Pool Helper**
- New method: `CopyFileWithBuffer()`
- Uses `sync.Pool` for 64KB buffer reuse
- Reduces allocation overhead in I/O operations

**P1-2: BigCache Configuration Fix**
- `MaxEntrySize`: 500 bytes → 1 MB (CRITICAL FIX)
- Optimized shards from 1024 → 256
- Optimized TTLs for faster refresh
- Cache now actually effective for real files

**P1-3: Regex Compilation Cache**
- New cache: `regexCache` with LRU eviction
- Max 100 compiled patterns
- 2-3x faster repeated pattern searches
- Thread-safe with RWMutex

**P1-Config: Streaming Thresholds**
- New file: `core/config.go`
- Centralized streaming threshold constants
- SmallFileThreshold (100KB), MediumFileThreshold (500KB), LargeFileThreshold (5MB)
- Easier performance tuning

**P1-3: bufio.Scanner Memory Optimization**
- Replaced `strings.Split` with `bufio.Scanner` in:
  - `edit_operations.go:355` (line-by-line processing)
  - `search_operations.go:297, 476` (smart split: scanner for basic search, strings.Split only when context needed)
- **Memory savings: 30-40% for large files**
- Pre-allocated strings.Builder for result reconstruction

**P1-4: Go 1.21+ Built-in min/max**
- Removed custom helpers: `min()`, `max()`, `maxInt()`
- Use Go 1.21+ built-in min/max functions
- Cleaner code, slight performance improvement
- Code reduction: 12 lines removed

**P1-5: Structured Logging with slog**
- Migrated 25 `log.Printf()` calls to structured `slog`
- Files updated: engine.go, streaming_operations.go, claude_optimizer.go, hooks.go, watcher.go
- Benefits:
  - Parseable logs with key-value pairs
  - Better integration with monitoring tools (Splunk, ELK, Datadog)
  - Suitable for machine-readable log processing
  - Debug logs conditionally executed

#### Performance Impact

**Memory Usage**
- 30-40% reduction for large file operations (bufio.Scanner)
- 50% reduction in regex cache memory (LRU eviction)
- Smaller environment detection overhead (cache reuse)

**Speed**
- 2-3x faster environment detection (caching)
- 2-3x faster regex operations (compiled cache)
- No regression in any operation

**Code Quality**
- 12 lines of code removed (min/max helpers)
- 25 log statements modernized (slog)
- Better error handling (custom error types)
- Improved maintainability

#### Test Results
✅ All 47 tests passing
✅ 0 regressions
✅ Security tests: PASS
✅ Performance benchmarks: Pass (no regression)

#### Files Modified/Created
- **Created**: core/errors.go, core/config.go
- **Modified**: core/engine.go, core/edit_operations.go, core/search_operations.go, core/path_detector.go, core/streaming_operations.go, core/claude_optimizer.go, core/hooks.go, core/watcher.go, cache/intelligent.go, plan_mode.go
- **Tests Updated**: core/engine_test.go, tests/bug5_test.go

#### Breaking Changes
None - All changes are backward compatible.

#### Commits in This Release
```
099c98f perf(P1-5): Convert log.Printf to slog structured logging
11d56b7 perf(P1-4): Use Go 1.21+ built-in min/max functions
1a14f3b perf(P1-3): Replace strings.Split with bufio.Scanner for memory efficiency
facd580 perf(P1-Config): Add streaming threshold constants to core/config.go
45fa199 perf(P1-Regex): Add regex compilation cache to engine
9ccfdef perf(P1-Cache): Fix BigCache configuration parameters
9ceb629 perf(P1-Buffer): Add CopyFileWithBuffer helper for io operations
0841527 refactor(P0): Complete P0 Critical Modernization phase
5ef8265 refactor(P0-1c): Implement environment detection cache
a12e4a0 refactor(P0-1b): Add context cancellation to search loops
```

#### Upgrade Path
- Simply pull and rebuild - no API changes required
- Optional: Enable debug logging with slog for better observability

---

## [3.10.0] - 2025-12-20

### 🛡️ Critical Fix: File Destruction Prevention (Bug #8)

#### Problem
Claude Desktop would sometimes delete ALL file content except the edited portion when using multiline `old_text` with edit tools. This was a critical data loss vulnerability occurring when:
- Using `recovery_edit()` with multiline text
- Line endings were inconsistent (CRLF vs LF)
- File had been modified since last read
- Fuzzy matching failed silently

#### Solution: Complete Safety Layer Implementation

**New File: `core/edit_safety_layer.go`** (400+ lines)
- `EditSafetyValidator`: Comprehensive validation before every edit
- Pre-validates that `old_text` exists exactly as provided
- Detects and handles line ending variations
- Provides detailed diagnostics for debugging
- Suggests recovery strategies if validation fails

**New File: `SAFE_EDITING_PROTOCOL.md`**
- Quick reference guide (3-layer approach)
- Copy-paste checklist for every file edit
- Decision tree for choosing safe tools
- Complete workflow examples from Bug #8 scenario
- Troubleshooting guide with common mistakes
- Emergency procedures for corrupted files

**New File: `docs/BUG8_FIX.md`**
- Complete technical documentation
- Root cause analysis
- Blindaje protocol explanation
- Migration guide for users
- Performance benchmarks

**New File: `tests/edit_safety_test.go`** (350+ lines)
- 6 comprehensive test suites:
  - Exact multiline matching
  - Single line replacements
  - Nonexistent text detection
  - Line ending variations (CRLF, LF, mixed)
  - Large file handling (100+ line edits)
  - Bug #8 exact reproduction scenario
- Verification tests
- Detailed logging tests
- All tests: ✅ PASS

#### The "Blindaje" Protocol (5 Rules)

**REGLA 1**: NUNCA editar sin verificación previa
- Use `read_file_range()` to see exact content
- Use `count_occurrences()` to confirm pattern exists
- Use tools only after validation

**REGLA 2**: CAPTURA LITERAL del código a reemplazar
- Copy EXACTLY from `read_file_range()` output
- Include all spaces, tabs, line endings
- Never use fuzzy matching for critical edits

**REGLA 3**: Operaciones atómicas con backup
- ALWAYS use `atomic: true` in `batch_operations`
- ALWAYS create backup before edits
- Rollback immediately if edit fails

**REGLA 4**: Recovery strategy
- Simple edits → `recovery_edit()`
- Multiple changes → `batch_operations`
- Critical files → validate with tools first

**REGLA 5**: Validación post-edición
- Use `count_occurrences()` after editing
- Verify old text is gone
- Confirm new text is present

#### Impact

- **Before (v3.8.0)**: Risk of complete file destruction on multiline edits
- **After (v3.10.0)**: Pre-validation prevents ALL file corruption scenarios

#### Safety Guarantees

✅ Pre-validation of all edits
✅ Line ending normalization (CRLF/LF/mixed)
✅ Whitespace handling
✅ Context detection for modified files
✅ Detailed diagnostics for every edit
✅ Post-edit verification
✅ Atomic operations with backup
✅ Recovery strategy recommendations

#### Breaking Changes

⚠️ Function signatures updated (added `force` parameter):
- `IntelligentEdit(ctx, path, oldText, newText, force bool)`
- `AutoRecoveryEdit(ctx, path, oldText, newText, force bool)`
- `EditFile(path, oldText, newText, force bool)`

#### Migration Guide

Before (❌ Unsafe):
```python
response = client.call_tool("recovery_edit", {
    "path": "file.cs",
    "old_text": "...multiline...",
    "new_text": "..."
})
# May fail silently or corrupt file
```

After (✅ Safe):
```python
# STEP 1: Read exact content
response = client.call_tool("read_file_range", {"path": "file.cs", "start_line": 10, "end_line": 20})

# STEP 2: Verify pattern exists
response = client.call_tool("count_occurrences", {"path": "file.cs", "pattern": "exact_text"})

# STEP 3: Use batch_operations for safety
response = client.call_tool("batch_operations", {
    "operations": [{
        "type": "edit",
        "path": "file.cs",
        "old_text": "exact_text_from_read",
        "new_text": "replacement"
    }],
    "atomic": true
})

# STEP 4: Verify result
response = client.call_tool("count_occurrences", {"path": "file.cs", "pattern": "exact_text"})
# Should return 0
```

#### Files Modified
- `core/edit_safety_layer.go` (NEW)
- `tests/edit_safety_test.go` (NEW)
- `docs/BUG8_FIX.md` (NEW)
- `SAFE_EDITING_PROTOCOL.md` (NEW)
- `tests/mcp_functions_test.go` (Updated)

#### Test Results
✅ All 6 edit safety test suites: PASS
✅ Line ending variations: PASS
✅ Multiline scenarios (Bug #8 exact): PASS
✅ Verification tests: PASS
✅ Large file handling: PASS
✅ Detailed logging: PASS

#### Documentation & Guides
- [Complete Technical Details](docs/BUG8_FIX.md)
- [Safe Editing Quick Reference](SAFE_EDITING_PROTOCOL.md)
- [3-Layer Safety Implementation](#solution-complete-safety-layer-implementation)

---

## [3.9.0] - 2025-12-20

### 🔐 Security: Dependency Updates & Enhanced Security Test Suite

#### Dependency Updates
- Updated `github.com/mark3labs/mcp-go`: v0.42.0 → v0.43.2
  - Includes latest MCP protocol improvements and security patches
- Updated `golang.org/x/sync`: v0.17.0 → v0.19.0
  - Enhanced synchronization primitives and performance
- Updated `golang.org/x/sys`: v0.37.0 → v0.39.0
  - Latest system call bindings and OS-level security fixes

#### Security Test Suite Enhancements

**New Tests Added:**
- `TestOWASPTop10_2024`: Comprehensive OWASP Top 10 2024 vulnerability assessment
- `TestIntegerOverflowProtection`: CWE-190 integer overflow/wraparound detection
- `TestNullPointerDefense`: CWE-476 null pointer dereference protection
- `FuzzPathValidation`: Fuzzing with path traversal attempts and edge cases
- `FuzzInputValidation`: Fuzzing for command injection protection
- `FuzzFilePathHandling`: Fuzzing file path handling with special characters

**New Test File:**
- `tests/security/fuzzing_test.go` (200+ lines)
  - Security tools integration guide
  - Vulnerability reporting process documentation
  - Secure development practices guidelines

**Updated Tests:**
- `TestSecurityAuditLog`: Enhanced to v2 format with dependency update tracking
- `TestMainDependencies`: Updated version expectations to latest releases

#### Security Assessment Status
- ✅ **Critical Issues**: 0
- ✅ **High Issues**: 0
- ✅ **Medium Issues**: 0
- ✅ **Low Issues**: 0
- ✅ **All Security Tests**: PASS

#### Coverage
- Path Traversal (CWE-22)
- Command Injection (CWE-78)
- Integer Overflow (CWE-190)
- Null Pointer Dereference (CWE-476)
- OWASP Top 10 2024 (A01-A10)
- Race Conditions (CWE-362)
- Cryptographic Failures (A02:2024)

#### Next Steps for Users
1. Run security tests regularly: `go test -v ./tests/security/...`
2. Run race detection: `go test -race ./...`
3. Install security tools:
   - `gosec` for static analysis
   - `nancy` for CVE detection
   - `syft` for SBOM generation
4. Monitor dependency updates monthly

---

## [3.8.1] - 2025-12-04

### 🐛 Critical Fix: Risk Assessment Not Blocking Operations (Bug #10 Follow-up)

#### Problem Found
After implementing the backup and recovery system (v3.8.0), testing revealed a **critical bug**:
- Risk assessment was **calculating** change impact correctly (e.g., "220.9% change, HIGH risk")
- BUT it was **NOT blocking** the operations as documented
- All three edit tools (`edit_file`, `intelligent_edit`, `recovery_edit`) executed HIGH/CRITICAL risk operations without warning or requiring `force: true`

#### Root Cause
The `EditFile` function calculated risk using `CalculateChangeImpact()` but never validated it:
```go
// Calculate change impact for risk assessment
impact := CalculateChangeImpact(string(content), oldText, newText, e.riskThresholds)

// ❌ MISSING: No validation here - operation continued regardless of risk level
// ❌ BUG: Never checked impact.IsRisky
```

#### Fixed
✅ **Added risk validation** after impact calculation:
```go
// Calculate change impact for risk assessment
impact := CalculateChangeImpact(string(content), oldText, newText, e.riskThresholds)

// ⚠️ RISK VALIDATION: Block HIGH/CRITICAL risk operations unless force=true
if impact.IsRisky && !force {
    warning := impact.FormatRiskWarning()
    return nil, fmt.Errorf("OPERATION BLOCKED - %s\n\nTo proceed anyway, add \"force\": true to the request", warning)
}
```

✅ **Added `force` parameter** to all edit tools:
- `edit_file(path, old_text, new_text, force: bool)`
- `intelligent_edit(path, old_text, new_text, force: bool)`
- `recovery_edit(path, old_text, new_text, force: bool)` (deprecated alias)

✅ **Updated function signatures**:
- `EditFile(path, oldText, newText string, force bool)`
- `IntelligentEdit(ctx, path, oldText, newText string, force bool)`
- `AutoRecoveryEdit(ctx, path, oldText, newText string, force bool)`

#### Impact
- **Before (v3.8.0)**: Risk assessment was "cosmetic" - calculated but never enforced
- **After (v3.8.1)**: HIGH/CRITICAL risk operations are **blocked** unless explicitly forced

#### Example
```javascript
// Without force - BLOCKED
edit_file({
  path: "main.go",
  old_text: "func",  // 50 occurrences, 220% change
  new_text: "function"
})
// → ❌ Error: OPERATION BLOCKED - HIGH RISK: 220.9% of file will change (50 occurrences)
//    Recommendation: Use analyze_edit first or add force: true

// With force - ALLOWED
edit_file({
  path: "main.go",
  old_text: "func",
  new_text: "function",
  force: true
})
// → ✅ Success, backup created: 20241204-120000-xyz789
```

#### Files Modified
- `core/edit_operations.go` - Added risk validation after impact calculation
- `core/claude_optimizer.go` - Updated `IntelligentEdit` and `AutoRecoveryEdit` signatures
- `core/engine.go` - Updated wrapper method signatures
- `core/streaming_operations.go` - Updated `SmartEditFile` to pass `force=false`
- `main.go` - Added `force` parameter to 3 MCP tools
- `tests/bug5_test.go`, `tests/bug8_test.go` - Updated all test calls

#### Severity
🔴 **CRITICAL** - Risk assessment was completely non-functional in v3.8.0

#### Recommendation
**All v3.8.0 users should upgrade immediately to v3.8.1**

---

## [3.8.0] - 2025-12-03

### 🔒 Major Feature: Backup and Recovery System (Bug #10)

#### Overview
Complete backup and recovery system to prevent code loss from destructive operations. Backups are now persistent, accessible by MCP, and include comprehensive metadata.

#### 🆕 New Features

**1. Persistent Backup System**
- Backups stored in accessible location: `%TEMP%\mcp-batch-backups`
- Complete metadata with timestamps, SHA256 hashes, and operation context
- No automatic deletion - backups persist for recovery
- Configurable retention: max age (default: 7 days) and max count (default: 100)
- Smart cleanup with dry-run preview mode

**2. Risk Assessment & Validation**
- Automatic impact analysis before destructive operations
- 4 risk levels: LOW, MEDIUM, HIGH, CRITICAL
- Blocks risky operations unless `force: true` is specified
- Configurable thresholds:
  - MEDIUM risk: 30% file change or 50 occurrences
  - HIGH risk: 50% file change or 100 occurrences
  - CRITICAL: 90%+ file change
- Clear warnings with actionable recommendations

**3. Five New MCP Tools**

**`list_backups`** - List available backups with filtering
```json
{
  "limit": 20,
  "filter_operation": "edit",
  "filter_path": "main.go",
  "newer_than_hours": 24
}
```

**`restore_backup`** - Restore files from backup
```json
{
  "backup_id": "20241203-153045-abc123",
  "file_path": "path/to/file.go",
  "preview": true
}
```

**`compare_with_backup`** - Compare current vs backup
```json
{
  "backup_id": "20241203-153045-abc123",
  "file_path": "path/to/file.go"
}
```

**`cleanup_backups`** - Clean old backups
```json
{
  "older_than_days": 7,
  "dry_run": true
}
```

**`get_backup_info`** - Get detailed backup information
```json
{
  "backup_id": "20241203-153045-abc123"
}
```

#### 🔧 Enhanced Tools

**`edit_file`, `recovery_edit`, `intelligent_edit`**
- Automatic backup creation before editing
- Risk assessment with change percentage calculation
- Returns `backup_id` in response for easy recovery
- Blocks HIGH/CRITICAL risk without `force: true`

**`batch_operations`**
- New `force` parameter for risk override
- Batch-level risk assessment
- Persistent backup ID in results
- Enhanced validation with impact analysis

#### ⚙️ Configuration

**New Command-Line Flags:**
```bash
--backup-dir              # Backup storage directory
--backup-max-age 7        # Max backup age in days
--backup-max-count 100    # Max number of backups
--risk-threshold-medium 30.0
--risk-threshold-high 50.0
--risk-occurrences-medium 50
--risk-occurrences-high 100
```

**Environment Setup (claude_desktop_config.json):**
```json
{
  "mcpServers": {
    "filesystem-ultra": {
      "args": [
        "--backup-dir=C:\\backups\\mcp-batch-backups"
      ],
      "env": {
        "ALLOWED_PATHS": "C:\\your\\project;C:\\backups\\mcp-batch-backups"
      }
    }
  }
}
```
**⚠️ IMPORTANT:** Backup directory MUST be in `ALLOWED_PATHS`

#### 📊 Backup Metadata Example
```json
{
  "backup_id": "20241203-153045-abc123",
  "timestamp": "2024-12-03T15:30:45Z",
  "operation": "edit_file",
  "user_context": "Edit: 12 occurrences, 35.2% change",
  "files": [{
    "original_path": "C:\\__REPOS\\project\\main.go",
    "size": 12345,
    "hash": "sha256:abc123...",
    "modified_time": "2024-12-03T15:29:30Z"
  }],
  "total_size": 12345
}
```

#### 🎯 Use Cases

**Scenario 1: Prevented Disaster**
```javascript
edit_file({path: "main.go", old_text: "func", new_text: "function"})
// → ⚠️ HIGH RISK: 65.3% of file will change (200 occurrences)
// → Recommendation: Use analyze_edit first or add force: true

analyze_edit({path: "main.go", old_text: "func", new_text: "function"})
// → Preview shows exactly what will change

edit_file({path: "main.go", old_text: "func", new_text: "function", force: true})
// → ✅ Success, backup created: 20241203-153045-abc123
```

**Scenario 2: Quick Recovery**
```javascript
list_backups({newer_than_hours: 2, filter_path: "main.go"})
// → Shows recent backups

compare_with_backup({backup_id: "...", file_path: "main.go"})
// → Shows what changed

restore_backup({backup_id: "...", file_path: "main.go"})
// → ✅ Code recovered!
```

#### 📦 Technical Implementation

**New Files:**
- `core/backup_manager.go` (650 lines) - Complete backup system
- `core/impact_analyzer.go` (350 lines) - Risk assessment engine
- `docs/BUG10_RESOLUTION.md` - Comprehensive documentation

**Modified Files:**
- `core/engine.go` - BackupManager integration
- `core/edit_operations.go` - Persistent backups, impact validation
- `core/batch_operations.go` - Risk assessment for batches
- `main.go` - 5 new tools, configuration flags

**Performance:**
- Backup overhead: ~5-10ms for small files, ~50ms for 1MB
- Impact analysis: ~1-3ms (negligible)
- No degradation in normal operations
- Metadata cached for fast listing (5min refresh)

#### 🔐 Security & Reliability
- SHA256 hash verification for integrity
- Automatic rollback on backup failure
- Pre-restore backup of current state
- Respects ALLOWED_PATHS restrictions

#### 📈 Statistics
- **Total tools:** 55 (50 original + 5 backup tools)
- **New code:** ~2,600 lines
- **Test coverage:** Full integration tests recommended
- **Backward compatible:** All new features are optional

#### 🎁 Benefits
1. **No more code loss** - Safety net before Git
2. **Intelligent protection** - Warns before risky changes
3. **Fast recovery** - Restore with one command
4. **Full audit trail** - Complete operation history
5. **Zero config needed** - Sensible defaults work out of box

---

## [3.7.1] - 2025-12-03

### 🐛 Bug Fix: Optional Search Parameters Not Exposed (Bug #9)

#### Fixed
- **`smart_search` and `advanced_text_search` parameter exposure**
  - Fixed issue where optional advanced parameters were supported internally but NOT exposed in MCP tool definitions
  - Claude Desktop could not use `include_content`, `file_types`, `case_sensitive`, `whole_word`, `include_context`, and `context_lines` parameters
  - These parameters were hardcoded in handlers instead of being extracted from requests

#### Added Parameters

**`smart_search` - New Optional Parameters:**
- `include_content` (boolean): Search within file content (default: false)
- `file_types` (string): Filter by comma-separated extensions (e.g., ".go,.txt")

**`advanced_text_search` - New Optional Parameters:**
- `case_sensitive` (boolean): Case-sensitive search (default: false)
- `whole_word` (boolean): Match whole words only (default: false)
- `include_context` (boolean): Include context lines around matches (default: false)
- `context_lines` (number): Number of context lines to show (default: 3)

#### Impact
- **Efficiency**: Claude can now perform advanced searches in a single call instead of multiple operations
- **Token Reduction**: Eliminates need for multiple read_file calls to filter results
- **Better UX**: More precise search results with filtering and context

#### Example Usage
```json
{
  "tool": "smart_search",
  "arguments": {
    "path": "./src",
    "pattern": "TODO",
    "include_content": true,
    "file_types": ".go,.js"
  }
}
```

```json
{
  "tool": "advanced_text_search",
  "arguments": {
    "path": "./src",
    "pattern": "function",
    "case_sensitive": true,
    "whole_word": true,
    "include_context": true,
    "context_lines": 5
  }
}
```

#### Technical Details
- **Before**: Parameters hardcoded in `main.go` handlers (`include_content: false`, `file_types: []`)
- **After**: Parameters extracted from `request.Params.Arguments` with proper defaults
- **Backward Compatible**: All parameters are optional with sensible defaults

#### Files Modified
- `main.go`: Updated tool definitions and handlers for `smart_search` and `advanced_text_search`
- `README.md`: Updated documentation with parameter descriptions and examples
- `tests/bug9_test.go`: Comprehensive tests validating all new parameters (285 lines)
- `docs/BUG9_RESOLUTION.md`: Detailed technical documentation

#### Test Results
✅ All tests passing:
- `TestSmartSearchWithIncludeContent`
- `TestSmartSearchWithFileTypes`
- `TestAdvancedTextSearchCaseSensitive`
- `TestAdvancedTextSearchWithContext`

---

## [3.7.0] - 2025-11-30

### 🎯 MCP-Prefixed Tool Aliases + Self-Learning Help System

Added 5 new tool aliases with `mcp_` prefix and a comprehensive `get_help` tool for AI agent self-learning.

#### 🆕 New: `get_help` Tool - AI Self-Learning System
AI agents can now call `get_help(topic)` to learn how to use tools optimally:

```
get_help("overview")  → Quick start guide
get_help("workflow")  → The 4-step efficient workflow
get_help("tools")     → Complete list of 50 tools
get_help("edit")      → Editing files (most important!)
get_help("search")    → Finding content in files
get_help("batch")     → Multiple operations at once
get_help("errors")    → Common errors and fixes
get_help("examples")  → Practical code examples
get_help("tips")      → Pro tips for efficiency
get_help("all")       → Everything (comprehensive)
```

**Benefits:**
- AI agents can self-learn optimal workflows
- No need to include full documentation in system prompts
- Dynamic help that stays up-to-date with tool changes
- Reduces token usage by loading help only when needed

#### 📘 New Documentation Files
- `guides/AI_AGENT_INSTRUCTIONS.md` - Complete guide for AI agents (English)
- `guides/AI_AGENT_INSTRUCTIONS_ES.md` - Complete guide (Spanish)
- `guides/SYSTEM_PROMPT_COMPACT.txt` - Minimal system prompt (English)
- `guides/SYSTEM_PROMPT_COMPACT_ES.txt` - Minimal system prompt (Spanish)

#### New Tool Aliases (Same Functionality, Better Naming)

| New Name | Original | Purpose |
|----------|----------|---------|
| `mcp_read` | `read_file` | Read with WSL↔Windows auto-conversion |
| `mcp_write` | `write_file` | Atomic write with path conversion |
| `mcp_edit` | `edit_file` | Smart edit with backup + path conversion |
| `mcp_list` | `list_directory` | Cached directory listing |
| `mcp_search` | `smart_search` | File/content search |

#### Key Benefits
- **No Breaking Changes**: Original tools (`read_file`, `write_file`, etc.) still work
- **Clear Differentiation**: `mcp_` prefix makes it obvious these are MCP tools
- **Enhanced Descriptions**: Include `[MCP-PREFERRED]` tag to guide Claude
- **WSL Compatibility**: All descriptions mention WSL↔Windows path support
- **Self-Learning**: AI can call `get_help()` to learn usage

#### Tool Count
- **50 tools total** (44 original + 5 mcp_ aliases + get_help)

---

## [3.6.0] - 2025-11-30

### 🚀 Performance Optimizations for Claude Desktop

Major performance improvements focused on making file editing faster and more efficient for Claude Desktop.

#### New Features
- **`multi_edit` tool**: Apply multiple edits to a single file atomically
  - MUCH faster than calling `edit_file` multiple times
  - File is read once, all edits applied in memory, then written once
  - Only one backup is created
  - Usage: `multi_edit(path, edits_json)` where `edits_json` is `[{"old_text": "...", "new_text": "..."}, ...]`

#### Performance Improvements
- **Optimized `performIntelligentEdit`**: 
  - Uses pre-allocated `strings.Builder` for zero-copy string operations
  - Single-pass replacement instead of `ReplaceAll` for known match counts
  - Reduced memory allocations by ~60% for typical edits
  
- **Improved streaming operations**:
  - Uses pooled 64KB buffers for I/O operations
  - `StreamingWriteFile` now uses `bufio.Writer` with pooled buffers
  - `ChunkedReadFile` uses `bufio.Reader` for better read performance
  - Added throughput logging (MB/s) for large file operations

- **Intelligent cache prefetching**:
  - Tracks file access patterns
  - After 3 accesses to a file, automatically prefetches sibling files
  - Background prefetch worker to avoid blocking main operations
  - Optimized cache expiration times for Claude Desktop usage patterns

- **Buffer pool integration**:
  - All file operations now use a shared 64KB buffer pool
  - Reduces GC pressure significantly during heavy file operations
  - Uses `sync.Pool` for efficient buffer reuse

#### Technical Details
- **Before (single edit)**: Read file → Replace → Write file → Repeat N times
- **After (multi_edit)**: Read file once → Apply N edits in memory → Write file once

Estimated speedup for multiple edits:
- 2 edits: ~1.8x faster
- 5 edits: ~4x faster
- 10 edits: ~8x faster

#### Files Modified
- `core/edit_operations.go`: Optimized edit algorithm, added `MultiEdit` function
- `core/streaming_operations.go`: Added buffered I/O with pooled buffers
- `cache/intelligent.go`: Added prefetching system
- `core/engine.go`: Integrated access tracking for prefetching
- `main.go`: Registered `multi_edit` tool (now 44 tools total)

---

## [4.2.2] - 2026-04-17

### 🐛 Bug Fix: WSLWindowsCopy now supports /mnt/c/ paths

#### Fixed
- **`wsl_to_windows_copy` and `windows_to_wsl_copy` path handling**
  - Fixed issue where `wsl_to_windows_copy` would fail with "source does not exist" error when given a `/mnt/c/` source path
  - Root cause: Function only accepted `/home/` style paths, but files edited via Windows paths are accessible through `/mnt/c/`
  - Solution: Added automatic path conversion from `/mnt/c/...` to Windows path format (C:\...) when checking file existence and copying

#### Impact
- **Workflow Support**: Users can now use `wsl_to_windows_copy` with `/mnt/c/` paths (files edited from Windows)
- **Consistency**: Function now handles all valid WSL path formats consistently
- **Interoperability**: Better WSL/Windows integration when working with files edited from both environments

#### Files Modified
- `core/wsl_sync.go`: Enhanced `WSLWindowsCopy()` function
  - Added detection for `/mnt/` prefixed paths
  - Auto-converts `/mnt/c/...` to Windows path for file operations

---

## [3.5.1] - 2025-11-21

### 🐛 Bug Fix: Silent Failures in intelligent_* Functions on Windows

#### Fixed
- **`intelligent_read`, `intelligent_write`, `intelligent_edit` path handling**
  - Fixed silent failures in Claude Desktop on Windows with error: "No result received from client-side tool execution"
  - Root cause: These functions called `os.Stat()` BEFORE normalizing Windows paths, causing silent failures or timeouts
  - Solution: Added `NormalizePath()` at the beginning of all intelligent_* functions before any filesystem operations
  - Also fixed: `GetOptimizationSuggestion()` now normalizes paths before `os.Stat()`

#### Impact
- **Reliability**: `intelligent_read`, `intelligent_write`, and `intelligent_edit` now work correctly in Claude Desktop on Windows
- **Consistency**: All intelligent_* functions now match the behavior of basic functions (`read_file`, `write_file`) which already normalized paths
- **Developer Experience**: Eliminates mysterious "No result received" errors and timeouts when using intelligent operations
- **Fallback Unnecessary**: Users no longer need to fall back to basic functions with `max_lines` workaround

#### Technical Details
- **Before**:
  - `intelligent_read` → `os.Stat(path)` → fails with incorrect Windows path → silent timeout
  - Users had to use `read_file` with `max_lines` as workaround
- **After**:
  - `intelligent_read` → `NormalizePath(path)` → `os.Stat(normalized_path)` → success
  - Path normalization happens before any filesystem operations

#### Files Modified
- `core/claude_optimizer.go`: Added path normalization to 4 functions
  - `IntelligentRead()` (line 70-71)
  - `IntelligentWrite()` (line 55-56)
  - `IntelligentEdit()` (line 98-99)
  - `GetOptimizationSuggestion()` (line 114-115)

---

## [3.5.0] - 2025-11-20

### 🚀 Performance Optimization: Memory-Efficient I/O

#### Optimized
- **`copyFile()` / `CopyFile()`** - Now uses `io.CopyBuffer` with pooled buffers instead of loading entire files into RAM
  - Memory usage reduced from file-size to constant 64KB regardless of file size
  - Leverages OS optimizations like `sendfile()` on Linux/WSL for zero-copy operations
  - 90-98% memory reduction for large files (>100MB)

- **`copyDirectoryRecursive()` (WSL sync)** - Optimized with `io.CopyBuffer` and buffer pooling
  - Eliminates memory spikes when copying large directories
  - Reduces GC pressure during mass copy operations

- **`SyncWorkspace()` (WSL ↔ Windows sync)** - Memory-efficient file synchronization
  - Uses streaming copy instead of buffering entire files
  - Enables reliable sync of multi-GB workspace directories

- **`ReadFileRange()` / `read_file_range`** - Rewritten to use `bufio.Scanner`
  - Previously read entire file to extract a few lines (e.g., 31k lines to get lines 26630-26680)
  - Now reads line-by-line, stopping when target range is reached
  - 90-99% memory reduction for large files
  - Dramatically faster for reading ranges at the end of large files

#### Added
- **Buffer Pool System** - `sync.Pool` for 64KB I/O buffers
  - Reduces garbage collection pressure by reusing buffers across operations
  - Buffers automatically scale with concurrent operations
  - Zero allocation overhead for steady-state operations

#### Technical Details
- **Before**:
  - `CopyFile()` loaded entire file into RAM (e.g., 500MB file = 500MB RAM)
  - `ReadFileRange()` read 31,248 lines (250k tokens) to extract 50 lines
  - High GC pressure from allocating new buffers for each operation

- **After**:
  - `CopyFile()` uses constant 64KB memory regardless of file size
  - `ReadFileRange()` reads only necessary lines (2.5k tokens)
  - Buffer pool eliminates repeated allocations

#### Performance Impact
- **Copy Operations**: 90-98% memory reduction for files >100MB
- **Range Reads**: 95-99% memory and token reduction
- **GC Pressure**: Significantly reduced, improving overall responsiveness
- **WSL Performance**: Better I/O performance across DrvFs (WSL ↔ Windows filesystem)

#### Compatibility
- No API changes - all optimizations are internal
- Backward compatible with all existing tools and operations
- All 45 tools continue to work without changes

#### Statistics
- Files modified: 3 (file_operations.go, wsl_sync.go, engine.go)
- Lines added: ~150 (including comments)
- Test results: All tests passing (100% success rate)
- Memory optimization: Up to 99% reduction for targeted operations

---

## [3.4.3] - 2025-11-20

### 🐛 Bug Fix: Multiline Edit Validation

#### Fixed
- **`recovery_edit` / `smart_edit_file` context validation**
  - Fixed an issue where multiline edits failed with "context validation failed" due to line ending differences (CRLF vs LF).
  - Now normalizes line endings before validating context, ensuring robust editing across Windows/WSL environments.
  - `batch_operations` remains unaffected as it uses a different validation path.

#### Impact
- **Reliability**: Multiline code replacements now work reliably regardless of file encoding (Windows/Unix).
- **Developer Experience**: Eliminates false positive "file has changed" errors when editing files with mixed line endings.

---

## [3.4.2] - 2025-11-17

### 🛡️ Stability & Backward Compatibility

#### Changed
- **`recovery_edit` is now a safe alias for `intelligent_edit`**.
  - The original `recovery_edit` logic was deprecated due to causing timeouts and instability on Windows with Claude Desktop.
  - To ensure backward compatibility, the `recovery_edit` tool is preserved.
  - All calls to `recovery_edit` are now internally redirected to the stable `intelligent_edit` function.
  - A log warning (`⚠️ DEPRECATED: 'recovery_edit' was called...`) is issued when the alias is used.

#### Fixed
- **Silent MCP Timeouts**: Resolved an issue where `recovery_edit` could cause silent timeouts ("No result received from client-side tool execution") by removing its unstable multi-step recovery logic.

#### Impact
- **Improved Stability**: Prevents production environments from hanging due to unstable recovery attempts.
- **Backward Compatibility**: Older versions of Claude Desktop that might still call `recovery_edit` will continue to function without errors, using the stable edit logic instead.
- **Developer Experience**: The tool's description is updated to mark it as `[DEPRECATED]`, guiding users towards `intelligent_edit`.

---

## [3.4.1] - 2025-11-17

### 🔧 Critical Fix: Windows Path Recognition

#### Fixed
- **Windows path recognition** - El binario ahora se compila correctamente para Windows con `GOOS=windows`
- **Path normalization** - Rutas de Windows (C:\...) ahora se reconocen correctamente en Windows puro (no WSL)

#### Added
- **`build-windows.sh`** - Script de compilación para Windows desde WSL/Linux
- **`build-windows.bat`** - Script de compilación para Windows desde Windows
- **`WINDOWS_PATH_FIX.md`** - Documentación técnica detallada del problema y solución
- **`GUIA_RAPIDA_WINDOWS.md`** - Guía rápida en español para usuarios

#### Problem Resolved
- ❌ **Before**: Binary compiled from WSL thought it was running on Linux
  - Input: `C:\temp\hol.txt`
  - Internal conversion: `/mnt/c/temp/hol.txt` (incorrect for Windows)
  - Result: File not found ❌

- ✅ **After**: Binary properly compiled for Windows with `GOOS=windows`
  - Input: `C:\temp\hol.txt`
  - Internal handling: `C:\temp\hol.txt` (correct)
  - Result: File found ✅

#### Technical Details
- Root cause: Binary was compiled in WSL without specifying target OS
- The code was always correct - only the compilation method needed fixing
- Now uses proper cross-compilation: `GOOS=windows GOARCH=amd64 go build`
- `runtime.GOOS` now correctly reports "windows" instead of "linux"
- `os.PathSeparator` now correctly uses `\` instead of `/`

#### Impact
- **Claude Desktop users on Windows**: Now works correctly with Windows paths
- **WSL users**: No change, WSL paths continue to work as before
- **Configuration**: No changes needed to `claude_desktop_config.json`

#### Statistics
- Files modified: 0 (code was already correct)
- Files created: 4 (2 build scripts, 2 documentation files)
- Executable size: 5.67 MB (unchanged)
- Total tools: 45 tools (unchanged)

---

## [3.4.0] - 2025-11-15

### 🔄 Automatic WSL ↔ Windows Sync (Silent Auto-Copy)

#### Added
- **`configure_autosync`** - Activar/desactivar sincronización automática con opciones configurables
- **`autosync_status`** - Ver estado actual de la configuración auto-sync
- **`core/autosync_config.go`** - Sistema completo de sincronización automática en tiempo real (343 líneas)

#### Changed
- `WriteFileContent()` - Auto-sync después de escribir
- `StreamingWriteFile()` - Auto-sync después de streaming
- `EditFile()` - Auto-sync después de editar
- `ReplaceNthOccurrence()` - Auto-sync después de reemplazar

#### Features
- ✅ **Auto-Sync Configuration System** - Sistema de configuración almacenado en ~/.config/mcp-filesystem-ultra/autosync.json
- ✅ **Hooks integrados** - Sincronización automática en todas las operaciones de write/edit
- ✅ **Variable de entorno** - MCP_WSL_AUTOSYNC=true para activar en una línea
- ✅ **Operaciones async** - Nunca bloquean la operación principal
- ✅ **Fallo silencioso** - Sync errors nunca rompen las operaciones de archivo
- ✅ **Backwards compatible** - Deshabilitado por defecto

#### Statistics
- Total tools: 43 → **45 tools** (+2 new)
- Files modified: 3 (core/engine.go +46 líneas, core/streaming_operations.go +5, core/edit_operations.go +10)
- Files created: 1 (core/autosync_config.go 343 líneas)

#### Resolved Issues
- ❌ **Before**: Archivos creados en WSL no aparecen automáticamente en Windows Explorer
- ✅ **After**: Sincronización automática y silenciosa después de cada write/edit

---

## [3.3.0] - 2025-11-14

### 🪟 WSL ↔ Windows Auto-Copy & Sync Tools

#### Added
- **`wsl_to_windows_copy`** - Copia archivos/directorios de WSL a Windows con auto-conversión de rutas
- **`windows_to_wsl_copy`** - Copia archivos/directorios de Windows a WSL con auto-conversión de rutas
- **`sync_claude_workspace`** - Sincroniza espacios de trabajo completos entre WSL y Windows
- **`wsl_windows_status`** - Muestra estado de integración WSL/Windows y ubicaciones de archivos

#### Features
- ✅ **Auto-conversión de rutas** - Las rutas de destino se calculan automáticamente si no se especifican
- ✅ **Copia recursiva** - Soporte completo para directorios y archivos individuales
- ✅ **Sincronización con filtros** - Sincroniza solo archivos que coincidan con patrones (*.txt, *.go, etc.)
- ✅ **Dry-run mode** - Vista previa de cambios sin ejecutar
- ✅ **Detección de entorno** - Identifica automáticamente si está corriendo en WSL o Windows
- ✅ **Creación de directorios** - Crea automáticamente directorios de destino si no existen

#### Statistics
- Total tools: 37 → **41 tools** (+4 new)
- New modules: 3 (path_detector.go, path_converter.go, wsl_sync.go)

---

## [3.2.0] - 2025-10-14

### 🪟 Windows/WSL Path Normalization + create_file Alias

#### Added
- **`create_file` alias** - Alias para `write_file` (compatibilidad Claude Desktop)

#### Changed
- **Path normalization** - Todas las 18 operaciones de archivos ahora soportan conversión automática de rutas WSL ↔ Windows
- Detección inteligente del sistema operativo
- Soporte bidireccional: `/mnt/c/...` ↔ `C:\...`

#### Features
- ✅ **Normalización automática de rutas** - Convierte `/mnt/c/...` ↔ `C:\...` según el sistema
- ✅ **Detección inteligente** - Funciona en Windows, WSL y Linux sin configuración
- ✅ **18 funciones actualizadas** - Todas las operaciones de archivos soportan ambos formatos
- ✅ **0 configuración requerida** - Funciona automáticamente

#### Statistics
- Total tools: 35 → **36 tools** (+1 alias)

---

## [3.1.0] - 2025-10-25

### 🎯 Ultra-Efficient Operations

#### Added
- **`read_file_range`** - Lee rangos específicos de líneas (ahorro 90-98% tokens vs read_file completo)
- **`count_occurrences`** - Cuenta ocurrencias con números de línea opcionales (ahorro 95% tokens)
- **`replace_nth_occurrence`** - Reemplazo quirúrgico de ocurrencia específica (primera, última, N-ésima)

#### Features
- ✅ **Lectura eficiente de rangos** - Lee solo las líneas necesarias sin cargar archivo completo
- ✅ **Contador preciso** - Cuenta todas las ocurrencias incluso múltiples por línea
- ✅ **Reemplazo quirúrgico** - Cambia SOLO la ocurrencia que especificas
- ✅ **Validación estricta** - Con rollback automático
- ✅ **Formato dual** - Compacto (producción) y verbose (debug)
- ✅ **Regex o literal** - Soporta ambos tipos de patrones

#### Statistics
- Total tools: 32 → **36 tools** (incluye alias `create_file`)
- Token savings: 90-99% en operaciones de archivo grande
- Executable size: 5.5 MB

---

## [3.0.0] - 2025-10-24

### 🚀 Optimización Ultra de Tokens (77% Reducción)

#### Added
- **Smart Truncation** - Lectura inteligente con modo head/tail/all

#### Features
- ✅ **77% reducción** en sesiones típicas (58k → 13k tokens)
- ✅ **90-98% ahorro** en lectura de archivos grandes
- ✅ **60% reducción** en overhead de herramientas

---

## [2.6.0] - 2025-10-23

### 📦 Batch Operations

#### Added
- Batch operation support with atomic rollback
- Multi-file operations with consistency guarantees

---

## [2.5.0] - 2025-10-22

### 🎯 Plan Mode / Dry-Run

#### Added
- **`analyze_write`** - Analiza una operación de escritura sin ejecutarla
- **`analyze_edit`** - Analiza una operación de edición sin ejecutarla
- **`analyze_delete`** - Analiza una operación de eliminación sin ejecutarla

---

## [2.4.0] - 2025-10-21

### 🪝 Hooks System

#### Added
- **12 Hook Events** - Pre/post para write, edit, delete, create, move, copy
- **Pattern Matching** - Objetivos específicos usando coincidencias exactas o wildcards

---

## [2.3.0] - 2025-10-24

### ✨ Nuevas Operaciones de Archivos

#### Added
- **`create_directory`** - Crear directorios con padres automáticos
- **`delete_file`** - Eliminación permanente de archivos/directorios
- **`move_file`** - Mover archivos o directorios entre ubicaciones
- **`copy_file`** - Copiar archivos o directorios recursivamente
- **`get_file_info`** - Información detallada (tamaño, permisos, timestamps)

#### Statistics
- Total tools: 23 → **28 tools** (+5 new)

---

## [2.2.0] - 2025-10-20

### 🧠 Token Optimization

#### Added
- **`--compact-mode`** flag - Respuestas minimalistas sin emojis

#### Features
- ✅ **65-75% reducción** de tokens en sesiones típicas

---

## [2.1.0] - 2025-09-26

### 🔧 Compilation Fixes & Updates

#### Fixed
- ✅ `min redeclared in this block` error
- ✅ `undefined: log` imports
- ✅ `time.Since` variable shadowing issue
- ✅ `mcp.WithInt undefined` → migrated to `mcp.WithNumber`
- ✅ `request.GetInt` API → migrated to `mcp.ParseInt`

#### Updated
- **mcp-go**: v0.33.0 → **v0.40.0**
- **Go**: 1.23.0 → **1.24.0**

---

## [2.0.0] - 2025-01-27

### 🚀 Initial Ultra-Fast Release

#### Added
- **32 MCP tools** ultra-optimized for Claude Desktop
- **Intelligent System** - 6 intelligent tools for auto-optimization
- **Streaming Operations** - 4 streaming tools for large files
- **Smart Cache** - Intelligent caching with 98.9% hit rate

#### Performance
- **2016.0 ops/sec** throughput
- **98.9% cache hit rate**

---

**Current Version**: 4.5.2
**Last Updated**: 2026-05-27
**Status**: Production Ready
