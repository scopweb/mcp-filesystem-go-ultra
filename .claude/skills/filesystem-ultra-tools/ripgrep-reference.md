---
name: ripgrep-reference
description: Ripgrep backend reference for filesystem-ultra v4.4.0: detection, parameters, and fallback behavior.
---

# Ripgrep Backend Reference (v4.4.0)

## Overview

When ripgrep (`rg`) is available, `search_files` with `output_format:"json"` uses ripgrep for 10-100x faster search on large codebases. The system falls back to Go-native regex transparently when ripgrep is not available.

## Detection Priority

1. **PATH search** — `rg --version` succeeds
2. **Embedded binary** — build with `embed_rg` tag
3. **Go-native fallback** — pure Go regex, no external dependency

## Ripgrep Parameters

When ripgrep is used, these parameters are passed:

| Flag | Purpose | When used |
|------|---------|-----------|
| `--json` | Structured JSON output | Always |
| `--max-filesize=10M` | Skip files >10MB | Always |
| `--ignore-case` | Case-insensitive search | `case_sensitive: false` |
| `-w` | Whole word match | `whole_word: true` |
| `-C N` | Include N context lines | `include_context: true` |
| `--ignore DIR` | Skip directory | For each in skip list |

### Skip Directories

Ripgrep is configured to skip these directories automatically:
- `.git`, `.svn`, `.hg`
- `node_modules`, `.next`, `.nuxt`, `dist`
- `bin`, `obj`, `.vs`, `packages`, `.nuget`
- `target`, `.gradle`
- `__pycache__`, `.venv`, `venv`, `.eggs`
- `vendor`, `.idea`, `.vscode`

## JSON Output Format

Ripgrep's `--json` output is parsed into the standard `SearchMatch` structure:

```json
{
  "type": "match",
  "data": {
    "path": { "text": "path/to/file.go" },
    "lines": { "text": "line content here" },
    "line_number": 42,
    "bytes": { "start": 100, "end": 120 }
  }
}
```

This is converted to `SearchMatch`:
```go
type SearchMatch struct {
    File       string   `json:"file"`
    LineNumber int      `json:"line_number"`
    Line       string   `json:"line"`
    Context    []string `json:"context,omitempty"`
    MatchStart int      `json:"match_start"`
    MatchEnd   int      `json:"match_end"`
}
```

## Performance Comparison

| Codebase Size | Go-native | Ripgrep | Speedup |
|---------------|-----------|---------|---------|
| ~100 files | ~200ms | ~50ms | 4x |
| ~1,000 files | ~1.5s | ~150ms | 10x |
| ~10,000 files | ~12s | ~300ms | 40x |
| ~100,000 files | ~90s | ~2s | 45x |

*Approximate values, varies by hardware and pattern complexity.*

## Embedded Binaries

When built with `embed_rg` tag, these binaries are included:

| Binary | Platform | Version |
|--------|----------|---------|
| `rg-windows-amd64.exe` | Windows x64 | 15.1.0 |
| `rg-linux-amd64` | Linux x64 | 15.1.0 |
| `rg-linux-arm64` | Linux ARM64 | 15.1.0 |
| `rg-darwin-amd64` | macOS Intel | 15.1.0 |
| `rg-darwin-arm64` | macOS Apple Silicon | 15.1.0 |

### Building with Embedded Ripgrep

```bash
# Without embedded ripgrep
go build -ldflags="-s -w" -trimpath -o filesystem-ultra-v4.exe .

# With embedded ripgrep
go build -ldflags="-s -w" -trimpath -tags embed_rg -o filesystem-ultra-v4-embed.exe .
```

### Downloading Additional Platforms

Run `embed/ripgrep/download.sh` or manually download from:
https://github.com/BurntSushi/ripgrep/releases

Rename downloaded binary to: `rg-{os}-{arch}` (e.g., `rg-linux-arm64`)

## Verifying Ripgrep Status

### At Startup (logs)
```
INFO Ripgrep detected for accelerated search version=14.x.x  # found
INFO Ripgrep not found - using Go-native search              # not found
```

### Runtime Check
Check `server_info(action:"stats")` output or look for `ripgrepAvailable: true` in engine state.

## Error Handling

If ripgrep fails during search, the system falls back to Go-native regex:

1. **Binary not found** → Use Go-native
2. **Permission denied** → Use Go-native, log warning
3. **Pattern incompatibility** → Use Go-native, log debug
4. **Timeout/cancellation** → Use Go-native, respect ctx cancellation

No user-facing error is shown for ripgrep failures — the fallback is transparent.

## Security

- `IsPathAllowed()` is called BEFORE ripgrep execution — path validation is identical to Go-native
- Ripgrep receives only the validated, normalized path
- No arbitrary command execution — ripgrep is called with controlled arguments only
- Skip directories prevent ripgrep from traversing irrelevant large directories