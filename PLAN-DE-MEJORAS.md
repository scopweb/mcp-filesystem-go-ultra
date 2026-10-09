# Plan de mejoras de MCP Filesystem Ultra

Fecha: 9 de octubre de 2026.
Estado: plan pendiente de ejecución.
Base de la revisión: `main` en `badcee4`, con cambios locales preexistentes.
Documento de origen: [mcp-filesystem-go-ultra-mejoras.md](mcp-filesystem-go-ultra-mejoras.md).

## Objetivo y orden obligatorio

Mejorar seguridad, fiabilidad de CI y compatibilidad sin mezclar correcciones urgentes con migraciones amplias.

**La primera acción de implementación será actualizar Go de 1.27.1 a 1.27.2.** Después se abordarán el fallo de copia y la recuperación de CI. No se actualizarán simultáneamente el SDK MCP y todas las dependencias.

Este documento no acredita que las mejoras estén implementadas. Las afirmaciones de reproducción del documento de origen deben verificarse con pruebas reproducibles antes de declarar cerrado cada hallazgo.

## Reglas de ejecución

- Leer las instrucciones vigentes del repositorio y revisar el estado de Git antes de editar.
- Preservar cambios locales ajenos y comprobar si algún punto ya se ha corregido.
- Ejecutar una fase cada vez, con un diff acotado y sus verificaciones.
- No cambiar contratos públicos, valores predeterminados o políticas de confirmación incidentalmente.
- No saltar tests portables para conseguir un CI verde ni desactivar controles de validación.
- No realizar commit, push, creación de etiquetas ni publicación de releases sin petición expresa.
- Registrar por fase: archivos modificados, pruebas ejecutadas, resultados y bloqueos pendientes.

## Resumen de fases

| Orden | Fase | Prioridad | Esfuerzo estimado |
|---|---|---|---|
| 1 | Actualizar Go a 1.27.2 | Primera acción | Bajo |
| 2 | Corregir copia hacia enlaces simbólicos | P0 | Medio |
| 3 | Recuperar CI y empaquetado | P0 | Medio |
| 4 | Endurecer recursos `file://` y dashboard | P1 | Medio |
| 5 | Definir y reforzar el límite CLI/Roots | P1 | Medio |
| 6 | Actualizar SDK y verificar compatibilidad MCP | P1 | Medio |
| 7 | Corregir anotaciones y documentación | P1–P2 | Bajo–medio |
| 8 | Completar salidas estructuradas incrementalmente | P2 | Medio |
| 9 | Diseñar y migrar gradualmente a `os.Root` | P2 | Alto |
| 10 | Evaluar mejoras funcionales y distribución | P3 | Variable |

Los esfuerzos son orientativos. Cada fase puede dividirse en cambios independientes; compartir prioridad no implica agruparlos en un único parche.

## Fase 1 — Actualizar Go a 1.27.2

### Trabajo

- [ ] Comprobar la versión efectiva mediante `go version` y la configuración de selección de toolchain.
- [ ] Actualizar la directiva `go` de `go.mod` de `1.27.1` a `1.27.2`.
- [ ] Actualizar `GO_VERSION` en `.github/workflows/ci.yml` y `.github/workflows/release.yml`.
- [ ] Localizar otras referencias activas a la toolchain y alinearlas. Conservar referencias históricas en changelogs.
- [ ] Asegurar que la compilación local y la automatizada utilizan realmente Go 1.27.2.
- [ ] Mantener las versiones de módulos actuales en esta fase, salvo necesidad técnica demostrada.

### Verificación y aceptación

- `go version` confirma Go 1.27.2 en el entorno de compilación utilizado.
- Compilar el servidor desde `./cmd/filesystem-ultra`, usando una salida temporal para evitar sobrescribir ejecutables en uso.
- Ejecutar `go vet ./...` y registrar los errores preexistentes; la revisión anterior encontró cuatro avisos de copia de mutex en `cache/accounting_test.go`.
- Ejecutar `govulncheck ./...` si está disponible y tiene acceso a su base de datos; registrar versión, fecha y resultado. Un escaneo no disponible no equivale a un resultado limpio.
- Inspeccionar el diff para confirmar que no se actualizaron dependencias incidentalmente.

