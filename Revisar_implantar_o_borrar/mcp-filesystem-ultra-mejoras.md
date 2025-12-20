# MCP Filesystem Ultra - Propuestas de Mejora

## ✅ ESTADO: COMPLETAMENTE IMPLANTADO (v3.1.0+)

**Fecha de Implementación:** 25 de Octubre de 2025
**Commit:** `3cbabbb` - "Add v3.1.0: Ultra-Efficient Operations (3 new tools)"
**Estado:** 🟢 **PRODUCCIÓN - TODAS LAS MEJORAS ACTIVAS**

### Resumen Rápido de Implementación:
- ✅ `read_file_range` - Implementada y funcional
- ✅ `count_occurrences` - Implementada y funcional
- ✅ `replace_nth_occurrence` - Implementada y funcional

**Todas las 3 herramientas críticas están disponibles en producción y listas para usar.**

---

## 📋 Contexto

Durante pruebas reales de uso del MCP filesystem-ultra, se identificaron limitaciones críticas al trabajar con archivos grandes y búsquedas de ocurrencias específicas.

### Caso de Uso Real que Falló:
- **Tarea:** Cambiar la última ocurrencia de 'CUMIEIRA' por 'ULTIMACUMIERA'
- **Problema:** Archivo con 31,248 líneas y 106 ocurrencias totales
- **Limitación:** No hay forma eficiente de:
  1. Leer un rango específico de líneas (ej: líneas 26630-26680)
  2. Reemplazar solo la última ocurrencia de un patrón
  3. Acceder a líneas específicas por número

---

## 🎯 Propuestas de Mejora (Prioridad y Utilidad Real)

### **#1: `read_file_range` - CRÍTICO** ⭐⭐⭐⭐⭐

**Función:**
```json
{
  "tool": "read_file_range",
  "path": "archivo.sql",
  "start_line": 26630,
  "end_line": 26680
}
```

**Por qué es necesario:**
- `read_file` con `max_lines` solo lee desde el inicio (head), final (tail) o overview (all)
- No permite saltar a un rango específico de líneas
- Para archivos grandes, leer todo el archivo para ver 50 líneas específicas es ineficiente

**Caso de uso:**
- Ver contexto alrededor de la línea 26,645 en un archivo de 31,248 líneas
- Inspeccionar errores en logs en líneas específicas
- Verificar cambios antes de editarlos

**Ahorro de tokens:** ~90% vs leer archivo completo

**Complejidad implementación:** BAJA
- Similar a `read_file` pero con `sed -n 'start,endp'` o equivalente

---

### **#2: `replace_nth_occurrence` - CRÍTICO** ⭐⭐⭐⭐⭐

**Función:**
```json
{
  "tool": "replace_nth_occurrence",
  "path": "C:\\temp\\archivo.sql",
  "pattern": "CUMIEIRA",
  "replacement": "ULTIMACUMIERA",
  "occurrence": -1,              // -1 = última, 1 = primera, 2 = segunda, etc.
  "recursive": false             // opcional: buscar en directorio
}
```

**Por qué es necesario:**
- `search_and_replace` reemplaza TODAS las ocurrencias
- No hay forma de reemplazar solo la primera, última o N-ésima ocurrencia
- Casos reales requieren precisión quirúrgica

**Casos de uso:**
- Cambiar solo la última entrada en un log
- Actualizar solo la primera definición de una variable
- Modificar ocurrencia específica sin tocar las demás

**Ahorro de tokens:** ~80% vs leer → analizar → editar manualmente

**Complejidad implementación:** MEDIA
- Requiere contar ocurrencias
- Identificar línea específica
- Aplicar reemplazo solo en esa línea

---

### **#3: `advanced_text_search` mejorado** ⭐⭐⭐⭐

**Mejoras propuestas:**
```json
{
  "tool": "advanced_text_search",
  "path": "archivo.sql",
  "pattern": "CUMIEIRA",
  "show_context": true,          // NUEVO: mostrar líneas antes/después
  "context_lines": 2,             // NUEVO: cuántas líneas de contexto
  "return_mode": "last"           // NUEVO: "all", "first", "last"
}
```

