# 🤖 MCP Filesystem Ultra - Instrucciones para Agentes IA (v3.7.0)

> **Este documento está diseñado para incluir en system prompts o contexto de agentes IA.**
> Copia todo el contenido a las instrucciones personalizadas o memoria de tu IA.
>
> 💡 **TIP**: ¡También puedes llamar `get_help()` en tiempo de ejecución para obtener esta información dinámicamente!

---

## 🆕 AUTO-APRENDIZAJE: Usa la herramienta `get_help()`

En lugar de leer toda la documentación, puedes llamar a `get_help` en cualquier momento:

```
get_help("overview")  → Guía de inicio rápido
get_help("workflow")  → El workflow eficiente de 4 pasos
get_help("tools")     → Lista completa de 50 herramientas
get_help("edit")      → Edición de archivos (¡lo más importante!)
get_help("errors")    → Errores comunes y soluciones
get_help("examples")  → Ejemplos prácticos de código
get_help("tips")      → Consejos pro para eficiencia
```

---

## ⚡ CRÍTICO: USA HERRAMIENTAS MCP, NO NATIVAS

Cuando tengas acceso a herramientas **mcp-filesystem-ultra**, **SIEMPRE prefierelas** sobre operaciones nativas:

### ✅ USA ESTAS (Herramientas MCP)
```
mcp_read, mcp_write, mcp_edit, mcp_list, mcp_search
```
O sus nombres originales:
```
read_file, write_file, edit_file, list_directory, smart_search
```

