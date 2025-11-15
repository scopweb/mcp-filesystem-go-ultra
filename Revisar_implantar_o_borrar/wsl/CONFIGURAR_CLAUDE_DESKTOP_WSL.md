# Configuración de Claude Desktop con WSL en Windows

## Problema

Cuando usas Claude Desktop en Windows con un servidor MCP corriendo en WSL, los archivos creados quedan en el subsistema Linux (`/home/user/claude/`) y no se sincronizan automáticamente a Windows.

## Solución: Usar Rutas Compartidas de WSL

WSL monta automáticamente las unidades de Windows en rutas accesibles desde Linux. La solución es configurar Claude Desktop para que guarde los archivos directamente en una carpeta de Windows que sea accesible desde WSL.

---

## Paso 1: Crear una Carpeta de Trabajo en Windows

1. Abre el Explorador de archivos en Windows
2. Crea una carpeta para los archivos de Claude, por ejemplo:
   ```
   C:\Users\TuUsuario\Documents\Claude-Files
   ```

---

## Paso 2: Acceder a WSL Desde la Ruta Compartida

En WSL, las unidades de Windows están disponibles en `/mnt/`:

```bash
# Verificar que puedes acceder
ls /mnt/c/Users/TuUsuario/Documents/Claude-Files
```

---

## Paso 3: Configurar el Archivo `claude_desktop_config.json`

El archivo de configuración de Claude Desktop se encuentra en:

**Windows:**
```
%APPDATA%\Claude\claude_desktop_config.json
```

O en la ruta completa:
```
C:\Users\TuUsuario\AppData\Roaming\Claude\claude_desktop_config.json
```

### Editar la Configuración

Abre el archivo con un editor de texto y busca la sección de tu servidor MCP. Modifica la configuración para usar rutas de WSL que apunten a la carpeta de Windows:

#### Ejemplo 1: Si tu servidor MCP está en WSL

```json
{
  "mcpServers": {
    "mcp-filesystem": {
      "command": "wsl",
      "args": [
        "bash",
        "-c",
        "cd /home/user/mcp-filesystem-go-ultra && ./mcp-filesystem-ultra"
      ],
      "env": {
        "MCP_BASE_PATH": "/mnt/c/Users/TuUsuario/Documents/Claude-Files"
      }
    }
  }
}
```

**Reemplaza:**
- `TuUsuario` con tu nombre de usuario de Windows
- La ruta a tu ejecutable de MCP según tu configuración

#### Ejemplo 2: Configuración Completa

```json
{
  "mcpServers": {
    "filesystem": {
      "command": "wsl",
      "args": [
        "bash",
        "-c",
        "cd /mnt/c/Users/TuUsuario/Documents/MCP && ./mcp-filesystem-ultra"
      ],
      "env": {
        "MCP_BASE_PATH": "/mnt/c/Users/TuUsuario/Documents/Claude-Files"
      },
      "disabled": false
    }
  }
}
```

---

## Paso 4: Reiniciar Claude Desktop

1. Cierra completamente Claude Desktop
2. Espera 5 segundos
3. Abre Claude Desktop de nuevo
4. Verifica que el servidor MCP aparezca como conectado

---

## Paso 5: Probar la Creación de Archivos

En Claude Desktop, pide que cree un archivo de prueba:

```
Crea un archivo de prueba llamado "test.txt" con el contenido "Hola desde Claude"
```

Luego verifica que el archivo aparezca en:
```
C:\Users\TuUsuario\Documents\Claude-Files\test.txt
```

---

## Ventajas de Esta Configuración

✅ **Automático:** Los archivos se crean directamente en Windows
✅ **Sin sincronización manual:** No necesitas copiar y pegar archivos
✅ **Accesible desde Windows:** Todos tus archivos están en una carpeta de Windows normal
✅ **Compatible con WSL:** Funciona perfectamente con servidores MCP en WSL

---

## Solución de Problemas

### Los archivos se crean en WSL, no en Windows

**Causa:** La variable de entorno `MCP_BASE_PATH` no está configurada o está incorrecta.

**Solución:**
1. Verifica que hayas agregado la sección `"env"` en `claude_desktop_config.json`
2. Asegúrate de que la ruta sea correcta (usa `/mnt/c/` en lugar de `C:\`)
3. Reinicia Claude Desktop

### Permiso denegado al crear archivos

**Causa:** Permisos insuficientes en la carpeta de Windows desde WSL.

**Solución:**
```bash
# Dale permisos de lectura y escritura a la carpeta desde WSL
chmod 777 /mnt/c/Users/TuUsuario/Documents/Claude-Files
```

### El servidor MCP no se conecta

**Causa:** La configuración JSON es inválida o la ruta al ejecutable es incorrecta.

**Solución:**
1. Valida que el JSON sea válido usando https://jsonlint.com/
2. Verifica que el ejecutable existe:
   ```bash
   wsl ls -la /home/user/mcp-filesystem-go-ultra/mcp-filesystem-ultra
   ```
3. Revisa los logs de Claude Desktop: `Ayuda > Mostrar logs`

### ¿Cuál es la ruta correcta para mi distribución de WSL?

Ejecuta este comando en PowerShell para encontrar tu distribución:

```powershell
wsl --list --verbose
```

Reemplaza `Ubuntu` en los comandos por el nombre que aparezca en la salida.

---

## Rutas de Acceso Comunes en WSL

Si tu usuario de Windows es `Juan` y tu carpeta está en `Documents`:

| Sistema | Ruta |
|---------|------|
| Windows | `C:\Users\Juan\Documents\Claude-Files` |
| WSL | `/mnt/c/Users/Juan/Documents/Claude-Files` |
| Claude Config | `/mnt/c/Users/Juan/Documents/Claude-Files` |

---

## Alternativa: Usar Symlinks en WSL

Si prefieres mantener los archivos en WSL pero acceder desde Windows:

```bash
# En WSL
mkdir -p /home/user/claude-output
ln -s /mnt/c/Users/TuUsuario/Documents/Claude-Files /home/user/claude-output-sync
```

Luego configura `MCP_BASE_PATH` en Claude Desktop para `/home/user/claude-output`.

---

## Referencias

- [Documentación oficial de WSL](https://learn.microsoft.com/es-es/windows/wsl/)
- [Claude Desktop Docs](https://claude.ai/claude-code)
- [Configuración de servidores MCP](https://modelcontextprotocol.io/)

---

**Última actualización:** Noviembre 2024
