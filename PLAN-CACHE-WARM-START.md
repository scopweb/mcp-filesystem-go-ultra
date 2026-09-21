# Plan de ataque — Caché fiable y warm-start medido

Fecha: 2026-09-21
Estado: diseño consensuado; implementación pendiente.

## Objetivo y decisión

Reducir el tiempo hasta completar la primera tarea útil y la latencia de lecturas repetidas, preservando autorización, coherencia y cierre ordenado.

**Corregir la caché actual → medir → persistir hints y precargar → añadir snapshot de contenido si gana → evaluar mmap por separado.**

Reiniciar el MCP vacía su RAM, pero no necesariamente la caché del sistema operativo. Una copia local persistida puede añadir trabajo frente a `os.ReadFile`. No se comprometen cifras de aceleración ni se presupone ROI alto de una L2.

Este plan desarrolla una línea de rendimiento. La evaluación real con agentes y la recuperación durable de `PLAN-PENDIENTE.md` conservan su alcance.

## Entregas y dependencias

Cada entrega debe poder revisarse y validarse por separado. Los identificadores son paquetes de trabajo, no PRs ya creados.

| Entrega | Alcance | Dependencia | Estado |
|---|---|---|---|
| CACHE-01 | Frescura y coherencia bytes/metadata | Ninguna | Hecho |
| CACHE-02 | Métricas fiables y contabilidad de memoria | CACHE-01 | Hecho |
| CACHE-03 | Prefetch autorizado y cierre ordenado | CACHE-01 | Hecho |
| CACHE-04 | TTL configurable y baseline reproducible | CACHE-01–03 | Hecho |
| WARM-01 | Experimento opt-in: manifiesto + precarga | Baseline CACHE-04 | Condicionado |
| WARM-02 | Experimento opt-in: snapshot de contenido | Resultados WARM-01 | Condicionado |
| MMAP-01 | Estudio y rediseño de mmap | Caso de uso medido | Diferido |

## Fase 0 — Corregir y medir

### CACHE-01 — Frescura

Archivos de partida: `cache/intelligent.go`, `core/read_dedup.go`, `core/engine.go` y pruebas adyacentes.

Hecho (2026-09-21). Verificación: `go test ./cache/... ./core/...` y `go test ./tests/...`. `go test -race` no se ejecutó aquí (`CGO_ENABLED` + gcc ausentes).

- [x] Exigir metadata para aceptar una entrada de contenido.
- [x] Invalidar ante tamaño distinto o mtime distinto, incluyendo timestamps anteriores.
- [x] Metadata ausente, error de stat o entrada inválida: miss y lectura del original por el flujo autorizado. No convertir un fallo de caché en rechazo de la operación.
- [x] Vincular bytes y metadata a una misma captura lógica; evitar el `Stat` tardío independiente de `SetFile`.
- [x] Diseñar comprobación antes/después de la lectura y tratamiento de sustituciones del archivo; usar identidad del archivo donde esté disponible.
- [x] Ante cambio detectado durante captura: no cachear el resultado; definir reintento acotado y respuesta final conforme al contrato de lectura.
- [x] Revisar carreras entre lectura en vuelo e invalidación para impedir repoblación obsoleta después de una mutación propia.
- [x] Documentar el límite: tamaño + mtime no detectan toda escritura que preserve ambos. No prometer snapshot atómico frente a cualquier escritor externo.

Aceptación: regresiones de mtime adelantado/atrasado, igual tamaño con mtime distinto, metadata ausente, borrado, reemplazo y mutación coordinada durante lectura. Usar sincronización determinista en pruebas de carreras, no depender únicamente de sleeps.

Límite vigente: `GetFileFresh` compara size + mtime; identidad de inodo/file-index solo si `Stat` la expone (Unix sí, Windows Stat no). Un rewrite in-place que preserve ambos valores puede servirse de caché. `ReadFileStable` reintenta hasta 3 veces si before/after no coinciden y, si sigue inestable, devuelve los bytes leídos sin cachear.

### CACHE-02 — Métricas y memoria

Hecho (2026-09-21). Verificación: `go test ./cache/... ./core/...` y `go test ./tests/...`.

- [x] Separar consultas internas de caché de lecturas lógicas solicitadas.
- [x] Registrar hit/miss de contenido después del veredicto de frescura; rechazos stale cuentan como miss.
- [x] Evitar multiplicar misses por consultas internas de `singleflight` y distinguir lectores que comparten una carga de lecturas físicas del original.
- [x] Separar actividad de prefetch de demanda real.
- [x] Sustituir `currentSize += len` / `currentSize -= 0` por una contabilidad definida que contemple reemplazo, invalidación, expiración, expulsión y flush.
- [x] Separar bytes de contenido residente y capacidad reservada; indicar qué valores son estimaciones. No presentar su suma como memoria real del proceso.
- [x] Revisar consumidores de métricas y conservar compatibilidad de contratos públicos.

