# Guía de Backup y Recuperación

**Versión:** 3.8.0  
**Fecha:** 3 de Diciembre de 2025

---

## 📦 Introducción

El sistema de backup de MCP Filesystem Ultra protege tu código contra pérdida accidental. Cada operación destructiva (edición, eliminación) crea automáticamente un backup persistente que puedes recuperar fácilmente.

### 🎯 Beneficios Clave

- ✅ **Backups automáticos** - No necesitas hacer nada, se crean solos
- ✅ **Validación inteligente** - Te avisa antes de cambios riesgosos
- ✅ **Recuperación rápida** - Un comando para restaurar código perdido
- ✅ **Auditoría completa** - Historial de todos los cambios

---

## 🔒 Backups Automáticos

### ¿Cuándo se crean backups?

Los backups se crean **automáticamente** antes de:

1. **Ediciones de archivos** (`edit_file`, `intelligent_edit`, `recovery_edit`)
2. **Eliminaciones** (`delete_file`, `soft_delete_file`)
3. **Operaciones batch** (`batch_operations` con `create_backup: true`)

### Ubicación de Backups

Por defecto, los backups se guardan en:
```
C:\Users\DAVID\AppData\Local\Temp\mcp-batch-backups\
```

Cada backup tiene su propio directorio con ID único:
```
20241203-153045-abc123\
├── metadata.json       # Información del backup
└── files\             # Archivos respaldados
    └── tu_archivo.go
```

### Configuración

Puedes personalizar el comportamiento en `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "filesystem-ultra": {
      "args": [
        "--backup-dir=C:\\MisBackups",
        "--backup-max-age=14",
        "--backup-max-count=200"
      ],
      "env": {
        "ALLOWED_PATHS": "C:\\__REPOS;C:\\MisBackups"
      }
    }
  }
}
```

**⚠️ IMPORTANTE:** El directorio de backups **DEBE** estar en `ALLOWED_PATHS`.

---

## ⚠️ Validación de Riesgo

### ¿Qué es la validación de riesgo?

Antes de editar un archivo, el sistema analiza el **impacto** del cambio:
- % del archivo que cambiará
- Número de ocurrencias a reemplazar
- Factores de riesgo específicos

### Niveles de Riesgo

| Nivel | Condiciones | Comportamiento |
|-------|------------|---------------|
| **LOW** | <30% cambio, <50 ocurrencias | Procede normalmente |
| **MEDIUM** | 30-50% cambio, 50-100 ocurrencias | Muestra advertencia |
| **HIGH** | 50-90% cambio, >100 ocurrencias | Requiere `force: true` |
| **CRITICAL** | >90% cambio | Requiere doble confirmación |

### Ejemplo de Advertencia

Cuando intentas un cambio riesgoso:

```javascript
edit_file({
  path: "main.go",
  old_text: "func",
  new_text: "function"
})
```

Respuesta:
```
⚠️  RISK LEVEL: HIGH
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Impact Analysis:
  • 65.3% of file will change
  • 200 occurrence(s) to replace
  • ~15234 characters affected

Risk Factors:
  ⚠️ Large portion of file affected (65.3%)
  ⚠️ Very high occurrence count (200 replacements)

Recommended Actions:
  1. Use 'analyze_edit' to preview changes
  2. Add 'force: true' to proceed
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
```

### Workflow Recomendado

Para cambios riesgosos:

1. **Preview primero** con `analyze_edit`:
   ```javascript
   analyze_edit({
     path: "main.go",
     old_text: "func",
     new_text: "function"
   })
   ```

2. **Revisa los cambios** que se mostrarán

3. **Confirma con force** si todo se ve bien:
   ```javascript
   edit_file({
     path: "main.go",
     old_text: "func",
     new_text: "function",
     force: true
   })
   ```

---

## 🔍 Gestión de Backups

### 1. Listar Backups Disponibles

```javascript
list_backups({
  limit: 20,                    // Máximo a mostrar
  filter_operation: "edit",     // edit, delete, batch, all
  filter_path: "main.go",       // Filtrar por archivo
  newer_than_hours: 24          // Solo últimas 24 horas
})
```

**Respuesta:**
```
📦 Available Backups (3)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

🔖 20241203-153045-abc123
   Time: 2024-12-03 15:30:45 (2 hours ago)
   Operation: edit_file
   Files: 1 (12.1KB)
   Context: Edit: 12 occurrences, 35.2% change

🔖 20241203-140230-def456
   Time: 2024-12-03 14:02:30 (3 hours ago)
   Operation: batch_operations
   Files: 47 (2.3MB)
   Context: Batch rename: 47 files

💡 Use restore_backup(backup_id) to restore files
```

