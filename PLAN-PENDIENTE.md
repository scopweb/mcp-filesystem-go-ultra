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

Shipped in v4.7.0. P1 eval with a real model stays P1 of the product. P2 durable e3 / multi-file patch / completions stay open.

## P1 — Evaluación real con agentes

Sigue abierto. La batería `TestE2E_E6_AgentEval` es integración scriptada, no un modelo. The harness pack does not close this.

One OpenCode + grok-4.6 session on this repo is recorded in `examples/harness/benchmark/eval-opencode-20260921.md`. It does not close P1 (single client/model; live MCP was `d2706b3` not HEAD; no concurrent writer/reviewer; no lost-response injection).

v4.7 operational reliability (FIABILIDAD-OPERATIVA) shipped engine telemetry that *can* back a real eval (MCP vs internal ops, applied/rejected/simulated, p50/p95). `examples/harness/benchmark -suite reliability` is a **scripted** wire gate (pagination, multi_edit rollback, project_replace detail, git remote). It does not replace running equivalent tasks on a real model.

- Varios modelos / clientes MCP (Claude Code, Codex, OpenCode)
- Repositorios representativos, no 40 archivos sintéticos
- Métricas: éxito de tarea, modificaciones incorrectas, conflictos, reintentos, llamadas, tokens, latencia p50/p95
- Inyectar respuesta perdida de verdad (no solo repetir `operation_id`)
- Escritor y revisor concurrentes de verdad (no secuencial)

## Plan de ataque — Caché fiable y warm-start

Plan consensuado: [PLAN-CACHE-WARM-START.md](PLAN-CACHE-WARM-START.md). Implementación pendiente.

- Fase 0: frescura bytes/metadata, métricas fiables, prefetch autorizado, cierre ordenado y TTL configurable con baseline.
- Fase 1: experimento opt-in de manifiesto + precarga desde originales.
- Fase 2: snapshot de contenido únicamente si las mediciones justifican el coste.
- mmap y recibos e3 durables mantienen líneas separadas. No se presuponen mejoras de latencia.

CACHE-01 y CACHE-02 hechos. Próxima entrega: **CACHE-03 — Prefetch autorizado y cierre ordenado**.

## P2 — Recuperación durable

- Recibos de retry (`e3-v1`) persistentes entre reinicios (hoy solo memoria, 24 h, prefijo de proceso)
- Definir `atomic create_dir` o dejarlo rechazado de forma permanente y documentarlo como límite

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