**Salida mejorada:**
```
Match #53 (last) at line 26645 in C:\temp\insert_portugal_final.sql:
26643: ('PT', '5040-321', 'SANTA MARTA DE PENAGUIÃO', 'Fontelas'...
26644: ('PT', '5040-322', 'SANTA MARTA DE PENAGUIÃO', 'Fornelos'...
26645: ('PT', '5040-323', 'CUMIEIRA', 'Vale da Cumieira'...
26646: ('PT', '5040-324', 'SANTA MARTA DE PENAGUIÃO', 'Galegas'...
26647: ('PT', '5040-325', 'SANTA MARTA DE PENAGUIÃO', 'Gondarém'...
```

**Por qué es necesario:**
- Actualmente solo muestra: `archivo.sql:123` sin contenido
- No permite ver contexto
- No permite filtrar primera/última ocurrencia fácilmente

**Ahorro de tokens:** ~60% vs buscar + leer archivo

**Complejidad implementación:** BAJA

---

## 📊 Comparativa de Prioridades

| Función | Impacto | Ahorro Tokens | Complejidad | Prioridad |
|---------|---------|---------------|-------------|-----------|
| `read_file_range` | 🔥🔥🔥🔥🔥 | 90% | Baja | **CRÍTICA** |
| `replace_nth_occurrence` | 🔥🔥🔥🔥🔥 | 80% | Media | **CRÍTICA** |
| `advanced_text_search` mejorado | 🔥🔥🔥🔥 | 60% | Baja | Alta |

---

## 🔧 Implementación Sugerida

### Para `read_file_range`:

**Pseudocódigo:**
```rust
fn read_file_range(path: String, start_line: usize, end_line: usize) -> Result<String> {
    let file = File::open(path)?;
    let reader = BufReader::new(file);
    
    let lines: Vec<String> = reader
        .lines()
        .enumerate()
        .filter(|(i, _)| *i >= start_line && *i <= end_line)
        .map(|(_, line)| line.unwrap())
        .collect();
    
    Ok(lines.join("\n"))
}
```

**Alternativa con comando shell:**
```bash
sed -n '${start_line},${end_line}p' "${path}"
```

---

### Para `replace_nth_occurrence`:

**Pseudocódigo:**
```rust
fn replace_nth_occurrence(
    path: String, 
    pattern: String, 
    replacement: String, 
    occurrence: i32
) -> Result<String> {
    let content = read_file(&path)?;
    let lines: Vec<String> = content.lines().map(|s| s.to_string()).collect();
    
    let mut matches: Vec<usize> = lines
        .iter()
        .enumerate()
        .filter(|(_, line)| line.contains(&pattern))
        .map(|(i, _)| i)
        .collect();
    
    if matches.is_empty() {
        return Err("Pattern not found");
    }
    
    let target_index = if occurrence == -1 {
        matches.len() - 1  // última
    } else if occurrence > 0 {
        (occurrence as usize) - 1  // N-ésima (1-indexed)
    } else {
        return Err("Invalid occurrence value");
    };
    
    if target_index >= matches.len() {
        return Err("Occurrence out of range");
    }
    
    let line_number = matches[target_index];
    lines[line_number] = lines[line_number].replace(&pattern, &replacement);
    
    write_file(&path, lines.join("\n"))?;
    Ok(format!("Replaced at line {}", line_number + 1))
}
```

---

## 🧪 Casos de Prueba

### Test 1: `read_file_range`
```json
Input:
{
  "path": "archivo.txt",  // 1000 líneas
  "start_line": 450,
  "end_line": 460
}

Expected: Solo líneas 450-460 (11 líneas)
Tokens: ~100 vs ~8000 (leer todo)
```

