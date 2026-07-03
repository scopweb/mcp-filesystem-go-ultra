# Security & Correctness Audit — `tools_git.go`

**Date:** 2026-07-02 · **Version:** 4.5.19 → **4.5.23** · **Auditor:** cyber-vuln-detect
**Scope:** `git` tool (dispatcher + handlers) and the last commits (v4.5.20–22).
**Method:** static review line-by-line + validation of every produced git command
against real git 2.34.1. Go toolchain unavailable in the audit sandbox, so
`go build`/`go test` are **pending on Windows** (see §Verification).

---

## Findings

| # | Sev | Location | Issue | Status |
|---|-----|----------|-------|--------|
| 1 | 🔴 Critical | `gitRestore` | `restore` subcommand only prepended in dry-run branch → real restore ran `git --staged -- file` | Fixed |
| 2 | 🟠 High | `gitRestore` | `source` passed positionally → `fatal: pathspec 'HEAD~1' did not match` | Fixed |
| 3 | 🟠 High | `gitAdd` | No `--` separator → path `"-A"` / `"--pathspec-from-file=…"` parsed as git option (option injection) | Fixed |
| 4 | 🟡 Medium | dispatcher / `gitBranch` | `force=true` required to delete a branch, but `force` escalates `-d`→`-D`: safe delete impossible | Fixed |
| 5 | 🟡 Medium | `gitRestore` | `dry_run` used `-n` which `git restore` rejects ("unknown switch") → preview always failed | Fixed |
| 6 | 🟢 Coherence | dispatcher | `rejectOptionLike` ran per-handler *after* the destructive gate | Centralized before gate |

---

## Detail & fixes

### 1 — 🔴 `restore` never invoked the `restore` subcommand
Non-dry-run path executed `execGitCommand("git", cmdArgs...)` where `cmdArgs`
began with `--staged` / `<source>` / `--`, never `"restore"`. The token was
prepended **only** inside the `dry_run` branch. Result: `git(action:"restore")`
was broken for every real call. Pre-existing (present before the v4.5.22 refactor).
**Fix:** `cmdArgs := []string{"restore"}` unconditionally.

### 2 — 🟠 `source` positional
`git restore HEAD~1 -- f` → git treats `HEAD~1` as a pathspec. Must be
`--source=<rev>` (or `-s`). **Fix:** `--source=`+source. Source-only restore
adds explicit `-- .` (git refuses a source restore with no pathspec).

### 3 — 🟠 Option injection in `git add`
`git add <path>` without `--`: a crafted path beginning with `-` becomes an
option. `["-A"]` → stages the whole tree; `["--pathspec-from-file=…"]` reads
an attacker-chosen file list. `paths` were normalized but never guarded.
**Fix:** `--` inserted before all user paths (list + single). Verified:
`git add -- -A` → `fatal: pathspec '-A' did not match` (inert). Dry-run `-n`
moved to right after `add` so it isn't swallowed by `--`.

### 4 — 🟡 `branch delete` safety inversion
`isDestructiveGitAction` returned true for `branch delete`, so the dispatcher
demanded `force=true`; but `gitBranch` maps `force`→`-D` (force delete). A user
could therefore never perform a safe `-d` (which git already refuses on
unmerged branches). **Fix:** removed `branch delete` from the destructive gate;
`-d` is the default, `force:true` opts into `-D`.

### 5 — 🟡 `restore` dry-run impossible
`git restore -n` and `--dry-run` both error "unknown switch/option" (git 2.34).
**Fix:** preview emulated with the equivalent `git diff` — `--cached` for
`staged`, `<source>` for source restore, working-tree diff otherwise — showing
exactly what the restore would overwrite.

### 6 — 🟢 Guard order (your noted pending item)
`rejectOptionLike("source"/"branch_name"/"commit_range")` now runs in the
dispatcher before the destructive gate, so option-like input is rejected
coherently regardless of gate/force state. Handler-level checks kept as
defense in depth.

---

## Not vulnerable (verified)

- **`restore` paths option injection** — already guarded by the `--` separator (line kept).
- **Command injection via `execGitCommand`** — args passed as an `exec.Command` slice, never shell-concatenated; the Windows `cmd /c git` fallback also passes args as a slice. OK.
- **Path/access control** — `IsPathAllowed(repoRoot)` enforced before every non-init action; `init` re-checks its target. OK.
- **`rejectOptionLike`** correctly rejects any leading-`-` ref/branch/range; git itself forbids such refs, so no legitimate use is lost.

---

## Verification

Validated against **git 2.34.1** — all green:

```
git restore --staged -- f.txt          OK (unstage)
git restore -- f.txt                    OK (discard WT)
git restore --source=HEAD~1 -- f.txt    OK
git restore --source=HEAD~1 -- .        OK (whole tree)
git add -- f.txt                        OK
git add -n -- f.txt                     OK (dry-run)
git add -- -A                           fatal: pathspec (inert — injection blocked)
git diff --cached / <source> / WT       OK (dry-run preview)
```

**Pending (run on Windows — no Go in audit sandbox):**

```bash
go build -ldflags="-s -w" -trimpath -o filesystem-ultra-v4.exe .
go test ./tests/... && go test ./core/... && go test -race ./...
```

Consider adding a `tests/git_restore_test.go` covering: staged unstage, WT
discard, `--source=` with/without paths, dry-run diff preview, and an
option-like path rejection for `git add`.
