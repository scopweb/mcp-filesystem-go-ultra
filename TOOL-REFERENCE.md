# filesystem-ultra Tool Reference (v4.5.12 — 20 tools)

17 core + `git` (v4.5.2+) + `minify_js` (v4.5.7+) + `help` (discovery).

### Reading Files

| Need | Tool | Parameters |
|------|------|------------|
| Read full file | `read_file` | `path` |
| Read multiple files | `read_file` | `paths` (JSON array: `'["a.go","b.go"]'`) |
| Read specific lines | `read_file` | `path`, `start_line`, `end_line` |
| Read first/last N lines | `read_file` | `path`, `max_lines`, `mode` ("head" or "tail") |
| Read binary as base64 | `read_file` | `path`, `encoding: "base64"` |

### Writing Files

| Need | Tool | Parameters |
|------|------|------------|
| Write/create file | `write_file` | `path`, `content` |
| Write binary from base64 | `write_file` | `path`, `content_base64` or `content` + `encoding: "base64"` |

### Editing Files

| Need | Tool | Parameters |
|------|------|------------|
| Replace exact text | `edit_file` | `path`, `old_text`, `new_text` |
| Multiple edits same file | `multi_edit` | `path`, `edits_json` (array of `{old_text, new_text}`); optional `diff_format` (`auto`\|`full`\|`summary`\|`stat`\|`none` — aggregate diff of the whole batch), `dry_run`, `expected_hash` |
| Regex find-replace all | `edit_file` | `path`, `mode: "search_replace"`, `pattern`, `replacement` |
| Replace Nth match | `edit_file` | `path`, `old_text`, `new_text`, `occurrence: N` (1=first, -1=last) |
| Regex with captures | `edit_file` | `path`, `mode: "regex"`, `patterns_json` |

### Search

| Need | Tool | Parameters |
|------|------|------------|
| Search files | `search_files` | `path`, `pattern`, `file_types` (optional) |
| Content search | `search_files` | `path`, `pattern`, `include_content: true` |
| Advanced search | `search_files` | `path`, `pattern`, `case_sensitive: true`, `include_context: true` |
| Count pattern | `search_files` | `path`, `pattern`, `count_only: true` |

### File Operations

| Need | Tool | Parameters |
|------|------|------------|
| List directory | `list_directory` | `path`; optional `output_format` (`compact` default \| `json` structured entries \| `tree` recursive), `max_depth` (tree, default 2) |
| File info | `get_file_info` | `path` |
| File info (batch) | `get_file_info` | `paths` (JSON array: `'["file1.txt","dir/"]'`) |
| Copy | `copy_file` | `source_path`, `dest_path` |
| Move/rename | `move_file` | `source_path`, `dest_path` |
| Delete (soft) | `delete_file` | `path` |
| Delete multiple | `delete_file` | `paths` (JSON array: `'["a.txt","b.txt"]'`) |
| Delete (permanent) | `delete_file` | `path`, `permanent: true` |
| Create directory | `create_directory` | `path` |

> **Soft-delete response (v4.5.11+)** — every `delete_file` (soft mode, the default) returns a **Soft-Delete ID** in the response. The file is moved to `<--backup-dir>/filesdelete/<sd-id>/<basename>` with a `metadata.json` sidecar. Use `backup(action:"restore_trash", sd_id:"...")` to restore. **If `--backup-dir` is not configured**, soft-delete uses a legacy walk-up layout and the response will say `(legacy)` — the file is recoverable only by manual `move_file`. See section 12 below for full recovery flow.

### Batch & Pipeline

| Need | Tool | Parameters |
|------|------|------------|
| Multi-file atomic ops | `batch_operations` | `request_json` — see below |
| Multi-step pipeline | `batch_operations` | `pipeline_json` — see below |
| Batch rename | `batch_operations` | `rename_json` |
| Project-wide find/replace | `project_replace` | `path`, `find`, `replace`, `preview` (bool), `parallel` (bool) |

