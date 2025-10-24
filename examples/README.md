# Examples / Ejemplos

Esta carpeta contiene archivos de ejemplo para configuración y uso del MCP.

## Archivos

- **hooks.example.json** - Ejemplo de configuración de hooks (auto-format, validación)
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

### Testing Requests
Los archivos JSON de ejemplo te muestran el formato correcto para hacer requests al servidor MCP.
