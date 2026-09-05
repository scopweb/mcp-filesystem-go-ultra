# Plan de mejoras — filesystem-ultra como FS MCP de agentes 2026

Estado base: **v4.5.38**. Objetivo: dejar de ser “un filesystem Go muy bueno” y ser el filesystem que un agente 2026 espera **sin** copiar el oficial hacia abajo ni reabrir el catálogo 59→20.

Principio: **no más tools de write**. Extender handlers, flags y protocolo. Tools nuevas solo si el modelo las busca por nombre o cierran un workflow que hoy no existe.

No toques OCC, backups, rewrite-guard, pipelines ni reabrir 59 tools. v4.6 = el agente deja de adivinar rutas, fail-closed, exploración/edición que los clientes MCP ya esperan.

---

## Estado vs reflexiones finales (2026-09-03)

Rama `feat/4.6-fail-closed`. A y B **ya están en código**. Lo de Z.ai que **no** adoptamos: tokens-por-lenguaje, `git ls-tree` como walker, tool `refresh_roots`, `subscribe` en v1.

| Decisión nueva | ¿Adoptar? | Qué hay hoy |
|---|---|---|
| D1 fail-closed | Sí, hecho | Exit 2 sin CLI y sin `--insecure-open`. Roots llegan **después** del handshake: hace falta CLI (o insecure) para arrancar. Vacío de roots del cliente **no** tumba el proceso ni borra CLI. |
| D2 roots pisan CLI | Sí, hecho | `--roots-mode=replace` default. `list_allowed` ya pone `source: cli\|roots\|union`. |
| D3 perfiles strict/ultra | **Aplazar** a post-C | Default `ultra` implícito (22 tools). No bloquea el tag. |
| D4 nombres canónicos | Parcial | `list_allowed_directories` + `directory_tree` hechos. `diff_files` + `apply_patch` = Sprint C. `read_text_file` **no** reabrir ahora. |
| D5 schema+sweep **mismo commit** | **No para A/B ya shipped** | `experimental.go` **panics** si una tool experimental declara `outputSchema`. A/B están en esa lista (`4.6.0`). Cambiar la policy a mitad de 4.6.0 rompe CI. **Para C:** o bien (1) `apply_patch`/`diff_files` salen experimentales sin schema (igual que A/B) y gradúan el ciclo siguiente, o (2) se cambia la policy a “schema permitido si hay sweep en el mismo commit” **antes** de C. Recomendación: **(1)** hasta tag 4.6.0; (2) en 4.6.1. |
| D6 envelope de error unificado | Sí, **antes de C** (A.4) | Hoy: texto + hint `FILESYSTEM MISMATCH?`. No hay `{error:{code,message,path,details,suggestion}}`. No reescribir 22 handlers de golpe: helper + mutadores/lectores de path; códigos nuevos en tools nuevas. |
| Reconsultar `roots/list` en cada `list_allowed_directories` | Sí, **A.4** | Hoy solo en `initialized` + `list_changed`. Sin `refresh_roots`. El fallback de Z.ai es reconsultar al listar. |
| Invalidar caché al cambiar roots | Sí, **A.4** | `SetAllowedPaths` no limpia BigCache. |
| `read_file(paths)` batch parcial | Ya hecho | Un fallo no aborta el lote (`ERROR:` por fichero). Falta array nativo (D, no C). |
| Tree cache mtime + `sort_by` | No en 4.6.0 | `max_nodes=500` es el backpressure. Perf gate 10k/500 &lt;2s = test opcional, no bloquea C. |
| `apply_patch` multi best-effort | **No en v1** | Un archivo, fail-closed. Multi = `results[]` más adelante. EOL del fichero destino, no del patch. Headers `a/` `b/` → allowlist, no exigir `C:\`. |
| Sprint E subscribe | **No en v1** | Coincide: resources `file://` + `listChanged` en 4.7; sin fsnotify por fichero. |

### A.4 — remiendos de protocolo (hacer **antes** de Sprint C)

Pequeño, cierra el “done” de A según el texto nuevo:

1. `list_allowed_directories` llama `RequestRoots` si el ctx tiene sesión (mismo `applyFromClient`). Sin API / error → lista lo que hay; `help()` documenta “vuelve a listar tras cambiar roots”.
2. `SetAllowedPaths` invalida caché de ficheros (`cache.Invalidate` / wipe de paths que ya no están en el sandbox).
3. Helper `pathError(code, message, path, details, suggestion)` usado en discovery + próximos handlers de C. Códigos: `NOT_ALLOWED`, `NOT_FOUND`, `OCC_MISMATCH`, `REWRITE_BLOCKED`, `ROOTS_EMPTY`. El resto (`SECRET_DENIED`, `READ_ONLY`, `PATCH_APPLY_FAILED`) se añade con la tool/flag que los dispara.
4. Tests: segundo `list_allowed` con mock `SessionWithRoots` cambia el listado; envelope en un acceso denied.

**No** cambiar default `max_depth` 2→3 (rompe compact). Alias `directory_tree` ya default 2; documentar, no retocar.

### Orden restante hasta tag v4.6.0

```
A.4 (reconsulta roots + envelope mínimo + cache)
  → prueba real Claude Desktop / VS Code
  → C (diff_files + apply_patch 1-file + write_file append)
  → tag v4.6.0
```

D (readonly, denylist, mime/sha256, paths nativo) y E (resources, sin subscribe) **no** bloquean el tag.

---


## 0. Ya ganado — no tocar, no clonar el oficial

| Ventaja | Dónde | Qué no hacer |
|---|---|---|
| OCC + `content_hash` / `expected_hash` / `--auto-occ` | `tools_core.go`, `core/feedback.go` | No sustituir FNV de sesión por SHA-256 en el token OCC (rompería encadenado). SHA-256 es extra en `get_file_info`. |
| Backups + undo chain + trash | `core/backup_manager.go`, tool `backup` | No reimplementar compare: ya hay `backup(action:"compare")`. |
| Rewrite-guard + risk + structure-check | `core/feedback.go`, `core/structure_check.go` | No relajar `allow_rewrite` vs `force`. |
| Pipelines / batch atómico | `batch_operations`, `core/pipeline_*.go` | No añadir tool nº 21 de “batch”. Reducir round-trips aquí gana más. |
| Windows: ADS, RTLO, reserved names, TOCTOU | `core/path_security.go` | El análisis original está **desactualizado**. El oficial es más simple; nosotros ya somos más hostiles a Windows/WSL. Seguir endureciendo, no rehacer. |
| Anotaciones MCP + `outputSchema` | 4 I/O core en `output_schemas.go` | Ampliar schemas al **graduar**, no a tools experimentales (`experimental.go` panics). |
| `read_file(paths)` batch | `tools_core.go` | Ya existe. Falta **nombre canónico** + array nativo. |
| `list_directory(output_format:"tree")` | `ListDirectoryTree`, depth default 2 | Ya existe el modo. Falta gitignore / exclude / nombre `directory_tree`. |
| Policy experimental 1 ciclo | `experimental.go` | Toda tool/modo nuevo entra aquí. 17 core congelados salvo bugfix. |

**No añadir:** más git (rebase/merge/push agresivo), `minify_js` como tool de FS (mover a `--profile=ultra` o binario/skill), shell/process, 20 tools más.

---

## 1. Gap real (análisis vs código)

### P0 — protocolo y descubrimiento (duele en clientes)

| Ítem | Hoy | Qué falta |
|---|---|---|
| `list_allowed_directories` | Solo flags / `GetAllowedPaths()` interno. El modelo no lo ve. | Tool de solo lectura, 0 params, lista roots efectivos. |
| MCP Roots | SDK **ya tiene** `server.WithRoots()` + `RequestRoots` (`mcp-go v1.0.0`). Ultra no lo usa. | Handshake + `notifications/roots/list_changed` → pisan `AllowedPaths`. |
| Fail-closed | `main.go`: `AllowedPaths: []` = disco entero. README lo documenta. | Exigir ≥1 root (CLI **o** Roots del cliente). `--insecure-open` escape hatch. |
| Resources `file://` | 0. SDK: `WithResourceCapabilities`, `AddResource`. | Template `file://{+path}` bajo allowed roots. Watch/subscribe **después**. |

### P1 — exploración (el oficial sí da; nosotros a medias)