### Test 2: `replace_nth_occurrence`
```json
Input:
{
  "path": "test.sql",
  "pattern": "CUMIEIRA",
  "replacement": "NUEVA",
  "occurrence": -1  // última
}

Archivo tiene 5 CUMIEIRA en líneas: 10, 25, 50, 100, 150
Expected: Solo cambia línea 150
```

### Test 3: `replace_nth_occurrence` - primera
```json
Input:
{
  "occurrence": 1  // primera
}

Expected: Solo cambia línea 10
```

---

## 📝 Notas Adicionales

### Por qué NO usar Bash como solución:
- El contenedor de Claude Desktop **no tiene acceso** a `/mnt/c/`
- Confirmado durante pruebas: `ls /mnt/c/temp/*.sql` → "No such file or directory"
- Bash no es confiable como fallback

### Alternativas descartadas:
- ❌ `find_last_occurrence` - Redundante si `advanced_text_search` se mejora
- ❌ `edit_file_by_line` - `replace_nth_occurrence` es más versátil
- ❌ `replace_at_line` - Cubierto por `replace_nth_occurrence`

---

## 🎯 Conclusión

**2 funciones críticas** que resolverían el 90% de limitaciones actuales:

1. **`read_file_range`** - Lectura eficiente de rangos específicos
2. **`replace_nth_occurrence`** - Reemplazos precisos (primera/última/N-ésima)

Con estas mejoras, el MCP filesystem-ultra sería significativamente más eficiente para:
- Archivos grandes (>10,000 líneas)
- Búsquedas y reemplazos precisos
- Workflows de análisis y edición
- Ahorro masivo de tokens

---

## 🔍 DETALLES DE IMPLEMENTACIÓN EN CÓDIGO

### Ubicaciones en el Código Fuente

#### 1. Definición de Herramientas (main.go)
- **Línea 1084**: `readRangeTool := mcp.NewTool("read_file_range", ...)`
- **Línea 1107**: `countOccurrencesTool := mcp.NewTool("count_occurrences", ...)`
- **Línea 1141**: `replaceNthTool := mcp.NewTool("replace_nth_occurrence", ...)`

#### 2. Implementaciones de Funciones

**ReadFileRange** (core/file_operations.go)
```go
func (e *UltraFastEngine) ReadFileRange(ctx context.Context, path string, startLine, endLine int) (string, error)
```
- Lectura eficiente de rangos específicos
- Normalización automática de rutas (Windows/WSL)
- Validación de seguridad integrada
- Manejo inteligente de límites de línea

**CountOccurrences** (core/search_operations.go)
```go
func (e *UltraFastEngine) CountOccurrences(ctx context.Context, path string, pattern string, useRegex, returnLineNumbers bool) (interface{}, error)
```
- Búsqueda sin cargar archivo completo
- Soporte para regex o patrones literales
- Retorno opcional de números de línea
- Optimización para archivos grandes

**ReplaceNthOccurrence** (core/edit_operations.go)
```go
func (e *UltraFastEngine) ReplaceNthOccurrence(ctx context.Context, path string, pattern, replacement string, occurrence int, wholeWord bool) (string, error)
```
- Reemplazos quirúrgicos (primera, última, N-ésima)
- Backup automático antes de modificar
- Validación de integridad post-edición
- Rollback automático en caso de error

### Historial de Commit

**Commit Principal:** `3cbabbb0e6c7d69f6fc400bf450359fb7211b51f`
```
Author: David Prats <scopweb@gmail.com>
Date: Sat Oct 25 10:32:37 2025 +0200
Subject: Add v3.1.0: Ultra-Efficient Operations (3 new tools)
```

**Archivos Modificados:**
- README.md - Documentación actualizada
- core/edit_operations.go - ReplaceNthOccurrence
- core/file_operations.go - ReadFileRange
- core/search_operations.go - CountOccurrences
- main.go - Registro de herramientas MCP

---

**Documento generado:** 2025-10-25
**Basado en:** Pruebas reales de uso con archivos SQL de 31K+ líneas
**Estado del Documento:** ✅ ARCHIVADO - Propuestas implementadas exitosamente
