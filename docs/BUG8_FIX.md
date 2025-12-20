# Bug #8 Fix: File Content Destruction Prevention

**Status**: FIXED (v3.10.0)
**Date**: 2025-12-20
**Severity**: CRITICAL
**Impact**: Multiline edits with `recovery_edit`, `smart_edit_file`, `intelligent_edit`

## Problem Description

Claude Desktop would sometimes delete ALL file content except the edited portion when using multiline `old_text` with edit tools. This was a destructive bug that could result in significant data loss.

### Root Cause

The issue was a combination of factors:

1. **Fuzzy Matching Failure**: When `recovery_edit` tried fuzzy matching on multiline text with inconsistent line endings or whitespace, it would fail silently
2. **Silent Fallback**: Instead of returning an error, the code would continue with default behavior
3. **Incomplete Validation**: No pre-validation to confirm `old_text` actually exists in the file
4. **Post-Edit Verification Missing**: No verification that the edit actually worked

### Symptoms

- ❌ `recovery_edit` fails with "old_text not found"
- ❌ `smart_edit_file` fails with same error
- ✅ `intelligent_edit` sometimes works (redirects to recovery_edit)
- ✅ `batch_operations` works fine (has different code path)

## Solution: The "Blindaje" Protocol

Implemented a complete safety layer based on Claude Desktop's recommended protocol:

```
PROTOCOLO BLINDADO: 0 RIESGO DE DESTRUCCIÓN DE ARCHIVOS

REGLA 1: NUNCA editar sin verificación previa
├─ read_file_range() → capturar líneas exactas
├─ count_occurrences() → confirmar patrón existe
└─ analyze_edit() → simular cambio ANTES de ejecutar

REGLA 2: CAPTURA LITERAL del código a reemplazar
├─ Copiar EXACTAMENTE desde read_file_range()
├─ Incluir espacios, tabulaciones, saltos de línea
└─ NO confiar en fuzzy matching

REGLA 3: Operaciones atómicas con backup
├─ SIEMPRE atomic: true en batch_operations
├─ SIEMPRE create_backup: true
└─ Si falla → restore_backup() inmediato

REGLA 4: Recovery strategy
├─ Para cambios simples → recovery_edit()
├─ Para múltiples cambios → batch_operations con validación
└─ Para críticos → analyze_edit() primero

REGLA 5: Validación post-edición
├─ count_occurrences() después de editar
├─ Verificar línea específica con read_file_range()
└─ Si algo raro → rollback inmediato
```

## Implementation

### New File: `core/edit_safety_layer.go`

A comprehensive safety validator implementing the blindaje protocol:

```go
// EditSafetyValidator implements the blindaje protocol
type EditSafetyValidator struct {
    verbose bool // Enable detailed logging
}

// ValidateEditSafety performs comprehensive validation before edit
// - Captures exact file state
// - Tests exact matches first
// - Tests normalized matches (handles whitespace/line endings)
// - Searches for context matches
// - Provides detailed diagnostics
func (esv *EditSafetyValidator) ValidateEditSafety(
    filePath, oldText, newText string) *ValidationResult

// VerifyEditResult checks if an edit was applied correctly (REGLA 5)
func (esv *EditSafetyValidator) VerifyEditResult(
    filePath, oldText, newText string) (bool, string)

// RecommendedEditStrategy suggests the safest way to perform the edit
func (esv *EditSafetyValidator) RecommendedEditStrategy(
    validation *ValidationResult) string
```

### Validation Result

```go
type ValidationResult struct {
    IsValid              bool
    CanProceed           bool
    MatchFound           bool
    MatchCount           int
    Confidence           float64 // 0.0 to 1.0
    FileHash             string
    OldTextHash          string
    SuggestedAlternative string
    Diagnostics          ValidationDiagnostics
}

type ValidationDiagnostics struct {
    FileSize              int64
    FileEncoding          string        // UTF-8, Unknown/Binary
    LineEndingType        string        // CRLF (Windows), LF (Unix)
    OldTextLength         int
    OldTextLineCount      int
    NewTextLength         int
    NewTextLineCount      int
    ExactMatches          int
    NormalizedMatches     int
    FuzzyMatches          int
    TextNormalizationNote string
    ContextFound          bool
    ContextMatches        int
    ErrorDetails          string
}
```

### Detailed Edit Log

Complete diagnostics for every edit attempt:

```
═══════════════════════════════════════════════════
              DETAILED EDIT LOG
═══════════════════════════════════════════════════
Timestamp:     2025-12-20 14:30:45
Operation:     edit_file
File:          /path/to/file.cs
Status:        true
Execution:     125ms

📊 VALIDATION:
  Match Found:    true
  Match Count:    1
  Confidence:     100%
  File Hash:      a1b2c3d4
  Old Text Hash:  e5f6g7h8

📈 DIAGNOSTICS:
  File Size:      15234 bytes
  Encoding:       UTF-8
  Line Endings:   CRLF (Windows)
  Old Text:       520 bytes, 6 lines
  New Text:       285 bytes, 3 lines
  Exact Matches:  1
  Normalized:     0
  Context Matches:0

═══════════════════════════════════════════════════
```

## Workflow: How to Use Safely

### Step 1: Validate Edit Before Executing

```go
validator := core.NewEditSafetyValidator(true)
validation := validator.ValidateEditSafety(filePath, oldText, newText)

if !validation.CanProceed {
    log.Printf("❌ Cannot proceed: %s", validation.Diagnostics.ErrorDetails)
    log.Printf("💡 Suggestion: %s", validation.SuggestedAlternative)
    return
}

log.Printf("✅ Validation passed (Confidence: %.0f%%)",
    validation.Confidence * 100)
```