**Cierre:** toolchain y configuración alineadas en 1.27.2, compilación comprobada y bloqueos conocidos registrados. Esta fase no exige presentar el CI como verde antes de corregirlo en la fase 3.

## Fase 2 — Corregir copia hacia enlaces simbólicos

### Trabajo

- [ ] Reproducir en un entorno temporal el destino de copia que es un symlink colgante hacia fuera de la raíz permitida.
- [ ] Añadir una prueba de regresión que compruebe que no se crea ni modifica ningún archivo fuera de la raíz.
- [ ] Revisar `CopyFile`, `copyFile`, `copyDirectory` y las rutas de copia de batch y pipeline.
- [ ] Corregir la validación del destino y el tratamiento de errores de `Stat`/`Lstat`.
- [ ] Usar creación exclusiva donde el contrato ya prohíbe sobrescribir, sin considerarla protección suficiente para los directorios intermedios.
- [ ] Verificar el tratamiento de enlaces en componentes intermedios y de cambios concurrentes de rutas.
- [ ] Mantener la migración completa a `os.Root` fuera de este parche urgente.

### Precauciones de diseño

`ResolveAndAuthorize()` descarta actualmente el indicador de enlace devuelto por `ResolveSymlinks()`. Además, la resolución tiene un fallback al ancestro existente. Añadir únicamente esa llamada al destino no demuestra que el fallo quede corregido.

`O_EXCL` protege el componente final frente al escenario descrito, pero no constituye por sí solo una solución integral para carreras en directorios intermedios.

### Verificación y aceptación

- Casos con destino inexistente normal, existente, enlace colgante y enlace hacia dentro/fuera de la raíz.
- Copia de archivos y directorios; entrada pública MCP, API core, batch y pipeline cuando corresponda.
- Pruebas en Linux y Windows. Si Windows no permite crear symlinks, documentar el caso no ejecutado y verificarlo en un entorno habilitado.
- Comprobar ausencia de efectos externos y compatibilidad con hooks, políticas y recuperación.

**Cierre:** el test reproduce el fallo antes del arreglo y pasa después; las copias legítimas siguen funcionando. No declarar eliminadas todas las carreras TOCTOU a partir de un único test.

## Fase 3 — Recuperar CI y empaquetado

### Trabajo

- [ ] Corregir los mensajes de `cache/accounting_test.go` que pasan por valor estructuras con `sync.RWMutex`; preferir campos concretos.
- [ ] Reproducir los fallos de tests Linux citados en el documento de origen sobre el estado actual.
- [ ] Convertir casos generales a rutas portables; separar casos realmente exclusivos de Windows sin eliminar cobertura equivalente.
- [ ] Renombrar `$exe` a `$stagedExe` en `scripts/pack-mcpb.ps1` para evitar colisión con `$Exe`; sustituir también `$input` por un nombre explícito.
- [ ] Descargar los binarios de ripgrep antes de los builds `embed_rg` del workflow de release.
- [ ] Corregir las referencias `../release/README.txt`: el archivo está en `release/README.txt` dentro del checkout.
- [ ] Corregir la selección de archivos para gofmt en push y PR, contemplando historial disponible y el primer push de una rama.
- [ ] Revisar la duplicación de caché entre `setup-go` y `actions/cache`, y la repetición innecesaria de suites.
- [ ] Actualizar las acciones en un cambio separado, verificando versiones oficiales y requisitos del runner antes de elegirlas.

### Verificación y aceptación

- `go vet ./...` pasa.
- Suites core, servidor, seguridad y E2E se ejecutan realmente y pasan en las plataformas previstas.
- Compilación normal y `embed_rg` verificadas desde un checkout limpio.
- Empaquetado `.mcpb` probado con ejecutable proporcionado y con compilación propia.
- Archivos de distribución contienen los binarios, instrucciones y checksums esperados.
- El CI remoto solo se declara recuperado tras una ejecución satisfactoria del cambio, cuando se autorice subirlo.

**Cierre:** validaciones locales completas y estado remoto explícito. Preparar artefactos o un workflow funcional no autoriza a publicar una release; el workflow actual crea un borrador.

