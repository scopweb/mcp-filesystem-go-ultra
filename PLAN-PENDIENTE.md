# Plan pendiente (después de v4.7.0)

Trabajo que **no** entra en esta release. v4.7.0 cierra Failure Intelligence (PR-0–PR-6). v4.6.2 cerró discovery para agentes.

## Hecho — pack de arnés (docs)

Copy-paste pack in [examples/harness/](examples/harness/): Claude Code agents + OpenCode permission snippets + Codex warning. Handshake instructions include subagent handoff. `list_allowed_directories` structured payload adds `profile`, `roots_mode`, `readonly`, `tool_count`. **Eval with a real model remains open** (this pack is config, not a benchmark).

## v4.7 Failure Intelligence

Canonical: [ROADMAP-v4.7-Failure-Intelligence.md](ROADMAP-v4.7-Failure-Intelligence.md). [ROADMAP-v4.7-Agent-Intelligence.md](ROADMAP-v4.7-Agent-Intelligence.md) is obsolete.

| PR | What | Status |
|----|------|--------|
| PR-0 | Baseline harness behind `cmd/proxy` | done |
| PR-1 | Envelope + `retryable` table | done |
| PR-2 | OCC Conflict Report (ranges default; diff only `detail=full` / `include_diff`) | done |
| PR-3 | Actionable payloads (PATCH_FAILED stays `retryable:false`) | done |
| PR-4 | `detail=summary\|normal\|full` on expensive tools only | done |
| PR-5 | Mutation budget per process (default off) | done |
| PR-6 | Path-aware risk on `impact_analyzer` | done |

Shipped in v4.7.0. P1 eval with a real model stays P1 of the product. P2 e3 receipts persist with `--receipt-dir` (below). `atomic create_dir` / multi-file patch / completions stay open.

## P1 — Evaluación real con agentes

Sigue abierto. La batería `TestE2E_E6_AgentEval` es integración scriptada, no un modelo. The harness pack does not close this.

One OpenCode + grok-4.6 session on this repo is recorded in `examples/harness/benchmark/eval-opencode-20260921.md`. It does not close P1 (single client/model; live MCP was `d2706b3` not HEAD; no concurrent writer/reviewer; no lost-response injection).

Avance 2026-09-22: tres sesiones nuevas de investigación de código con OpenCode + Grok 4.6, HEAD `9086033` y cambios locales registrados: 100 llamadas MCP, 0 errores MCP, mediana de tarea 94,713 s. Dos respuestas tuvieron una interpretación incorrecta de `BigCache.MaxEntrySize`; no se declara éxito semántico pleno. Evidencia local: `C:\temp\agent-cache-eval-20260922\grok-run-04\REPORT.md`. Solo lectura, sin A/B de TTL ni contadores de caché filesystem: P1 continúa abierto.

**Cierre 2026-09-23 — recibo e3 en vivo.** OpenCode 1.18.32 + Grok 4.6, mismo proceso MCP. Sonda `operation_id:"probe"` devolvió el prefijo `db9be87e743214e18e916380e7d54eca`. Extract `:once` a las 11:25:00, 53 ms, respuesta entregada. Reintento del mismo id a las 11:25:05, 0,5 ms, mismos 650 bytes: no reextrajo. Archivos: `clients\lost-call\src.txt` = `second`, `dst.txt` = `prefix` / `first`. Proxy: `C:\temp\mcp-proxy-logs\proxy.jsonl` request_id 19–22. El cliente vio la primera respuesta; no fue un corte en el cable. El corte scriptado sigue en `C:\temp\agent-lost-response-20260922\REPORT.md`: con `e3-v1` un solo efecto; sin contrato, el reintento vuelve a ejecutar.

Escritor/revisor concurrentes (2026-09-22, `C:\temp\agent-concurrent-eval-20260922\p1-01\REPORT.md`): 2/3 pruebas pasaron con OCC real y recuperación del revisor; la tercera agotó 300 s y dejó `price.go` vacío. No cierra P1.

Claude Haiku no invocó el MCP; Codex no refrescó el token. Otros clientes siguen abiertos.

v4.7 operational reliability (FIABILIDAD-OPERATIVA) shipped engine telemetry that *can* back a real eval (MCP vs internal ops, applied/rejected/simulated, p50/p95). `examples/harness/benchmark -suite reliability` is a **scripted** wire gate (pagination, multi_edit rollback, project_replace detail, git remote). It does not replace running equivalent tasks on a real model.

- Varios modelos / clientes MCP (Claude Code, Codex, OpenCode)
- Repositorios representativos, no 40 archivos sintéticos
- Métricas: éxito de tarea, modificaciones incorrectas, conflictos, reintentos, llamadas, tokens, latencia p50/p95
- Inyectar respuesta perdida de verdad (no solo repetir `operation_id`)
- Escritor y revisor concurrentes de verdad (no secuencial)

## Plan de ataque — Caché fiable y warm-start

Plan: [PLAN-CACHE-WARM-START.md](PLAN-CACHE-WARM-START.md). CACHE-01–04 implementados y baseline local real ejecutado; [cifras y límites](examples/harness/benchmark/cache-baseline-20260921.md).

- Fase 0: frescura bytes/metadata, métricas fiables, prefetch autorizado, cierre ordenado y TTL configurable con baseline.
- Fase 1: experimento opt-in de manifiesto + precarga desde originales.
- Fase 2: snapshot de contenido únicamente si las mediciones justifican el coste.
- mmap y recibos e3 durables mantienen líneas separadas. No se presuponen mejoras de latencia.

