---
name: filesystem-ultra-tools
description: Tool catalog for filesystem-ultra MCP server v4.8.0. 34 tools ultra / 19 agent / 18 strict. First call list_allowed_directories. If the client already lists the tools, do not call help(). help(tool:X) only when a parameter is missing. Host filesystem, post-write verify, aliases disabled. Recommended flags: --profile=agent --compact-mode --roots-mode=union.
---

# Filesystem Ultra v4.8.0 — Tool Discovery

## Canonical source and deployment

Maintain this repository's `.claude/skills/filesystem-ultra-tools/SKILL.md` as the canonical source. OpenCode discovers the project skill directly. External client copies must be synchronized from this source and checked at release time; do not maintain independent catalogs. Restart the client after changing its configuration or installed skills. Enable one filesystem server per context; keep `ultra` for this repository's advanced development flow.

`llms.txt` (repository root) is the agent-first companion: the guarantees the server actually keeps (OCC, `apply_patch`, CRLF dry-run artefacts, backups, paths, `retryable`) and the strict inventory, for pasting into a system prompt.

## Recommended server flags

`--profile=agent --compact-mode --roots-mode=union` (`--readonly` off) when the agent needs git without the full catalog. `--profile=strict` remains the 18-tool core without git. `--git-network` only if the agent must `git push`/`fetch`/`pull` (`pull` is fast-forward only; honored by ultra and agent, ignored by strict). `--response-mode=structured` is opt-in and keeps file bodies once in structuredContent; default `compat` is unchanged. `--git-remote-allow` is optional (empty = any remote). GitHub+GitLab: `--git-remote-allow=github.com,gitlab.com`. `--profile=ultra` (default) keeps all 34 tools including `analyze_code`, graduated `github_issues` / `gitlab_issues`, and experimental `context_pack` and `workspace`. Optional `--file-security-config` is owner-controlled and immutable; `security_policy` does not list rules.

For JSON queries, byte diagnostics or baseline hashes, use `--profile=ultra --compact-mode --roots-mode=union`. `json_query`, `file_bytes` and `file_hash` are absent from both `agent` and `strict`, and work under `--readonly`. After changing the binary/profile, reconnect the client so it refreshes the catalog.

## Bind each project to one filesystem tool family

- `filesystem-ultra` operates on the real host filesystem (`C:\...`, host-mounted `/mnt/...`). Runtime-native `create_file`, `str_replace`, `view` may target a different sandbox.
- First call `list_allowed_directories` (before any read). If the client already lists the tools, do not call `help()`. `help(tool:"X")` only when a parameter is missing from the schema. `project_map` when the shape of a project is unknown; `directory_tree` only when the actual tree is needed. Do not dump the full catalog at startup.
- For host projects, use only filesystem-ultra tools. If the harness also offers native Read/Grep/Edit, do not call both. After every host create/edit, verify with `get_file_info` or `list_directory`; `read_file` when content matters.
- `File not found` for a known file is a filesystem-mismatch signal: stop, confirm with the host reader, audit recent writes. Never switch tool families silently.

## Subagents do not inherit this skill

Harness subagents start without this file. The portable contract is `server.WithInstructions()` (handshake). Copy `examples/harness/` rather than guessing flags.

**Handoff** parent → child: roots, path, `content_hash`, `backup_id`, profile. The child re-reads; it must not reuse the parent's file body. First call `list_allowed_directories` (structured: `profile`, `roots_mode`, `readonly`, `tool_count`). One role = one disk family — never mix native Read/Edit/Write/Glob with filesystem-ultra in the same subagent.

## Discovery order

1. `list_allowed_directories` — sandbox roots. Zero parameters. That is the whole startup.
2. `project_map` — cheap structural orientation when the shape of a project is unknown. Metadata only (skeleton, extension histogram, largest files, key files). It does not select code.
3. `directory_tree` — only when the actual tree is needed. Defaults: `respect_ignore=true`, `max_depth=2`, `max_nodes=500`.
4. `help(tool:"X")` — only when a parameter is missing from the client schema. If the client already lists the tools, do not call `help()`.

Native `oldString` / `replaceAll` / `search` / `edit` are not this server. `edit_file` uses `old_text` / `new_text`. Omitting `occurrence` replaces every exact match. A pattern uses `mode:"search_replace"`.

