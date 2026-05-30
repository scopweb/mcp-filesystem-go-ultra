# Bug Report: Prompt Injection en servidor MCP `filesystem-ultra` — FALSO POSITIVO

## Estado: CERRADO (FALSO POSITIVO)

## Resumen original
El servidor `filesystem-ultra` supuestamente inyectaba tags `<userStyle>` y `<system><functions>` al final de cada mensaje del usuario.

## Investigación

### Hallazgo: Servidor LIMPIO

El código del servidor `filesystem-ultra` NO genera los tags `<userStyle>` ni `<system><functions>`.

Sin embargo, durante la auditoría se encontraron y corrigieron **otros problemas menores**:

1. **`serverInstructions`** en `main.go` — contenía ~25 líneas de reglas imperativas (RULES:, AVOID:, etc.) que se enviaban durante el handshake MCP. Reducido a una línea mínima.

2. **Descripción del tool `help`** en `tools_aliases.go` — contenía "CALL THIS FIRST to discover all 16 filesystem tools..." — imperativos innecesarios hacia el LLM. Corregido.

3. **Skill `filesystem-ultra-tools`** — contenía secciones "Never use bash alternatives" y "Recommended workflow" con instrucciones imperativas. Limpiadas.

Estos cambios fueron mergeados en `a4ddee2` independientemente del origen real de los tags.

## Causa real de los tags observados

### `<userStyle>`
El tag `<userStyle>` proviene del sistema de **Styles** de Claude.ai (`Settings → Profile → Styles`). Si el usuario tiene un Style personalizado activo, ese contenido se inyecta en cada turno del usuario.

**No es** causado por el servidor `filesystem-ultra` ni por ningún MCP.

### `<system><functions>`
Es el comportamiento normal del cliente MCP de Claude.ai — re-envía el catálogo de tools en cada turno. Es un patrón del cliente, no una inyección maliciosa.

**No es** causado por el servidor `filesystem-ultra`.

## Lecciones aprendidas

1. Antes de diagnosticar "prompt injection", verificar si el contenido proviene del servidor, del cliente MCP, o de la configuración del usuario (Styles).
2. Los tags `<userStyle>` son característicos del sistema de Styles de Anthropic — si aparecen, revisar la configuración de Styles del perfil en claude.ai.
3. Los tags `<system><functions>` en el payload del modelo son comportamiento estándar del cliente MCP.

## Referencias

- Especificación MCP: https://modelcontextprotocol.io/
- Sistema de Styles en Claude.ai: `Settings → Profile → Styles`

---
**Fecha de cierre**: 2026-04-24
**Investigado por**: Claude Opus 4.7
**Commit de cleanup**: a4ddee2
