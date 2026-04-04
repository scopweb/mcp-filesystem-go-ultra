# Solución: Problema con Rutas de Windows

## Problema Identificado

Cuando Claude Desktop en Windows puro (no WSL) intenta usar rutas de Windows como `C:\temp\hol.txt`, el MCP no las reconocía correctamente.

### Causa Raíz

El binario `filesystem-ultra.exe` estaba siendo compilado **desde WSL (Linux)** sin especificar el sistema operativo objetivo. Esto causaba que:

1. El binario compilado tenía `runtime.GOOS = "linux"` incrustado
2. Cuando se ejecutaba en Windows, el programa PENSABA que estaba en Linux
3. La función `NormalizePath()` convertía incorrectamente rutas de Windows a rutas WSL:
   - Input: `C:\temp\hol.txt`
   - Output (incorrecto): `/mnt/c/temp/hol.txt`
4. El sistema operativo Windows no podía encontrar `/mnt/c/temp/hol.txt` porque esa es una ruta de WSL

### Diagnóstico

```go
// Cuando se compilaba desde WSL sin GOOS=windows:
runtime.GOOS = "linux"           // ❌ Incorrecto para Windows
os.PathSeparator = '/'           // ❌ Incorrecto para Windows

// La función NormalizePath detectaba la ruta C:\ pero como pensaba
// que estaba en Linux, la convertía a /mnt/c/
if len(path) >= 3 && path[1] == ':' {
    if os.PathSeparator == '/' {  // TRUE porque fue compilado en Linux
        // Convierte C:\temp\hol.txt -> /mnt/c/temp/hol.txt
        return "/mnt/" + driveLetter + "/" + remainder
    }
}
```

## Solución

### Compilación Correcta para Windows

El binario DEBE compilarse específicamente para Windows usando:

```bash
GOOS=windows GOARCH=amd64 go build -o filesystem-ultra.exe .
```

Esto asegura que:
- `runtime.GOOS = "windows"` ✅
- `os.PathSeparator = '\\'` ✅
- Las rutas de Windows se manejan correctamente ✅

### Scripts Proporcionados

#### Desde WSL/Linux:
```bash
./build-windows.sh
```

#### Desde Windows:
```cmd
build-windows.bat
```

## Instrucciones para el Usuario

### Paso 1: Recompilar el Binario

**Opción A - Desde WSL:**
```bash
cd /mnt/c/MCPs/clone/mcp-filesystem-go-ultra
./build-windows.sh
```

**Opción B - Desde Windows PowerShell:**
```powershell
cd C:\MCPs\clone\mcp-filesystem-go-ultra
.\build-windows.bat
```

### Paso 2: Verificar la Compilación

El nuevo `filesystem-ultra.exe` debe estar en el directorio del proyecto.

### Paso 3: Reiniciar Claude Desktop

Cierra y vuelve a abrir Claude Desktop para que cargue el nuevo binario.

### Paso 4: Probar

En Claude Desktop, intenta una operación con ruta de Windows:
```
Lee el archivo C:\temp\hol.txt
```

Debería funcionar correctamente ahora.

## Verificación Técnica

Para verificar que el binario está compilado correctamente:

```bash
# Desde WSL, verifica que es un ejecutable Windows PE:
file filesystem-ultra.exe
# Debe mostrar: "PE32+ executable (console) x86-64, for MS Windows"

# NO debe mostrar: "ELF 64-bit LSB executable"
```

## Notas Importantes

1. **Siempre compilar para Windows:** Cuando generes un `.exe` para usar en Windows, SIEMPRE usa `GOOS=windows`

2. **Diferentes binarios para diferentes sistemas:**
   - Windows puro: `GOOS=windows` (filesystem-ultra.exe)
   - WSL: `GOOS=linux` (filesystem-ultra) - Dentro de WSL
   - Linux nativo: `GOOS=linux` (filesystem-ultra)

3. **La configuración de Claude Desktop está correcta:** No necesitas cambiar tu `claude_desktop_config.json`

4. **WSL Auto-Sync sigue funcionando:** Esta solución no afecta la funcionalidad WSL ↔ Windows cuando se ejecuta DESDE WSL

## Resumen

- ❌ **Antes:** Binario compilado en Linux → No reconoce rutas Windows
- ✅ **Ahora:** Binario compilado para Windows → Reconoce rutas Windows correctamente

El código de `NormalizePath()` siempre fue correcto. El problema era la forma de compilación.