### ❌ EVITA ESTAS (Nativas/WSL)
- Herramientas nativas de lectura de archivos
- Comandos WSL directos para operaciones de archivos
- Cualquier herramienta que no maneje conversión `/mnt/c/` ↔ `C:\`

**¿Por qué?** Las herramientas MCP convierten automáticamente rutas entre WSL y Windows.

---

## 🎯 LA REGLA DE ORO: Ediciones Quirúrgicas Ahorran 98% de Tokens

### ❌ DERROCHADOR (Nunca hagas esto)
```
read_file(archivo_grande_completo) → write_file(archivo_grande_completo)
Archivo de 5000 líneas = 250,000+ tokens desperdiciados
```

### ✅ EFICIENTE (Siempre haz esto)
```
smart_search(archivo, patrón) → read_file_range(inicio, fin) → edit_file(viejo, nuevo)
Archivo de 5000 líneas = 2,000 tokens (¡98% de ahorro!)
```

---

## 📋 LISTA COMPLETA DE HERRAMIENTAS (49 en v3.7.0)

### 🆕 Aliases con Prefijo MCP (NUEVO en v3.7.0)
Usa estos para evitar conflictos con herramientas nativas:

| Herramienta | Descripción |
|-------------|-------------|
| `mcp_read` | Leer archivo con conversión de rutas WSL↔Windows |
| `mcp_write` | Escritura atómica con conversión automática |
| `mcp_edit` | Edición inteligente con backup + conversión |
| `mcp_list` | Listado de directorio con caché |
| `mcp_search` | Búsqueda de archivos/contenido |

### 📖 Lectura de Archivos
| Herramienta | Cuándo Usar |
|-------------|-------------|
| `read_file` | Archivos pequeños (<1000 líneas) |
| `read_file_range` | **PREFERIDO** - Leer solo líneas N a M |
| `intelligent_read` | Auto-optimiza según tamaño |
| `chunked_read_file` | Archivos muy grandes (>1MB) |

### ✏️ Escritura y Edición
| Herramienta | Cuándo Usar |
|-------------|-------------|
| `write_file` | Crear o sobrescribir archivos |
| `create_file` | Alias de write_file |
| `edit_file` | **PREFERIDO** - Reemplazo quirúrgico de texto |
| `multi_edit` | Múltiples ediciones en una operación atómica |
| `replace_nth_occurrence` | Reemplazar ocurrencia específica (1ª, última, etc.) |
| `intelligent_write` | Auto-optimiza según tamaño |
| `intelligent_edit` | Auto-optimiza según tamaño |
| `streaming_write_file` | Archivos muy grandes |
| `smart_edit_file` | Edición de archivos grandes |
| `recovery_edit` | Edición con recuperación de errores |

### 🔍 Búsqueda
| Herramienta | Cuándo Usar |
|-------------|-------------|
| `smart_search` | Encontrar ubicación (devuelve números de línea) |
| `mcp_search` | Mismo con nombre MCP explícito |
| `advanced_text_search` | Búsqueda compleja de patrones |
| `search_and_replace` | Buscar y reemplazar masivo |
| `count_occurrences` | Contar coincidencias sin leer archivo |

### 📁 Operaciones de Archivos
| Herramienta | Cuándo Usar |
|-------------|-------------|
| `copy_file` | Duplicar archivo/directorio |
| `move_file` | Mover a nueva ubicación |
| `rename_file` | Renombrar archivo/directorio |
| `delete_file` | Eliminación permanente |
| `soft_delete_file` | Eliminación segura (a papelera) |
| `get_file_info` | Metadatos (tamaño, fecha, etc.) |

### 📂 Operaciones de Directorio
| Herramienta | Cuándo Usar |
|-------------|-------------|
| `list_directory` | Listar contenidos |
| `mcp_list` | Mismo con nombre MCP explícito |
| `create_directory` | Crear directorio (+ padres) |

### 🔄 Sincronización WSL ↔ Windows
| Herramienta | Cuándo Usar |
|-------------|-------------|
| `wsl_to_windows_copy` | Copiar de WSL a Windows |
| `windows_to_wsl_copy` | Copiar de Windows a WSL |
| `sync_claude_workspace` | Sincronizar workspace completo |
| `wsl_windows_status` | Verificar estado de sync |
| `configure_autosync` | Habilitar/deshabilitar auto-sync |
| `autosync_status` | Verificar config de auto-sync |

### 📊 Análisis y Monitoreo
| Herramienta | Cuándo Usar |
|-------------|-------------|
| `analyze_file` | Obtener recomendaciones de optimización |
| `analyze_write` | Análisis dry-run de escritura |
| `analyze_edit` | Análisis dry-run de edición |
| `analyze_delete` | Análisis dry-run de eliminación |
| `get_edit_telemetry` | Monitorear eficiencia de ediciones |
| `get_optimization_suggestion` | Obtener consejos |
| `performance_stats` | Rendimiento del servidor |

### 📦 Operaciones en Lote
| Herramienta | Cuándo Usar |
|-------------|-------------|
| `batch_operations` | Múltiples operaciones atómicamente |

### 💾 Artefactos
| Herramienta | Cuándo Usar |
|-------------|-------------|
| `capture_last_artifact` | Guardar código en memoria |
| `write_last_artifact` | Escribir código guardado a archivo |
| `artifact_info` | Info sobre artefacto guardado |

---

## 🔄 EL WORKFLOW EFICIENTE DE 4 PASOS

Para CUALQUIER edición de archivo, sigue este workflow:

### Paso 1: LOCALIZAR
```
smart_search(archivo, "nombre_funcion")
→ Devuelve: "Encontrado en líneas 45-67"
```

### Paso 2: LEER (Solo lo necesario)
```
read_file_range(archivo, 45, 67)
→ Devuelve: Solo esas 22 líneas
```

### Paso 3: EDITAR (Quirúrgicamente)
```
edit_file(archivo, "texto_viejo", "texto_nuevo")
→ Devuelve: "OK: 1 changes"
```

### Paso 4: VERIFICAR (Opcional)
```
get_edit_telemetry()
→ Objetivo: >80% targeted_edits
```

---

## 📏 ÁRBOL DE DECISIÓN POR TAMAÑO

```
¿El archivo tiene < 1000 líneas?
├── SÍ → read_file() está OK
└── NO → DEBES usar smart_search + read_file_range + edit_file

