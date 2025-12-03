# Bug #9 - Resolución: Parámetros Opcionales de Búsqueda No Expuestos

## Problema Identificado

El servidor MCP `filesystem-ultra` soportaba internamente parámetros opcionales avanzados en las herramientas `smart_search` y `advanced_text_search`, pero estos **NO estaban expuestos** en las definiciones de las herramientas MCP que recibe Claude Desktop.

### Síntomas:
- Claude no podía usar `include_content` para búsquedas de contenido en `smart_search`
- Claude no podía filtrar por tipo de archivo con `file_types`
- Claude no podía usar opciones avanzadas en `advanced_text_search` (`case_sensitive`, `whole_word`, `include_context`, `context_lines`)
- Los valores estaban hardcodeados en `main.go`
- La documentación mencionaba que estaban "desactivados" o "en futuras versiones"

### Causa Raíz:
```go
// ANTES - Parámetros hardcodeados:
smartSearchTool := mcp.NewTool("smart_search",
    mcp.WithString("path", mcp.Required()),
    mcp.WithString("pattern", mcp.Required()),
    // NO había parámetros opcionales expuestos
)

engineReq := localmcp.CallToolRequest{
    Arguments: map[string]interface{}{
        "include_content": false,  // ← Hardcodeado
        "file_types": []interface{}{} // ← Hardcodeado
    }
}
```

## Solución Implementada

### 1. Exposición de Parámetros en `smart_search`

**Archivo:** `main.go` (líneas 443-449)

```go
smartSearchTool := mcp.NewTool("smart_search",
    mcp.WithDescription("Search files by name/content"),
    mcp.WithString("path", mcp.Required(), mcp.Description("Base directory or file")),
    mcp.WithString("pattern", mcp.Required(), mcp.Description("Regex or literal pattern")),
    mcp.WithBoolean("include_content", mcp.Description("Include file content search (default: false)")),
    mcp.WithString("file_types", mcp.Description("Comma-separated file extensions (e.g., '.go,.txt')")),
)
```

**Handler actualizado:**
```go
// Extract optional parameters
includeContent := false
fileTypes := []interface{}{}

if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
    if ic, ok := args["include_content"].(bool); ok {
        includeContent = ic
    }
    if ft, ok := args["file_types"].(string); ok && ft != "" {
        // Parse comma-separated extensions
        parts := strings.Split(ft, ",")
        for _, part := range parts {
            fileTypes = append(fileTypes, strings.TrimSpace(part))
        }
    }
}
```

### 2. Exposición de Parámetros en `advanced_text_search`

**Archivo:** `main.go` (líneas 493-500)

```go
advancedTextSearchTool := mcp.NewTool("advanced_text_search",
    mcp.WithDescription("Advanced text search with context"),
    mcp.WithString("path", mcp.Required(), mcp.Description("Directory or file")),
    mcp.WithString("pattern", mcp.Required(), mcp.Description("Regex or literal pattern")),
    mcp.WithBoolean("case_sensitive", mcp.Description("Case sensitive search (default: false)")),
    mcp.WithBoolean("whole_word", mcp.Description("Match whole words only (default: false)")),
    mcp.WithBoolean("include_context", mcp.Description("Include context lines (default: false)")),
    mcp.WithNumber("context_lines", mcp.Description("Number of context lines (default: 3)")),
)
```

**Handler actualizado:**
```go
// Extract optional parameters
caseSensitive := false
wholeWord := false
includeContext := false
contextLines := 3

if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
    if cs, ok := args["case_sensitive"].(bool); ok {
        caseSensitive = cs
    }
    if ww, ok := args["whole_word"].(bool); ok {
        wholeWord = ww
    }
    if ic, ok := args["include_context"].(bool); ok {
        includeContext = ic
    }
    if cl, ok := args["context_lines"].(float64); ok {
        contextLines = int(cl)
    }
}
```

### 3. Actualización de Documentación

**Archivo:** `README.md` (líneas 542-570 y 572-600)