| Ítem | Hoy | Qué falta |
|---|---|---|
| Árbol acotado | `list_directory` tree JSON, sin exclude, sin `.gitignore` | gitignore-aware + `exclude` + cap de nodos. Alias `directory_tree`. |
| Sizes vs listing | JSON ya trae `size`. Compact no. | No crear `list_directory_with_sizes`. `output_format:"json"\|"sizes"` basta. |
| Multi-read | `paths` como **string JSON** | Array nativo + alias `read_multiple_files` / `read_text_file`. |
| Media | `encoding:"base64"` → texto | `ImageContent` / `AudioContent` + MIME (`mcp.NewToolResultImage`). |

### P1 — edición agente 2026

| Ítem | Hoy | Qué falta |
|---|---|---|
| `apply_patch` | `edit_file` / `multi_edit` = replace, no hunks | **Única tool nueva de edición.** Diff → dry-run → apply, con backup + OCC + rewrite-guard. |
| `diff_files` | diffs de edit + `backup compare` | Comparar dos paths (o file vs backup) como tool/acción explícita. |
| `write_file` append | no | `mode:"append"` atómico (temp+rename o open-append con lock). Logs/NDJSON. |

### P2 — higiene y seguridad extra

| Ítem | Hoy | Qué falta |
|---|---|---|
| gitignore | ripgrep corre `--no-ignore` a propósito (paridad con walk nativo) | Default **respetar** `.gitignore` / `.cursorignore`; `no_ignore:true` para el caso `.env`. |
| Denylist secretos | sandbox de paths no basta si root = home del proyecto | `.env`, `*.pem`, `id_rsa`, `*.key`, `*.p12` — fail-closed en read/search. |
| `--readonly` | no | Bloquea write/edit/delete/git-mutating. Review agents. |
| Stat enriquecido | `get_file_info` size/perm/dates | `mime`, `sha256`, `token_estimate` opcionales (`hash:true`). OCC sigue en FNV. |

---

## 2. Tres decisiones de producto (cambiar ahora)

### D1 — Default seguro

```
allowed = CLI --allowed-paths | positional args | MCP Roots
si len(allowed)==0 Y no --insecure-open → EXIT 2
Roots del cliente (si el cliente declara roots) pisan args
IsPathAllowed sigue resolviendo symlinks; security checks (ADS/RTLO) always-on
```

Migración: Claude Desktop configs actuales que omiten paths **rompen**. Documentar en CHANGELOG + README un bloque de 5 líneas. Flag `--insecure-open` para labs.

### D2 — Nombres que el modelo ya conoce, sin reabrir 13 aliases

Reactivar **solo** discovery aliases (mismo handler, 0 lógica nueva):

| Alias | Target | Por qué |
|---|---|---|
| `list_allowed_directories` | nuevo handler, no alias | El oficial lo llama al arrancar. |
| `directory_tree` | `list_directory` tree | Primera tool de orientación. |
| `read_text_file` | `read_file` | Nombre oficial. |
| `read_multiple_files` | `read_file(paths)` | Entrenado con ese nombre. |

**No** reactivar: `View/Edit/Write/LS/GlobTool/GrepTool`, `fs` super-tool, `search/edit/write/create_file`.

`paths` pasa a **array nativo** (como `git.paths`) **y** sigue aceptando string JSON (normalizer). El validador ya tiene `ParamArray`.

### D3 — Perfiles, no 40 tools

```
--profile=strict  → 12 core FS + help
                  read_file, write_file, edit_file, multi_edit, list_directory,
                  search_files, get_file_info, move_file, copy_file, delete_file,
                  create_directory, help
                  + list_allowed_directories, apply_patch (cuando existan)

--profile=ultra   → strict + batch_operations, backup, analyze_operation,
                    project_replace, wsl, git, server_info
                    minify_js queda experimental / fuera del default ultra

--profile=compat  → ultra + 4 aliases de D2
```

Default: `ultra` (no romper usuarios actuales). `strict` para clientes con lazy tool loading.

Implementación: `registerTools` filtra por set. `help()` y `serverInstructions` reflejan el perfil. `tools/list_changed` ya está habilitado (`WithToolCapabilities(true)`).

---

## 3. Sprints (techo vs “los mejores”)

Un sprint = un release cycle. Experimental un ciclo → sweep `output_schema_sweep_test.go` → graduar.

### Sprint A — protocolo (P0)  ← el que más sube el techo