### 2. Obtener Información Detallada

```javascript
get_backup_info({
  backup_id: "20241203-153045-abc123"
})
```

**Respuesta:**
```
📦 Backup Details: 20241203-153045-abc123
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

⏰ Timestamp: 2024-12-03 15:30:45 (2 hours ago)
🔧 Operation: edit_file
📝 Context: Edit: 12 occurrences, 35.2% change
📊 Total Size: 12.1KB
📁 Files: 1

Files in backup:
   • C:\__REPOS\project\main.go (12.1KB)

🔗 Backup Location: C:\Users\DAVID\AppData\Local\Temp\mcp-batch-backups\20241203-153045-abc123
```

### 3. Comparar con Estado Actual

Antes de restaurar, ve qué cambió:

```javascript
compare_with_backup({
  backup_id: "20241203-153045-abc123",
  file_path: "C:\\__REPOS\\project\\main.go"
})
```

**Respuesta:**
```
=== Comparison for C:\__REPOS\project\main.go ===
Backup lines: 245
Current lines: 268
Difference: +23 lines

First differences:
Line 12:
  - BACKUP:  func oldName() {
  + CURRENT: func newName() {

Line 45:
  - BACKUP:  // TODO: implement
  + CURRENT: // DONE: implemented
```

---

## 🔄 Recuperación de Archivos

### Modo Preview (Recomendado)

Primero, usa el modo preview para ver qué se restaurará:

```javascript
restore_backup({
  backup_id: "20241203-153045-abc123",
  file_path: "C:\\__REPOS\\project\\main.go",
  preview: true
})
```

**Respuesta:**
```
📊 Preview Mode - Changes to be restored:

=== Comparison for C:\__REPOS\project\main.go ===
[muestra el diff]
```

### Restauración Real

Si el preview se ve bien, procede con la restauración:

```javascript
restore_backup({
  backup_id: "20241203-153045-abc123",
  file_path: "C:\\__REPOS\\project\\main.go"
})
```

**Respuesta:**
```
✅ Restore completed successfully

📁 Restored 1 file(s):
   • C:\__REPOS\project\main.go

💡 A backup of the current state was created before restoring
```

**Nota:** Se crea un backup del estado actual antes de restaurar, así tienes doble protección.

### Restaurar Todos los Archivos

Omite `file_path` para restaurar todo:

```javascript
restore_backup({
  backup_id: "20241203-140230-def456"  // Backup con 47 archivos
})
```

---

## 🧹 Limpieza de Backups

### ¿Por qué limpiar?

Los backups ocupan espacio en disco. Limpia regularmente los antiguos.

### Dry Run (Recomendado)

Primero, ve qué se eliminaría:

```javascript
cleanup_backups({
  older_than_days: 7,
  dry_run: true
})
```

**Respuesta:**
```
🔍 Dry Run Mode - Preview of cleanup operation

Would delete: 45 backup(s)
Would free: 120.5MB

💡 Run with dry_run: false to actually delete backups
```

### Ejecutar Limpieza

Si estás de acuerdo, ejecuta la limpieza:

```javascript
cleanup_backups({
  older_than_days: 7,
  dry_run: false
})
```

**Respuesta:**
```
✅ Cleanup completed

Deleted: 45 backup(s)
Freed: 120.5MB
```

### Limpieza Automática

El sistema limpia automáticamente cuando:
- Se excede `backup_max_count` (default: 100)
- Los backups más antiguos se eliminan primero

---

## 📋 Casos de Uso Comunes

### Caso 1: Edición Masiva Segura

**Situación:** Necesitas cambiar "func" por "function" en todo el archivo.

```javascript
// 1. Analiza el impacto
analyze_edit({
  path: "main.go",
  old_text: "func",
  new_text: "function"
})

// 2. Si es seguro, procede
edit_file({
  path: "main.go",
  old_text: "func",
  new_text: "function",
  force: true  // Si es riesgoso
})

// 3. Si algo salió mal, restaura
restore_backup({
  backup_id: "20241203-153045-abc123",
  file_path: "main.go"
})
```

### Caso 2: Recuperación de Emergencia

**Situación:** Sobrescribiste un archivo importante por error.

```javascript
// 1. Lista backups recientes
list_backups({
  newer_than_hours: 2,
  filter_path: "importante.go"
})

// 2. Encuentra el backup correcto
get_backup_info({
  backup_id: "20241203-140230-def456"
})

// 3. Compara para estar seguro
compare_with_backup({
  backup_id: "20241203-140230-def456",
  file_path: "importante.go"
})

// 4. Restaura
restore_backup({
  backup_id: "20241203-140230-def456",
  file_path: "importante.go"
})
```

