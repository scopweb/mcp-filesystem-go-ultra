# Security Policy

## Supported Versions

| Version | Supported |
|---------|-----------|
| 4.x     | Yes       |
| 3.x     | Security fixes only |
| < 3.0   | No        |

These existing support declarations are retained pending maintainer confirmation, particularly security-only maintenance of 3.x. The technical model below describes the current 4.x implementation, not every historical release.

## Reporting a Vulnerability

If you discover a security vulnerability in this project, please report it responsibly.

**Do NOT open a public GitHub issue for security vulnerabilities.**

### How to Report

1. **GitHub Private Reporting**: The [private reporting link](https://github.com/scopweb/mcp-filesystem-go-ultra/security/advisories/new) is retained for when the feature is enabled. GitHub's repository API reported it **disabled on October 9, 2026**; do not assume this link currently accepts private reports.
2. **Maintainer contact**: The [GitHub profile](https://github.com/scopweb) lists a public email address. Its use as a security-reporting channel needs maintainer confirmation; no dedicated security address is documented here. If private reporting is unavailable, use the published contact to ask how to send a report privately before sharing sensitive details.

### What to Include

- Description of the vulnerability
- Steps to reproduce
- Affected versions
- Operating system, server flags, effective allowed directories, and relevant Roots, file-policy or hook settings (redact credentials)
- Potential impact
- Suggested fix (if any)

### Response Timeline (Maintainer Confirmation Pending)

The previously published response targets are retained below pending maintainer confirmation. This review has not verified that these deadlines can currently be met:

- **Acknowledgment**: Within 48 hours
- **Assessment**: Within 7 days
- **Fix release**: Within 30 days for confirmed vulnerabilities

### Scope

The following are in scope:

- Path traversal or symlink escape from the effective allowed-directory boundaries (CLI paths and MCP Roots, as described below)
- Arbitrary file read/write outside allowed paths
- Command injection via tool parameters
- Backup ID manipulation or path traversal
- Denial of service via resource exhaustion
- Information disclosure through error messages
- Vulnerabilities in dependencies that affect this server; include the affected dependency and a project-specific impact or reproduction. Coordinate with upstream where appropriate, without excluding reports to this project.

### Out of Scope

- Issues requiring physical access to the machine
- Social engineering

## Security Model

The server enforces application-level controls on its tool operations. It runs with the operating-system privileges of its process; it is not an OS sandbox and does not provide total isolation for Git, configured hook commands, or other processes. Treat client Roots, tool requests, repository content, Git configuration and hook inputs according to their trust level. Client-side approval and OS permissions remain separate controls.

### Allowed Paths and MCP Roots

- Normal server startup requires a non-empty `--allowed-paths` list (or positional paths) unless `--insecure-open` is supplied; otherwise startup exits with code 2. Roots received later do not replace this startup requirement.
- `--roots-mode=replace` is the default. Valid, non-empty client Roots replace the CLI list; an empty or unusable Roots list preserves the CLI list. A failed Roots request leaves the current list unchanged.
- `--roots-mode=union` combines CLI paths with client Roots. Either `replace` or `union` can authorize paths outside the original CLI list.
- `--roots-mode=ignore` ignores client Roots. Use it with a non-empty CLI allowlist to retain exclusively the configured CLI paths, for example `--allowed-paths="C:\Projects\Example" --roots-mode=ignore`.
- Roots are refreshed after initialization and Roots-change notifications; `list_allowed_directories` also refreshes them when a client session is available. Inspect that tool's effective list rather than assuming the CLI list is immutable.
- `--insecure-open` permits startup without an allowlist. An empty effective list removes directory containment restrictions, but path validation, the default secret-name denylist and any loaded file policy still apply. Supplying this flag does not erase a non-empty CLI list or prevent Roots from establishing one.

### Built-in Controls

This server includes several built-in security measures:

- **Path containment**: `IsPathAllowed()` compares resolved targets against effective allowed directories using boundary-aware `filepath.Rel` checks. Allowed bases are pre-resolved; existing targets use `filepath.EvalSymlinks()`, and new paths resolve through an existing ancestor. Relevant I/O routes also re-resolve and authorize the canonical target. These checks reduce symlink escape risk; they are not a guarantee of immutable filesystem state or OS-level isolation.
- **Temporary names and backup IDs**: Helpers use `crypto/rand` for random suffixes. `secureRandomSuffix()` has a timestamp fallback on random-source failure, so not every possible temporary name is guaranteed to be cryptographically random.
- **Backup ID sanitization**: Only `[a-zA-Z0-9_-]` allowed, preventing path traversal
- **File permissions**: Backup metadata and several internal files request POSIX mode `0600`. Other writes may preserve an existing mode or use `0644`, including streaming temporary files. POSIX mode bits do not configure equivalent Windows ACLs; access also depends on the platform and directory permissions.
- **Risk assessment**: Ordinary edit risk is informational; separate bulk-operation gates and other rejection controls are described below.
- **Path security layer** (`core/path_security.go`): Always-on checks for ADS, Unicode attacks, reserved names (see below)
- **WSL path containment**: WSL paths are subject to the effective allowed directories like other paths (no blanket bypass).
- **16-event hook system**: Configured, enabled hooks can add checks to supported read, write, edit, delete, create, move, copy and search routes. Coverage and payloads vary; rollback skips mutating hooks, streaming writes may pass metadata without content, and post-hook failure is not a general undo guarantee. Hooks execute trusted operator-configured commands, not isolated scanners.
- **File security policy** (`--file-security-config`): owner-controlled `normal` / `read_only` / `protected` / `hidden` rules. Not an OS sandbox. The dashboard must be given the same file; the server flag does not cover it. `git` and WSL sync are disabled while a policy is active.

### Secret-Name Protection and Additional Policy

By default, `IsPathAllowed()` denies paths whose case-insensitive basename is `.env`, starts with `.env.`, is `credentials.json`, `.npmrc` or `.pypirc`, starts with `id_rsa`, `id_ed25519` or `id_ecdsa`, or has extension `.pem`, `.key`, `.p12` or `.pfx` (`core/secrets.go`). This shared path check is used beyond reads, so the restriction can also reject writes and other operations on matching paths, even inside an allowed directory or in open-access mode. It is a name/extension denylist, **not a universal content-based secret detector**; credentials in other filenames are not identified by this check.

`--allow-secrets` disables this built-in path denial; it does not override containment or a loaded file policy. Search/list ignore handling also uses secret-path patterns, so enabling access is not a promise that every discovery route will include these files.

Enabled `pre-read` hooks with `failOnError:true` can add credential-path checks where those hooks run. A file policy is an independent path-rule layer: `read_only` permits reads but rejects mutations, `protected` allows discovery and redacted metadata but denies content access and mutations, and `hidden` suppresses discovery and returns not-found denials. The most restrictive applicable rule wins. Invalid policy configuration aborts startup; policy rules are immutable until restart. Neither hooks nor file rules provide universal content scanning.

### Edit Risk, Bulk Gates and Recovery

- Ordinary `edit_file` and `multi_edit` risk assessment uses configurable percentage/occurrence thresholds and path floors. `ChangeImpact.ShouldBlockOperation()` always returns false: risk alone does not require `force:true` or human confirmation. Ordinary edits use backup-backed commits and may return informational notices or integrity warnings.
- Pipeline `edit`, `multi_edit` and `regex_transform` steps block HIGH/CRITICAL bulk changes unless pipeline `force:true` is supplied (or the call is a dry run). `edit` uses 50 files or 500 occurrences for HIGH, and 80 files or 1,000 occurrences for CRITICAL. `multi_edit` uses 50 files or 50 edit definitions for HIGH, and 80 files or 100 edit definitions for CRITICAL. `regex_transform` uses file counts only: 50 for HIGH and 80 for CRITICAL.
- `project_replace` uses matching-file/replacement counts: 50 files or 500 replacements for HIGH, and 80 files or 1,000 replacements for CRITICAL. Without `force:true`, such an application request returns a blocked preview and writes nothing. `preview:true` also writes nothing.
- Independent checks can still reject an operation: path/secret/file-policy denial, read-only mode, configured pre-hooks, stale `expected_hash` or blocking automatic concurrency checks, accidental-rewrite and match-count guards, mutation budgets, resource limits, or I/O/backup failures. `force:true` is not a universal bypass and is **not evidence of human approval**.
- Backups and atomic writes aid recovery; bulk journal rollback is in-process, not crash-durable. It can report complete, partial or failed recovery and avoids overwriting files changed after the recorded mutation. It does not run mutating hooks or restore arbitrary directory trees. Streaming hook payloads and bulk backup options differ from ordinary edits; do not assume identical guarantees across routes.

---

## AI-Era Threat Mitigations and Technical History

**Indirect prompt injection remains partially mitigated.** Instructions embedded in repository files, downloaded HTML or comments may influence an AI client into requesting credential access or malicious writes. The filesystem server cannot fully resolve this at its layer. Use narrow effective allowed directories, secret-name protection, additional file rules or supported hooks, and client-side review/approval.

The repository's documented hardening history is retained below (including the AI-era hardening entry in the v4.2.1 changelog). Current behavior is described by the implementation, rather than the historical proof-of-concept examples:

| Historical vector | Mitigation retained in current code |
|-------------------|-------------------------------------|
| WSL blanket path bypass | `core/engine.go`: WSL paths no longer get an unconditional allow return; effective-directory containment applies. v4.6.0 subsequently introduced fail-closed startup. |
| NTFS Alternate Data Streams | `core/path_security.go`: on Windows, `hasNTFSAlternateDataStream()` rejects colons beyond the drive-letter syntax, such as `file.txt:stream`. |
| RTLO extension spoofing and zero-width hook evasion | `core/path_security.go`: rejects 18 explicitly listed code points (including U+202E, U+200B, U+FEFF and U+2028/U+2029), Unicode category Cf, and ASCII controls below 0x20. |
| Windows reserved device names | `core/path_security.go`: case-insensitive, extension-stripped basename checks reject device names such as `CON`, `NUL`, `COM0`–`COM9` and `LPT0`–`LPT9`, including on other platforms. |
| Cross-platform command-hook failure | `core/hooks.go`: command hooks use `cmd /C` on Windows and `sh -c` elsewhere. With `failOnError:true`, a nonzero exit code produces `HookDeny`; this is configuration-dependent, not automatic universal enforcement. |

Later changes removed the argument-concatenating Windows Git fallback, added explicit Git mutation parameters and introduced immutable file-policy rules. Their present limits are documented in the security model and Git section rather than treated as complete isolation guarantees.

---

## Git Tool Security

The `git` tool executes Git commands on behalf of the AI. It includes the following protections:

- **Access Control**: Actions other than `init` check the repository root against effective allowed directories; `init` checks its target path. This is not OS-level containment of Git's configuration, hooks or subprocesses. The entire tool is blocked while a file security policy is active.
- **Command Injection Protection**: Git execution uses argument passing rather than the former argument-concatenating Windows fallback. Option-like revisions, branch names and remote names are rejected, and pathspec arguments use `--` separators in relevant commands.
- **Working-tree restore**: `git(action:"restore", paths:["file.txt"], force:true)` is required to discard working-tree changes. Explicit non-empty `paths` are required; there is no implicit whole-tree restore.
- **Index restore**: `git(action:"restore", paths:["file.txt"], staged:true)` does not require `force:true`. It restores the index without modifying the working tree; an optional `rev` selects the source revision. This can change staged content and should still be reviewed.
- **Branch deletion**: `git(action:"branch", name:"feature/old", delete:true)` runs `git branch -d`, retaining Git's merged-branch check. Adding `force:true` escalates to `git branch -D`. A name alone never requests deletion, and `delete:true` cannot be combined with `checkout:true`.
- **Network opt-in**: Push/fetch require server startup with `--git-network` (off by default). This is a gate for those tool actions, not a network sandbox for every Git subprocess. Force push uses `--force-with-lease`; an optional `--git-remote-allow` restricts destinations and is not bypassed by `force:true`.
- **Hook Integration**: `init` runs create hooks; restore and branch deletion run delete hooks, and other supported mutations have action-specific hooks. This does not imply hook coverage of every internal Git file change.

The tool is annotated `destructiveHint:true`. Neither this annotation nor an agent-supplied `force:true` proves human approval; approval must be enforced by the client or operator.

---

## Path-aware risk (v4.7)

For ordinary edits, these path floors only raise `ChangeImpact.RiskLevel` (never lower it) and do not create a confirmation gate. `_test.go`, `README*` and paths under `docs/` have no floor; percentage/occurrence risk still applies. Secret-name paths are excluded from floor calculation and denied by default unless `--allow-secrets` is enabled.

| glob / class | floor |
|--------------|--------|
| Paths under `.github/workflows/`, basename `Dockerfile`, `go.mod`, `go.sum`, or suffix `.csproj` | high |
| `**/*_test.go` | none (does not raise) |
| `cmd/**`, `internal/**` if change% ≥ medium threshold | medium |
| `README*`, `docs/**` | none |

---

## Known Residual Risks

| Risk | Severity | Status | Recommendation |
|------|----------|--------|----------------|
| Indirect Prompt Injection | HIGH | Partially mitigated | Use a narrow effective allowlist (`--roots-mode=ignore` for CLI-only scope), secret-name protection and additional file rules/hooks; enforce user review in the client. |
| WSL path enumeration (`--insecure-open`) | MEDIUM | Mitigated (v4.6.0 fail-closed) | Do not use `--insecure-open` in production |
| Hook JSON content injection (file content in HookContext.Content) | LOW | Accepted | Hook scripts should treat HookContext as untrusted input |
| Bulk hook/recovery coverage | MEDIUM | Limited by operation | Forward operations have route-specific hooks; journal rollback skips mutating hooks and may be partial or failed. Inspect recovery results. |
| Pipeline regex_transform + large file hooks | LOW-MEDIUM | Partially mitigated (2026) | `regex_transform` now runs pre/post-edit hooks. Content is provided, but StreamingWriteFile for very large files only passes metadata. |
| Git tool command injection on Windows | MEDIUM | **Mitigated (2026)** | Removed dangerous string concatenation in `execGitCommand` fallback. Arguments are now passed properly. |
| Destructive Git requests without human approval | MEDIUM | Parameter gates, not human confirmation | Working-tree restore requires `force:true`; index-only restore (`staged:true`) does not. Branch deletion requires `delete:true` (`-d`); `force:true` escalates to `-D`. Enforce approval in the client. |
| ReDoS via regex patterns in `search_files` | LOW | Mitigated | `CompileRegex` uses Go's non-backtracking `regexp` engine (RE2 syntax); backreferences are rejected. This does not eliminate resource use from large inputs or broad searches. |