## Fase 4 — Endurecer recursos `file://` y dashboard

### 4A. Recursos

- [ ] Revisar `internal/mcpserver/resources.go` y reutilizar controles de acceso y lectura segura compatibles con su contrato.
- [ ] Aplicar un límite durante la lectura, no solo una consulta previa del tamaño.
- [ ] Preservar la autorización de políticas y secretos ya existente.
- [ ] Devolver texto válido o contenido binario con MIME y representación adecuada.
- [ ] Contemplar el crecimiento de base64 y el límite final de respuesta.
- [ ] Incorporar auditoría y cancelación coherentes con el resto del servidor.

**Aceptación:** tests de archivos grandes, límites exactos, binarios, denegaciones y enlaces; sin lecturas ilimitadas. Añadir capacidades multimedia queda fuera de este arreglo.

### 4B. Dashboard

- [ ] Añadir validación global del host HTTP que cubra GET y POST.
- [ ] Distinguir dirección de escucha (`--host`) de hosts HTTP permitidos; pasar `--host` no debe desactivar la protección.
- [ ] Definir validación de nombre/IP y puerto, incluidos IPv4 e IPv6.
- [ ] Mantener las defensas de Origin y Fetch Metadata para operaciones que modifican estado.

**Aceptación:** hosts válidos aceptados y hosts ajenos rechazados antes de servir logs o backups. Una prueba con `curl` demuestra aceptación o rechazo de Host, no por sí sola un ataque completo de DNS rebinding en navegador.

## Fase 5 — Definir y reforzar el límite CLI/Roots

### Trabajo

- [ ] Documentar y comprobar `replace`, `union` e `ignore` actuales.
- [ ] Definir explícitamente si la allowlist CLI debe ser un límite máximo para Roots recibidas del cliente.
- [ ] Diseñar `intersect` solo después de resolver la semántica de ausencia de Roots e intersección vacía.
- [ ] Separar internamente «acceso abierto solicitado» de «ninguna ruta autorizada».
- [ ] Cubrir raíces iguales, anidadas, disjuntas, ancestros del cliente, UNC y diferencias de plataforma.
- [ ] Evaluar compatibilidad antes de cambiar defaults de perfiles o del bundle.

**Riesgo imprescindible:** actualmente una lista vacía puede significar acceso abierto. Una intersección sin coincidencias nunca debe traducirse en acceso a todo el disco.

**Aceptación:** pruebas que demuestren que el modo restrictivo no amplía el permiso CLI, tampoco tras refrescos. Hasta decidir un nuevo modo, `--roots-mode=ignore` es la opción existente para mantener las rutas CLI.

## Fase 6 — Actualizar SDK y verificar compatibilidad MCP

### Trabajo

- [ ] Con CI utilizable, actualizar `mcp-go` de v1.0.0 a v1.2.0 en un cambio aislado, tras revisar sus notas oficiales.
- [ ] Actualizar otros módulos por separado o solo cuando sean necesarios para ese cambio.
- [ ] Evaluar recuperación de panics en handlers de herramientas y recursos, con respuestas de error verificables.
- [ ] Probar MCP 2025-11-25 y MCP 2026-07-28 mediante stdio real.
- [ ] Decidir entre soportar explícitamente 2026-07-28 o restringir versiones de forma efectiva y comprobada.
- [ ] Revisar Roots tanto desde las notificaciones legacy como desde `list_allowed_directories`, que también invoca su refresco.
- [ ] No eliminar timeout ni asincronía de Roots solo porque cambie el SDK; verificar clientes sin soporte, errores y bloqueos.
- [ ] Mantener el SDK actual como familia tecnológica; una migración al SDK oficial necesita justificación independiente.

### Verificación y aceptación

- E2E de initialize legacy y de server/discover moderno con los campos `_meta` oficiales.
- Pruebas de negociación, versiones no admitidas, Roots, cancelación y errores.
- Catálogo, esquemas, contenido estructurado y anotaciones compatibles con los clientes previstos.
- Documentación y manifiesto reflejan el soporte realmente probado, no solo el anunciado por el SDK.

