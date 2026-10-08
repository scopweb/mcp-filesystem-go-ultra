# Hoja de ruta — mcp-filesystem-go-ultra

Fecha: 2026-10-08. Base: `main` v4.8.0 (MCP 2025-11-25, 29/18/17 tools).
El P1 abierto (evaluación real de agentes) se mantiene. Esto no lo sustituye.

No entra: overlay / begin-promote, ProjFS, WinFsp, TxF, "Shadow Workspace" (término de Cursor, sep 2024, y es otra cosa).

## Fase A — adopción (1 semana)

Lo que decide si alguien cambia el server oficial por este.

1. Tabla de comparación en el README, contra `@modelcontextprotocol/server-filesystem`:
   - roots Windows (`file:///c%3A`, UNC, 8.3, reserved names, ADS)
   - búsqueda de contenido vs nombres
   - undo
   - dry-run / diff
2. Corregir el listado de MCPHub: sigue apuntando a `mcp-filesystem-server-ultra`. (2026-10-08: no está en el repo ni en una página pública con ese nombre. El manifiesto usa `filesystem-ultra`. El directorio externo no se ha editado.)
3. Empaquetar el exe en un `.mcpb` (zip + `manifest.json`) para Claude Desktop. Un doble clic. Envío al directorio de extensiones.
4. Anunciar la spec que se implementa: 2025-11-25, stdio. 2026-07-28 (stateless, tasks fuera del core) queda fuera, igual que el roadmap actual.

## Fase B — seguridad por defecto (1 semana)

Hoy el bloqueo de secretos depende de un hook que el usuario tiene que montar. Los forks nuevos lo traen de serie.

1. Denegar por defecto lectura y escritura de `.env`, `*.pem`, `id_rsa`, `*.pfx`, `credentials.json`. Flag `--allow-secrets` para opt-in. Hecho: `IsSecretPath` + `IsPathAllowed`. No hace falta el hook.
2. Sustituir el regex de `search_files` por RE2, o rechazar patrones con backtracking. Hecho: `CompileRegex` es el RE2 de Go. `SECURITY.md` ya no lo deja como aceptado.
3. `openWorldHint: true` en `git`, `github_issues`, `gitlab_issues`. Hecho en los dos de issues. `git` solo con `--git-network`: un status local no es mundo abierto. No se fuerza a true.
4. Test de identidad de fichero en edit/write: birthtime, hardlink y file id de NTFS sobreviven. El oficial pasó a in-place en POSIX (PR 4516) y dejó Windows en temp+rename. Aquí también hay rename, en todas las plataformas. No sobreviven: el test lo fija y README/SECURITY lo dicen.

## Fase C — paridad Windows que el oficial sigue fallando

Issues abiertos del reference server en 2026. Cada uno, un test, no una feature.

1. Root `file:///c%3A/temp` aceptado (issue 3174).
2. UNC dentro del allowlist.
3. `search_files` no parte paths por el separador (issue 3965).
4. `realpath` ENOENT no tumba el server (issue 3948).
5. Roots del cliente se unen a los del CLI con `--roots-mode=union` ya existente: un test de regresión con el caso OpenCode y otro con VS Code.

## Fase D — la prueba que el roadmap ya pide

No se cierra con el harness sintético.

- Más de un cliente y un modelo (Claude Desktop, Claude Code, OpenCode).
- Un repo real, no el árbol de 40 ficheros.
- Éxito de tarea, edits malos, conflictos, reintentos, tokens, p50/p95.
- Un writer concurrente de verdad.

## Fuera

- Overlay, materialize, checkpoints COW. El mercado no lo pide: los issues del oficial son paths y search.
- MCP Tasks y Streamable HTTP. La spec 2026-07-28 los sacó del core.
- Reabrir alias `View` / `Edit` / `fs`.
