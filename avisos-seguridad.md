# Avisos de seguridad (borradores privados) — scopweb/mcp-filesystem-go-ultra

Cómo crearlos: en el repo, ve a Security → Advisories → "New draft security advisory" (https://github.com/scopweb/mcp-filesystem-go-ultra/security/advisories/new) y pega cada bloque. Los avisos quedan privados hasta que se publiquen.

---

## Aviso A

- **Título:** Escape del sandbox: `copy_file` escribe fuera de los directorios permitidos a través de un symlink colgante
- **Ecosistema / paquete:** Go — `github.com/mcp/filesystem-ultra` (repo `scopweb/mcp-filesystem-go-ultra`)
- **Versiones afectadas:** `<= 4.8.0` (al menos `main` @ `badcee4`)
- **Versiones parcheadas:** (pendiente)
- **Severidad:** Alta
- **CWE:** CWE-59 (Improper Link Resolution Before File Access, "Link Following")

### Descripción

`copy_file` (también la acción `copy` de `batch_operations` y pipelines) puede crear un fichero **fuera de los directorios permitidos** si el destino es un symlink colgante dentro del sandbox. Un symlink colgante es uno cuyo destino todavía no existe.

### Reproducción

1. El directorio permitido es `allowed/`. Dentro hay un fichero `allowed/src.txt` y un symlink `allowed/evil -> <fuera>/copy.txt`, cuyo destino no existe.
2. Se llama a `copy_file(source: "allowed/src.txt", destination: "allowed/evil")`, o a `engine.CopyFile(ctx, src, allowed/evil)`.
3. Resultado: `err == nil` y se crea `<fuera>/copy.txt` con el contenido de `src.txt`.

Test mínimo (Go, paquete `tests`):

```go
os.Symlink(filepath.Join(outside, "copy.txt"), filepath.Join(allowed, "evil"))
err := engine.CopyFile(ctx, filepath.Join(allowed, "src.txt"), filepath.Join(allowed, "evil"))
// err == nil y os.Stat(filepath.Join(outside, "copy.txt")) existe
```

Con el mismo escenario, `write_file` y `move_file` **no** escapan (temp+rename y comprobaciones transaccionales).

**Vector realista:** un repositorio clonado que contiene un symlink de este tipo (por ejemplo hacia `~/.config/autostart/x.desktop` o `~/.ssh/authorized_keys` si no existen), más una inyección de prompt que pida copiar un fichero a esa ruta.

### Causa raíz

1. `core/engine.go`, `IsPathAllowed` (aprox. L1286-1313): si `filepath.EvalSymlinks(target)` falla porque el destino no existe, el código sube por los padres hasta encontrar uno que resuelva y valida `padre_resuelto + "evil"`. Es decir, valida el **nombre del enlace**, no su destino. Es el mismo patrón ("fallback al directorio padre") que causó CVE-2025-53109 en el servidor oficial `@modelcontextprotocol/server-filesystem`.
2. `core/file_operations.go`:
   - `CopyFile` (aprox. L530-610) comprueba si el destino existe con `os.Stat`, que en un enlace colgante devuelve "no existe".
   - `ResolveSymlinks` solo se aplica al **origen**.
   - `copyFile` (aprox. L654) abre el destino con `os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)`, que **sigue el symlink**.
3. Usan el mismo camino: `core/pipeline.go:903` (acción copy) y `copyDirectory`. Revisar también `core/wsl_sync.go` L117 y L271 (mismo `OpenFile` con `O_CREATE|O_TRUNC`).

### Corrección propuesta

1. **Parche inmediato**
   - En `IsPathAllowed`, al subir por padres inexistentes, hacer `os.Lstat` de cada componente. Si alguno es un symlink, resolverlo con `os.Readlink`, convertirlo a ruta absoluta y volver a validarlo contra la allowlist, o rechazarlo directamente.
   - En `copyFile` (y `copyDirectory`/`wsl_sync`), abrir el destino con `os.O_CREATE|os.O_EXCL|os.O_WRONLY`. En POSIX, `O_EXCL` falla si el destino es un symlink, aunque esté colgante. Es coherente con que `copy_file` ya se niega a sobrescribir.
   - Aplicar `ResolveSymlinks` / `ResolveAndAuthorize` también al **destino** en `CopyFile`, y comprobar la existencia con `os.Lstat` en vez de `os.Stat`.
2. **Estructural:** abrir un `os.Root` por cada raíz permitida (Go 1.24+; Go 1.25 añadió `MkdirAll`, `WriteFile`, `Rename`, `RemoveAll`, `Readlink`, etc.) y hacer las E/S de mutación a través de él. Así el kernel impide escapar del árbol y desaparece la ventana TOCTOU entre comprobar y usar.
3. **Test de regresión** en `tests/security/cves_test.go`: symlink colgante como destino de `copy_file`, de la acción copy de pipeline/batch y de una copia de directorio.

### Referencias

- CVE-2025-53109: https://nvd.nist.gov/vuln/detail/cve-2025-53109
- Análisis de Cymulate (EscapeRoute): https://cymulate.com/blog/cve-2025-53109-53110-escaperoute-anthropic/
- Go `os.Root`: https://go.dev/blog/osroot · https://go.dev/doc/go1.25

---

## Aviso B

- **Título:** El dashboard no valida la cabecera `Host`: DNS rebinding permite leer backups y logs de auditoría
- **Ecosistema / paquete:** Go — `github.com/mcp/filesystem-ultra/cmd/dashboard`
- **Versiones afectadas:** `<= 4.8.0`
- **Versiones parcheadas:** (pendiente)
- **Severidad:** Media
- **CWE:** CWE-350 (Reliance on Reverse DNS Resolution for a Security-Critical Action) / CWE-346 (Origin Validation Error)

### Descripción

El dashboard (`cmd/dashboard`) escucha por defecto en `127.0.0.1` y no tiene autenticación. Protege los POST con `Sec-Fetch-Site`/`Origin` (`rejectCrossSite`), pero **no valida la cabecera `Host`** en ninguna ruta.

Con un ataque de DNS rebinding, una web maliciosa (`attacker.example`, que tras el rebinding resuelve a `127.0.0.1`) hace peticiones *same-origin* al puerto del dashboard y puede leer:
- `/api/backups/file/...` y `/api/trash/file/...`: contenido de los ficheros respaldados.
- `/api/backups/search-content`, `/api/operations`, `/api/metrics`, etc.

Para los POST, `rejectCrossSite` compara el host de `Origin` con `r.Host`; con rebinding ambos son `attacker.example`, así que también pasaría.

### Reproducción

```bash
dashboard --log-dir=/tmp/dlogs --backup-dir=/tmp/dbk --port=9177
curl -s -o /dev/null -w "%{http_code}\n" \
  -H "Host: attacker.example:9177" -H "Sec-Fetch-Site: same-origin" \
  http://127.0.0.1:9177/api/backups
# -> 200
```

### Causa raíz

`cmd/dashboard/main.go`: las rutas se registran en el `mux` (L218-236) sin ningún middleware que valide `r.Host`. `rejectCrossSite` (aprox. L1856-1874) solo se aplica a endpoints que mutan y no cubre el rebinding.

### Corrección propuesta

1. Envolver el `mux` con un middleware que devuelva **403** si `r.Host` (sin puerto, o con el puerto configurado) no está en `{localhost, 127.0.0.1, [::1]}`. Si el usuario pasa `--host` explícito, añadir ese valor a la lista, y permitir ampliarla con un flag tipo `--allowed-hosts`.
2. En `rejectCrossSite`, comparar `Origin` con la misma lista blanca, no con `r.Host`.
3. Opcional: token aleatorio por arranque, impreso en consola y exigido como cookie o cabecera, para tener autenticación real.
4. Tests en `cmd/dashboard/main_test.go`: `Host: attacker.example` → 403 en GET y POST; `Host: 127.0.0.1:<port>` → 200.

### Referencias

- Spec MCP, transports, "Security Warning" (validar Origin contra DNS rebinding y escuchar solo en localhost): https://modelcontextprotocol.io/specification/2025-11-25/basic/transports
