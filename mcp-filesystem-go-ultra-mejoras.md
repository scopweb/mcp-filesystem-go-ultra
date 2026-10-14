# mcp-filesystem-go-ultra: novedades y mejoras propuestas

Fecha: 8 oct 2026 (hora de Madrid). Base analizada: `main` @ `badcee4` ("security: close phase B and pack the server from the build", 08-10-2026 18:19 Madrid). Todo el análisis se hizo con un clon local de solo lectura; no se ha tocado nada en GitHub.

---

## 1. Qué hace hoy el repo

| Aspecto | Estado actual |
|---|---|
| Versión | v4.8.0 (`internal/mcpserver/run.go`, `serverVersion`) |
| Lenguaje / toolchain | Go 1.27.1 (`go.mod`), sin CGO |
| SDK MCP | **`github.com/mark3labs/mcp-go v1.0.0`** (comunitario, no el oficial `modelcontextprotocol/go-sdk`) |
| Transporte | **Solo stdio** (`server.ServeStdio` en `run.go`) |
| Spec declarada | "MCP 2025-11-25". 2026-07-28 aparece como "fuera de alcance" (README, ROADMAP, `bundle/manifest.json`) |
| Capacidades | tools (listChanged), resources (plantilla `file:///{+path}` en `resources.go`), roots (cliente), logging, instructions |
| Herramientas | 29 en `--profile=ultra` (por defecto), 18 en `agent`, 17 en `strict`: `read_file`, `write_file`, `edit_file`, `multi_edit`, `apply_patch`, `diff_files`, `search_files`, `list_directory`, `directory_tree`, `get_file_info`, `create_directory`, `move_file`, `copy_file`, `delete_file` (soft-delete), `backup` (undo/restore/trash), `batch_operations` (pipelines DAG + rollback), `project_replace`, `analyze_operation`, `analyze_code`, `context_pack`, `git`, `github_issues`, `gitlab_issues`, `wsl`, `minify_js`, `server_info`, `security_policy`, `help`, `list_allowed_directories` |
| Seguridad | Allowlist obligatoria (fail-closed desde v4.6.0), `IsPathAllowed` con `EvalSymlinks` + `filepath.Rel`, `ResolveAndAuthorize` como defensa TOCTOU, bloqueo de NTFS ADS, Unicode RTLO/zero-width, nombres reservados de Windows, secretos denegados por defecto (`--allow-secrets`), política de ficheros `--file-security-config`, `--readonly`, `--mutation-budget`, protección de la raíz permitida |
| Seguridad de edición | Backups con undo paso a paso, OCC (`content_hash`/`expected_hash`, `--auto-occ`), guarda contra reescrituras accidentales, evaluación de riesgo, dry-run |
| Salida estructurada | `outputSchema` + `structuredContent` en 16 de 29 herramientas; errores en un sobre JSON con `retryable`/`recovery` |
| Roots | `--roots-mode=replace` (por defecto) / `union` / `ignore`; conversión de URIs de Windows y UNC (`core/file_uri.go`) |
| Rendimiento | Caché en 3 niveles (BigCache + mapas TTL), watcher, streaming/chunking hasta 50 MB, ripgrep opcional embebido (`embed_rg`) |
| Extras | Dashboard HTTP (binario aparte), proxy stdio con métricas de tokens, `.mcpb` para Claude Desktop (solo win32) |
| Tests | Mucha cobertura: unitarios en `core/` e `internal/mcpserver/`, suite caja negra en `tests/`, fuzzing en `tests/security/`, E2E stdio en `tests/e2e/` |
| CI | `ci.yml` (test, build y security en ubuntu y windows) y `release.yml` (borrador de release al hacer push de una etiqueta `v*`) |
| Releases | **0 releases publicadas** en GitHub. Solo existe la etiqueta `v4.5.25` |
| Issues | 0 abiertas |

---

## 2. Hallazgos verificados (bugs y riesgos)