### Other

| Need | Tool | Parameters |
|------|------|------------|
| Backup management (pre-edit) | `backup` | `action` (list/info/compare/cleanup/restore/undo_last/undo_chain), `backup_id` |
| Restore pre-edit backup | `backup` | `action: "restore"`, `backup_id`, `file_path` (optional) |
| Undo last edit | `backup` | `action: "undo_last"`, `preview: true` (optional) |
| List soft-deleted files | `backup` | `action: "list_trash"`, `filter_path` (optional), `older_than_days` (optional), `limit` (default 50) |
| Restore soft-deleted file | `backup` | `action: "restore_trash"`, `sd_id` (required) |
| Permanently delete old trash | `backup` | `action: "purge_trash"`, `older_than_days` (default 7), `dry_run` (default true) |
| Analyze before doing | `analyze_operation` | `path`, `operation` (file/edit/delete/write/optimize) |
| Performance stats | `server_info` | `action: "stats"` |
| Help | `server_info` | `action: "help"`, `topic` (optional) |
| Artifact capture | `server_info` | `action: "artifact"`, `sub_action` (capture/write/info) |
| WSL sync | `wsl` | `wsl_path` or `windows_path`, `direction` |
| WSL status | `wsl` | `action: "status"` |

### Version Control (v4.5.2+)

| Need | Tool | Parameters |
|------|------|------------|
| Git operations | `git` | `action` (init/status/diff/log/add/commit/restore/branch), `path`, `message` (or `auto_message: true` for conventional commits) |

### JavaScript (v4.5.7+)

| Need | Tool | Parameters |
|------|------|------------|
| Minify JavaScript (pure Go, no Node) | `minify_js` | `path`, `content` (optional, in-memory) |

### Discovery

| Need | Tool | Parameters |
|------|------|------------|
| List all tools + capabilities | `help` | (no params) |

---

## Critical Rules

### 1. ALWAYS verify paths before using them
- Copy-paste paths exactly from `list_directory` or `search_files` results
- **Never retype paths from memory** — typos cause silent failures
- If a tool returns "file not found", double-check the path character by character
- **Caso documentado (2026-06-11):** un path con capitalización mal (ej. `estats.razor` en vez de `Estats.razor`) se resuelve correctamente en Windows (case-insensitive) pero el archivo editado downstream registra la clase con la capitalización del path pasado → errores de compilación que aparecen 3 capas más abajo (compilador Razor: `RZ10011 class estats`). **El path SIEMPRE debe copiarse de `list_directory` o `read_file`, nunca escribirse de memoria, especialmente cuando hay capitalización, acentos, o guiones vs underscores en juego.**

### 2. Read before editing
- **ALWAYS** read the file (or relevant range) before calling `edit_file` or `multi_edit`
- Use the exact text from the read result as `old_text`
- `old_text` must match the file content exactly — it's a literal match, not a regex

### 3. batch_operations format
Supported operation types: `write`, `edit`, `search_and_replace`, `copy`, `move`, `delete`, `create_dir`

```json
{
  "operations": [
    {"type": "edit", "path": "file1.cs", "old_text": "old", "new_text": "new"},
    {"type": "edit", "path": "file2.cs", "old_text": "old", "new_text": "new"},
    {"type": "search_and_replace", "path": "file3.cs", "old_text": "pattern", "new_text": "replacement"},
    {"type": "copy", "source": "a.txt", "destination": "b.txt"}
  ],
  "atomic": true,
  "create_backup": true
}
```

**Do NOT use types that don't exist** (e.g. `search_replace`, `find_replace`). Only the 7 types listed above.

### 4. Pipeline JSON — double-escape regex
Inside `pipeline_json`, regex backslashes need double-escaping because it's JSON-in-JSON:
- `\.` → `\\\\.`
- `\b` → `\\\\b`
- `\d+` → `\\\\d+`

For complex regex patterns, prefer using `edit_file` with `mode:"regex"` directly instead of inside a pipeline.