Host files = `read_file` / `search_files` / `edit_file` / `write_file`. Native Read/Edit/Write/Grep = error. On failure, read `path` and `retryable`; `retryable:false` means stop.

Search first: `search_files` (omitted `include_content` searches content; globs stay filename-only; `include_content:false` is names only). Empty results name the scope. Page is 40. Then `read_file` only with a range.

If a client asks for `read_multiple_files` / `read_text_file`, use `read_file` (`paths[]` / `mode` head|tail).

## Strict profile (18) — always available when `--profile=strict`

| Tool | When / notes |
|------|----------------|
| `list_allowed_directories` | First call |
| `project_map` | Cheap structural orientation of an unknown project. Metadata only. `detail:"summary"` (top level) / `"normal"` (directories only, default) / `"full"` (directories + files). Bounded: 5000 entries, 2.5 s, 512 KiB of metadata; a limit sets `truncated=true` + `summary_scope="visited_entries"`. Never follows symlinks; hidden paths (file-security policy) are not counted or named |
| `directory_tree` | Only when the actual tree is needed. Gitignore ON |
| `list_directory` | Copy exact paths (case) before edits |
| `search_files` | Gitignore ON. Omit `max_results` and the page is 40. Omitted `include_content` searches content; a glob (`* ? [`) stays filename-only; `include_content:false` is names only. Empty results name the scope. Do not pair `count_only` with `detail=full` |
| `get_file_info` | Verify after a host mutation. Batch `paths[]` |
| `read_file` | Full / range / head / tail / base64 / `paths[]`. Logs: `mode:"tail"` `max_lines:40` |
| `write_file` | New files or whole-file rewrite. `mode:"append"` skips rewrite-guard |
| `edit_file` | Targeted edits. Override rewrite-guard with `allow_rewrite:true` (not `force`) |
| `multi_edit` | Several anchors in one file. Ambiguous `old_text` (>1 match) rejects the batch (file unchanged). The error lists each edit (index, `FAILED`/`AMBIGUOUS`); do not treat `Failed: .` as empty |
| `apply_patch` | One file: `path` is the dest. Several files: `path` is the directory root; in-process all-or-nothing (not crash-durable). Happy path: `dry_run` + `expected_hash` (single-file). Dest EOL wins. If `PATCH_FAILED`: `read_file` and regenerate; do not retry the same patch |
| `diff_files` | Preview before `apply_patch`, or two paths / `against:"backup"` |
| `create_directory` | `mkdir -p` |
| `move_file` | Move or rename |
| `delete_file` | Soft-delete default; `permanent:true` hard |
| `backup` | Undo: `undo_last` / `undo_chain` / `restore` (`backup_id`). Trash: `list_trash` / `restore_trash` (`sd_id`, not `backup_id`) / `purge_trash` |
| `help` | Skip when the client already lists the tools. Otherwise only for a missing parameter |
| `security_policy` | Read-only policy description; does not expose rules |

## Ultra only (absent in `--profile=strict`)

- `json_query` — experimental, ultra only, read-only. Standard JMESPath (not jq), default 100/max 1000 array items, `json|table|csv|text`. Reuse the arguments from response `continuation` including `expected_hash`; no input `continuation`. `lot.json` already groups problems. Example: `json_query(path:"lot.json", query:"problemes[?mena == 'text']", output_format:"table")`.
- `file_bytes` — experimental, ultra only, read-only. `mode:encoding|bytes|all`; diagnostic prefix defaults to 1 MiB, dump to 512 bytes. Byte offsets are 0-based; line ranges 1-based. Check `scan_complete` for partial statistics. `end_byte_offset` preserves exclusive range ends in continuations.
- `file_hash` — experimental, ultra only, read-only. `path` XOR `paths` (max 64); SHA-256 of original bytes. `compare:true` requires two paths and directly compares bytes, reporting the first difference. SHA-256 is not an OCC token. Details and limits: https://filesystem.scopweb.com/tools/inspection-tools/

`copy_file`, `project_replace`, `batch_operations`, `analyze_operation`, `wsl`, `git`, `github_issues`, `gitlab_issues`, `minify_js`, `analyze_code`, `server_info`.