### Step 2: Execute Edit

```go
// With the validation passed, execute the edit
// The edit tools now perform pre-validation automatically
result, err := engine.EditFile(filePath, oldText, newText, force)
```

### Step 3: Verify Result

```go
// Always verify after critical edits
verified, msg := validator.VerifyEditResult(filePath, oldText, newText)
if !verified {
    log.Printf("⚠️  Verification failed: %s", msg)
    // Consider rollback here
}
```

## Safety Guarantees

After this fix:

✅ **Pre-validation**: All edits are validated before execution
✅ **Line Ending Normalization**: Handles both CRLF and LF correctly
✅ **Whitespace Handling**: Normalizes spaces and tabs
✅ **Context Detection**: Finds partial matches if exact text changed
✅ **Detailed Diagnostics**: Complete logging for debugging
✅ **Verification**: Post-edit verification confirms success
✅ **Atomic Operations**: Backup and rollback support
✅ **Recovery Strategy**: Recommends safe edit approach

## Testing

Comprehensive test coverage in `tests/edit_safety_test.go`:

```bash
# Run all edit safety tests
go test -v ./tests -run EditSafety

# Run specific scenario
go test -v ./tests -run EditSafetyMultilineScenarios

# Benchmark validation performance
go test -bench EditSafetyValidator ./tests
```

### Test Scenarios

1. **Exact multiline match** - 5+ line edits work correctly
2. **Single line match** - Simple replacements verified
3. **Nonexistent text** - Safely detects missing text
4. **Line ending variations** - Handles CRLF, LF, mixed
5. **Large multiline edits** - 100+ line scenarios
6. **Bug #8 exact reproduction** - Original problem fixed

## Migration Guide for Users

### Before (Risky)

```python
# ❌ NO VALIDATION - File could be corrupted
response = await client.call_tool(
    "filesystem-ultra:recovery_edit",
    {
        "path": "file.cs",
        "old_text": "...multiline text...",
        "new_text": "...new text..."
    }
)
```

### After (Safe)

```python
# ✅ WITH VALIDATION - Safe to proceed
response = await client.call_tool(
    "filesystem-ultra:read_file_range",
    {"path": "file.cs", "start_line": 10, "end_line": 20}
)
# Verify the exact text you want to edit

response = await client.call_tool(
    "filesystem-ultra:count_occurrences",
    {"path": "file.cs", "pattern": "old_text"}
)
# Confirm pattern exists

# OR use batch_operations for safety
response = await client.call_tool(
    "filesystem-ultra:batch_operations",
    {
        "operations": [
            {
                "type": "edit",
                "path": "file.cs",
                "old_text": "exact_text_from_read",
                "new_text": "replacement"
            }
        ],
        "atomic": true
    }
)
```

## For Claude Desktop Users

The `EditSafetyValidator` is designed to work with Claude Desktop's recommended protocol. When using `recovery_edit` or `intelligent_edit`:

1. **Always use `read_file_range()` first** to capture exact content
2. **Copy the text LITERALLY** - don't paraphrase or normalize
3. **Use `count_occurrences()` to confirm** the pattern exists
4. **Consider `batch_operations`** for multiline edits (more reliable)

## Performance Impact

The validation layer adds minimal overhead:

```
File Size    | Validation Time | Overhead
─────────────|─────────────────|──────────
1 KB         | < 1 ms          | negligible
10 KB        | < 5 ms          | negligible
1 MB         | 10-20 ms        | ~0.1%
100 MB       | 100-200 ms      | ~0.1%
```

## Known Limitations

⚠️ **Character Encoding**: Binary files may not validate correctly
⚠️ **Very Large Files**: 500MB+ files may be slow
⚠️ **Fuzzy Matching**: Doesn't handle semantic similarity
⚠️ **Concurrent Edits**: Assumes single-threaded file access

## Troubleshooting

### "old_text not found in file"

**Solution**: Use `read_file_range()` to see the exact content and copy it literally.

```python
# See what's really in the file
response = await client.call_tool(
    "filesystem-ultra:read_file_range",
    {"path": "file.cs", "start_line": 10, "end_line": 20}
)
# Copy exactly from the response
```

### "Context found but exact text not found"

**Solution**: The file was modified since you read it. Re-read and update your `old_text`.

### "Large multiline edit detected - use batch_operations"

**Solution**: Split your edit into smaller chunks or use `batch_operations`:

```python
response = await client.call_tool(
    "filesystem-ultra:batch_operations",
    {
        "operations": [
            {"type": "edit", "path": "...", "old_text": "line1", "new_text": "line1_new"},
            {"type": "edit", "path": "...", "old_text": "line2", "new_text": "line2_new"},
            {"type": "edit", "path": "...", "old_text": "line3", "new_text": "line3_new"},
        ],
        "atomic": true
    }
)
```

## Version History

- **v3.10.0** (2025-12-20): EditSafetyValidator implemented, Bug #8 fixed
- **v3.8.1** (2025-12-04): Backup and Recovery System (Bug #10)
- **v3.8.0** (2025-11-15): Initial Backup System

## References

- [Claude Desktop Protocol](guides/PREVENT_UNNECESSARY_SEARCHES.md)
- [Backup & Recovery System](docs/BUG10.md)
- [Windows Filesystem Persistence](guides/WINDOWS_FILESYSTEM_PERSISTENCE.md)
- [Edit Operations](core/edit_operations.go)
