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

- README: tabla de 24 tools; las 4 de discovery/patch con “cuándo usarla”
- `server.WithInstructions()`: primera llamada `list_allowed_directories`; luego `directory_tree` o `help(tool:X)`; sin catálogo al arrancar; sin `minify_js`
- Skill + `help()` + CLAUDE.md + USAGE alineados (sin “directory_tree experimental”, sin “llama help() primero”)
- `read_file`: si piden `read_multiple_files` / `read_text_file` → `read_file` (`paths[]` / `mode` head|tail)
- `--profile=strict|ultra` (default ultra). strict = 15 tools de agente
- `apply_patch`: un fichero por llamada; skill PATCH_APPLY_FAILED → read + regenerar; test CRLF dest + LF patch
- `directory_tree` / `search_files`: gitignore ON; `truncated` + `hidden_count` explícitos
- `--git-network` (default off): `push`/`fetch` ausentes del enum; `openWorldHint` true solo con red
- Tests: fail-closed exit 2; `list_allowed_directories` shape; perfil strict exacto; apply_patch dry_run/OCC/CRLF; sweep outputSchema

## Hecho en v4.6.1

- Graduados: `list_allowed_directories`, `directory_tree`, `diff_files`, `apply_patch`, entradas nativas, `strict`, `e3-v1`
- `outputSchema` + sweep para los cuatro tools de discovery/patch
- Contrato: tipos JSON, enums e `items` de arrays alineados con el esquema MCP
