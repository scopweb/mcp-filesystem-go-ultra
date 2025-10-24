# Instrucciones de Memoria para Claude Desktop

## 📋 Copia y Pega Esto en la Memoria Personalizada de Claude Desktop

---

## MCP Filesystem Ultra-Fast v3.0 - Instrucciones de Uso Eficiente

### 🎯 Principios Clave de Optimización de Tokens

1. **SIEMPRE usa `max_lines` al leer archivos grandes**
   - Para archivos >100 líneas, NUNCA leas todo el archivo
   - Empieza con 50 líneas, incrementa solo si necesitas más
   - Usa `mode` apropiado: head, tail, o all

2. **Lectura Progresiva** - Estrategia de 3 pasos:
   ```
   Paso 1: Lee 20-50 líneas para contexto inicial
   Paso 2: Si necesitas más, lee 100 líneas
   Paso 3: Solo lee archivo completo si es absolutamente necesario
   ```

3. **Operaciones en Lote** - Para cambios múltiples:
   - Usa `batch_operations` en lugar de operaciones individuales
   - Siempre con `atomic: true` para seguridad
   - Valida primero con `validate_only: true`

### 📖 Herramientas Disponibles (32 total)

#### Lectura de Archivos (Prioridad: OPTIMIZACIÓN DE TOKENS)

**`read_file`** - Lectura principal (USA SIEMPRE max_lines)
```json
{
  "path": "archivo.txt",
  "max_lines": 50,        // OBLIGATORIO para archivos >100 líneas
  "mode": "head"          // "head", "tail", o "all"
}
```

**Cuándo usar cada modo:**
- `mode: "head"` → Ver inicio de logs, inicio de código, configuraciones
- `mode: "tail"` → Ver final de logs, últimos resultados, outputs
- `mode: "all"` → Overview rápido (muestra inicio + final con gap en medio)

**`intelligent_read`** - Auto-selecciona estrategia óptima
- Úsalo cuando no estés seguro del tamaño
- Automáticamente usa chunking para archivos >50KB

**`chunked_read_file`** - Para archivos gigantes (>1MB)
- Solo cuando sepas que el archivo es enorme

#### Escritura de Archivos

**`write_file`** - Escritura estándar (archivos <50KB)

**`intelligent_write`** - Auto-optimiza (RECOMENDADO)
- Usa esto por defecto
- Detecta tamaño y elige mejor estrategia

**`streaming_write_file`** - Para archivos muy grandes (>1MB)

#### Edición de Archivos

**`edit_file`** - Edición estándar
- Para archivos pequeños/medianos

**`intelligent_edit`** - Edición optimizada (RECOMENDADO)
- Usa esto por defecto
- Auto-selecciona mejor estrategia

**`recovery_edit`** - Cuando edit_file falla
- Fuzzy matching automático
- Normalización de espacios
- 95% de éxito vs fallos comunes

#### Operaciones en Lote (NUEVO v2.6)

**`batch_operations`** - Operaciones atómicas
```json
{
  "request_json": "{
    \"operations\": [
      {\"type\": \"write\", \"path\": \"file1.txt\", \"content\": \"...\"},
      {\"type\": \"edit\", \"path\": \"file2.txt\", \"old_text\": \"...\", \"new_text\": \"...\"},
      {\"type\": \"move\", \"source\": \"old.txt\", \"destination\": \"new.txt\"}
    ],
    \"atomic\": true,
    \"create_backup\": true,
    \"validate_only\": false
  }"
}
```

**Cuándo usar:**
- Refactoring de múltiples archivos
- Reorganización de proyectos
- Cambios que deben ser todo-o-nada

**IMPORTANTE:** Siempre valida primero:
```json
{"validate_only": true}  // Paso 1: Validar
{"validate_only": false} // Paso 2: Ejecutar si validación OK
```

#### Plan Mode / Dry-Run (v2.5)

**`analyze_write`** - Analiza escritura sin ejecutar
**`analyze_edit`** - Analiza edición sin ejecutar
**`analyze_delete`** - Analiza eliminación sin ejecutar

**Cuándo usar:**
- Antes de operaciones críticas
- Para verificar qué se va a cambiar
- Para evaluar riesgos

#### Búsqueda

**`smart_search`** - Búsqueda por nombre/contenido
**`advanced_text_search`** - Búsqueda avanzada con contexto
**`search_and_replace`** - Búsqueda y reemplazo recursivo

#### Gestión de Archivos

**`list_directory`** - Listar directorio
**`create_directory`** - Crear directorio
**`delete_file`** - Eliminar permanentemente (¡CUIDADO!)
**`soft_delete_file`** - Eliminar a carpeta trash (SEGURO)
**`move_file`** - Mover/renombrar
**`copy_file`** - Copiar archivo/directorio
**`rename_file`** - Renombrar
**`get_file_info`** - Información detallada

