# Examples / Ejemplos

Esta carpeta contiene archivos de ejemplo para configuración y uso del MCP.

## Archivos

- **hooks.example.json** - Ejemplo completo de configuración de hooks (16 eventos, todos deshabilitados por defecto — actívalos selectivamente con `enabled: true`)
- **hooks-test.json** - Configuración de hooks con todos los eventos habilitados y `cmd /c echo` (Windows-friendly), útil para verificar que el sistema dispara los hooks correctamente. Incluye hooks de bloqueo (`failOnError: true`) para `.env` y `.key` que puedes activar para probar el rechazo.
- **request.json** - Ejemplos de requests MCP
- **test_request.json** - Requests de prueba

## Uso

### Hooks
Copia `hooks.example.json` a `hooks.json` y personaliza según tus necesidades:

```bash
cp examples/hooks.example.json hooks.json
```

Luego inicia el servidor con:

```bash
mcp-filesystem-ultra.exe --hooks-enabled --hooks-config=hooks.json
```

**Para verificar que el sistema de hooks funciona** (dispara todos los 16 eventos con logs visibles), copia `hooks-test.json` en su lugar:

```bash
cp examples/hooks-test.json hooks.json
mcp-filesystem-ultra.exe --hooks-enabled --hooks-config=hooks.json
```

Después de cualquier operación de archivo, verás los mensajes `[PRE-WRITE]`, `[POST-EDIT]`, etc. en los logs del servidor.

### Hook post-delete con SD-ID (v4.5.11+)

Desde v4.5.11, el `post-delete` recibe `sd_id` y `dest_path` en el `metadata` del hook context cuando `--backup-dir` está configurado. Esto te permite auditar/registrar el ID de la papelera por archivo:

```json
"post-delete": [
  {
    "pattern": "*",
    "hooks": [{
      "type": "command",
      "command": "jq -r '\"sd_id=\\(.metadata.sd_id) dest=\\(.metadata.dest_path)\"' >> audit.log",
      "failOnError": false,
      "enabled": true
    }]
  }
]
```

El hook context completo está documentado en `docs-website/src/content/docs/features/hooks.md`.

### Testing Requests
Los archivos JSON de ejemplo te muestran el formato correcto para hacer requests al servidor MCP.
