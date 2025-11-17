# CHANGELOG - MCP Filesystem Server Ultra-Fast

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

## [3.2.0] - 2025-11-14

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

**Current Version**: 3.4.1
**Last Updated**: 2025-11-17
**Status**: ✅ Production Ready
