# Windows Filesystem Persistence Guide

## Problema Identificado (Bug #4)

Cuando se utiliza `recovery_edit` con filesystem-ultra en Claude Desktop en Windows, los cambios no persisten en el filesystem de Windows aunque la herramienta reporte éxito.

### Causa Raíz

Claude Desktop ejecuta el servidor MCP en un subsistema Linux (WSL o similar). Aunque filesystem-ultra **escribe correctamente los cambios al disco**, la sincronización automática entre el filesystem de Linux y Windows no está garantizada en tiempo real.

## Solución Recomendada

Para garantizar que los cambios persistan en Windows, **evita `recovery_edit` y usa alternativas de escritura directa**:

### ✅ Métodos Recomendados (Funcionan en Windows)

1. **`write_file`** - Lo más confiable
   ```json
   {
     "tool": "write_file",
     "path": "C:\\__REPOS\\tu_proyecto\\archivo.txt",
     "content": "contenido completo del archivo"
   }
   ```

2. **`intelligent_write`** - Automático y optimizado
   ```json
   {
     "tool": "intelligent_write",
     "path": "C:\\__REPOS\\tu_proyecto\\archivo.txt",
     "content": "contenido completo del archivo"
   }
   ```

3. **`streaming_write_file`** - Para archivos grandes
   ```json
   {
     "tool": "streaming_write_file",
     "path": "C:\\__REPOS\\tu_proyecto\\archivo.txt",
     "content": "contenido completo del archivo"
   }
   ```

### ❌ Métodos a Evitar (No persisten en Windows)

- `recovery_edit` - Cambios en memoria, no persisten
- `edit_file` - Mismo problema que recovery_edit
- `smart_edit_file` - Mismo problema que recovery_edit
- Cualquier herramienta de "edición" que no sea reemplazo completo

## Workflow Correcto para Windows

```
1. Leer archivo completo
   ↓
2. Realizar modificaciones en memoria (en tu código)
   ↓
3. Escribir archivo completo usando write_file o intelligent_write
   ↓
4. Verificar cambios (opcional: read_file)
   ↓
5. Hacer commit con Git (si aplica)
```

### Ejemplo Práctico

❌ **INCORRECTO - Los cambios NO persisten:**
```
1. recovery_edit para cambiar una línea
2. ❌ Los cambios se pierden
```

✅ **CORRECTO - Los cambios SÍ persisten:**
```
1. read_file para obtener contenido completo
2. Modificar contenido en memoria (reemplazo de texto)
3. write_file con contenido modificado completo
4. ✅ Los cambios persisten
```

## Configuración de Rutas

Para que los cambios persistan correctamente:

```
write_file
├── path: "C:\\__REPOS\\tu_proyecto\\archivo.txt"  ✅ Ruta Windows completa
├── content: "contenido aquí"
└── Resultado: Cambios persisten en Windows
```

**NO usar:**
- `/mnt/c/...` (ruta Linux)
- Rutas relativas sin base conocida
- Paths que dependen de mapeos WSL

## Resumen

| Herramienta | Windows | Linux/Mac | Recomendación |
|---|---|---|---|
| `write_file` | ✅ | ✅ | **USAR** |
| `intelligent_write` | ✅ | ✅ | **USAR** |
| `streaming_write_file` | ✅ | ✅ | **USAR** |
| `recovery_edit` | ❌ | ⚠️ | **EVITAR** |
| `edit_file` | ❌ | ⚠️ | **EVITAR** |
| `smart_edit_file` | ❌ | ⚠️ | **EVITAR** |

## Limitaciones Conocidas

- Las herramientas de edición en memoria de filesystem-ultra no persisten en Windows debido a la arquitectura de Claude Desktop
- Este es un problema de la arquitectura, no del código de filesystem-ultra
- Se espera que una futura versión de Claude Desktop mejore la sincronización

## Ver También

- [Claude Desktop Performance Guide](Claude_Desktop_Performance_Guide.md)
- [Claude Desktop Setup](CLAUDE_DESKTOP_SETUP.md)
