# Guía Rápida: Claude Desktop + WSL

## TL;DR (Lo Más Importante)

Los archivos creados en WSL aparecen en Windows usando la ruta `/mnt/c/` en lugar de `C:\`

---

## 3 Pasos Rápidos

### 1️⃣ Crear carpeta en Windows
```
C:\Users\TuUsuario\Documents\Claude-Files
```

### 2️⃣ Editar claude_desktop_config.json
Archivo: `C:\Users\TuUsuario\AppData\Roaming\Claude\claude_desktop_config.json`

Agregar esta sección `"env"`:
```json
"env": {
  "MCP_BASE_PATH": "/mnt/c/Users/TuUsuario/Documents/Claude-Files"
}
```

### 3️⃣ Reiniciar Claude Desktop
Cierra y abre Claude Desktop de nuevo.

---

## Mapeo de Rutas

```
Windows  → C:\Users\Usuario\Documents\Claude-Files
WSL      → /mnt/c/Users/Usuario/Documents/Claude-Files
```

**Regla de oro:** En archivos de configuración, siempre usa `/mnt/c/` en lugar de `C:\`

---

## Validar Que Funciona

```bash
# En WSL, verifica que la carpeta existe
ls /mnt/c/Users/TuUsuario/Documents/Claude-Files

# Crea un archivo de prueba
echo "test" > /mnt/c/Users/TuUsuario/Documents/Claude-Files/test.txt
```

Luego abre el Explorador de archivos de Windows y ve a `C:\Users\TuUsuario\Documents\Claude-Files` para confirmar.

---

## Errores Comunes

| Error | Solución |
|-------|----------|
| JSON inválido | Usa https://jsonlint.com/ para validar |
| Ruta no encontrada | Cambia `C:\` a `/mnt/c/` |
| Permiso denegado | Ejecuta `chmod 777 /ruta/en/wsl` |
| Servidor no conecta | Reinicia Claude Desktop completamente |

---

## Encontrar tu Carpeta de Configuración

```powershell
# En PowerShell
$env:APPDATA + "\Claude"
```

O simplemente ve a: `C:\Users\TuUsuario\AppData\Roaming\Claude`

---

**¿Problemas?** Lee el archivo [CONFIGURAR_CLAUDE_DESKTOP_WSL.md](./CONFIGURAR_CLAUDE_DESKTOP_WSL.md) para más detalles.