Cada hallazgo se ha reproducido en el box, no solo se ha deducido leyendo código.

### H1. Escape del sandbox con `copy_file` hacia un symlink colgante (ALTO)
- **Reproducción:** dentro del directorio permitido hay un symlink `evil -> <fuera>/copy.txt` cuyo destino no existe. `engine.CopyFile(src, allowed/evil)` devuelve `err=nil` y **crea `<fuera>/copy.txt`**. Probado con un test temporal sobre la API pública de `core` (`write_file` y `move_file` no escapan en el mismo escenario).
- **Causa:**
  1. `IsPathAllowed` (`core/engine.go`, ~L1286-1313): cuando `EvalSymlinks` falla porque el destino del enlace no existe, sube a padres y valida `padre_resuelto + "evil"`. Valida el nombre del enlace, no su destino. Es el mismo patrón ("fallback al directorio padre") que causó **CVE-2025-53109** en el servidor oficial ([Cymulate](https://cymulate.com/blog/cve-2025-53109-53110-escaperoute-anthropic/), [NVD](https://nvd.nist.gov/vuln/detail/cve-2025-53109)).
  2. `CopyFile` comprueba el destino con `os.Stat`, que en un enlace colgante dice "no existe". Además `ResolveSymlinks` solo se aplica al **origen**. Después `copyFile` abre con `os.OpenFile(dst, O_CREATE|O_WRONLY|O_TRUNC)` (`core/file_operations.go` ~L654), que **sigue el enlace**.
- **Vector realista:** un repo clonado que trae un symlink, más una inyección de prompt que pida "copia X a Y". Ejemplo de destino: `~/.config/autostart/x.desktop`.

### H2. CI en rojo en `main` desde hace al menos 8 ejecuciones, y nunca se ha publicado una release (ALTO para el proyecto)
- `go vet ./...` falla en `cache/accounting_test.go` (L26, 38, 61, 64: *"call of t.Fatalf copies lock value: CacheStats contains sync.RWMutex"*). Como el vet va antes que los tests, **los tests no se ejecutan en CI** ni en Linux ni en Windows.
- Al lanzar la suite localmente en Linux fallan 6 tests ajenos a la mejora del SDK: `TestPatchHeaderMatches`, `TestPathFloorFor_Table`, `TestApplyPathFloor_RaisesLowToHigh` (usan rutas `C:\...` sin guarda de GOOS), `TestReadFileBytesDeduped_ConcurrentSingleDiskRead`, `TestLogicalServerName` (cmd/proxy) y `TestAdvancedTextSearch_SkipsMinifiedContent`.
- **Build de Windows:** falla en `scripts/pack-mcpb.ps1`. En PowerShell las variables **no distinguen mayúsculas**, así que `$exe = Join-Path $serverDir ...` **pisa el parámetro `$Exe`**. Luego `Test-Path $Exe` mira la ruta de staging, que todavía no existe. El log de CI lo confirma: `missing built server: ...\dist\mcpb-stage\server\filesystem-ultra.exe`. De paso, `$input` es una variable automática de PowerShell y conviene renombrarla.
- **`release.yml`:** no ejecuta `embed/ripgrep/download.sh` (en `ci.yml` sí está), así que `go build -tags embed_rg` falla con `pattern all:rg-*: no matching files found` en Linux y Windows. Por eso no hay ninguna release.
- La comprobación de `gofmt` compara `origin/main...HEAD`: en un push a `main` el diff está vacío, y en PRs con checkout superficial `origin/main` no existe. En la práctica **no comprueba nada**.
- Las acciones usan Node 20 (`checkout@v4`, `setup-go@v5`, `cache@v4`) y el runner ya avisa de que está obsoleto.

### H3. El binario ya habla MCP 2026-07-28 aunque la documentación diga que no (MEDIO)
- `mcp-go v1.0.0` ya "support[s] the 2026-07-28 specification" ([release v1.0.0](https://github.com/mark3labs/mcp-go/releases/tag/v1.0.0)). Probado por stdio: `server/discover` con `_meta.protocolVersion=2026-07-28` responde `supportedVersions: ["2026-07-28","2025-11-25",...]`, y `tools/call` sin `initialize` funciona.
- **Efecto:** con un cliente moderno no llega `notifications/initialized`, así que `registerRootsSync` (`internal/mcpserver/roots_sync.go`) nunca se dispara y las Roots del cliente se ignoran en silencio (`source: cli`). Al menos sigue siendo fail-closed. README, ROADMAP y `.mcpb` dicen lo contrario.

### H4. Dashboard vulnerable a DNS rebinding (MEDIO)
- `cmd/dashboard` escucha en 127.0.0.1 y protege los POST con `Sec-Fetch-Site`/`Origin`, pero **no valida la cabecera `Host`**. Probado: `curl -H "Host: attacker.example:9177" -H "Sec-Fetch-Site: same-origin" /api/backups` devuelve 200. Con DNS rebinding, una web externa podría leer `/api/backups/file/...`, que sirve contenido de backups. La spec MCP pide expresamente defenderse del DNS rebinding en servidores HTTP locales ([transports, Security Warning](https://modelcontextprotocol.io/specification/2025-11-25/basic/transports)).

### H5. `openWorldHint` incorrecto (BAJO)
- En `tools/list`, 22 herramientas puramente locales (`read_file`, `write_file`, `edit_file`, `delete_file`, `list_directory`, etc.) salen con `openWorldHint: true`, que es el valor por defecto de mcp-go. Solo `security_policy`, `analyze_code`, `context_pack` y `git` lo fijan a `false`. El servidor oficial pone `openWorldHint: false` en todas sus herramientas ([README](https://github.com/modelcontextprotocol/servers/blob/main/src/filesystem/README.md)). `server_info` aparece como no read-only.

### H6. Plantilla de recursos `file:///{+path}` sin límites (BAJO-MEDIO)
- `internal/mcpserver/resources.go`: hace `os.ReadFile` **sin límite de tamaño** (el resto del servidor corta en 50 MB), devuelve binarios como `TextResourceContents`, no pasa por `ResolveAndAuthorize` y no deja rastro en la auditoría.

### H7. Documentación desalineada (BAJO)
- El README dice `--backup-max-age 72h` / `--backup-max-count 50`, pero el código usa **7 días / 100** (`run.go`).
- "25 tools / 16 strict" en *Available Tools*, frente a 29/17 en la cabecera.
- El módulo se llama `github.com/mcp/filesystem-ultra`, que no coincide con la URL del repo, así que `go install github.com/scopweb/...@latest` no funciona.

---

## 3. Contexto externo (octubre 2026)

- **Spec MCP 2026-07-28** ([changelog](https://modelcontextprotocol.io/specification/2026-07-28/changelog), [blog](https://blog.modelcontextprotocol.io/posts/2026-07-28/)):
  - Núcleo stateless: desaparece `initialize`, se añade `server/discover` y cada petición lleva `_meta` con versión y capacidades.
  - **Multi Round-Trip Requests** (`resultType: "input_required"`) sustituyen a las peticiones iniciadas por el servidor (`roots/list`, `elicitation/create`, `sampling`).
  - Tasks pasan a extensión.
  - `ttlMs`/`cacheScope` en los listados.
  - Orden determinista de `tools/list`.
  - **Roots, Sampling y Logging quedan *deprecated*** (SEP-2577) con una ventana de al menos 12 meses. Para Roots se recomienda pasar los directorios por configuración o por parámetros.
- **mcp-go** ([releases](https://github.com/mark3labs/mcp-go/releases)): v1.0.0 (02-09-2026, soporte 2026-07-28), v1.1.0, v1.1.1 y **v1.2.0 (08-10-2026)**. Cambios relevantes:
  - [#977](https://github.com/mark3labs/mcp-go/pull/977) (v1.1.1): stdio atiende cada petición fuera del bucle de lectura (evita los bloqueos que motivaron el timeout de 2 s de `roots_sync.go`).
  - [#1002](https://github.com/mark3labs/mcp-go/pull/1002): recupera panics en el bucle de lectura stdio.
  - [#989](https://github.com/mark3labs/mcp-go/pull/989): arrays vacíos en vez de `null`.
  - [#999](https://github.com/mark3labs/mcp-go/pull/999) y [#1027](https://github.com/mark3labs/mcp-go/pull/1027): errores tipados y resultados nulos.
  - [#1038](https://github.com/mark3labs/mcp-go/pull/1038): suite oficial de conformidad en CI.
- **SDK oficial Go** `modelcontextprotocol/go-sdk` **v1.8.0** ([release](https://github.com/modelcontextprotocol/go-sdk/releases/tag/v1.8.0)). Es uno de los 4 SDK *Tier 1* actualizados para 2026-07-28 ([blog](https://blog.modelcontextprotocol.io/posts/2026-07-28/)). Novedades: `ServerOptions.SupportedProtocolVersions` (permite restringir versiones), `StdioTransport.MaxLineLength` y `SetCacheable`.
- **Go**: 1.27.2 publicado el 08-10-2026 con *security fixes* en `os`, `net/http`, `crypto/tls`, `html/template` y el comando `go` ([release history](https://go.dev/doc/devel/release)). `os.Root` (Go 1.24) ofrece acceso a ficheros resistente a traversal y symlinks, y Go 1.25 le añadió `MkdirAll`, `WriteFile`, `ReadFile`, `Rename`, `RemoveAll`, `Readlink` y más ([blog](https://go.dev/blog/osroot), [Go 1.25](https://go.dev/doc/go1.25)).
- **Servidores comparables:**
  - Oficial `@modelcontextprotocol/server-filesystem` 0.6.3 ([README](https://github.com/modelcontextprotocol/servers/blob/main/src/filesystem/README.md)): `read_media_file` (imagen/audio en base64), `list_directory_with_sizes`, `openWorldHint:false`, distribución por Docker/NPX.
  - `rust-mcp-filesystem` v0.4.5 ([README](https://github.com/rust-mcp-stack/rust-mcp-filesystem)): solo lectura por defecto, Roots desactivado por defecto, desactivación de herramientas una a una, ZIP, instalación por Homebrew/npm/Docker/scripts.

---

## 4. Mejoras priorizadas

### 1. Cerrar el escape por symlink colgante y migrar a `os.Root`. Prioridad P0, esfuerzo medio
- **Qué:**
  - (a) *Parche inmediato:* en el camino de subida de `IsPathAllowed`, hacer `os.Lstat` de cada componente que no existe. Si es un symlink, resolverlo con `os.Readlink` y volver a validarlo, o rechazarlo.
  - En `copyFile` abrir el destino con `O_CREATE|O_EXCL`. En POSIX, `O_EXCL` falla si el destino es un symlink, aunque esté colgante. Es coherente porque `copy_file` ya se niega a sobrescribir.
  - Aplicar `ResolveSymlinks`/`ResolveAndAuthorize` también al destino.
  - (b) *Estructural:* abrir un `os.Root` por cada raíz permitida y hacer las E/S mediante `root.OpenFile/MkdirAll/Rename/RemoveAll`. Así el kernel impide escapar y desaparece la ventana TOCTOU.
- **Por qué:** es la misma clase de fallo que CVE-2025-53109 en el servidor oficial y rompe la promesa principal del proyecto ("strict path security").
- **Dónde:** `core/engine.go` (`IsPathAllowed`, `ResolveAndAuthorize`), `core/path_security.go` (`ResolveSymlinks`), `core/file_operations.go` (`CopyFile`, `copyFile`, `copyDirectory`), `core/pipeline.go:903` (acción copy), `core/wsl_sync.go:117,271`.
- **Test:** añadir el PoC (symlink colgante como destino de copy, también en pipeline y en batch) a `tests/security/cves_test.go`.

### 2. Arreglar el CI y conseguir una primera release. Prioridad P0, esfuerzo bajo
- **Qué:**
  - Pasar un puntero o campos sueltos (no el struct con `sync.RWMutex`) a `t.Fatalf` en `cache/accounting_test.go` para que pase `go vet`.
  - Añadir `if runtime.GOOS != "windows" { t.Skip() }` (o normalizar `\`) en `core/path_risk_test.go` y `core/patch_test.go`, y revisar los otros 3 tests que fallan.
  - Renombrar `$exe` a `$stagedExe` y `$input` a `$src` en `scripts/pack-mcpb.ps1`.
  - Añadir el paso `bash embed/ripgrep/download.sh` a los dos jobs de `release.yml`.
  - Arreglar el gate de gofmt (`fetch-depth: 0` y comparar con `github.event.before` / `github.base_ref`).
  - Actualizar las acciones: `checkout` v4→**v7.0.1**, `setup-go` v5→**v7.0.0**, `cache` v4→**v6.1.0**, `upload-artifact` v4→**v7.0.2**, `download-artifact` v4→**v8.0.2**, `softprops/action-gh-release` v2→**v3.0.3** (últimas etiquetas en sus repos de GitHub).
  - Subir `GO_VERSION` y `go.mod` (o `toolchain`) a **go1.27.2**.
- **Por qué:** hoy no hay red de seguridad, porque los tests no corren en CI, y los usuarios no tienen binarios descargables (0 releases).
- **Dónde:** `.github/workflows/ci.yml`, `.github/workflows/release.yml`, `scripts/pack-mcpb.ps1`, `cache/accounting_test.go`, los tests citados.

### 3. Subir mcp-go v1.0.0 → v1.2.0 y activar la recuperación de panics. Prioridad P1, esfuerzo bajo
- **Qué:** `go get github.com/mark3labs/mcp-go@v1.2.0` junto con `x/sys` v0.48.0, `x/sync` v0.23.0 y `x/text` v0.42.0. Añadir `server.WithRecovery()` y `server.WithResourceRecovery()` en `newFilesystemMCPServer()`.
- **Comprobado en el box:** compila, `go vet` sin errores nuevos e `internal/mcpserver` pasa entero. Los fallos de `core` son los mismos que antes de la subida.
- **Por qué:** corrige bloqueos y panics en stdio (#977, #1002), resultados `null` (#989) y errores tipados (#1027). Después se puede revisar si el timeout de 2 s y la goroutine de `roots_sync.go` siguen haciendo falta.
- **Dónde:** `go.mod`, `internal/mcpserver/run.go` (`newFilesystemMCPServer`).

### 4. Decidir la postura ante MCP 2026-07-28. Prioridad P1, esfuerzo bajo a medio
- **Qué:** el binario ya la negocia (H3). Hay dos opciones:
  - (a) **Asumirla:** documentarla, añadir un test E2E en `tests/e2e/` con un cliente moderno (`server/discover` + `tools/call` con `_meta`), fijar `server.WithMethodCacheHints("tools/list", ttl, private)` (el catálogo es fijo por proceso) y sustituir la sincronización de Roots basada en `notifications/initialized`.
  - (b) **Limitarla:** quedarse en 2025-11-25. mcp-go no tiene una opción de servidor para esto; habría que probar `WithStdioContextFunc` + `server.WithSupportedProtocolVersions(ctx, ...)` (`server/discover.go`). El go-sdk oficial sí la tiene (`ServerOptions.SupportedProtocolVersions`).
- **Por qué:** clientes como Claude ya están desplegando 2026-07-28 ([Claude blog](https://claude.com/blog/bringing-mcp-2026-07-28-to-claude)), y ahora mismo el comportamiento real y la documentación no coinciden.
- **Dónde:** `internal/mcpserver/run.go`, `roots_sync.go`, README/ROADMAP/`bundle/manifest.json`.

### 5. Roots: que el cliente solo pueda restringir, nunca ampliar. Prioridad P1, esfuerzo bajo
- **Qué:** añadir `--roots-mode=intersect`, en el que las Roots del cliente solo se aceptan si caen dentro de la allowlist del CLI. Usarlo por defecto en `strict`/`agent` y en el `.mcpb`.
- **Por qué:** con `replace` (el valor por defecto), un cliente puede enviar `file:///` y abrir todo el disco, lo que contradice el diseño fail-closed. Además Roots está *deprecated* en 2026-07-28 y la spec recomienda configurar los directorios en el servidor ([changelog, Deprecated](https://modelcontextprotocol.io/specification/2026-07-28/changelog)).
- **Dónde:** `core/roots.go` (`MergeAllowedPaths`, `ParseRootsMode`), `internal/mcpserver/run.go` (flag), `roots_sync.go`, `bundle/manifest.json`.

### 6. Dashboard: lista blanca de `Host`. Prioridad P1, esfuerzo bajo
- **Qué:** un middleware que rechace con 403 cualquier `r.Host` distinto de `localhost`, `127.0.0.1` o `[::1]` (con el puerto configurado), salvo que se pase `--host` explícito. Aplicarlo a todo el `mux`, también a los GET.
- **Por qué:** hoy un ataque de DNS rebinding puede leer backups y logs (H4). Es la defensa que exige la spec MCP para servidores HTTP locales.
- **Dónde:** `cmd/dashboard/main.go` (registro del `mux`, L218-236, junto a `rejectCrossSite`). Añadir un test en `cmd/dashboard/main_test.go`.

### 7. Corregir las anotaciones de herramientas. Prioridad P2, esfuerzo bajo
- **Qué:**
  - `mcp.WithOpenWorldHintAnnotation(false)` en las 22 herramientas locales.
  - Revisar `server_info` (read-only), `copy_file` (idempotente) y `minify_js`.
  - Hacer que `core/tool_contract.go` y su test exijan un valor explícito de `openWorldHint` en cada herramienta.
- **Por qué:** los clientes usan estas pistas para decidir cuándo pedir confirmación ([spec tools/annotations](https://modelcontextprotocol.io/specification/2025-11-25/server/tools)), y el servidor oficial ya lo hace así.
- **Dónde:** `internal/mcpserver/tools_core.go`, `tools_files.go`, `tools_search.go`, `tools_batch.go`, `tools_patch.go`, `tools_platform.go`, `tools_discovery.go`, `tools_aliases.go`, `tools_minify.go`.

### 8. Completar la salida estructurada y validarla. Prioridad P2, esfuerzo medio
- **Qué:**
  - Añadir `outputSchema` a las 13 herramientas que no lo tienen: `get_file_info`, `move_file`, `copy_file`, `delete_file`, `create_directory`, `git`, `project_replace`, `analyze_operation`, `server_info`, `help`, `wsl`, `minify_js`, `context_pack`.
  - Activar `server.WithOutputSchemaValidation()` (y opcionalmente `WithInputSchemaValidation()`) al menos en tests y E2E.
- **Por qué:** con un `outputSchema`, la spec exige que `structuredContent` sea conforme, y mcp-go puede comprobarlo en tiempo de ejecución. `get_file_info` y `move_file` son justo las que el README recomienda para verificar cambios.
- **Dónde:** `internal/mcpserver/output_schemas.go`, los `tools_*.go` correspondientes, `output_schema_sweep_test.go`.

### 9. Endurecer el recurso `file://` y añadir lectura de imagen/audio. Prioridad P2, esfuerzo medio
- **Qué:**
  - En `resources.go`: límite de tamaño (`MaxResponseSize`), detección de binarios con `BlobResourceContents` + `mimeType`, paso por `ResolveAndAuthorize` y por la auditoría.
  - Una herramienta o modo `read_file(mode:"media")` que devuelva `ImageContent`/`AudioContent`, como hace `read_media_file` en el servidor oficial.
  - Devolver `resource_link` en resultados de búsqueda y listados cuando el cliente los soporte.
- **Por qué:** evita picos de memoria y JSON con binario mal codificado, iguala al servidor oficial y permite que los agentes "vean" capturas.
- **Dónde:** `internal/mcpserver/resources.go`, `tools_core.go` (`read_file`), `output_schemas.go`.

### 10. Progreso y confirmación para operaciones largas o arriesgadas. Prioridad P3, esfuerzo medio-alto
- **Qué:**
  - Emitir `notifications/progress` cuando la petición traiga `progressToken`, en `project_replace`, `batch_operations`, `search_files` (contenido) y `directory_tree`. En 2026-07-28 el progreso sigue siendo una notificación ligada a la petición ([changelog](https://modelcontextprotocol.io/specification/2026-07-28/changelog)).
  - Para riesgo HIGH/CRITICAL y para `delete_file(permanent:true)`, pedir confirmación con `server.InputRequestBuilder` de mcp-go (MRTR). Según su documentación (`server/mrtr.go`), funciona con clientes de ambas eras gracias a un puente legacy. Si el cliente no soporta elicitation, se mantiene el `force` actual.
- **Por qué:** los clientes con timeout corto (OpenCode, 5 s por defecto) agradecen el progreso, y confirmar con una persona es más seguro que dejar que el modelo ponga `force:true`.
- **Dónde:** `core/project_replace.go`, `core/batch_operations.go`, `core/search_operations.go`, `core/impact_analyzer.go`, los handlers de `tools_batch.go`, `tools_files.go` y `tools_core.go`.

### 11. Distribución: `go install`, más plataformas e imagen Docker. Prioridad P3, esfuerzo medio
- **Qué:**
  - Renombrar el módulo a `github.com/scopweb/mcp-filesystem-go-ultra` (reescritura mecánica de imports y de los `-ldflags -X`) para que funcione `go install ...@latest`.
  - Añadir builds para `darwin/arm64`, `darwin/amd64` y `linux/arm64` en `release.yml`.
  - `.mcpb` también para macOS y Linux (`compatibility.platforms` en el manifiesto).
  - Imagen Docker mínima (distroless) con el directorio de trabajo montado.
  - Valorar GoReleaser.
- **Por qué:** el servidor oficial y `rust-mcp-filesystem` ofrecen Docker/NPX/Homebrew. Hoy aquí solo se puede compilar a mano.
- **Dónde:** `go.mod` y todos los imports, `build-windows.sh`, `release.yml`, `bundle/manifest.json`, `scripts/pack-mcpb.ps1`.

### 12. Coherencia de la documentación. Prioridad P3, esfuerzo bajo
- **Qué:** corregir los valores por defecto de backups (7 días / 100), los recuentos de herramientas (29/18/17), la frase "strict shrinks to 16" y la línea de protocolo (H3). Añadir un test que compare los defaults del README con `flag.Lookup`.
- **Dónde:** `README.md`, `ROADMAP.md`, `HOJA-DE-RUTA.md`, `internal/mcpserver/run.go`.

---

## 5. ¿Migrar al SDK oficial `modelcontextprotocol/go-sdk`?

**Recomendación: no por ahora. Seguir en mcp-go v1.2.0 y volver a evaluarlo más adelante.**
- **A favor de migrar:** es Tier 1 y oficial; trae `SupportedProtocolVersions`, límites de tamaño por trama en stdio y conformidad oficial ([v1.8.0](https://github.com/modelcontextprotocol/go-sdk/releases/tag/v1.8.0)).
- **En contra:** mcp-go también soporta 2026-07-28, saca versiones a menudo (v1.2.0 es de hoy) y ya pasa la suite oficial de conformidad en su CI (#1038). Además el repo depende mucho de su API: 29 registros de herramientas, esquemas, `ToolContract`, el cliente E2E de `tests/e2e/` y los tests de barrido. Una migración sería de esfuerzo **alto** sin ganancia funcional clara para un servidor solo-stdio.
- **Cuándo reconsiderarlo:** si hace falta restringir versiones de forma nativa (mejora 4b) o si mcp-go deja de mantener el ritmo.

---

## 6. Dependencias desactualizadas

| Dependencia | Actual | Última | Fuente |
|---|---|---|---|
| `github.com/mark3labs/mcp-go` | v1.0.0 | **v1.2.0** (08-10-2026) | [releases](https://github.com/mark3labs/mcp-go/releases) / `go list -m -u` |
| `golang.org/x/sys` | v0.47.0 | v0.48.0 | `go list -m -u all` |
| `golang.org/x/sync` | v0.22.0 | v0.23.0 | `go list -m -u all` |
| `golang.org/x/text` (indirecta) | v0.41.0 | v0.42.0 | `go list -m -u all` |
| `github.com/allegro/bigcache/v3` | v3.2.0 | v3.2.0 (al día) | proxy.golang.org |
| Go toolchain | 1.27.1 | **1.27.2** (security fixes) | [go.dev release history](https://go.dev/doc/devel/release) |
| `actions/checkout` | v4 | v7.0.1 | etiquetas del repo |
| `actions/setup-go` | v5 | v7.0.0 | etiquetas del repo |
| `actions/cache` | v4 | v6.1.0 | etiquetas del repo |
| `actions/upload-artifact` / `download-artifact` | v4 / v4 | v7.0.2 / v8.0.2 | etiquetas del repo |
| `softprops/action-gh-release` | v2 | v3.0.3 | etiquetas del repo |
| (alternativa) `modelcontextprotocol/go-sdk` | — | v1.8.0 | [release](https://github.com/modelcontextprotocol/go-sdk/releases/tag/v1.8.0) |

`govulncheck` (toolchain 1.27.1) no informa de vulnerabilidades a fecha de hoy.

---

## 7. Fuentes
- Spec MCP 2026-07-28, changelog: https://modelcontextprotocol.io/specification/2026-07-28/changelog
- Blog oficial 2026-07-28: https://blog.modelcontextprotocol.io/posts/2026-07-28/
- Claude y 2026-07-28: https://claude.com/blog/bringing-mcp-2026-07-28-to-claude
- Transports (DNS rebinding): https://modelcontextprotocol.io/specification/2025-11-25/basic/transports
- Tools / annotations: https://modelcontextprotocol.io/specification/2025-11-25/server/tools
- mcp-go releases: https://github.com/mark3labs/mcp-go/releases (v1.0.0, v1.2.0; PRs #977, #989, #999, #1002, #1027, #1038)
- go-sdk v1.8.0: https://github.com/modelcontextprotocol/go-sdk/releases/tag/v1.8.0
- Servidor filesystem oficial: https://github.com/modelcontextprotocol/servers/blob/main/src/filesystem/README.md
- rust-mcp-filesystem: https://github.com/rust-mcp-stack/rust-mcp-filesystem
- CVE-2025-53109/53110: https://nvd.nist.gov/vuln/detail/cve-2025-53109 · https://cymulate.com/blog/cve-2025-53109-53110-escaperoute-anthropic/
- Go `os.Root`: https://go.dev/blog/osroot · https://go.dev/doc/go1.25
- Go 1.27.2: https://go.dev/doc/devel/release
- Logs de CI citados: https://github.com/scopweb/mcp-filesystem-go-ultra/actions/runs/37807938793 y https://github.com/scopweb/mcp-filesystem-go-ultra/actions/runs/37774225186