### 5. Large edits — use anchors
For replacing large code blocks (>10 lines):
1. Use a short, unique anchor line as `old_text` in `edit_file` to insert the new block
2. Then use a second `edit_file` to remove the remaining old block
3. **Do NOT** try to put 50+ lines of code as `new_text` inside `batch_operations` — the JSON escaping will break

### 6. One edit at a time on the same file
- After each `edit_file` on a file, re-read before the next edit
- The file content changes after each edit — stale `old_text` from a previous read will fail
- Exception: `multi_edit` handles multiple edits in one call (preferred for same-file changes)

### 7. edit_file modes
- Default mode: replaces ONE exact text match — use for targeted, precise edits
- `mode:"search_replace"`: replaces ALL occurrences of a pattern (regex or literal) — use for global refactors
- `mode:"regex"`: advanced regex with capture groups — use for complex transformations
- Always run `search_files` with `count_only:true` before global replace to verify impact

### 8. Dry Run Before Destructive Operations
- Use `analyze_operation` to preview the impact of write, edit, or delete operations
- Use `edit_file` with `dry_run: true` for regex mode to validate patterns
- Use `batch_operations` pipeline with `dry_run: true` to preview pipeline execution

### 9. Error recovery
- If `edit_file` says "old_text not found": re-read the file and try again with exact text
- If `batch_operations` fails: check the error for which operation failed and why
- If a tool returns no response (timeout): retry once, the file system may have been briefly locked

### 10. Disaster Recovery — UNDO and backup restore

**Backup format in responses:**
- Compact: `M file.go | 7@+7-0 | 42L | UNDO:20260501-123650 | chain:20260501-123649`
  - `UNDO:20260501-123650` — truncated ID (12 chars), use for display or `backup(action:"restore", backup_id:"...")`
  - `chain:20260501-123649` — parent backup ID, indicates previous version available for step-through undo
- Verbose: `✓ UNDO:20260501-123650-333c964cc3af7a82 ← chain:20260501-123649-...`

**Undo operations:**
- Quick undo (no backup_id needed): `backup(action:"undo_last")`
- **Step-through undo** (recomendado): `backup(action:"undo_last", file_path:"...")` — recorre la cadena de backups hacia atrás, uno a uno
- Preview next step: `backup(action:"undo_last", file_path:"...", preview:true)`
- Ver cadena completa: `backup(action:"undo_chain", file_path:"...")`
- Restore specific backup: `backup(action:"restore", backup_id:"20260501-123650-333c964cc3af7a82")` (full ID required)
- Find backups for a specific file: `backup(action:"list", filter_path:"filename")`

**Auto-verify integrity**: operaciones HIGH/CRITICAL incluyen verificación automática post-edit (legibilidad, tamaño, hash). Si el archivo quedó corrupto o truncado, se reporta inmediatamente.

For HIGH risk edits, verify the result: `read_file(path, mode:"tail")` to confirm file is complete.

Full recovery guide: `server_info(action:"help", topic:"recovery")`

### 11. Soft-delete recovery (v4.5.11+) — SD-ID and `restore_trash`

**Every soft-delete returns a Soft-Delete ID (SD-ID)** in the response — a string like `sd-20260611-150455-a1b2c3d4`. The file is moved to `<--backup-dir>/filesdelete/<sd-id>/<basename>` with a `metadata.json` sidecar. The SD-ID is the only handle the AI has to restore the file.

**Response shapes (v4.5.11+):**
```
Compact:
D foo.razor | SD:sd-20260611-150455-a1b2c3d4 | restore: backup(action:"restore_trash", sd_id:"sd-20260611-150455-a1b2c3d4")

Verbose:
OK: C:\path\to\foo.razor soft-deleted
   SD-ID: sd-20260611-150455-a1b2c3d4
   Trash: C:\backup-dir\filesdelete\sd-20260611-150455-a1b2c3d4\foo.razor
   Restore: backup(action:"restore_trash", sd_id:"sd-20260611-150455-a1b2c3d4")
   Or manually: move_file(<trash_path> → <original_path>)
```