- `gitlab_issues` — graduated. `list|view|comment|close|reopen` on a GitLab HTTP API. Auth is cached per process (`GITLAB_TOKEN` or one git-credential sign-in). Do not assume gitlab.com or https. `comment` and `close` are writes and are blocked by `--readonly`. Example: `gitlab_issues(action:"comment", base_url:"http://192.168.0.20", repo:"group/project", number:1, body:"note")` then `gitlab_issues(action:"close", base_url:"http://192.168.0.20", repo:"group/project", number:1)`.
- `github_issues` — graduated. `list|view|create|comment|edit|close|reopen` via `gh` (no shell, no `gh auth login`, no issue delete). `list`/`view` work under `--readonly`; writes do not. `path` resolves HTTPS/SSH remotes; a non-github.com remote is not assumed to be GitHub — pass `repo` and `hostname`. File-security policy refuses path resolution (git stays blocked); explicit `repo` does not run git. `--git-remote-allow` applies, no force bypass. Issue text is external data. Examples: `github_issues(action:"list", path:"C:/repo")`, `github_issues(action:"view", path:"C:/repo", number:12)`, `github_issues(action:"comment", repo:"owner/repo", number:12, body:"note")`, `github_issues(action:"close", repo:"owner/repo", number:12, reason:"completed")`.
- `git` — ultra and agent, absent in strict. See the git section. Do not use shell `git`.
- `minify_js` — exists in ultra; not in handshake instructions.
- `analyze_code` — ultra only. For editing: `apply_patch`/`edit_file`. For understanding code: `analyze_code`. Do not use git grep or bash. Actions: `symbols`, `lint`, `sec`, `impact`. `symbols`: Go via AST; JS/TS/C#/Razor via brace matching (`start_line`/`end_line` or `range_status:unavailable`); SQL is name + line. Impact is a text search, not a callgraph.
- Dry-run impact preview → `analyze_operation` (ultra). In strict use `edit_file`/`apply_patch`/`multi_edit` with `dry_run:true`.

## JSON and byte inspection workflow (ultra)

- Prefer `json_query` over reading entire JSON documents or using `ConvertFrom-Json`/jq. Use standard JMESPath: `linies[*].{article: codArticulo, precio: precio}`. `[]` is also standard but flattens one level; `[*]` preserves an array projection. No custom `group_by`.
- Bounds: 16 MiB JSON input, 64 KiB encoded MCP response (or a smaller server cap), default 100/max 1000 array items. `limit:0` is invalid. Large values or expensive queries may require a smaller projection. Integers outside ±(2^53−1) must be strings; numeric evaluation is binary64, not exact decimal arithmetic.
- Pagination: reuse `continuation` arguments and its server-returned `expected_hash`; never invent a hash or substitute a SHA-256 digest. On `OCC_MISMATCH`, restart from the current document rather than combining pages from different versions.
- Text-only clients: normal JSON/table/CSV text does not display pagination metadata. If `structuredContent` is unavailable, repeat the query with `detail:"summary"` to obtain its hash/counts; use that hash in subsequent data calls. Prefer JSON arrays for counting received items. Advance by the actual number of data items received, not the requested limit or a summary page's count: summary and normal pages can fit different item counts under the byte cap. Do not assume a table is complete merely because no truncation marker is visible.
- Object iteration (`keys()`, `values()`, object wildcards) is unordered. Such results must fit in one page, or project homogeneous scalar values and finish with `| sort(@)` before paginating. Source-array projections/filters retain source order.
- `file_bytes`: `max_bytes` limits the dump (default 512/max 4096); `max_scan_bytes` limits diagnostic sampling (default 1 MiB; server ceiling controlled by `--max-inspect-scan-bytes`). Check `scan_complete` independently of dump `truncated`. `sample_boundary_pending:true` is a sample cut, not evidence of invalid encoding. Non-ASCII is not an error; inspect `invisible` and `invalid_sequences` separately.
- For a truncated line dump, reuse the returned byte-offset continuation, including exclusive `end_byte_offset`, so it stays within the selected range. Do not combine byte selectors with line selectors.
- `file_hash(compare:true)` compares original bytes directly, including BOM/EOL. A first-difference byte of null means EOF. A detected concurrent change fails the whole batch; do not interpret an error as partial equality. SHA-256 hashes are not OCC tokens.

