# CHANGELOG - MCP Filesystem Server Ultra-Fast

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

## [Unreleased]

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

**Current Version**: 3.7.1
**Last Updated**: 2025-12-03
**Status**: ✅ Production Ready