## Fase 7 — Corregir anotaciones y documentación

- [ ] Declarar `openWorldHint` explícitamente según el comportamiento real de cada herramienta.
- [ ] Revisar anotaciones considerando todas las acciones de las herramientas multipropósito.
- [ ] Conservar `server_info` como no read-only mientras incluya `artifact/write`.
- [ ] Verificar idempotencia y destructividad sin inferirlas únicamente del nombre de una operación.
- [ ] Corregir recuentos de herramientas, defaults de backups y soporte de protocolo.
- [ ] Contrastar SECURITY.md con el estado actual, incluyendo confirmaciones, Git, Roots y límites de protección.
- [ ] Priorizar la documentación que afecta a configuración segura antes que los ajustes cosméticos.

**Aceptación:** tests de contratos y catálogo; ejemplos y valores por defecto contrastados con el registro y los flags actuales. No inventar compromisos de soporte ni plazos del mantenedor.

## Fase 8 — Completar salidas estructuradas incrementalmente

- [ ] Empezar por `get_file_info` y operaciones estables de archivos.
- [ ] Definir esquemas que cubran las variantes de cada herramienta y añadir cobertura al sweep de handlers.
- [ ] Evaluar validación de salida del SDK en tests/E2E antes de activarla globalmente.
- [ ] Preservar la compatibilidad del fallback textual.
- [ ] Graduar `context_pack` en el ciclo correspondiente antes de añadirle un esquema.

**Restricción vigente:** `experimental.go` provoca un panic si una herramienta experimental declara `outputSchema`. No añadir esquemas indiscriminadamente a todas las herramientas sin revisar su estabilidad.

**Aceptación:** payloads conformes y pruebas de variantes relevantes; ninguna herramienta experimental viola la política de registro.

## Fase 9 — Diseñar y migrar gradualmente a `os.Root`

- [ ] Inventariar las rutas de E/S y separar operaciones internas de procesos externos como Git y hooks.
- [ ] Diseñar propiedad, cierre y reemplazo de handles ante cambios de Roots.
- [ ] Definir copias y movimientos entre raíces, backup, restauración, temporales y rollback.
- [ ] Probar restricciones y comportamiento en Windows, Linux, UNC y WSL donde estén soportados.
- [ ] Migrar por grupos de operaciones, manteniendo políticas de archivos y secretos.
- [ ] Medir impacto de rendimiento y comprobar ausencia de fugas de handles.

**Aceptación:** pruebas de contención y concurrencia por operación migrada, con límites de plataforma documentados. `os.Root` no es un sandbox de procesos ni sustituye las políticas de autorización.

## Fase 10 — Mejoras opcionales y decisiones independientes

### Progreso

- [ ] Emitir notificaciones solo cuando corresponda al protocolo y exista un token de progreso.
- [ ] Vincularlas a cancelación y etapas reales de la operación.
- [ ] Medir comportamiento en clientes objetivo; no prometer que una notificación amplía su timeout.

### Multimedia

- [ ] Diseñar lectura de imagen/audio con límites de tamaño y negociación apropiada.
- [ ] Respetar la congelación del núcleo y el ciclo experimental antes de añadir un modo a `read_file`.
- [ ] Evaluar enlaces a recursos sin cambiar inadvertidamente los resultados existentes.

### Confirmación humana mediante MRTR

- [ ] Tomar una decisión de producto explícita: cambia la política de ejecución actual.
- [ ] Vincular aprobación, argumentos y versión del archivo; evitar efectos antes de confirmar.
- [ ] Diseñar reintentos, expiración y comportamiento con clientes sin soporte.
- [ ] No presentar `force:true` como prueba de aprobación humana.

### Distribución

- [ ] Resolver identidad del módulo y semantic import versioning para versiones v4, incluido `/v4` cuando corresponda.
- [ ] Documentar `go install` apuntando al paquete ejecutable `cmd/filesystem-ultra`, no a la raíz del módulo.
- [ ] Añadir plataformas de compilación y probar cada paquete distribuido.
- [ ] Evaluar bundles adicionales, Docker y GoReleaser según demanda real.

