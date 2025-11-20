# Resolución del Bug 8: Error en `recovery_edit` con texto multilínea

## 🔍 Análisis del Problema

El usuario reportó que `filesystem-ultra:recovery_edit` fallaba con el error:
`"context validation failed: old_text not found in current file - file has likely changed"`

Esto ocurría al intentar reemplazar un bloque de texto multilínea, mientras que `batch_operations` (con ediciones de una línea) funcionaba correctamente.

### Causa Raíz

El problema estaba en la función `validateEditContext` en `core/edit_operations.go`.

1.  **Validación Estricta sin Normalización**: La función verificaba la presencia de `old_text` usando `strings.Contains(currentContent, oldText)`.
2.  **Diferencia de Saltos de Línea**: Si el archivo tenía saltos de línea Windows (`\r\n`) y el `old_text` proporcionado tenía saltos Unix (`\n`) (o viceversa), la comparación estricta fallaba inmediatamente.
3.  **Comportamiento de `batch_operations`**: La herramienta `batch_operations` (en su implementación actual) no realiza esta validación estricta (y de hecho, parece sobrescribir el archivo, lo cual es un problema separado pero explica por qué no fallaba con este error específico).

## 🛠️ Solución Implementada

Se modificó `core/edit_operations.go` para normalizar los saltos de línea antes de la validación.

```go
func (e *UltraFastEngine) validateEditContext(currentContent, oldText string) (bool, string) {
	// Normalize line endings for validation
	normalizedContent := normalizeLineEndings(currentContent)
	normalizedOldText := normalizeLineEndings(oldText)

	// If oldText not found at all, it's definitely invalid
	if !strings.Contains(normalizedContent, normalizedOldText) {
		return false, "old_text not found in current file - file has likely changed"
	}
    // ...
```

Esto asegura que la validación de contexto sea robusta frente a diferencias en los saltos de línea (`\r\n` vs `\n`), permitiendo que `recovery_edit` y `smart_edit_file` funcionen correctamente con bloques multilínea en entornos mixtos (Windows/WSL).

##  respuestas a las preguntas del usuario

1.  **¿`recovery_edit` debería aceptar old_text multilínea o solo single-line?**
    Sí, debe aceptar multilínea. La corrección asegura que funcione correctamente independientemente del formato de los saltos de línea.

2.  **¿Hay diferencia entre cómo `recovery_edit` y `batch_operations` normalizan el texto?**
    Sí. `recovery_edit` realiza una validación de contexto previa que era estricta con los saltos de línea. `batch_operations` (en la versión revisada) tiene una implementación más simple (y potencialmente peligrosa) que salta esta validación.

3.  **¿El fuzzy matching tiene umbral de confianza?**
    La validación inicial (`validateEditContext`) es binaria (pasa/no pasa). La edición posterior (`performIntelligentEdit`) tiene mecanismos de coincidencia flexible, pero si la validación inicial falla, no se llega a esa etapa. Ahora la validación inicial es más permisiva con los saltos de línea.

4.  **¿Hay límite de caracteres para old_text en `recovery_edit`?**
    No hay un límite explícito, pero bloques muy grandes aumentan la probabilidad de conflictos si el archivo cambia.

## ✅ Estado
Corregido en `core/edit_operations.go`.
