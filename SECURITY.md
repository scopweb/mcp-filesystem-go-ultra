# Security Policy

## Supported Versions

| Version | Supported |
|---------|-----------|
| 4.x     | Yes       |
| 3.x     | Security fixes only |
| < 3.0   | No        |

## Reporting a Vulnerability

If you discover a security vulnerability in this project, please report it responsibly.

**Do NOT open a public GitHub issue for security vulnerabilities.**

### How to Report

1. **GitHub Private Reporting** (preferred): Use [GitHub's private vulnerability reporting](https://github.com/scopweb/mcp-filesystem-go-ultra/security/advisories/new) to submit a report directly.
2. **Email**: Send details to the repository maintainer via the email listed on the [GitHub profile](https://github.com/scopweb).

### What to Include

- Description of the vulnerability
- Steps to reproduce
- Affected versions
- Potential impact
- Suggested fix (if any)

### Response Timeline

- **Acknowledgment**: Within 48 hours
- **Assessment**: Within 7 days
- **Fix release**: Within 30 days for confirmed vulnerabilities

### Scope

The following are in scope:

- Path traversal or symlink escape from `--allowed-paths` boundaries
- Arbitrary file read/write outside allowed paths
- Command injection via tool parameters
- Backup ID manipulation or path traversal
- Denial of service via resource exhaustion
- Information disclosure through error messages

### Out of Scope

- Vulnerabilities in dependencies (report upstream)
- Issues requiring physical access to the machine
- Social engineering

## Security Features

This server includes several built-in security measures:

- **Path allowlist** (`--allowed-paths`): Restricts all operations to specified directory trees
- **Symlink resolution**: `filepath.EvalSymlinks()` before every path check prevents symlink escape
- **Cryptographic randomness**: Temp files and backup IDs use `crypto/rand`
- **Backup ID sanitization**: Only `[a-zA-Z0-9_-]` allowed, preventing path traversal
- **File permissions**: Temp files and metadata written with `0600`
- **No unsafe code**: Zero usage of Go's `unsafe` package in production
- **Risk assessment**: Destructive edits above configurable thresholds require explicit confirmation