### Caso 3: Batch Operations Seguras

**Situación:** Necesitas renombrar 50 archivos.

```javascript
// 1. Batch con backup automático
batch_operations({
  operations: [
    {type: "edit", path: "file1.go", old_text: "old", new_text: "new"},
    // ... 49 más
  ],
  atomic: true,
  create_backup: true,
  force: true  // Si el análisis lo requiere
})

// 2. Si algo falla, el rollback es automático
// O puedes restaurar manualmente si es necesario
```

### Caso 4: Auditoría de Cambios

**Situación:** Quieres ver qué cambios se hicieron hoy.

```javascript
// Lista todos los backups de hoy
list_backups({
  newer_than_hours: 24,
  limit: 100
})

// Revisa cada uno
get_backup_info({
  backup_id: "20241203-XXXXXX-XXXXXX"
})

// Compara con el estado actual
compare_with_backup({
  backup_id: "20241203-XXXXXX-XXXXXX",
  file_path: "archivo.go"
})
```

---

## ⚙️ Configuración Avanzada

### Thresholds de Riesgo Personalizados

Ajusta la sensibilidad de la validación de riesgo:

```json
{
  "args": [
    "--risk-threshold-medium=40.0",
    "--risk-threshold-high=60.0",
    "--risk-occurrences-medium=75",
    "--risk-occurrences-high=150"
  ]
}
```

**Defaults:**
- MEDIUM: 30% cambio o 50 ocurrencias
- HIGH: 50% cambio o 100 ocurrencias

### Retención de Backups

Controla cuántos backups mantener:

```json
{
  "args": [
    "--backup-max-age=14",      // Días
    "--backup-max-count=200"    // Cantidad
  ]
}
```

**Defaults:**
- Max age: 7 días
- Max count: 100 backups

---

## 🔐 Seguridad y Confiabilidad

### Integridad de Datos

- ✅ **Hash SHA256** de cada archivo
- ✅ Verificación de integridad al restaurar
- ✅ Metadata JSON para auditoría

### Manejo de Errores

- ✅ Rollback si falla la creación del backup
- ✅ Backup del estado actual antes de restaurar
- ✅ Mensajes de error descriptivos

### Performance

- **Overhead mínimo:** ~5-10ms por archivo pequeño
- **Cache inteligente:** Metadata en memoria (refresh cada 5 min)
- **Sin bloqueos:** Operaciones no bloquean el sistema

---

## 💡 Tips y Mejores Prácticas

### 1. Usa Preview Siempre

Antes de restaurar, usa el modo preview:
```javascript
restore_backup({backup_id: "...", file_path: "...", preview: true})
```

### 2. Limpia Regularmente

Establece una rutina semanal:
```javascript
cleanup_backups({older_than_days: 7, dry_run: false})
```

### 3. Valida Cambios Grandes

Para ediciones masivas, siempre usa `analyze_edit` primero.

### 4. Documenta Contexto

Los backups incluyen contexto automático, pero puedes agregar más información en batch operations.

### 5. Mantén el Directorio de Backups Accesible

Asegúrate de que esté en `ALLOWED_PATHS` para acceso completo.

---

## ❓ FAQ

### ¿Los backups se eliminan automáticamente?

No después de éxito, pero sí cuando:
- Excedes `backup_max_count`
- Corres `cleanup_backups`

### ¿Puedo acceder a los backups manualmente?

Sí, están en el filesystem normal. Puedes copiarlos, moverlos, etc.

### ¿Qué pasa si edito un archivo sin backup?

El sistema **siempre** crea un backup antes de editar. Es automático.

### ¿Puedo deshabilitar los backups?

No directamente, pero puedes usar `--backup-max-age=0` para eliminarlos inmediatamente.

### ¿Los backups incluyen contenido binario?

Sí, se respalda cualquier tipo de archivo.

### ¿Qué pasa si no tengo espacio en disco?

El sistema avisará, pero es mejor limpiar regularmente con `cleanup_backups`.

---

## 📚 Recursos Adicionales

- **Documentación Técnica:** `docs/BUG10_RESOLUTION.md`
- **Configuración Claude Desktop:** `guides/CLAUDE_DESKTOP_SETUP.md`
- **Changelog:** `CHANGELOG.md` (v3.8.0)

---

**Versión de la guía:** 1.0  
**Última actualización:** 3 de Diciembre de 2025