1. Fail-closed + `--insecure-open`
2. `list_allowed_directories` (experimental)
3. MCP Roots: `server.WithRoots()`, `RequestRoots` post-initialize, apply → `engine.config.AllowedPaths` + `resolveAllowedPaths()`, notification handler
4. Roots pisan CLI; union si `--roots-mode=union` (default **replace**)
5. Tests: no-args exit 2; roots swap en caliente; list_allowed refleja roots

**No** resources todavía. **No** nuevas tools de I/O.

Archivos: `main.go`, `core/engine.go` (`SetAllowedPaths` runtime-safe), `tools_platform.go` o `tools_discovery.go` nuevo, `experimental.go`.

### Sprint B — exploración gitignore-aware

1. `core/ignore.go`: parser `.gitignore` + `.cursorignore` + `.fsultraignore` (gitignore syntax)
2. `ListDirectoryTree(path, opts)`: `max_depth`, `exclude []string`, `respect_ignore bool` (default true), `max_nodes` (default 500)
3. `search_files`: default respeta ignore; `no_ignore:true` restaura comportamiento actual (necesario para secretos en `.env` gitignored — pero denylist P2 los tapa igual)
4. Alias `directory_tree` → mismo handler con `output_format` default `tree`
5. Compact tree (indentado) además de JSON — el JSON actual es ruidoso para orientación

Ripgrep: quitar `--no-ignore` por defecto; pasar `--ignore` + extra ignore files. Paridad nativo/rg **obligatoria** (ya hay tests de paridad).

### Sprint C — `apply_patch` + `diff_files` (gap de producto #1)

Ver schemas en §4. Experimental. Backup + OCC + rewrite-guard + `dry_run`. Reusa `core/diff.go`, `atomicWriteFile`, `renameWithRetry`.

`diff_files`: dos paths **o** `path` + `backup_id`. No clonar `backup compare`; extraer helper compartido.

`write_file mode:"append"`: mismo sprint, es un param, no una tool.

### Sprint D — interop de nombres + media + readonly + denylist

1. Aliases D2 + `paths` array nativo
2. `read_file`: si MIME image/audio → `ImageContent`/`AudioContent` (cap tamaño, p.ej. 8 MB)
3. `--readonly` + denylist secretos (capa `IsSecretPath` en `IsPathAllowed` / read/search)
4. `get_file_info`: `hash` (sha256), `mime`, `token_estimate` (~chars/4)

### Sprint E — resources `file://` + subscribe

1. `WithResourceCapabilities(subscribe=true, listChanged=true)`
2. Template `file://{+path}` → mismo motor que `read_file` (allowed + denylist + OCC hash en metadata)
3. Subscribe → reusar `core/watcher.go` (hoy invalida cache). Emit `notifications/resources/updated`
4. **Después** de A–D. Watch sin Roots/fail-closed es un aviso en un sandbox abierto.

---

## 4. Schemas concretos (alineados a handlers actuales)

Convenciones ya usadas: `NormalizePath` al entrar, `param_validator` estricto, `auditWrap`, compact vs verbose, `content_hash` FNV-1a 8 hex, backup_id `timestamp-random`, `dry_run` no escribe, experimental **sin** `outputSchema` el primer ciclo.

### 4.1 `list_allowed_directories`

```
name: list_allowed_directories
experimental: 4.6.0
annotations: readOnly, idempotent, not destructive
input: {}   # cero params — los agentes oficiales no pasan nada
text:  una ruta absoluta por línea (canonical, post-EvalSymlinks)
structured (al graduar):
  { "directories": ["C:\\proj"], "source": "cli"|"roots"|"union", "readonly": false }
```

Handler: `engine.GetAllowedPaths()` resueltos. Si fail-closed nunca llega vacío. Si `--insecure-open`: devolver `["*"]` + warning en text (el oficial a veces lista el cwd; nosotros no fingimos un root).

`server_info(action:"help")` y `help()` lo listan primero: “call this before the first read”.

### 4.2 MCP Roots (no es tool)

```
NewMCPServer(..., server.WithRoots())

post initialize:
  if client.capabilities.roots:
    res, err := s.RequestRoots(ctx, mcp.ListRootsRequest{})
    applyRoots(res.Roots)  // file:// URI → path Windows/WSL via NormalizePath

on notifications/roots/list_changed:
  RequestRoots de nuevo
  engine.SetAllowedPaths(paths)  // mutex + resolveAllowedPaths
  // no tools/list_changed (las tools no cambian; el sandbox sí)
```

