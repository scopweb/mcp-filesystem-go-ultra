# 🚫 Cómo Prevenir Búsquedas Innecesarias en Claude Desktop

## 🎯 El Problema

Claude Desktop tiende a buscar automáticamente **incluso cuando el usuario puede proporcionar la información directamente**:

```
Usuario: "Modifica la función ProcessData en main.go"
Claude: "Déjame buscar dónde está definida ProcessData..." 
        [Ejecuta smart_search innecesariamente]
```

**Resultado**: Desperdicio de tokens y tiempo cuando el usuario ya sabe la ubicación.

---

## ✅ La Solución: ASK FIRST, SEARCH LATER

### Regla de Oro

**ANTES de buscar automáticamente, PREGUNTA al usuario si conoce la ubicación:**

```
Usuario: "Modifica la función ProcessData"

Claude: "¿En qué archivo está ProcessData? Si no lo sabes, puedo buscarlo."

Usuario: "En main.go, líneas 150-180"
Claude: [Lee directamente esas líneas y edita]
```

---

## 📋 Casos de Uso

### ✅ CASO 1: Usuario menciona archivo específico

```
Usuario: "Modifica ProcessData en main.go"
Claude: "¿Sabes aproximadamente en qué línea está? Si no, puedo buscarlo."
```

### ✅ CASO 2: Usuario menciona función pero no archivo

```
Usuario: "Modifica la función ProcessData"
Claude: "¿En qué archivo está ProcessData?"
Usuario: "main.go"
Claude: "¿Sabes en qué línea? Si no, puedo buscarla."
```

### ✅ CASO 3: Usuario claramente no sabe

```
Usuario: "No sé dónde está ProcessData pero necesito modificarla"
Claude: "Voy a buscarla..." [Ejecuta smart_search]
```

### ✅ CASO 4: Usuario da path completo

```
Usuario: "En /home/user/project/src/main.go línea 150"
Claude: [Lee directamente, NO busca]
```

---

## 🎓 Instrucciones para AI Agent

Agrega esto a tu system prompt o custom instructions:

```
CRITICAL: Avoid Unnecessary Searches

BEFORE executing any search operation (smart_search, mcp_search, etc.):

1. Check if user already provided file path
2. Check if user provided line numbers
3. If unsure, ASK user first: "Do you know where X is located?"

ONLY search if:
- User explicitly says "I don't know where it is"
- User asks to "find" or "search for" something
- User clearly doesn't have the information

Token savings: Up to 90% on searches that could be avoided
```

---

## 📊 Ahorro de Tokens

| Escenario | Con búsqueda automática | Con pregunta primero | Ahorro |
|-----------|------------------------|---------------------|--------|
| Usuario sabe ubicación | 2,000 tokens | 200 tokens | **90%** |
| Usuario sabe archivo | 1,500 tokens | 500 tokens | **67%** |
| Usuario no sabe | 2,000 tokens | 2,000 tokens | 0% (necesario) |

---

## 🔧 Implementación Técnica

### Para Desarrolladores de AI Agents

Si estás desarrollando tu propio agente, agrega esta lógica:

```python
def before_search(query: str, context: dict):
    """Check if search is necessary before executing"""
    
    # Parse user message
    user_message = context.get("user_message", "")
    
    # Check if user provided explicit path
    if has_file_path(user_message):
        return {"skip_search": True, "reason": "User provided path"}
    
    # Check if user provided line numbers
    if has_line_numbers(user_message):
        return {"skip_search": True, "reason": "User provided lines"}
    
    # Check if it's an exploratory question
    if is_exploratory(query):
        return {"should_ask_first": True}
    
    return {"proceed_with_search": True}
```

### Para Claude Desktop

Actualiza tu `claude_desktop_config.json` con custom instructions:

```json
{
  "mcpServers": {
    "filesystem-ultra": {
      "customInstructions": [
        "Before searching, ask user if they know the location",
        "Only search if user explicitly needs to find something",
        "Save tokens by asking first"
      ]
    }
  }
}
```

---

## 💡 Tips para Usuarios

### Sé específico en tus requests:

❌ **Vago (trigger búsqueda)**:
```
"Modifica la función ProcessData"
```

✅ **Específico (sin búsqueda)**:
```
"En main.go línea 150, modifica ProcessData para usar async"
```

✅ **Semi-específico (búsqueda reducida)**:
```
"En main.go, modifica ProcessData (busca la línea si no la ves)"
```

---

## 🎯 Ejemplos Prácticos

### Ejemplo 1: Usuario experimentado

```
Usuario: "En core/engine.go línea 245, cambia timeout de 30 a 60"
Claude: [Lee línea 245, edita directamente]
Tokens: ~300
```

### Ejemplo 2: Usuario necesita ayuda

```
Usuario: "Cambia el timeout en engine.go"
Claude: "¿Sabes en qué línea está el timeout?"
Usuario: "No"
Claude: [Busca "timeout" en engine.go, encuentra línea 245]
Tokens: ~1,200
```

### Ejemplo 3: Usuario da función

```
Usuario: "Modifica la función SetTimeout"
Claude: "¿En qué archivo está SetTimeout?"
Usuario: "engine.go"
Claude: "¿Sabes la línea aproximada? Si no, la busco."
Usuario: "No, búscala"
Claude: [Busca SetTimeout]
Tokens: ~1,500
```

---

## ⚠️ Errores Comunes

### ❌ ERROR 1: Buscar sin preguntar

```
Usuario: "Modifica X"
Claude: "Déjame buscar X..." [DESPERDICIO]
```

**Fix**: Preguntar primero si usuario sabe ubicación

### ❌ ERROR 2: Preguntar demasiado

```
Usuario: "Busca todas las funciones que usen 'timeout'"
Claude: "¿En qué archivo?" [INNECESARIO]
```

**Fix**: Si usuario pide búsqueda explícita, ejecutarla directamente

### ❌ ERROR 3: No usar info del contexto

```
Usuario: "Ahora modifica esa función" [referencia a mensaje anterior]
Claude: [Busca de nuevo] [DESPERDICIO]
```

**Fix**: Usar contexto de conversación previo

---

## 🚀 Resultado Final

Siguiendo estas prácticas:

- ✅ **90% menos tokens** en búsquedas evitables
- ✅ **Respuestas más rápidas** cuando usuario sabe ubicación
- ✅ **Mejor experiencia** al no repetir trabajo innecesario
- ✅ **Flexibilidad** para usuarios que necesitan búsqueda

---

## 📝 Resumen Ejecutivo

**1 Línea**: Pregunta al usuario si sabe la ubicación ANTES de buscar automáticamente.

**3 Líneas**:
1. Antes de `smart_search`, pregunta: "¿Sabes dónde está X?"
2. Solo busca si usuario dice "no sé" o pide búsqueda explícita
3. Ahorra 90% tokens en búsquedas innecesarias

---

**Version**: 1.0.0  
**Autor**: Based on user feedback - Token optimization  
**Fecha**: December 2025
