# Plan pendiente (después de v4.6.1)

Trabajo que **no** entra en esta release. v4.6.1 cierra la graduación de experimentales y el contrato único (tipos, enums, `items`).

## P1 — Evaluación real con agentes

La batería `TestE2E_E6_AgentEval` es integración scriptada, no un modelo.

- Varios modelos / clientes MCP (Claude Code, Codex, OpenCode)
- Repositorios representativos, no 40 archivos sintéticos
- Métricas: éxito de tarea, modificaciones incorrectas, conflictos, reintentos, llamadas, tokens, latencia p50/p95
- Inyectar respuesta perdida de verdad (no solo repetir `operation_id`)
- Escritor y revisor concurrentes de verdad (no secuencial)

## P1 — Documentación

- Recortar contradicciones restantes entre `help()`, `CLAUDE.md` y la skill
- Menos reglas al agente, más garantías en el servidor

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

## Hecho en v4.6.1

- Graduados: `list_allowed_directories`, `directory_tree`, `diff_files`, `apply_patch`, entradas nativas, `strict`, `e3-v1`
- `outputSchema` + sweep para los cuatro tools de discovery/patch
- Contrato: tipos JSON, enums e `items` de arrays alineados con el esquema MCP
