# ROADMAP v4.7 — Failure Intelligence

**Repo:** mcp-filesystem-go-ultra  
**Base:** v4.6.2 (strict/ultra, OCC, envelope, harness pack, `analyze_code`)  
**Sustituye:** `ROADMAP-v4.7-Agent-Intelligence.md` (descartado como fuente).  
**No es v4.7:** pack de arnés (ya en `examples/harness/`), Project Map, Workspace Session, Smart Retry / merge de hunks, segundo `symbols`.

## Filosofía

El servidor no piensa por el agente. Entrega la máxima información **determinista** para que el agente necesite menos llamadas para pensar.

Invariantes:

- Misma entrada + mismo estado del disco + mismos flags ⇒ misma forma de respuesta.
- Cero estado implícito de “último fichero / último agente”.
- OCC sigue siendo estricto. Un mismatch no se “arregla” en el servidor.
- `retryable: true` significa “puedes reintentar **después** de usar los datos del envelope”, no “reenvía el mismo payload”.
- Identidad de presupuesto = **proceso MCP**, no un `agent_id` inventado.
- Medir con `mcp-proxy` antes de recortar payloads.

## Fuera de v4.7 (explícito)

| Idea | Por qué no |
|------|------------|
| `path_ref:"last_read"` / Workspace Session | Dos subagentes, un stdio: “último” no tiene dueño. El handoff es path+hash. |
| Smart Retry (fusionar hunks no solapados) | No-solape sintáctico ≠ independencia semántica. |
| `output_mode:"adaptive"` | No determinista. Sustituido por `detail`. |
| Project Map / hotspots P0 | Señales objetivas más adelante; no compite con OCC payloads. |
| Duplicar `analyze_code(action:"symbols")` | Ya existe en ultra. |
| `spawn_subagent` dentro de ultra | Orquestación = arnés, no filesystem. |
| Adapter `*** Begin Patch` (Codex) | Otro contrato. |
| Recibos e3 durables entre procesos | Sigue en PLAN-PENDIENTE P2. |
| Ampliar `core/feedback.go` `sessionState.lastRead` como API | Es heurística interna de auto-OCC. No es el contrato del agente. |

`core/feedback.go` y `feedback_adaptive.go` **no se reescriben** en PR-1. Auto-OCC (`off|warn|block`) se queda. Failure Intelligence añade un envelope **explícito** en el wire, no más mapas `path → last`.

---

## PR-0 — Baseline harness (gate)

**Objetivo:** números antes de optimizar. Sin este PR no se elige qué tool lleva `detail` ni qué error merece diff.

### Piezas

- Escenarios reproducibles bajo `bench/failure/` o `examples/harness/benchmark/`:
  1. Explore: `list_allowed_directories` → `directory_tree` → `search_files` en un tree ≥ 2k ficheros.
  2. Edit + OCC clash: dos writes; el segundo con hash viejo.
  3. `apply_patch` que no aplica (`PATCH_FAILED`).
  4. `read_file` batch / `help()` sin `tool`.
- Correr detrás de `cmd/proxy` (ya estima tokens).
- Informe: calls, tokens in/out, latencia, códigos de error, retries.

### Métricas mínimas

```text
scenario          calls  tokens_out  occ_mismatch  patch_failed  retries
explore_2k
occ_clash
patch_fail
help_catalog
```

### Criterio de salida

- Script documentado en README del directorio.
- Un JSON de muestra committed (redactado).
- Decisión escrita: “las N tools más caras son …”.

**Código de producto:** ninguno obligatorio. No tocar `core/` salvo hooks de log si el proxy no ve structured errors.

---

## PR-1 — Envelope estable + tabla `retryable`

### Contrato wire (todas las tools de mutación y las de lectura que ya usan structuredContent)

```json
{
  "error": {
    "code": "OCC_MISMATCH",
    "retryable": true,
    "message": "file changed since expected_hash",
    "suggestion": "Rebase against current content; retry with current_hash.",
    "details": {}
  }
}
```

Campos siempre presentes en error: `code`, `retryable`, `message`.  
`suggestion` y `details` según código.

### Tabla canónica

| code | retryable | details mínimos | siguiente paso del agente |
|------|-----------|-----------------|---------------------------|
| `OCC_MISMATCH` | true | ver PR-2 | rebase + `current_hash` |
| `HASH_REQUIRED` | true | `path` | `read_file` y reenviar hash |
| `PATCH_FAILED` | **false** | hunk, line, reason | **no** reenviar el mismo patch; `read_file` + generar otro |
| `REWRITE_BLOCKED` | false | `pattern`, `path` | `edit_file` / `apply_patch` |
| `NOT_ALLOWED` | false | `path`, `roots` | otro path o roots |
| `SECRET_DENIED` | false | pattern | `--allow-secrets` o no tocar |
| `READONLY` | false | tool | quitar `--readonly` |
| `BUDGET_EXCEEDED` | false | used, limit, window | esperar o subir flag |
| `VALIDATION` | false | field | arreglar args |
| `NOT_FOUND` | false | path | list/tree |
| `TOOL_UNAVAILABLE` | false | tool (p.ej. staticcheck) | otra action / instalar fuera |

