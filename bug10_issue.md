## 🐛 Descripción del Problema

Se perdió código debido a operaciones batch que sobrescribieron archivos, y el backup estaba en una ubicación inaccesible para el MCP (fuera de `ALLOWED_PATHS`). Actualmente dependemos 100% de Git para recuperación, sin red de seguridad intermedia.

## 🔍 Situación Actual

- ❌ Backups en ubicación temporal: `path + ".backup"` (no accesible por MCP)
- ❌ Backups eliminados automáticamente tras éxito (`defer os.Remove(backupPath)`)
- ❌ Sin metadata: timestamp, operación, tamaño, hash
- ❌ Sin herramientas MCP para listar o restaurar backups
- ❌ Sin validación de impacto antes de ediciones masivas

## ✅ Solución Propuesta

### 1. Backups Accesibles
Crear backups en `C:\Users\DAVID\AppData\Local\Temp\mcp-batch-backups` (ruta permitida por MCP) con metadata completa.

### 2. Protección Anti-Sobrescritura
Validar impacto antes de editar - si cambia >30% del archivo o >50 ocurrencias, requerir `analyze_edit` primero o `force: true`.

### 3. Herramientas de Restauración
Nuevas tools MCP:
- `list_backups()` - Listar backups disponibles
- `restore_backup(backup_id, file?)` - Restaurar desde backup
- `compare_with_backup(backup_id, file)` - Ver diferencias
- `cleanup_backups(older_than_days)` - Limpiar backups antiguos
- `get_backup_info(backup_id)` - Información detallada

## 🎯 Criterios de Aceptación

- [ ] Backups en ubicación accesible por MCP
- [ ] Metadata completa (timestamp, hash, tamaño, operación)
- [ ] Validación de riesgo en ediciones masivas
- [ ] 5 nuevas herramientas MCP funcionales
- [ ] Backups persistentes (no eliminados automáticamente)
- [ ] Sin degradación de performance
- [ ] Documentación completa

## 📋 Impacto

**Severidad:** HIGH  
**Prioridad:** HIGH  
**Afectación:** Pérdida potencial de código en operaciones destructivas
