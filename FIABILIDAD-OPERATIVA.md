# Fiabilidad operativa — cierre de estado

Estado: **código y pruebas cerrados en v4.7.1** (`d2706b3`, 2026-09-17).  
Pendiente: evaluación real con modelo, documentada en [PLAN-PENDIENTE.md](PLAN-PENDIENTE.md).  
Fuente de verdad de lo enviado: [CHANGELOG.md](CHANGELOG.md) `[4.7.1]`.

Este archivo **no es un plan vacío**. Sustituye el borrador de implantación
(casillas sin marcar) por el cierre: hecho, decisiones, residual y abierto.

## Origen

Informe de campo sobre un hilo real. No era verdad absoluta sobre el
binario; sí casos útiles. El marco de actuación:

1. Respuestas completas y no ambiguas primero.
2. Tokens y latencia se miden después, con eval real.

v4.7.0 ya tenía `--git-network` fuera del enum por defecto, rollback
atómico, `detail`, `truncated`/`hidden_count`. Seguían tres fallos caros:

1. `push` compacto: `OK: pushed to origin` — sin URL.
2. `search_files` recortaba la presentación aunque se pidiera más.
3. Rollback que incrementaba `FailedEdits` (ambiguous) y a veces resumía `Failed: .`.

Orden ejecutado: rollback + URL de destino → paginación de búsqueda →
consulta de remotos + allowlist → telemetría de motor. La entrega 1
(reproducción de binario/schema del incidente de campo) no se archivó
como evidencia; las regresiones cubren los tres fallos en el código actual.

## Hecho (v4.7.1)

| Área | Qué quedó |
|------|-----------|
| multi_edit | Diagnóstico incluye `ambiguous` y el resto de fallos contabilizados. Índice + causa. Fichero intacto. Sin `Failed: .`. |
| git push/fetch | Destino efectivo (`pushurl` + `insteadOf`), compacto y largo. Credenciales ocultas. |
| git remote | `git(action:"remote")` de solo lectura, **sin** `--git-network`. |
| allowlist | `--git-remote-allow` sobre la URL efectiva. `force:true` no la elude. Nombre `origin` no basta. |
| search_files | Eliminados topes ocultos 10/20. `max_results` = tamaño de página. `offset` continúa. |
| project_replace | Ya no sugiere `verbose=true`. Lista larga: `preview:true` o acotar `file_types`. |
| telemetría | MCP vs ops internas; aplicadas/rechazadas/simuladas; backups de proceso vs históricos; ops/s por delta; media + p50/p95 (anillo 256). |

### Pruebas de cierre

- `TestMultiEdit_AmbiguousAfterValid_DiagnosedAndUnchanged`
- `TestMultiEdit_HandlerDiagnosesAmbiguousWithoutBackup`
- `TestGitPush_CompactShowsDestination`
- `TestGitPush_AllowlistRejectsBeforeNetwork`
- `TestGitRemoteAction_NoGitNetwork`
- `TestSearch_CompactHonorsMaxResultsOverHidden20`
- `TestSearch_PaginationNoDupNoGap`
- `TestMetrics_*`

## Decisiones (no reabrir sin causa)

**Paginación — offset, no cursor.** Orden estable: path, línea, `match_start`.
Unidad: filename-only = un path; content = una línea coincidente;
`count_only` es un total, no una página. Si el árbol cambia entre páginas,
puede haber saltos o duplicados: reiniciar en `offset:0`.
`hidden_count` = exclusión por filtros, no resultados pendientes.
`match_count` es total exacto solo si el walk terminó.

**Allowlist vacía = cualquier destino.** Compatibilidad: sin
`--git-remote-allow`, push/fetch no se restringen más allá de
`--git-network`. La visibilidad de la URL no impide un envío al remoto
equivocado; la prevención exige allowlist no vacía.

**Mostrar la URL ≠ prevenir.** Consultar con `git(action:"remote")` y
validar el destino efectivo **antes** de `push`.

**517 backups ≠ 517 edits de esta sesión.** `ops:N` de motor no es
el recuento de llamadas MCP del cliente. Impresiones de rapidez no son
medición.

## Residual (sigue siendo cierto)

- **Allowlist por defecto abierta.** En entornos con GitHub y GitLab
  interno, hay que pasar `--git-remote-allow`. Si no, el agente puede
  empujar a cualquier URL configurada una vez hay `--git-network`.
- **Compact `project_replace`** lista ficheros con `detail:"full"`
  (Unreleased). El default compacto sigue siendo solo contadores.
- **Entrega 1 incompleta como registro.** No hay ficha del binario/schema
  del hilo de campo. Las pruebas cubren el código de v4.7.1, no demuestran
  qué cliente vio el incidente original.

## Abierto

**Eval real con modelo** sigue abierto ([PLAN-PENDIENTE.md](PLAN-PENDIENTE.md) § P1).

Hay un gate **scriptado** (no es un modelo):

```
go run ./examples/harness/benchmark -suite reliability -proxy <mcp-proxy> -server <filesystem-ultra>
```

Cubre paginación de búsqueda, diagnóstico de `multi_edit`, lista `detail:full` de `project_replace` y `git remote`. La telemetría de motor *puede* respaldar la eval real. No sustituye Claude Code / Codex / OpenCode sobre un repo de verdad.

Hasta que esa eval exista, no hay cifra de ahorro de tokens ni de velocidad
que se pueda vender.

## Fuera de esta secuencia

- Política integral de escritura sobre metadatos `.git`.
- `STALE_READ` persistente: solo si se reproduce en **este** binario
  (v4.7.0+ ya avisa una vez por fichero y se silencia con `expected_hash`).

## Criterio global

El agente en v4.7.1 puede:

1. Identificar por qué falló cada edición de un `multi_edit` atómico.
2. Recuperar todos los resultados de una búsqueda con `offset`.
3. Consultar el destino Git sin red y ver la URL en `push`/`fetch`.
4. Interpretar stats sin mezclar proceso, sesión e histórico.

No puede, por defecto, **bloquear** un push al remoto equivocado:
hace falta `--git-remote-allow`.