`SetAllowedPaths` es el único hueco de engine hoy (tests mutan `config.AllowedPaths` a pelo y disparan re-resolve por len mismatch). Hacerlo API formal con lock.

URI: aceptar `file:///C:/proj`, `file:///c:/proj`, `file://wsl.localhost/Ubuntu/home/...`. Reusar `core/path_converter.go`.

### 4.3 `list_directory` / alias `directory_tree`

Params **nuevos** (no romper los 3 actuales):

```
path            string   required
output_format   compact|json|tree|sizes     # sizes = compact + size human
max_depth       number   default 2 (tree)
exclude         array|string JSON globs     # ["node_modules","*.min.js"]
respect_ignore  bool     default true
max_nodes       number   default 500
```

Alias `directory_tree`: si `output_format` omitido → `tree`. Mismo `listDirHandler`.

Tree compact (nuevo default del alias, JSON sigue disponible):

```
proj/
  cmd/
    dashboard/ ...
  core/
    engine.go  48k
    ...
  [truncated: max_nodes=500, skipped gitignore=1204]
```

Engine: extender `ListDirectoryTree(ctx, path, TreeOpts)`. Skip dirs = `searchSkipDirs` ∪ gitignore ∪ exclude. Symlinks: no seguir (paridad `copyDirectory`).

### 4.4 `read_file` — array nativo + media (sin tool nueva)

```
path     string
paths    array<string> | string JSON   # normalizer acepta ambos
...existentes...
media    auto|never|always   default auto
```

