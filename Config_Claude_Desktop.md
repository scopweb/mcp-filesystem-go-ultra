# 🔧 Configuración Claude Desktop - MCP Filesystem Ultra

## 📍 Ubicación del Archivo de Configuración

**Windows:**
```
%APPDATA%\Claude\claude_desktop_config.json
```

**macOS:**
```
~/Library/Application Support/Claude/claude_desktop_config.json
```

## ⚙️ Configuración Básica

```json
{
  "mcpServers": {
    "filesystem-enhanced": {
      "command": "C:\\MCPs\\clone\\mcp-filesystem-go-ultra\\mcp-filesystem-ultra.exe",
      "args": [
        "--cache-size", "200MB",
        "--parallel-ops", "8",
        "--log-level", "info"
      ]
    }
  }
}
```

## 📚 Parámetros de Configuración (args)

### 🗂️ `--cache-size` 
**Tamaño del caché en memoria**
- **Default**: `100MB`
- **Formato**: `<número><unidad>` (KB, MB, GB)
- **Ejemplos**:
  - `--cache-size 50MB` - Para sistemas con poca RAM
  - `--cache-size 500MB` - Para máximo rendimiento
  - `--cache-size 1GB` - Para proyectos enormes

**Recomendaciones:**
- 💻 **4-8GB RAM**: 50-100MB
- 🖥️ **8-16GB RAM**: 200-500MB
- 🚀 **16GB+ RAM**: 500MB-1GB

### 📁 `--allowed-paths`
**Rutas permitidas para acceso y edición**
- **Default**: Ninguno (acceso completo al sistema de archivos no restringido si no se especifica)
- **Formato**: Lista de rutas separadas por comas
- **Ejemplos**:
  - `--allowed-paths "C:\\MCPs\\clone\\,C:\\temp\\"` - Restringe el acceso solo a estas dos carpetas
  - `--allowed-paths "C:\\Users\\David\\Projects\\"` - Permite acceso solo a una carpeta de proyectos específica

**Nota**: Esta funcionalidad ha sido implementada para restringir el acceso del servidor a directorios específicos, mejorando la seguridad y el control. Los caminos se normalizan para prevenir ataques de traversal de directorios, y solo se permiten operaciones dentro de las rutas configuradas. Si no se especifica, el servidor tiene acceso completo al sistema de archivos. Solo un --allowed-paths pero muchas rutas separadas por ","

**Recomendación**: Configura esta opción con las rutas específicas a las que deseas permitir acceso para minimizar riesgos de seguridad, especialmente en entornos compartidos o no confiables.

### ⚡ `--parallel-ops`
**Operaciones concurrentes máximas**
- **Default**: Auto-detect (2x CPU cores, máx 16)
- **Rango**: 1-32
- **Ejemplos**:
  - `--parallel-ops 4` - Para CPUs básicos
  - `--parallel-ops 8` - Para desarrollo típico
  - `--parallel-ops 16` - Para máximo throughput

**Recomendaciones por CPU:**
- 🔹 **2-4 cores**: 4-6 ops
- 🔸 **6-8 cores**: 8-12 ops
- 🔶 **8+ cores**: 12-16 ops
**Operaciones concurrentes máximas**
- **Default**: Auto-detect (2x CPU cores, máx 16)
- **Rango**: 1-32
- **Ejemplos**:
  - `--parallel-ops 4` - Para CPUs básicos
  - `--parallel-ops 8` - Para desarrollo típico
  - `--parallel-ops 16` - Para máximo throughput

**Recomendaciones por CPU:**
- 🔹 **2-4 cores**: 4-6 ops
- 🔸 **6-8 cores**: 8-12 ops
- 🔶 **8+ cores**: 12-16 ops

### 📊 `--binary-threshold`
**Umbral para protocolo binario**
- **Default**: `1MB`
- **Formato**: `<número><unidad>` (KB, MB, GB)
- **Ejemplos**:
  - `--binary-threshold 512KB` - Más agresivo
  - `--binary-threshold 2MB` - Menos agresivo
  - `--binary-threshold 5MB` - Para archivos grandes

**Qué hace:** Archivos mayores al umbral usan protocolo binario optimizado.

### 📝 `--log-level`
**Nivel de logging**
- **Default**: `info`
- **Opciones**: `debug`, `info`, `warn`, `error`
- **Ejemplos**:
  - `--log-level error` - Solo errores (producción)
  - `--log-level info` - Información básica
  - `--log-level debug` - Todo (desarrollo)

### 🔧 `--debug`
**Modo debug avanzado**
- **Default**: `false` (sin flag)
- **Uso**: `--debug` (activa modo debug)
- **Efectos**:
  - Logging detallado con archivos y líneas
  - Métricas adicionales
  - Validaciones extra

### 🎯 `--vscode-api`
**Integración con VSCode**
- **Default**: `true`
- **Uso**: `--vscode-api` (activar) o `--vscode-api=false` (desactivar)
- **Función**: Habilita APIs específicas para VSCode cuando esté disponible