## Criterio global de finalización

Una fase está completada cuando su cambio está aplicado, el diff ha sido revisado y sus verificaciones han producido evidencia registrada. Los tests omitidos, los entornos no disponibles y las decisiones pendientes deben quedar explícitos.

El primer hito es: **Go 1.27.2 efectivo, fallo de copia corregido mediante regresión y suites de CI nuevamente ejecutables**. Las nuevas funcionalidades no deben retrasar ese hito.

## Registro de ejecución (2026-10-09)

Sin commit ni push.

### Fase 1 — hecha
Archivos: `go.mod`, `.github/workflows/ci.yml`, `.github/workflows/release.yml`, `build-windows.bat`, `README.md`, `CLAUDE.md`. Changelog histórico intacto. `go.sum` sin cambios de dependencias.
Evidencia: `go version` → go1.27.2 (toolchain auto desde 1.27.1). Binario temporal `filesystem-ultra-v4-1272.exe` compilado con go1.27.2. `govulncheck` v1.7.0, DB 2026-10-08, sin vulnerabilidades. `go vet ./...` seguía fallando en los 4 avisos de mutex de `cache/accounting_test.go` (fase 3).

### Fase 2 — hecha
El test reproducía el escape en Linux (`CopyFile` devolvía nil y creaba el archivo fuera). Arreglo: `resolveContainmentPath` no trata un symlink colgante como nombre bajo el padre; la copia rechaza destinos que atraviesan symlinks y crea con `O_EXCL`. Cubre API core, directorio, batch, pipeline y `copy_file`.
Evidencia Linux (WSL, Go 1.27.2): PASS de los tests de symlink. Windows: `os.Symlink` falla con privilegio insuficiente; casos omitidos, no ejecutados aquí. `O_EXCL` no cierra carreras en directorios intermedios. `go test ./core/` en Windows: ok.

### Fase 3 — hecha en local, CI remoto no ejecutado
`go vet ./...` pasa. Tests de ruta/`logicalServerName` pasan en Linux. `pack-mcpb.ps1` probado con ejecutable dado y con compilación propia. `embed_rg` compilado en Windows y en WSL. `release.yml` descarga ripgrep y usa `release/README.txt`. gofmt del workflow contempla PR, push y primer push. Caché duplicada de `actions/cache` retirada; la de `setup-go` queda. Suites Windows duplicadas retiradas.
Pendiente: subir el cambio para declarar el CI remoto verde. Bump de acciones (checkout/setup-go/cache/artifacts/gh-release) no aplicado: el plan lo pide en un cambio separado y no se verificaron las etiquetas oficiales en esta sesión.

### Fase 4 — hecha
`file://` lee con tope durante la lectura (50 MB), distingue texto y binario, autoriza la ruta resuelta y audita. Dashboard valida `Host` en GET y POST; `--host 0.0.0.0` no abre hosts ajenos. `rejectCrossSite` se mantiene.
Evidencia: `TestReadFileResource_*`, `TestHostHeader_*`, `TestGuardHost_BlocksBeforeHandler`. No es una prueba de DNS rebinding en navegador.

### Fase 5 — parcial
Una lista vacía que no es `--insecure-open` ya no abre el disco (`TestSetAllowedPaths_EmptyNonInsecureDenies`). `replace`/`union`/`ignore` documentados. `intersect` no se añade: una intersección vacía no debe significar acceso total. Defaults de perfil y bundle no cambiados.

### Fase 6 — hecha, aislada
`mcp-go` v1.0.0 → v1.2.0. Notas oficiales v1.2.0 revisadas (2026-10-08). No se subieron `x/sys` ni `x/sync`. `WithRecovery` y `WithResourceRecovery` activos. El timeout de Roots no se tocó. El anuncio de protocolo sigue en 2025-11-25; no se añade una restricción que el SDK no aplica.
Evidencia: `go test ./internal/mcpserver/ ./core/` y `TestRecovery_ToolPanicBecomesJSONRPCError`.

### Fases 7–10 — no aplicadas
Anotaciones, esquemas, `os.Root` y las decisiones de producto (MRTR, multimedia, `/v4`) quedan pendientes.