`auto`: MIME image/* o audio/* y size ≤ cap → `NewToolResultImage` / audio. Text fallback siempre presente (clientes que ignoran image blocks). `content_hash` se queda en structured / text para no-media.

Normalizer: si `paths` es `[]interface{}`, no exigir string. `param_validator`: `paths` `ParamArray` **o** string — el validador actual es un tipo; añadir `ParamStringOrArray` o dual-register. Preferir dual en normalizer (coerce array→queda array; string JSON→array) y declarar `ParamArray`.

Alias `read_multiple_files` / `read_text_file` → `readFileHandler`. `read_text_file` con media auto sigue pudiendo devolver image (documentar; el nombre miente un poco, igual que el oficial).

### 4.5 `apply_patch` — tool nueva experimental

No es un modo de `edit_file`. Los agentes buscan este nombre. Un modo más en `edit_file` se pierde en 7 modos.

```
name: apply_patch
experimental: 4.6.x
annotations: not readOnly, not idempotent, destructive=false (backup+atomic)
input:
  path            string   required   # archivo destino (1 file / call; batch = N calls o paths+multi-file patch)
  patch           string   required   # unified diff (--- a / +++ b / @@)
  dry_run         bool     default false
  expected_hash   string   optional   # OCC del archivo actual
  allow_rewrite   bool     default false  # si el patch toca >50% del archivo
  force           bool     default false  # umbral de riesgo, NO el rewrite-guard
  diff_format     auto|full|summary|stat|none
  create_backup   bool     default true
```

Reglas (fail-closed, estilo Codex/j0hanz):

1. Parse unified diff. Un solo archivo por call en v1 (el header `+++` debe coincidir con `path` o ser `/dev/null` para create). Multi-file patch → error con “split into N apply_patch calls”.
2. Context lines deben matchear. Si fallan: error con hunk nº + ancla, **no** fuzzy apply en v1.
3. `dry_run`: mismo output que apply (diff_format, risk, backup_id vacío) sin write.
4. Apply: backup → splice hunks → `CheckEditRewrite` sobre old vs new → `atomicWriteFile` + `renameWithRetry` → `RecordWriteHash` + structured como `edit_file`.
5. Create (`+++` file, `--- /dev/null`): equivalente a `write_file` con backup si existía.
6. Delete (`+++ /dev/null`): equivalente a `delete_file` soft.

Reuso: `core/diff.go` (hunks), `core/splice.go`, `core/feedback.go`, `editStructuredFromContents`.

**No** outputSchema el primer ciclo. Al graduar: mismo shape que `edit_file` + `"hunks_applied": n`.

Parser: stdlib, no dependencia. Tests: LF/CRLF, new-file, delete-file, context mismatch, OCC fail, rewrite-guard, Windows path en header (`--- a/core/engine.go`).

### 4.6 `diff_files`

Opción A (preferida, 0 tools nuevas): `backup` ya tiene `compare`. Añadir:

```
action: "diff"
file_path: A
dest_path: B          # si omitido, vs último backup de A
diff_format: ...
```

Opción B (si los agentes buscan el nombre): tool experimental `diff_files` readOnly, params `path_a`, `path_b`. Handler delgado sobre `core.DiffFiles(a,b)`.

Sprint C arranca con A; alias de nombre B solo si el skill/README no basta.

### 4.7 `write_file` append

```
mode: "write" (default) | "append"
```

Append: no usa rewrite-guard. Backup del original. OCC opcional (`expected_hash` del file pre-append). Atómico: leer+concat+`atomicWriteFile` para archivos < umbral; para logs grandes, `O_APPEND` + flush (documentar no-OCC en ese path).

### 4.8 Denylist + `--readonly`

```
core/secrets.go
  defaultDeny = []glob{".env", ".env.*", "*.pem", "*.key", "id_rsa", "id_ed25519", "*.p12", "*.pfx", "credentials.json", ".npmrc", ".pypirc"}
  IsSecretPath(path) bool
```

Read/search/tree: error `secret file denied (override with --allow-secrets)`. Write/edit a secretos: denied siempre (ni `--allow-secrets` — un agente no debe reescribir `.env`). Flag `--allow-secrets` solo para read, labs.

`--readonly`: middleware en `addTool` / `auditWrap`. Tools con `destructiveHint` o lista explícita (`write_file, edit_file, multi_edit, delete_file, move_file, copy_file, create_directory, batch_operations, project_replace, git mutating, minify_js, backup restore`) → error `server is --readonly`. `git status/diff/log/show` OK.

---

## 5. Restricciones de implementación (no romper lo que diferencia)

1. **17 core congelados** salvo params aditivos backward-compat (`mode`, `exclude`, `paths` array). Behavior change de default (gitignore on, fail-closed) va en **minor 4.6.0** con CHANGELOG ruidoso, no en 4.5.x patch.
2. Toda tool nueva → `experimentalFeatures` + **sin** `outputSchema` + tests handler-level. Graduar el release siguiente.
3. `param_validator` + `normalizer` + `help(tool:)` + skill `filesystem-ultra-tools` se actualizan **en el mismo commit** que el handler. Si no, el modelo llama mal.
4. No bash en tests de comportamiento FS; tests Go (`go test ./...`). Smoke stdio para Roots/fail-closed.
5. Compact mode: `list_allowed_directories` y tree compact son el default útil en Claude Desktop.
6. WSL: Roots `file://` y allowed paths siguen pasando por `NormalizePath`. Fail-closed no puede dejar `/mnt/c` abierto si el root es `C:\proj`.
7. Contador de tools: `strict` ~14, `ultra` ~22 (20 actuales − minify opcional + `list_allowed_directories` + `apply_patch` + 0–2 aliases). Techo duro: **24 registrados**.
8. `minify_js`: no borrar código; no registrar en `strict` ni en default `ultra` a partir de 4.6. `--enable-minify` o profile `full`.

---

## 6. Orden si solo hay un sprint

Hacer **Sprint A completo**. Con 1 de 3 (list_allowed + Roots + fail-closed) dejas de ser un FS “que el agente adivina”. `apply_patch` y tree gitignore son el segundo sprint; sin sandbox visible no importan.

Criterio de hecho Sprint A:

- `filesystem-ultra.exe` sin args → exit 2, mensaje con `--allowed-paths` / Roots
- cliente con roots → `list_allowed_directories` lista esas rutas, no las de CLI
- `go test ./...` verde
- skill + `help()` mencionan “call list_allowed_directories first”
- CHANGELOG 4.6.0 breaking: fail-closed

---

## 7. Fuera de alcance (recordatorio)

- Más git remoto / rebase / merge
- Shell, process, Desktop Commander
- Reescribir OCC a SHA-256
- `directory_tree` como implementación paralela (es alias + opts)
- `read_multiple_files` como segundo reader (es alias)
- 13 aliases Claude Code
- Copiar el oficial: no quitamos backups, OCC, rewrite-guard, pipelines