**Fase 0 cerrada por decisión del usuario (2026-09-22).** Baseline: 3 repeticiones por TTL, 576 lecturas, 0 errores; envejecido >3m: 3m = 72 misses, 10m = 72 hits. p50/p95 = 4,326/8,065 ms frente a 4,194/11,303 ms, sin ventaja consistente de latencia. Se incorpora la evaluación real de solo lectura con Grok descrita en P1; no sustituye el A/B sintético. Default 3m mantenido, WARM-01 en pausa, WARM-02 no iniciado y mmap diferido. Conservar benchmark para regresiones; reabrir persistencia solo ante evidencia representativa de coste relevante de lectura/reinicio. SO frío, red y otros sistemas quedan fuera del alcance probado.

## P2 — Recuperación durable

### Recibos e3-v1 persistentes — decisión (2026-09-23)

Contrato que no cambia: clave `sesión + kind + id`; SHA-256 de los argumentos; 24 h; tope 4096 sin eviction; solo batch/pipeline opt-in `retry_contract:"e3-v1"`. Sin `--receipt-dir` el epoch sigue siendo de proceso y un id ajeno no se reejecuta.

**Almacén.** Flag `--receipt-dir` (vacío = solo memoria). Layout: `<dir>/<project-id>/epoch` y `<dir>/<project-id>/r/<sha256(key)>.json`. `project-id` = 16 hex de SHA-256 de `--allowed-paths` canónicos ordenados (`CanonicalOCCKey`). Dos proyectos no mezclan recibos aunque compartan el flag. `--insecure-open` (sin paths) usa el hash del conjunto vacío. Dirs `0700`, ficheros `0600`. Solo el proceso del servidor, mismo usuario OS. No hay tool MCP ni resource. El operador deja el dir fuera del sandbox.

**Epoch.** `secureRandomSuffix()` (16 bytes de `crypto/rand`, 32 hex). Se publica una vez con temp+rename. Sobrevive al reinicio; no es timestamp, PID ni hostname. Epoch ausente o corrupto → se genera otro; los recibos viejos quedan huérfanos y sus ids se rechazan (epoch desconocido), no se reejecutan.

**Publicación atómica.** Antes de mutar: recibo `pending`. Al terminar: `done` con `result`/`error`. Crash a medias deja `pending` o el `done` anterior, nunca un JSON a medias que parezca válido (temp+rename). `pending`, `unknown` o bytes truncados = resultado desconocido: no reejecutar. Si falla persistir el `done`, este proceso puede devolver el resultado; el reinicio ve `pending` y no repite.

**Recibo de error.** `done` con `error`. El reintento devuelve el mismo error y no muta.

**Expirado (24 h).** Tombstone: se anula el resultado, hay que reconciliar, no reejecutar. El archivo permanece (el id no se reutiliza) y cuenta en el tope.

**Tope 4096.** Sin eviction. Al límite: rechazo, ninguna mutación. Escape: otro dir o borrar el almacén (nuevo epoch → ids viejos = epoch desconocido).

**Prueba.** `TestE3ReceiptsSurviveProcessRestart` arranca un proceso hijo, aplica, termina, arranca otro con el mismo almacén y el mismo id; el archivo no cambia. Cubre epoch ajeno, argumentos distintos y recibo truncado. No se declara durabilidad sin ese reinicio real.

`atomic create_dir` sigue rechazado. Documentar el límite o definirlo queda abierto.

## P2 — Comodidad de edición

- Parche multiarchivo estilo Codex sobre el núcleo transaccional E2 (un `apply_patch` por archivo hasta entonces)
- Autocompletado MCP (`completions`) de rutas y acciones

## Fuera de alcance cercano

- Resources / Prompts MCP
- Tasks MCP para operaciones largas
- Streamable HTTP / OAuth (el servidor es stdio)
- Reabrir aliases View/Edit/fs

## Hecho en v4.6.2

- README / skill / help aligned on discovery: first `list_allowed_directories`, then `directory_tree` or `help(tool:X)`; no full catalog dump at start; `minify_js` omitted from instructions.
- `--profile=strict` (16 tools incl. `backup` for undo) vs `--profile=ultra` (25 tools incl. `analyze_code`). Default ultra for compatibility.
- `analyze_code` (ultra only): symbols (go/ast), lint (go vet + staticcheck), sec (local regex), impact (search-based). readOnly; no shell; allowlisted bins only.
- `apply_patch`: one file per call; dest EOL wins; skill guidance for PATCH_FAILED; CRLF test.
- `directory_tree` / `search_files`: gitignore default ON; `truncated` + `hidden_count` in structured output + outputSchema.
- `--git-network` (default off): push/fetch removed from default enum; `openWorldHint` only when enabled.
- Tests: profile exact counts (16/25), output schema sweep, fail-closed exit 2, apply_patch dry-run/OCC/CRLF, git network gate.
- `backup` promoted into strict core so agents can always undo.

## Hecho en v4.6.1

- Graduados: `list_allowed_directories`, `directory_tree`, `diff_files`, `apply_patch`, entradas nativas, `strict`, `e3-v1`
- `outputSchema` + sweep para los cuatro tools de discovery/patch
- Contrato: tipos JSON, enums e `items` de arrays alineados con el esquema MCP