¿El archivo tiene > 5000 líneas?
├── NO → El workflow estándar está bien
└── SÍ → CRÍTICO: Nunca leas el archivo completo
```

---

## ⚠️ ERRORES COMUNES Y SOLUCIONES

### "context validation failed"
**Causa:** El archivo cambió desde que lo leíste
**Solución:** Re-ejecuta `smart_search()` + `read_file_range()` para obtener contenido fresco

### "no match found"
**Causa:** El texto no existe exactamente como se especificó
**Solución:** 
1. Usa `smart_search()` para verificar ubicación
2. Revisa diferencias de espacios/indentación
3. Usa `count_occurrences()` para verificar que el texto existe

### "multiple matches found"
**Causa:** El mismo texto aparece múltiples veces
**Solución:** Usa `replace_nth_occurrence(archivo, patrón, nuevo, occurrence=-1)`
- `1` = primero, `2` = segundo, `-1` = último, `-2` = penúltimo

### "Tool not found: create_file"
**Causa:** `create_file` era previamente un alias
**Solución:** Usa `write_file()` en su lugar - crea archivos si no existen

### Errores de ruta con /mnt/c/ o C:\
**Causa:** Formato de ruta no coincide
**Solución:** Usa herramientas MCP - auto-convierten rutas. Usa `mcp_read`, `mcp_write`, etc.

---

## 🎯 TABLA DE REFERENCIA RÁPIDA

| Quiero... | Usa esta herramienta |
|-----------|---------------------|
| Leer un archivo pequeño | `mcp_read` o `read_file` |
| Leer líneas específicas | `read_file_range` ⭐ |
| Crear un archivo nuevo | `mcp_write` o `write_file` |
| Editar texto en un archivo | `mcp_edit` o `edit_file` ⭐ |
| Hacer múltiples ediciones | `multi_edit` ⭐ |
| Encontrar dónde está el código | `mcp_search` o `smart_search` |
| Contar ocurrencias | `count_occurrences` |
| Reemplazar solo la última | `replace_nth_occurrence` |
| Listar directorio | `mcp_list` o `list_directory` |
| Copiar/Mover archivos | `copy_file`, `move_file` |
| Eliminar de forma segura | `soft_delete_file` |
| Múltiples operaciones | `batch_operations` |
| Verificar mi eficiencia | `get_edit_telemetry` |

⭐ = Recomendado para eficiencia de tokens

---

## 💡 EJEMPLOS DE EFICIENCIA DE TOKENS

### Ejemplo 1: Editar una función en un archivo de 5000 líneas

**❌ Enfoque derrochador: ~250,000 tokens**
```
read_file("grande.py")        # 125,000 tokens
# ... procesar ...
write_file("grande.py", todo)  # 125,000 tokens
```

**✅ Enfoque eficiente: ~2,500 tokens**
```
smart_search("grande.py", "def mi_funcion")  # 500 tokens
read_file_range("grande.py", 234, 256)       # 1,000 tokens
edit_file("grande.py", "viejo", "nuevo")     # 500 tokens
```

**Ahorro: 247,500 tokens (¡99% de reducción!)**

### Ejemplo 2: Múltiples ediciones en un archivo

**❌ Derrochador: 5 llamadas separadas a edit_file**
```
edit_file(ruta, viejo1, nuevo1)  # Leer → Editar → Escribir
edit_file(ruta, viejo2, nuevo2)  # Leer → Editar → Escribir (¡otra vez!)
edit_file(ruta, viejo3, nuevo3)  # Leer → Editar → Escribir (¡otra vez!)
...
```

**✅ Eficiente: 1 llamada a multi_edit**
```
multi_edit(ruta, [
  {"old_text": "viejo1", "new_text": "nuevo1"},
  {"old_text": "viejo2", "new_text": "nuevo2"},
  {"old_text": "viejo3", "new_text": "nuevo3"}
])
# Archivo leído UNA VEZ, todas las ediciones aplicadas, escrito UNA VEZ
```

**Ahorro: ~80% menos operaciones de archivo**

---

## 🔧 MANEJO DE RUTAS

Todas las herramientas MCP manejan automáticamente la conversión de rutas:

| Tú proporcionas | La herramienta convierte a |
|-----------------|---------------------------|
| `/mnt/c/Users/Juan/archivo.txt` | `C:\Users\Juan\archivo.txt` (en Windows) |
| `C:\Users\Juan\archivo.txt` | `/mnt/c/Users/Juan/archivo.txt` (en WSL) |

**¡No necesitas conversión manual!**

---

## 📌 RECUERDA

1. **Siempre prefiere herramientas `mcp_*`** sobre operaciones nativas
2. **Nunca leas archivos grandes completos** - usa `read_file_range`
3. **Usa `edit_file` no `write_file`** para cambios
4. **Usa `multi_edit`** para múltiples cambios en un archivo
5. **Usa `smart_search` primero** para encontrar ubicaciones exactas
6. **Revisa `get_edit_telemetry`** para monitorear tu eficiencia

---

*Versión: 3.7.0 | Última actualización: 2025-11-30*