## 🌍 Variables de Entorno (env)

### 📦 `NODE_ENV`
**Modo de ejecución del servidor**
- **Valores**: `production`, `development`, `test`
- **Default**: Si no se especifica, usa `development`
- **Ejemplo**:
  ```json
  "env": {
    "NODE_ENV": "production"
  }
  ```

**Efectos por modo:**
- **`production`**:
  - Logging mínimo y optimizado
  - Desactiva validaciones de desarrollo
  - Máximo rendimiento
  - Sin stack traces detallados
  
- **`development`**:
  - Logging verbose
  - Validaciones adicionales
  - Stack traces completos
  - Métricas de debug
  
- **`test`**:
  - Sin cache para testing
  - Logging de test
  - Validaciones extra

### 🔧 Otras Variables de Entorno Opcionales

```json
"env": {
  "NODE_ENV": "production",
  "MCP_LOG_FILE": "C:\\logs\\mcp-filesystem.log",
  "MCP_CACHE_DIR": "C:\\temp\\mcp-cache",
  "MCP_MAX_FILE_SIZE": "100MB"
}
```

- **`MCP_LOG_FILE`**: Archivo específico para logs
- **`MCP_CACHE_DIR`**: Directorio personalizado para cache temporal
- **`MCP_MAX_FILE_SIZE`**: Límite máximo de archivo procesable

## 🚀 Configuraciones Predefinidas

### 🏠 **Desarrollo Personal**
```json
"args": [
  "--cache-size", "200MB",
  "--parallel-ops", "8",
  "--log-level", "info"
],
"env": {
  "NODE_ENV": "development"
}
```

**Nota**: Esta configuración ha sido actualizada para recomendar "Máximo Rendimiento" como la opción predeterminada para aprovechar al máximo las optimizaciones recientes. Si tu sistema tiene 16GB+ de RAM y 8+ núcleos, considera usar la configuración siguiente para un rendimiento óptimo con Claude Desktop.

### ⚡ **Máximo Rendimiento** (Recomendado Post-Optimización)
```json
"args": [
  "--cache-size", "500MB", 
  "--parallel-ops", "16",
  "--binary-threshold", "2MB",
  "--log-level", "error",
  "--allowed-paths", "C:\\MCPs\\clone\\,C:\\temp\\,C:\\Users\\David\\AppData\\Roaming\\Claude\\"
],
"env": {
  "NODE_ENV": "production"
}
```

### ⚡ **Máximo Rendimiento**
```json
"args": [
  "--cache-size", "500MB", 
  "--parallel-ops", "16",
  "--binary-threshold", "2MB",
  "--log-level", "error",
  "--allowed-paths", "C:\\MCPs\\clone\\,C:\\temp\\,C:\\Users\\David\\AppData\\Roaming\\Claude\\"
],
"env": {
  "NODE_ENV": "production"
}
```

### 🐛 **Debug/Desarrollo**
```json
"args": [
  "--cache-size", "100MB",
  "--parallel-ops", "4", 
  "--log-level", "debug",
  "--debug"
],
"env": {
  "NODE_ENV": "development",
  "MCP_LOG_FILE": "C:\\logs\\mcp-debug.log"
}
```

### 💻 **Sistema Limitado**
```json
"args": [
  "--cache-size", "50MB",
  "--parallel-ops", "4",
  "--binary-threshold", "512KB",
  "--log-level", "warn"
],
"env": {
  "NODE_ENV": "production"
}
```

## 🔍 Verificación de Configuración

### ✅ Después de reiniciar Claude Desktop:

1. **Verifica herramientas disponibles** - Debe aparecer `filesystem-enhanced`
2. **Prueba lectura simple**:
   ```
   Lee el archivo README.md
   ```
3. **Verifica métricas**:
   ```
   Muestra performance stats
   ```

### 🚨 Solución de Problemas

**Error: Comando no encontrado**
- Verifica la ruta del `.exe` en `command`
- Usa barras dobles `\\` en Windows

**Error: Argumentos inválidos**
- Revisa sintaxis de `--cache-size` (ej: "100MB", no "100 MB")
- Verifica que `--parallel-ops` sea número

**Rendimiento lento**
- Aumenta `--cache-size`
- Reduce `--parallel-ops` si hay mucha competencia
- Cambia `--log-level` a `error`

## 📊 Monitoreo

**Ver estadísticas en tiempo real:**
```
Ejecuta: performance_stats
```

**Métricas clave:**
- Cache hit rate (objetivo: >80%)
- Operaciones/segundo
- Tiempo promedio de respuesta
- Uso de memoria

## 🔄 Recarga de Configuración

Para aplicar cambios:
1. Guarda `claude_desktop_config.json`
2. **Reinicia Claude Desktop completamente**
3. Verifica que las nuevas herramientas estén disponibles

---

💡 **Tip**: Empieza con configuración básica y ajusta según necesidades específicas de tu flujo de trabajo.