Full limits and evaluator cancellation semantics: https://filesystem.scopweb.com/tools/inspection-tools/. These tools inspect/query content; they do not translate or modify JSON values.

## git

Use the `git` tool, never shell `git` or `git grep`. `help(tool:"git")` is the live schema. A file security policy blocks git. Path must be inside a repo, or use `init`.

Actions: `status`, `diff`, `review`, `log`, `show`, `add`, `commit`, `push`, `fetch`, `restore`, `branch`, `init`, `remote`.

- Compact `status` without `output` is one line (`repo (branch) | +1 ~2 ?3 | dirty`). Pass `output:"name-only"` or `"full"` for the file list.
- `diff` defaults to `stat`. `output:"full"` is the patch (porcelain v2 is normal, not a failure). `paths` is the pathspec. `staged:true` is `--cached`.
- `review` always pages. Continue with `offset` plus `expected_status_hash`. A mismatch is `STALE_GIT` and returns no diff body.
- `add` with `update:true` is `git add -u` (tracked files only; it does not stage untracked paths such as `.agent/`).
- `commit` requires `message`. `risk` is informational and does not block.
- `branch` with `name` never deletes. Delete requires `delete:true` (`-d`); `force:true` only then becomes `-D`. `checkout:true` switches, creating the branch if it is missing.
- `push` and `fetch` need `--git-network`. `force:true` on push is `--force-with-lease`, never plain `--force`. `remote` is a name, not a URL. `fetch` with `prune:true` drops stale remote-tracking refs. `pull` is fast-forward only (`--ff-only`) and needs `--git-network`. `stash` uses commit OIDs; drop/pop require `force:true`.
- `git(action:"remote")` is read-only and works without `--git-network`. Credentials in URLs are redacted. `--git-remote-allow` matches the effective URL; `force:true` does not bypass it.

## Never bash for filesystem

Do not use bash `cat`, `head`, `tail`, `cut`, `sed -n`, `ls`, `dir`, `tree`, `grep`, `find`, `wc`, `rg`, or `stat` on host project files. `$HOME/mnt` is a sandbox path; use the Windows path from `list_allowed_directories`.

| Need | Call |
|------|------|
| Orient yourself in an unknown project | `project_map(path, depth:2)` — metadata only, bounded |
| Filter/project JSON | `json_query(path, query, limit:100)` (ultra) |
| BOM/EOL, invisible characters or hex | `file_bytes(path, mode:"all")` (ultra) |
| Baseline byte parity / SHA-256 | `file_hash(paths:[baseline, generated], compare:true)` (ultra) |
| Last 40 log lines | `read_file(path, mode:"tail", max_lines:40)` |
| Line census (replaces find \| wc -l) | `list_directory(path, output_format:"lines", include:".cs,.json,.csproj")` |
| First N lines | `read_file(path, max_lines:N)` (mode omitted/`"all"`) or `mode:"head"` |
| Exact line range | `read_file(path, start_line, end_line)` — also honored on `paths[]` |
| Line cut override | `max_line_length:N` or `0` to disable |

## search_files aliases

| Native | Alias | Purpose |
|--------|-------|---------|
| `file_types` + `include` | Independent filters | Their intersection applies in names, content and count modes before pagination. `include` expands `{a,b}` (`{A.cs,B.cs}`, `{Test,ObservacionesPedido}/*.razor`). Commas inside braces are alternatives. `*` does not cross `/`; `**` includes zero directory levels. Windows separators are accepted. Invalid globs/braces return `VALIDATION` with `retryable:false`. `file_types` does not expand braces. |
| `output_format` | `output` | `"text"` or `"json"`. Omit for grouped `path` header + `line:text` (any match count, including context). `"text"` is the legacy emoji layout. Legacy `content`/`files_with_matches`/`count` are **not** implemented. |

`include_context:true` stays in that grouped layout; context lines are ordered. `output_format:"json"` uses ripgrep when `rg` is on PATH or embedded (`embed_rg`). See `ripgrep-reference.md` in this folder (v4.8.0, not v4.7.0). A file security policy skips ripgrep. When truncated, call the same search with `offset` from `continuation` — do not reformulate. There is no hidden 10/20 presentation cap.

## Key behaviors (strict-safe)