Se actualizó la documentación para:
- Eliminar menciones de "desactivado" o "futuras versiones"
- Añadir descripciones completas de los parámetros opcionales
- Incluir ejemplos de uso con los parámetros

**Ejemplos añadidos:**

```json
{
  "tool": "smart_search",
  "arguments": {
    "path": "./src",
    "pattern": "TODO",
    "include_content": true,
    "file_types": ".go,.js"
  }
}
```

```json
{
  "tool": "advanced_text_search",
  "arguments": {
    "path": "./src",
    "pattern": "func",
    "case_sensitive": true,
    "whole_word": true,
    "include_context": true,
    "context_lines": 5
  }
}
```

### 4. Tests de Validación

**Archivo:** `tests/bug9_test.go` (nuevo archivo - 285 líneas)

Se crearon tests completos para validar:

1. **TestSmartSearchWithIncludeContent** ✅
   - Búsqueda sin contenido (`include_content: false`)
   - Búsqueda con contenido (`include_content: true`)

2. **TestSmartSearchWithFileTypes** ✅
   - Filtrado por extensiones de archivo
   - Verificación de exclusión de archivos no coincidentes

3. **TestAdvancedTextSearchCaseSensitive** ✅
   - Búsqueda case-insensitive (default)
   - Búsqueda case-sensitive

4. **TestAdvancedTextSearchWithContext** ✅
   - Búsqueda sin contexto
   - Búsqueda con líneas de contexto

**Resultado:** Todos los tests pasan exitosamente ✅

## Beneficios

### Para Claude Desktop:
1. **Búsquedas más eficientes**: Puede buscar contenido en una sola llamada en lugar de múltiples operaciones
2. **Filtrado preciso**: Puede limitar búsquedas a tipos de archivo específicos
3. **Control fino**: Case-sensitive, whole-word, y opciones de contexto disponibles
4. **Menor uso de tokens**: Una búsqueda bien parametrizada vs múltiples búsquedas básicas

### Para Usuarios:
1. **Respuestas más rápidas**: Menos llamadas a herramientas = respuestas más rápidas
2. **Mayor precisión**: Búsquedas más específicas = resultados más relevantes
3. **Mejor experiencia**: Claude puede resolver consultas complejas de búsqueda sin "dar vueltas"

## Ejemplo de Uso Mejorado

### ANTES (Bug #9):
```
Usuario: "Busca todos los archivos .go que contengan la función 'ParseConfig'"
Claude: 
  1. smart_search (solo encuentra nombres de archivo)
  2. read_file en cada archivo .go
  3. grep manualmente el patrón
  → Múltiples llamadas, muchos tokens
```

### DESPUÉS (Bug #9 resuelto):
```
Usuario: "Busca todos los archivos .go que contengan la función 'ParseConfig'"
Claude:
  1. smart_search(pattern="ParseConfig", include_content=true, file_types=".go")
  → Una sola llamada, resultado directo ✅
```

## Archivos Modificados

1. **`main.go`**
   - Líneas 443-449: Definición de `smart_search` con parámetros opcionales
   - Líneas 450-481: Handler de `smart_search` actualizado
   - Líneas 493-500: Definición de `advanced_text_search` con parámetros opcionales
   - Líneas 501-539: Handler de `advanced_text_search` actualizado

2. **`README.md`**
   - Líneas 542-570: Documentación de `smart_search` actualizada
   - Líneas 572-600: Documentación de `advanced_text_search` actualizada

3. **`tests/bug9_test.go`** (nuevo)
   - 285 líneas de tests completos
   - 4 funciones de test principales
   - Helper function para crear engine de test

## Compatibilidad

✅ **Backward compatible**: Los parámetros son opcionales con valores default
✅ **Sin breaking changes**: Código existente sigue funcionando
✅ **Extensible**: Fácil añadir más parámetros opcionales en el futuro

## Fecha de Resolución
3 de Diciembre, 2025
