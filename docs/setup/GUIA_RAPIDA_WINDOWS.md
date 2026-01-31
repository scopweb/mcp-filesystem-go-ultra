# 🔧 Guía Rápida: Arreglar Rutas de Windows

## El Problema

El MCP no reconocía rutas de Windows como `C:\temp\hol.txt` porque el binario `.exe` fue compilado desde WSL y "pensaba" que estaba en Linux.

## La Solución Rápida

### Paso 1: Recompilar (desde WSL)

```bash
cd /mnt/c/MCPs/clone/mcp-filesystem-go-ultra
./build-windows.sh
```

O desde Windows PowerShell:

```powershell
cd C:\MCPs\clone\mcp-filesystem-go-ultra
.\build-windows.bat
```

### Paso 2: Reiniciar Claude Desktop

Cierra completamente Claude Desktop y vuelve a abrirlo.

### Paso 3: Probar

```
Lee el archivo C:\temp\hol.txt
```

¡Debería funcionar ahora! ✅

## ¿Por Qué Pasó Esto?

El binario anterior fue compilado en Linux (WSL) sin especificar que era para Windows. Esto hacía que:

- Ruta que le pasabas: `C:\temp\hol.txt`
- Lo que el MCP entendía: `/mnt/c/temp/hol.txt` (ruta WSL)
- Windows buscaba: `/mnt/c/temp/hol.txt` ❌ No existe en Windows puro

Con el nuevo binario compilado correctamente:

- Ruta que le pasas: `C:\temp\hol.txt`
- Lo que el MCP entiende: `C:\temp\hol.txt` ✅
- Windows encuentra: `C:\temp\hol.txt` ✅

## Regla de Oro

**Para Windows → Compilar con `GOOS=windows`**
**Para WSL → Compilar con `GOOS=linux` (o sin especificar desde WSL)**

## Soporte

El código siempre fue correcto. Solo era un problema de compilación.

Tu configuración en `claude_desktop_config.json` está bien, no necesitas cambiarla.