Aceptación: regresiones en `cache/accounting_test.go` y `TestReadFileContent_DemandCountsOncePerCall`.

Contrato público: `GetHitMiss`/`GetHitRate` = demanda file+dir+meta tras frescura (prefetch y peeks internos excluidos). `GetMemoryUsage` = bytes de contenido residente rastreados, no RSS ni capacity de BigCache. `Memory()` separa `ResidentBytes` y `CapacityBytes` (reservado, estimación). `CoalescedReads` cuenta callers de `singleflight.Do` con `shared=true` (el owner también si hubo waiters). `DiskLoads` = capturas físicas. BigCache eviction se detecta en el siguiente lookup.

### CACHE-03 — Prefetch y lifecycle

Hecho (2026-09-21). Verificación: `go test ./cache/... ./core/...` y `go test ./tests/...`. `go test -race` no se ejecutó aquí (cgo/gcc ausentes).

- [x] Elegir durante implementación entre mover la coordinación al engine o inyectar un lector autorizado. Toda lectura anticipada debe usar la política efectiva de raíces, secretos y resolución de enlaces.
- [x] Revalidar al ejecutar la lectura, no solo al encolar una ruta.
- [x] Acotar trabajo, concurrencia, cola y mapa de patrones; evitar precargas repetidas inútiles.
- [x] Definir cierre: impedir nuevos productores → cancelar o terminar trabajo pendiente → esperar al worker → cerrar/vaciar caché.
- [x] Hacer el cierre idempotente y evitar envíos a canales cerrados y escrituras posteriores al cierre.
- [x] Documentar cancelación y espera de I/O en curso: cancelar contexto no garantiza interrumpir un `os.ReadFile` ya bloqueado.

Elección: `PrefetchAuthorizer` inyectado desde el engine (`IsPathAllowed` + `ResolveAndAuthorize`). Sin autorizador no se rellena. Cola 100, 3 hermanos <100KB, un scan por path al 3er acceso, mapa de patrones tope 4096. Close: `closed` + close canal bajo mutex → el worker descarta el resto de la cola → `Wait` → Flush → Close BigCache. Idempotente (`closeOnce`). Un `ReadFileStable` ya bloqueado no se interrumpe; Close espera a que termine.

### CACHE-04 — TTL y baseline

Hecho (2026-09-21). Verificación: `go test ./cache/... ./core/... ./internal/mcpserver/...`.

- [x] Añadir `--cache-ttl` para contenido, validar duración positiva y propagarla a BigCache.
- [x] Mantener inicialmente 3 minutos por compatibilidad; comparar con 10 minutos antes de cambiar el default.
- [x] Revisar semántica real de expiración/limpieza de BigCache; no asumir que cada acceso renueva el TTL.
- [x] Alinear vida de metadata con contenido; conservar miss si falta metadata.
- [x] Documentar que TTL gobierna retención, no coherencia; no cambiar automáticamente TTL de listados/metadata genérica.
- [x] Preparar escenarios reproducibles y registrar resultados del baseline corregido.

`--cache-ttl` (default `3m`, mínimo `1s`, Go duration). BigCache `LifeWindow` no se renueva en Get. Metadata de frescura (`fstat:`) usa el mismo TTL; listados y metadata genérica no. Default se queda en 3m hasta un A/B medido vs 10m.

Baseline a medir (aún sin cifras; no se asume ganancia):
1. Caché corregida, TTL 3m (default)
2. Misma carga, TTL 10m (`--cache-ttl=10m`)
3. Reinicio MCP con page cache caliente y fría; SSD local
Métricas: time-to-ready, p50/p95 lecturas tempranas, hit rate de demanda, resident bytes. No promover 10m a default sin ese A/B.

**Salida de Fase 0:** CACHE-01–04 verificados, pruebas relevantes aprobadas y baseline registrado. La implementación de persistencia espera a este punto.

## Fase 1 — WARM-01: manifiesto + precarga

Experimento opt-in, sin persistir contenido.

- [ ] Guardar top-K de rutas canónicas, frecuencia y recencia de accesos de demanda; excluir accesos especulativos del ranking.
- [ ] Versionar y acotar el manifiesto; publicación atómica y recuperación como manifiesto vacío ante corrupción.
- [ ] Reautorizar rutas con las raíces y política actuales en carga y ejecución; descartar entradas ya no permitidas.
- [ ] Cargar originales en background después de que el servidor esté disponible.
- [ ] Acotar bytes, número de archivos, tiempo de programación y concurrencia; dar prioridad a lecturas de demanda.
- [ ] Definir ubicación privada, aislamiento por proyecto y comportamiento de varios procesos antes de escribir el manifiesto.
- [ ] Integrar guardado periódico y cierre best-effort, sin depender de cierre limpio para conservar todos los hints.
- [ ] Medir utilidad: bytes precargados utilizados, desperdicio, tiempo hasta primera tarea y latencia temprana.