- **Modify existing files** → `edit_file` or `apply_patch` (one path). Whole-file rewrite → `write_file`.
- **Several edits same file** → `multi_edit` (each `old_text` unique in the original file). On atomic rollback, read the per-edit causes; the file was not written.
- **Dry-run** → `edit_file(dry_run:true)` / `multi_edit(dry_run:true)` / `apply_patch(dry_run:true)`.
- **OCC** → every successful read/edit returns `content_hash`; pass as `expected_hash` on the next mutation. `--auto-occ` `off`/`warn` (default)/`block` flags *external* changes only.
- **STALE_READ** (`edit_file` / `multi_edit`): non-blocking. Suppressed when every `old_text` matches once. Consecutive edits on the same file do not need re-reads.
- **Structured results** → prefer `structuredContent`: `status` (`applied`/`simulated`/`empty`/`partial`), `truncated`, `hidden_count`, hashes. Empty search is `status:empty` (`isError=false`). Errors have `retryable` — do not auto-retry when false. `search_files.message` and `list_allowed_directories.message` are summaries; hits are in `matches`, roots are in `paths`. The text content block still lists them.
- **read_file truncation** → `truncated:false` and `status:ok` when the requested range, head, tail, or `max_lines` selection is complete, even if the file is longer. `truncated:true` only for a server cut (auto-cap, omitted head/tail cap, line width). Do not follow a continuation that is not present. `content_hash` is the full-file snapshot, not the displayed slice. Clients that JSON-encode `structuredContent` escape the body; the text block is the readable copy. Do not prefix line numbers onto `content`.
- **Line-based edits** → `edit_file` `mode:"delete_range"` / `"replace_range"` (1-based inclusive).
- **Undo** → `backup(action:"undo_last")` / `undo_chain` / `restore` with `backup_id`.
- **Soft-delete recovery** → `backup(action:"list_trash")`; restore with `backup(action:"restore_trash", sd_id:"...")` (not `backup_id`); purge with `purge_trash`. `delete_file` soft-deletes files and directories.

## Ultra behaviors (only if those tools are registered)

- **Project-wide token rename** → `project_replace` (1 call). Pass `paths` for an explicit file list (skips the walk). `preview:true` is the dry run. `create_backup:true` snapshots before writes.
- **Batch / pipeline / extract** → `batch_operations`. `extract` moves lines `[start_line,end_line]` from source to destination atomically.
- **Impact preview** → `analyze_operation`.

## Critical workflow rules

### Always copy paths from `list_directory` / `read_file` — never from memory

Windows is case-insensitive at the FS layer. Passing `estats.razor` for `Estats.razor` succeeds, then Razor/MSBuild fail later (`RZ10011: class estats`). Copy the path character-by-character from a prior listing.

### Never use `edit_file` for whole-file rewrites

`edit_file` replaces only the matched `old_text`. A small header + full-file `new_text` concatenates. The server blocks `new_text > 2× old_text` with leftover file content. Override is `allow_rewrite:true`, not `force`. Prefer `write_file`.

| Situation | Use |
|-----------|-----|
| Targeted small change | `edit_file` mode `replace` |
| Replace all occurrences | `edit_file` mode `search_replace` |
| Whole-file rewrite | `write_file` |
| Multiple targeted changes same file | `multi_edit` |
| Delete/replace known line range | `edit_file` `delete_range` / `replace_range` |
| Unified diff (one file or multi-file transaction) | `apply_patch` (regenerate on `PATCH_FAILED`) |
| Rename token project-wide | `project_replace` (ultra) |

## Disabled (not registered)

Aliases `read_text_file`, `search`, `edit`, `write`, `create_file`, `View`, `Edit`, `Write`, `Replace`, `LS`, `GlobTool`, `GrepTool`, and the `fs` super-tool. A runtime-native tool with one of those names is not a filesystem-ultra alias.

## project_replace (ultra)

`path`, `find`, `replace` required. `literal` default true. `file_types` e.g. `.php`. `paths` is an explicit file list (skips the walk). `exclude_paths` globs. `preview` / `create_backup` (default true) / `parallel` / `max_files` (default 1000). Compact default is counters only; `detail:"full"` lists each file. JSON/XML/`.csproj` edits append a non-blocking syntax warning if the file is left ill-formed.
