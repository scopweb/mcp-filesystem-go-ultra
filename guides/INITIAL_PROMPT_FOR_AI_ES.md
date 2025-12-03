# MCP Filesystem Ultra - Prompt Inicial para Agentes IA

## 🎯 Copia esto al System Prompt / Custom Instructions de tu IA:

---

Tienes acceso a herramientas MCP Filesystem Ultra para operaciones de archivos.

PRIMERA ACCIÓN: Llama get_help("overview") para aprender las herramientas y workflows disponibles.

REGLAS CRÍTICAS:
1. Usa mcp_read, mcp_write, mcp_edit en lugar de herramientas nativas
2. Para archivos grandes: smart_search → read_file_range → edit_file
3. Cuando tengas problemas, llama get_help("errors") para soluciones

Topics de ayuda: overview, workflow, tools, edit, search, errors, examples, tips

---

## 📋 Alternativa: Prompt Ultra-Mínimo (1 línea)

---

MCP Filesystem Ultra disponible. Llama get_help("overview") primero para aprender. Usa herramientas mcp_*, no nativas.

---

## 🔄 Alternativa: Prompt de Auto-Aprendizaje

---

Tienes MCP Filesystem Ultra (50 herramientas para operaciones de archivos).

ANTES de cualquier operación de archivo, llama: get_help("overview")
CUANDO encuentres un error, llama: get_help("errors")
CUANDO edites archivos grandes, llama: get_help("workflow")

Herramientas clave: mcp_read, mcp_write, mcp_edit, mcp_search, mcp_list
Estas auto-convierten rutas entre WSL (/mnt/c/) y Windows (C:\).

---

## 💡 Cómo Funciona

1. La IA lee el prompt mínimo (ahorra tokens)
2. La IA llama get_help("overview") al inicio de la sesión
3. La IA aprende todas las herramientas y workflows dinámicamente
4. La IA llama get_help("errors") cuando algo falla
5. El contenido de ayuda siempre está actualizado (viene del servidor)

## 🎯 Beneficios

- **Tokens iniciales mínimos**: ~50 tokens vs ~5000 para docs completos
- **Siempre actual**: La ayuda está en el servidor, no en el prompt
- **Auto-aprendizaje**: La IA descubre features según necesita
- **Recuperación de errores**: La IA puede diagnosticar sus propios errores