Aceptación funcional: archivo corrupto, versión desconocida, rutas revocadas, secretos, archivo desaparecido, cierre durante precarga y competencia con lecturas reales.

**Puerta de decisión:** comparar con CACHE-04 usando las mismas tareas. Conservar como opt-in o descartar si el beneficio no es reproducible o el coste domina. No promover a default por un único benchmark favorable.

## Fase 2 — WARM-02: snapshot de contenido

Solo con evidencia de un coste residual de lecturas que justifique duplicar bytes. Comparar también directamente con baseline, no únicamente con WARM-01.

- [ ] Empezar por snapshot acotado de BigCache para repoblar L1; no introducir inicialmente una L2 consultada en cada miss ni otra base de datos.
- [ ] Persistir contenido seleccionado y su metadata coherente, formato versionado y verificación de integridad.
- [ ] Revalidar original y autorización antes de promover/servir; un hash del blob no prueba que el original siga igual.
- [ ] Tratar ausencia, corrupción o incompatibilidad como miss, sin impedir arrancar.
- [ ] Separar límite de disco, límite de warm-up, tamaño máximo por archivo y política de limpieza.
- [ ] Definir publicación atómica, concurrencia multiproceso y permisos/ACL adecuados en Windows.
- [ ] Mantener listados, árboles y resultados de búsqueda fuera del MVP.

**Puerta de decisión:** adoptar solo si mejora de forma reproducible el escenario objetivo con costes de CPU, RAM, disco y startup aceptables. Si solo gana en red, documentar ese alcance y mantener opt-in.

## Matriz de evaluación

| Variante | Pregunta |
|---|---|
| Caché corregida, TTL 3m | Baseline fiable |
| Caché corregida, TTL 10m | Beneficio sin persistencia |
| Manifiesto + precarga | Valor de recordar qué se utiliza |
| Snapshot de contenido | Valor de duplicar los bytes |

- Escenarios: reinicio MCP con page cache caliente; page cache fría con método de preparación documentado; SSD local; red; Windows y Linux/WSL según disponibilidad.
- Reiniciar solo el MCP no cuenta como page cache fría. No vaciar cachés globales de una máquina de trabajo como preparación rutinaria.
- Cargas: repositorios representativos, archivos pequeños/medios, relecturas, conjunto de trabajo cambiante, sesiones que superen el TTL y modificaciones externas.
- Métricas: time-to-ready, tiempo hasta primera tarea útil, p50/p95 de lecturas tempranas y sostenidas, CPU, RAM, bytes I/O, escrituras de snapshot y aciertos útiles de prefetch.
- Registrar commit y binario usado, hardware, SO, flags, dataset, secuencia de operaciones, repeticiones y dispersión; alternar orden de variantes para reducir sesgo de calentamiento.
- Fijar antes de comparar candidatos el umbral de mejora y presupuestos de regresión, apoyándose en variabilidad del baseline. No elegir umbrales después de ver el ganador.
- Separar benchmark del engine, latencia MCP y evaluación real con agentes; uno no sustituye a los otros.

## Validación técnica durante implementación

Pruebas dirigidas por entrega; al integrar Fase 0:

```text
go test ./cache/... ./core/... ./internal/mcpserver/...
go test ./tests/...
go test -race ./cache/... ./core/... ./internal/mcpserver/...
go build ./cmd/filesystem-ultra
```

Documentar restricciones del entorno para el detector de carreras. Incluir pruebas de regresión relevantes en cada entrega y verificar los contratos existentes si se modifican métricas expuestas. Estas comprobaciones están pendientes; este documento no acredita su ejecución.

## Líneas separadas y cierre

- **MMAP-01:** solo con caso medido. Rediseñar referencias, liberación y vida útil de buffers, acceso concurrente y truncados externos antes de integrar mmap real por plataforma.
- **e3 durable:** sigue en `PLAN-PENDIENTE.md`; recibos y journals necesitan garantías de recuperación distintas de una caché descartable.
- **Watcher:** su integración no es requisito del warm-start; si se propone, justificarla con medidas y conservar validación ante eventos perdidos.
- **PMEM/Optane:** fuera de este plan.

Fase 0 (CACHE-01–04) cerrada. Próxima acción: WARM-01 solo si se decide persistir hints; no hay L2 de contenido hasta que el baseline lo pida.