Si un código actual del texto humano no coincide, **alias interno → code estable**. No romper el texto fallback byte-a-byte en compact mode salvo que el test de sweep lo permita; structuredContent es la fuente nueva.

### Ficheros

- `core/errors.go` — tipos; añadir `WireError` / codes.
- `core/feedback.go` — no convertir PatternID en codes de wire; mapear en el borde MCP.
- `internal/mcpserver/` — helpers de envelope (ya hay patrón en discovery/patch).
- Tests: sweep de `code` + `retryable` por tool de mutación.

### Help

Una página en `help_content.go`: tabla code → acción. Skill y README enlazan; no tres copias divergentes.

---

## PR-2 — OCC Conflict Report

Cuando `expected_hash != current_hash` bajo el lock (`OCCMismatchError` en `core/errors.go`):

### Payload por defecto (`detail` ausente o `summary`/`normal`)

```json
{
  "error": {
    "code": "OCC_MISMATCH",
    "retryable": true,
    "expected_hash": "abc…",
    "current_hash": "def…",
    "conflict": {
      "path": "…",
      "changed_ranges": [{"start": 84, "end": 91}],
      "hunk_count": 1,
      "bytes_delta": 120
    },
    "suggestion": "Rebase the intended edit against the current content and retry with current_hash."
  }
}
```

### Diff opcional

- Solo si `detail:"full"` **o** `include_diff:true` en esa llamada.
- Unified diff **acotado**: contexto ±20 líneas, tope p.ej. 8 KiB. Si el fichero cambió de tamaño de forma salvaje (umbral en `core/config.go`), no diff: `conflict.reason = "too_large_delta"` y hint “releer”.
- Reusar `core/diff.go`. No generar el diff dos veces (lock → hash → ranges → optional diff) fuera del lock más de lo necesario: ranges con un algoritmo lineal sobre líneas (LCS barato o hash-por-línea). Suficiente con “líneas que no coinciden en una alineación greedy”; documentar que no es un merge-tool.

### Algoritmo (implementación)

1. Con lock de `core/file_lock*.go` ya tomado (mismo camino que hoy OCC).
2. Leer bytes actuales, `current_hash`.
3. Si `expected_hash` vacío y política auto-OCC `block` → `HASH_REQUIRED` o el comportamiento actual; no inventar last_read como hash esperado en el wire.
4. Si mismatch:
   - split lines (respetar EOL del destino, igual que `apply_patch`).
   - Si el caller envió el edit/patch: ranges = unión de líneas tocadas en disco vs snapshot si aún está en backup; si no hay snapshot, ranges = líneas distintas vs… **no compares contra el payload old_text como verdad**. Preferencia:
     - snapshot del backup OCC si existe para ese hash esperado;
     - si no, `changed_ranges` vacío + `reason:"no_baseline"` + hashes. Mejor vacío que mentir.
5. Soltar lock. No escribir.

### Ficheros

- `core/errors.go` — `OCCMismatchError` gana `Ranges`, `BytesDelta`, `Reason`.
- `core/edit_operations.go`, `core/patch.go`, `core/file_txn.go` — un helper `BuildOCCConflict(expected, actualBytes, baselineBytes) `.
- `core/diff.go` — cap + ranges.
- Tests: fichero pequeño con una línea cambiada; CRLF; baseline ausente; `include_diff` tope.

### Semántica para subagentes

El hijo **no** usa `sessionState.knownHash`. Usa el hash del handoff del padre. El report habla de esos dos hashes.

---

## PR-3 — Actionable payloads en el resto de recuperables

Mismo envelope. Completar `details` por código:

| code | details |
|------|---------|
| `PATCH_FAILED` | `hunk_index`, `path`, `reason` (context_not_found / overlap / malformed), `line_hint` si existe |
| `REWRITE_BLOCKED` | `path`, `old_len`, `new_len`, `pattern` (accidental_rewrite) |
| `NOT_ALLOWED` | `path`, `roots[]`, `source` (cli/roots/union) |
| `HASH_REQUIRED` | `path` |
| `VALIDATION` | `field`, `expected` |
| `BUDGET_EXCEEDED` | PR-5 |

Texto `suggestion` estable (string literal en tests).  
`PATCH_FAILED` permanece `retryable:false`.

Ficheros: `internal/mcpserver/tools_patch.go`, tools de edit/write, `core/edit_safety_layer.go`, `core/edit_policy.go`.

---

## PR-4 — Explicit detail (`summary|normal|full`)

