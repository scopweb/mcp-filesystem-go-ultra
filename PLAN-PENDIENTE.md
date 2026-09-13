# Plan pendiente (después de v4.6.2)

Trabajo que **no** entra en esta release. v4.6.2 cierra discovery para agentes: instrucciones cortas, `--profile=strict|ultra`, `apply_patch` usable, git de red fuera del camino crítico.

## P1 — Evaluación real con agentes

Sigue abierto. La batería `TestE2E_E6_AgentEval` es integración scriptada, no un modelo.

- Varios modelos / clientes MCP (Claude Code, Codex, OpenCode)
- Repositorios representativos, no 40 archivos sintéticos
- Métricas: éxito de tarea, modificaciones incorrectas, conflictos, reintentos, llamadas, tokens, latencia p50/p95
- Inyectar respuesta perdida de verdad (no solo repetir `operation_id`)
- Escritor y revisor concurrentes de verdad (no secuencial)

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
- `apply_patch`: one file per call; dest EOL wins; skill guidance for PATCH_APPLY_FAILED; CRLF test.
- `directory_tree` / `search_files`: gitignore default ON; `truncated` + `hidden_count` in structured output + outputSchema.
- `--git-network` (default off): push/fetch removed from default enum; `openWorldHint` only when enabled.
- Tests: profile exact counts (16/25), output schema sweep, fail-closed exit 2, apply_patch dry-run/OCC/CRLF, git network gate.
- `backup` promoted into strict core so agents can always undo.

## Hecho en v4.6.1

- Graduados: `list_allowed_directories`, `directory_tree`, `diff_files`, `apply_patch`, entradas nativas, `strict`, `e3-v1`
- `outputSchema` + sweep para los cuatro tools de discovery/patch
- Contrato: tipos JSON, enums e `items` de arrays alineados con el esquema MCP
