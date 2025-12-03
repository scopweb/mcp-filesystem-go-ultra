# Guides / Guías de Usuario

Esta carpeta contiene todas las guías prácticas para usar el MCP Filesystem Server Ultra-Fast.

## 🚀 INICIO RÁPIDO PARA AGENTES IA

### Paso 1: Copia el prompt inicial
Copia **UNO** de estos al System Prompt de tu IA:

| Archivo | Idioma | Descripción |
|---------|--------|-------------|
| `SYSTEM_PROMPT_COMPACT.txt` | English | ⭐ **Recomendado** - Prompt compacto con instrucciones |
| `SYSTEM_PROMPT_COMPACT_ES.txt` | Español | ⭐ **Recomendado** - Prompt compacto en español |
| `INITIAL_PROMPT_FOR_AI.md` | English | Opciones de prompts (mínimo, normal, auto-learning) |
| `INITIAL_PROMPT_FOR_AI_ES.md` | Español | Opciones de prompts en español |

### Paso 2: La IA aprende automáticamente
El prompt le dice a la IA que llame `get_help("overview")` al inicio. Esto le enseña:
- Las 50 herramientas disponibles
- El workflow eficiente de 4 pasos
- Cómo evitar errores comunes

### Paso 3: Auto-recuperación de errores
Cuando la IA encuentre un error, el prompt le dice que llame `get_help("errors")` para auto-diagnosticar.

## 📚 Cómo funciona `get_help()`

```
get_help("overview")  → Inicio rápido
get_help("workflow")  → Workflow de 4 pasos
get_help("tools")     → Lista de 50 herramientas
get_help("edit")      → Cómo editar archivos
get_help("errors")    → Solución de errores
get_help("examples")  → Ejemplos de código
get_help("tips")      → Consejos de eficiencia
```

## 📂 Documentación Completa

### Para AI/Agentes
- **AI_AGENT_INSTRUCTIONS.md** - 📘 Guía completa (English)
- **AI_AGENT_INSTRUCTIONS_ES.md** - 📘 Guía completa (Español)

### Para Usuarios/Configuración
- **BACKUP_RECOVERY_GUIDE.md** - ⭐ **NUEVO v3.8.0** - Sistema de backup automático, validación de riesgo, y recuperación
- **CLAUDE_DESKTOP_SETUP.md** - Cómo configurar el MCP en Claude Desktop
- **Claude_Desktop_Performance_Guide.md** - Guía de rendimiento
- **BATCH_OPERATIONS_GUIDE.md** - Operaciones en lote
- **HOOKS.md** - Sistema de hooks
- **TOOL_REFERENCE.txt** - Referencia de herramientas (deprecado, usar get_help)

## 💡 Beneficios del Sistema de Auto-Aprendizaje

1. **Tokens mínimos**: El prompt inicial usa ~100 tokens vs ~5000 de docs completos
2. **Siempre actualizado**: La ayuda viene del servidor, no del prompt
3. **Auto-recuperación**: La IA puede diagnosticar sus propios errores
4. **Aprendizaje progresivo**: La IA aprende más según necesita