#### Análisis y Optimización

**`analyze_file`** - Analiza archivo y sugiere estrategia
**`get_optimization_suggestion`** - Recomendación de herramienta óptima
**`performance_stats`** - Estadísticas de rendimiento

### 🎯 Workflows Recomendados

#### Workflow 1: Leer Archivo Grande para Análisis
```
1. get_file_info("archivo.log")           // Ver tamaño
2. read_file("archivo.log", max_lines=50, mode="head")  // Contexto inicial
3. Si necesitas más → read_file(..., max_lines=100)
4. Solo si absolutamente necesario → read_file sin límites
```

#### Workflow 2: Refactoring Multi-Archivo
```
1. Identifica archivos afectados
2. batch_operations con validate_only=true  // Validar
3. Revisa el análisis
4. batch_operations con validate_only=false // Ejecutar
```

#### Workflow 3: Edición con Seguridad
```
1. analyze_edit(path, old_text, new_text)  // Dry-run
2. Revisa el análisis de riesgo
3. Si es seguro → edit_file o intelligent_edit
4. Si falla → recovery_edit (fuzzy matching)
```

#### Workflow 4: Operación Crítica
```
1. analyze_delete(path)  // Ver impacto
2. Si riesgoso → usa soft_delete_file en lugar de delete_file
3. Si procedes → batch_operations con backup
```

### ❌ Antipatrones - EVITA ESTOS ERRORES

**❌ NO HAGAS:**
```json
// Leer archivo completo sin límite
{"tool": "read_file", "path": "large_file.log"}  // ❌ Malgasta tokens
```

**✅ HAZ ESTO:**
```json
// Leer con límite inteligente
{"tool": "read_file", "path": "large_file.log", "max_lines": 50, "mode": "head"}  // ✅
```

**❌ NO HAGAS:**
```json
// Múltiples operaciones individuales
write_file("file1.txt", ...)
write_file("file2.txt", ...)
write_file("file3.txt", ...)  // ❌ Sin atomicidad
```

**✅ HAZ ESTO:**
```json
// Una operación batch atómica
{
  "tool": "batch_operations",
  "operations": [
    {"type": "write", "path": "file1.txt", ...},
    {"type": "write", "path": "file2.txt", ...},
    {"type": "write", "path": "file3.txt", ...}
  ],
  "atomic": true  // ✅ Todo o nada
}
```

**❌ NO HAGAS:**
```json
// Edit directo sin analizar
edit_file(path, old_text, new_text)  // ❌ Puede fallar
```

**✅ HAZ ESTO:**
```json
// Analiza primero, luego edita
1. analyze_edit(path, old_text, new_text)  // ✅ Ver qué pasará
2. Si OK → edit_file(...)
```

### 💡 Tips de Eficiencia

1. **Para logs**: Siempre usa `mode="tail"` para ver entradas recientes
2. **Para código**: Usa `mode="head"` para ver imports/estructura
3. **Para debugging**: Usa `mode="all"` para overview rápido
4. **Para refactoring**: SIEMPRE usa `batch_operations` con validación
5. **Para eliminaciones**: Prefiere `soft_delete_file` sobre `delete_file`

### 📊 Métricas de Éxito

**Token Usage Óptimo:**
- Lectura de archivo 1000 líneas: ~2,500 tokens (con max_lines=100)
- Operación batch (5 ops): ~500 tokens
- Session típica (100 ops): ~13,000 tokens

**Si usas más tokens:**
- Revisa si estás usando `max_lines`
- Verifica que uses batch operations
- Confirma que lees progresivamente

### 🎯 Regla de Oro

**"Empieza pequeño, escala solo si necesitas"**

- 20-50 líneas para contexto inicial
- 100 líneas si necesitas más detalle
- Sin límite solo como último recurso

### 🔧 Configuración del Usuario

Este MCP está configurado con:
- `--compact-mode`: Respuestas optimizadas
- `--max-search-results`: 20
- `--max-list-items`: 50

**Resultado esperado:** 80-90% reducción de tokens vs baseline

---

## 🎓 Recuerda Siempre

1. ✅ `max_lines` es tu amigo - úsalo siempre
2. ✅ `batch_operations` para cambios múltiples
3. ✅ `analyze_*` antes de operaciones críticas
4. ✅ Lectura progresiva (pequeño → grande)
5. ✅ `intelligent_*` herramientas por defecto
6. ✅ `soft_delete` mejor que `delete`
7. ✅ Valida antes de ejecutar batches

---

**Con estas instrucciones, usarás este MCP de forma óptima, ahorrando tokens y siendo más eficiente.**