**Restore flow (when `--backup-dir` is configured — recommended):**
1. Capture the SD-ID from the soft-delete response
2. If unsure the file is recoverable, list: `backup(action:"list_trash", filter_path:"foo.razor")`
3. Restore: `backup(action:"restore_trash", sd_id:"<sd-id>")`
4. The file moves back to its original location. Refuses to overwrite an existing file (returns 409 Conflict).

**Bulk cleanup:**
- `backup(action:"list_trash", older_than_days:7)` — preview what would be purged
- `backup(action:"purge_trash", older_than_days:7, dry_run:false)` — permanently delete entries older than 7 days
- Dashboard Trash tab provides a UI for the same actions.

**When `--backup-dir` is NOT configured** (legacy mode): the file goes to a parallel `filesdelete/` folder near the project root. The response will say `(legacy)` and **no SD-ID is returned** — recovery is only via manual `move_file`. Always pass `--backup-dir` to enable `restore_trash`.

**Prefer `write_file` for whole-file rewrites** — see Rule 12.

### 12. ⚠️ Nunca `edit_file` para rewrite completo (bug del 2026-06-11)

**Síntoma observado:** `edit_file(path, old_text=<15 líneas header>, new_text=<archivo completo ~150 líneas>)` produjo un archivo de **298 líneas con el SP/procedimiento duplicado**. El header se reemplazó correctamente, pero el resto del archivo viejo quedó concatenado debajo del `new_text`.

**Causa:** `edit_file` en modo default (`replace`) hace match EXACTO de `old_text` y sustituye **solo ese fragmento**. El resto del archivo permanece intacto. El "rewrite" que el modelo tenía en cabeza nunca ocurrió — solo se intercambió un bloque pequeño.

**Regla:** Cuando hay que reescribir un archivo entero o la mayoría de su contenido → usar **`write_file`** directamente, nunca `edit_file`.

**Heurística de decisión:**

| Situación | Herramienta correcta |
|-----------|---------------------|
| `len(new_text) > 2 * len(old_text)` y archivo tiene contenido más allá del match | **`write_file`** |
| Cambio pequeño y puntual (`len(new_text) ≈ len(old_text)`, mismo rango) | `edit_file` mode `replace` |
| Reemplazo global de un patrón (todas las ocurrencias) | `edit_file` mode `search_replace` |
| Renombrar tokens en árbol completo | `project_replace` o `batch_operations` con `search_and_replace` |
| Múltiples cambios pequeños en el mismo archivo | `multi_edit` con varios anchors |

**Truco anti-bug:** Antes de llamar `edit_file`, calcular mentalmente el ratio `len(new_text) / len(old_text)`. Si es > 2 y la operación no es un rename global, replantear con `write_file` o fragmentar en `multi_edit`.

**Detección server-side (✅ activa desde v4.5.10, 2026-06-11):** `edit_file` ahora BLOQUEA automáticamente este patrón. La guarda `core.CheckEditRewrite` dispara cuando se cumplen 3 señales: (1) `new_text > 2× old_text`, (2) el archivo tiene >50% de contenido fuera del match, (3) `new_text > 500 bytes` Y `>50%` del archivo. El bloque devuelve un error claro sugiriendo `write_file`. Override: pasar `force:true` — crea un backup de seguridad y aplica. El audit log registra `feedback_pattern: "accidental_rewrite"` cuando dispara.

### 13. Use filesystem tools — never bash alternatives
- **Search files** → `search_files` (never `grep`, `find`, `rg`)
- **Read files** → `read_file` (never `cat`, `type`, `bat`)
- **List directories** → `list_directory` (never `ls`, `dir`)

The filesystem MCP server provides structured, cached, annotated results. Bash commands bypass the cache, skip audit logging, and return untyped output.
