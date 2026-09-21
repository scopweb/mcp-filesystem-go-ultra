# Cache baseline — ejecución real 2026-09-21

**Completado:** 3 repeticiones por TTL, 24 archivos por fase, 4 fases, **576 lecturas verificadas, 0 errores**, prefetch = 0. La retención de 10m evita recargas a los ~191s, pero **no se observa una mejora consistente de latencia**. Se conserva el default 3m. No justifica todavía persistencia ni L2.

Este informe corrige el cierre anterior de CACHE-04: los tests y el flag TTL estaban implementados, pero se había declarado cerrado el baseline sin ejecutarlo ni aportar cifras.

## Artefactos y procedencia

Directorio real: **`C:\temp\cache-baseline-20260921`**.

- `cache-report.json`: informe completo, `complete:true`, muestras individuales, IDs, tiempos ns, bytes, errores, counters antes/después, corpus SHA256, flags, commit y estado local.
- `cache-summary.json`: agregación offline sobre muestras individuales; dispersión por repetición incluida.
- `cache-run-447314219\`: corpus original y logs `session-00..05\proxy.jsonl`, `restart-00..05\proxy.jsonl`.
- `existing-suites.json`: ejecución wire `-suite all` (PR-0 + reliability).
- `filesystem-ultra.exe`, `mcp-proxy.exe`, `benchmark.exe`: binarios usados. `benchmark-summary.exe`: runner con agregador offline añadido después de la medición, sin repetir ni modificar las muestras.
- `validation.md`: comandos de validación, resultados y limitaciones del entorno.

La ruta solicitada `C:\tempo` fue rechazada por `filesystem-ultra` al no estar en sus raíces. Se utilizó `C:\temp`, autorizada, con creación y verificación mediante herramientas FS. No se afirma haber escrito en `C:\tempo`.

Base: `3329257876c5dfede2f604943642e84170ab7605` **más cambios locales** (estado completo en JSON). Sin commit/push. Binarios compilados con `-trimpath`:

- Servidor SHA256: `b89f011ebe804e1c1c1d98679cfaf080e686481f27e64039cb1bb59b39990171`.
- Proxy SHA256: `8d1816bfc31e583b62089ee3e19f57ee55694277f7060d3762fe7b4937908d85`.

Máquina: Windows 11 Pro 10.0.26200, Intel Core i7-14700KF (20 cores / 28 lógicos), Samsung SSD 990 PRO 1TB NVMe; Go 1.27.1 windows/amd64. Host de trabajo, sin aislar carga externa, afinidad ni plan energético. No se ejecutaron tests/builds en paralelo con las lecturas medidas.

## Método

Ver [README](README.md#cache-suite-real-ttl-baseline) para comando y contrato. Ejecución 16:04:24–16:07:38 UTC. Alternancia 3m/10m/3m/10m/3m/10m; procesos independientes vivos durante una espera compartida. Las mediciones se hacen secuencialmente.

Corpus determinista compartido: 24 archivos, 1/8/32/128 KiB ×6; **1.038.336 bytes**. Una carpeta por archivo impide prefetch de hermanos. `read_file` en base64 devuelve bytes completos, con SHA256 comprobado; usa la ruta de demanda `ReadFileSnapshot` / `GetFileFresh`. No se deducen hits de los tiempos: se comprueban deltas de contadores file-demand y disk loads.

Se esperan 190s desde la última respuesta inicial completada, cota superior de la última inserción. Las repeticiones inmediatas deben ser hits sin nuevas cargas. Las cotas conservadoras de edad medidas abarcaron **190,728–191,416s**, dentro de (3m,10m). Ningún acceso al corpus durante la espera. Lectura envejecida es el tercer acceso; la ausencia de hermanos y contadores prefetch en cero evitan renovaciones incidentales.

Reinicio = cerrar el proceso y lanzar proxy/MCP nuevo contra los mismos archivos. Caché SO **sin purgar**, presumiblemente caliente por creación/lecturas, sin medir directamente su residencia. Incluso el escenario inicial no es SO frío.

## Latencias agrupadas

72 muestras por celda (24 × 3). Percentiles nearest-rank sobre las muestras originales; **no promedio de percentiles**. Unidad: ms; captura original ns mediante QPC (Windows). Las columnas runner incluyen serialización, proxy log/forward y decodificación JSON. El proxy termina antes de escribir su log y reenviar la respuesta.

| TTL | Fase | Proxy p50 | Proxy p95 | Runner p50 | Runner p95 | Hits / misses | Cargas original |
|---|---|---:|---:|---:|---:|---:|---:|
| 3m | Inicial | 2,306 | 3,780 | 2,469 | 4,624 | 0 / 72 | 72 |
| 10m | Inicial | 2,296 | 3,810 | 2,460 | 4,427 | 0 / 72 | 72 |
| 3m | Inmediata | 2,172 | 3,491 | 2,311 | 4,337 | 72 / 0 | 0 |
| 10m | Inmediata | 2,242 | 3,545 | 2,342 | 4,352 | 72 / 0 | 0 |
| 3m | Edad >3m | 4,326 | 8,065 | 4,925 | 9,136 | 0 / 72 | 72 |
| 10m | Edad >3m | 4,194 | 11,303 | 4,506 | 12,330 | 72 / 0 | 0 |
| 3m | Reinicio MCP | 2,201 | 3,587 | 2,420 | 4,492 | 0 / 72 | 72 |
| 10m | Reinicio MCP | 2,142 | 3,397 | 2,401 | 4,537 | 0 / 72 | 72 |

Dispersión envejecida (repeticiones 1/2/3):

- 3m p50: **4,488 / 4,324 / 4,322 ms**; p95: **7,658 / 8,712 / 5,783 ms**.
- 10m p50: **4,401 / 4,920 / 3,812 ms**; p95: **9,248 / 8,591 / 12,107 ms**.

La variación entre fases es mayor que la diferencia de medianas entre TTL. No se atribuye a la caché la causa del incremento tras la espera; tampoco se interpreta el p95 mayor de 10m como una regresión causal demostrada.

### Lanzamiento → primera lectura útil validada

Incluye arranque proxy/MCP, initialize, discovery, stats previas y primer `read_file` de 1 KiB verificado. No equivale a tiempo de servidor listo ni a completar una tarea de agente.

| TTL | Inicial, repeticiones 1/2/3 (ms) | Tras reinicio, repeticiones 1/2/3 (ms) |
|---|---|---|
| 3m | 399,088 / 100,594 / 98,077 | 101,317 / 97,473 / 122,566 |
| 10m | 95,465 / 100,060 / 119,545 | 96,069 / 98,819 / 92,341 |

La primera ejecución de 3m es un outlier de startup; se conserva, no se descarta.

### Bytes, contabilidad y errores

- Por TTL/fase (72 lecturas): **3.115.008 bytes de contenido validado**, **8.568 bytes de argumentos**.
- Respuestas proxy: **8.325.990 bytes** en inicial/reinicio, **8.326.008 bytes** en inmediata/envejecida. Incluyen envelope y contenido base64 duplicado en fallback/structured; no son bytes leídos del dispositivo.
- Total contenido verificado: **24.920.064 bytes**; argumentos **68.544 bytes**; respuestas **66.607.992 bytes**.
- Resident tracked: 0 antes de inicial/reinicio, **1.038.336 bytes** después. Antes/después de inmediata/envejecida permanece en 1.038.336 para ambas TTL.
- Esta cifra **no demuestra retención válida**: el seguimiento es perezoso y puede conservar expirados hasta la consulta. El miss real a los ~191s es el indicador pertinente para 3m. Capacidad reservada en JSON; no se midió RSS.
- **0 errores de lectura, 0 errores de integridad y 0 actividad de prefetch**. `disk_loads` cuenta capturas de archivo original, no I/O físico del SSD.

## Validación

- Builds servidor, proxy y runner: aprobados.
- `go test ./...`: aprobado (incluye cache/core/server/proxy/harness/tests/security).
- Tests específicos de alta resolución, proxy rápido, cache stats, corpus y escenarios: aprobados. Agregación offline validada posteriormente con `go test ./examples/harness/benchmark`.
- `-suite all` wire: aprobado; sus tres errores intencionados (OCC, patch y edición ambigua) son los escenarios esperados.
- `go vet ./cmd/proxy ./internal/benchclock ./core ./internal/mcpserver ./examples/harness/benchmark`: aprobado.
- `go vet ./...`: bloqueado por cuatro advertencias preexistentes `copylocks` en `cache/accounting_test.go:26,38,61,64` (formateo de `CacheStats` con mutex).
- `go test -race ./cmd/proxy ./internal/benchclock`: no ejecutable en este entorno: `-race requires cgo`.

## Alcance de la conclusión

Se acredita el funcionamiento real de retención 3m vs 10m y el vaciado de RAM al reiniciar. El corpus es sintético, pequeño, local, con codificación base64 y OS cache caliente; no representa red, un repositorio completo, archivos enormes, SO frío ni un modelo real. Son solo tres repeticiones por configuración. No se han medido CPU/RSS ni controlado el scheduler del host.

**Decisión:** mantener 3m; WARM-01 y L2 siguen condicionados a una mejora reproducible en una carga representativa. El baseline ya tiene cifras, pero no avala una promesa de aceleración.
