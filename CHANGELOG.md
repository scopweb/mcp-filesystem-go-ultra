# CHANGELOG - MCP Filesystem Server Ultra-Fast

## [Unreleased / 4.5.5] - 2026-05-30

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