No `adaptive`. Default `normal` (comportamiento actual).

Empezar **solo** por las tools que PR-0 marque. Candidatas previsibles:

- `directory_tree`
- `search_files`
- `read_file` (batch / help text)
- `help` (sin `tool:`)

Contrato:

| detail | tree | search | help |
|--------|------|--------|------|
| summary | paths + truncated/hidden_count, sin stats extra | paths + count | nombres de tools |
| normal | actual | actual | actual |
| full | + tamaños / más nodos hasta max_nodes | + snippets | catálogo + ejemplos |

Misma entrada + mismo FS + mismo `detail` ⇒ mismo JSON shape (campos fijos; listas más cortas en summary, no campos que aparecen y desaparecen salvo omisión documentada).

Param nativo `detail` enum en ToolContract + sweep. Compact-mode sigue existiendo; si ambos: compact recorta texto fallback, `detail` recorta structuredContent. Documentar precedencia: `detail` gana en structured; compact gana en text.

No aplicar `detail` a `apply_patch` resultado ok en este PR (el conflict report ya tiene su propio tope).

---

## PR-5 — Mutation budget (proceso)

### Semántica

- Contador **en el proceso** del servidor MCP (todas las tools mutadoras: write, edit, multi_edit, apply_patch, delete, move, project_replace, batch, pipeline mutante).
- Ventana deslizante o cupo de sesión de proceso: `--mutation-budget=N` (default 0 = off).
- Unidad: **mutaciones aplicadas** (no dry_run, no read). Opcional segundo flag `--mutation-budget-bytes` más adelante; v4.7 solo count.
- Al exceder: no aplicar, `BUDGET_EXCEEDED`, `retryable:false`, `details: {used, limit}`.
- `--readonly` sigue siendo el corte duro; budget es el corte de runaway.
- No `agent_id`. Subagentes que comparten el stdio **comparten el cupo**. Eso es deseable.

### Ficheros

- `core/config.go` + flags en `internal/mcpserver/run.go`
- `core/engine.go` o `mutation_journal.go` — increment on success
- Test: budget=2, tercera write falla; dry_run no cuenta.

---

## PR-6 — Path-aware risk

Extender `core/impact_analyzer.go` + `core/edit_policy.go`. No nueva capa.

Reglas deterministas (tabla en código + test):

| glob / clase | efecto |
|--------------|--------|
| `.github/workflows/*`, `**/Dockerfile`, `go.mod`, `go.sum`, `*.csproj` | floor = high |
| `**/*_test.go` | floor no sube |
| `cmd/**`, `internal/**` vs `README*` / `docs/**` | floor medium en código de paquete si %change ≥ umbral medium |
| roots exactamente (`delete`/`move` del allowed root) | ya bloqueado; no tocar |
| secret denylist paths | ya `SECRET_DENIED`; no doble risk |

`ChangeImpact` gana `PathFloor` / `Reasons[]`. HIGH/CRITICAL siguen disparando verificación post-edit que ya existe.

Flag no necesario. Documentar la tabla en SECURITY.md o help.

---

## Orden y criterio “v4.7 cerrado”

```
PR-0 baseline
PR-1 envelope + retryable table
PR-2 OCC conflict report
PR-3 payloads resto
PR-4 detail (solo tools caras del baseline)
PR-5 mutation budget
PR-6 path-aware risk
```

Se puede fusionar PR-1+2 si el diff es pequeño. No saltar PR-0.

**Hecho** cuando:

- [x] Baseline JSON en repo
- [x] Sweep CI de codes + retryable
- [x] Test OCC con ranges + caso sin baseline
- [x] Test PATCH_FAILED no retryable
- [x] `detail` en ≥1 tool cara con snapshot de schema
- [x] Budget off por defecto; on en test
- [x] PLAN-PENDIENTE actualizado; Agent-Intelligence MD marcado obsolete
- [ ] Eval con modelo real **sigue abierta** (no es gate de v4.7 código)

## Tests que no negociar

- Fail-closed sin paths = exit 2
- OCC clash no escribe
- CRLF destino en patch (regresión)
- Profile strict tool count
- Text fallback compact: no inflar con diffs a menos que `include_diff`

## Riesgos

- Diff en cada OCC en repos grandes → por eso default sin diff.
- `sessionState` de feedback confundido con el report → review: cero campos nuevos en lastRead como API.
- Budget global fricción en pipelines largos → default off; documentar que batch cuenta 1 por op aplicada, no 1 por batch entero (decidir en PR-5 y testificarlo: **1 por op interna aplicada**).

## Relación con PLAN-PENDIENTE.md

Añadir sección “v4.7 Failure Intelligence” con los PRs.  
P1 eval real con agentes permanece P1 de producto, no bloquea merge de PR-0–6.  
P2 durable e3 / multi-file patch / completions: intactos, detrás de v4.7.
