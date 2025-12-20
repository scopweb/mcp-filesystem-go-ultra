# Plan de Implementación - Bug10: Sistema de Backup y Protección Mejorados

**Fecha:** 3 de Diciembre de 2025  
**Versión:** 1.0  
**Estado:** PROPUESTA - Revisión de viabilidad

---

## 📋 Resumen Ejecutivo

Este documento analiza la viabilidad e implementación de tres mejoras críticas solicitadas en Bug10.txt:

1. **Backups accesibles** - Ubicación permitida por MCP con metadata completa
2. **Protección anti-sobrescritura** - Validación de impacto antes de ediciones masivas
3. **Herramientas de restauración** - Nuevas tools MCP para gestionar backups

### Motivación Principal
El usuario perdió código debido a operaciones batch que sobrescribieron archivos, y el backup estaba en una ubicación inaccesible para el MCP (fuera de `ALLOWED_PATHS`). Actualmente depende 100% de Git para recuperación, sin red de seguridad intermedia.

---

## ✅ Análisis de Viabilidad

### Estado Actual del Sistema

**Backups existentes:**
- ✅ Función `createBackup()` en `core/edit_operations.go` (línea 244)
- ✅ `BatchOperationManager` con soporte de backup en `core/batch_operations.go`
- ❌ Backups en ubicación temporal: `path + ".backup"` (no accesible por MCP)
- ❌ Backups eliminados automáticamente tras éxito (`defer os.Remove(backupPath)`)
- ❌ Sin metadata: timestamp, operación, tamaño, hash

**Sistema de tools MCP:**
- ✅ Estructura `mcp.NewTool()` + `s.AddTool()` en `main.go`
- ✅ ~50 tools ya implementadas (ver líneas 210-1500 aprox. en `main.go`)
- ✅ Patrón claro para agregar nuevas tools
- ✅ Sistema de hooks pre/post operaciones ya implementado

**Validaciones existentes:**
- ✅ `validateEditContext()` en `edit_operations.go` - validación de contexto
- ✅ `analyze_edit` tool - modo plan/dry-run
- ✅ Telemetría de ediciones con `LogEditTelemetry()`
- ❌ Sin detección de cambios masivos (% de archivo afectado)

### Conclusión: **✅ TOTALMENTE VIABLE**

El código base está bien estructurado y permite implementar todas las mejoras solicitadas:
- Sistema de backup ya existe, solo necesita mejoras
- Arquitectura de tools MCP es extensible
- Sistema de validación presente, solo necesita ampliarse

---

## 🏗️ Arquitectura Propuesta

### Componente 1: Sistema de Backup Mejorado

**Nuevo archivo:** `core/backup_manager.go`

```go
package core

// BackupMetadata contiene información sobre un backup
type BackupMetadata struct {
	BackupID      string    // Timestamp único: 20241203-153045-abc123
	OriginalPath  string    // Ruta original del archivo
	BackupPath    string    // Ruta del backup
	Timestamp     time.Time // Fecha/hora del backup
	FileSize      int64     // Tamaño en bytes
	FileHash      string    // SHA256 del contenido
	Operation     string    // edit, delete, batch, etc.
	UserContext   string    // Info adicional
}

// BackupManager gestiona todos los backups del sistema
type BackupManager struct {
	backupDir      string
	maxBackups     int
	maxAgeDays     int
	mutex          sync.RWMutex
	metadataCache  map[string]*BackupMetadata
}

// Métodos principales:
// - CreateBackup(path, operation) -> (backupID, error)
// - ListBackups(limit, filter) -> ([]BackupMetadata, error)
// - RestoreBackup(backupID, targetPath, preview) -> error
// - CompareBackup(backupID, currentPath) -> (DiffResult, error)
// - CleanupOldBackups(olderThanDays) -> (int, error)
// - GetBackupPath(backupID) -> (string, error)
```

**Ubicación de backups:**
```
C:\Users\DAVID\AppData\Local\Temp\mcp-batch-backups\
├── 20241203-153045-abc123\
│   ├── metadata.json
│   └── files\
│       └── <estructura original>
├── 20241203-154120-def456\
│   ├── metadata.json
│   └── files\
│       └── <estructura original>
└── index.json (cache de metadatas)
```

**Metadata JSON:**
```json
{
  "backup_id": "20241203-153045-abc123",
  "timestamp": "2024-12-03T15:30:45Z",
  "operation": "batch_operations",
  "files": [
    {
      "original_path": "C:\\__REPOS\\project\\src\\main.go",
      "backup_path": "files\\src\\main.go",
      "size": 12345,
      "hash": "sha256:abc123...",
      "modified_time": "2024-12-03T15:29:30Z"
    }
  ],
  "user_context": "Batch rename operation: 47 files"
}
```

### Componente 2: Validación de Impacto

**Extensión de:** `core/edit_operations.go`

```go
// ChangeImpact analiza el impacto de un cambio
type ChangeImpact struct {
	TotalLines       int     // Líneas totales del archivo
	Occurrences      int     // Número de coincidencias
	ChangePercentage float64 // % del archivo que cambiará
	CharactersChanged int64  // Caracteres afectados
	IsRisky          bool    // ¿Requiere confirmación?
	RiskLevel        string  // low, medium, high, critical
	RiskFactors      []string
}

// CalculateChangeImpact analiza el impacto de un edit
func (e *UltraFastEngine) CalculateChangeImpact(
	content, oldText, newText string,
) *ChangeImpact

// Thresholds:
// - >30% del archivo → MEDIUM risk (warning)
// - >50 ocurrencias → MEDIUM risk (warning)
// - >50% del archivo → HIGH risk (requires force:true)
// - >100 ocurrencias → HIGH risk (requires force:true)
// - Archivo completo → CRITICAL risk (double confirmation)
```

**Integración en tools existentes:**

```go
// En edit_file, recovery_edit, intelligent_edit:

// 1. Calcular impacto
impact := engine.CalculateChangeImpact(content, oldText, newText)

// 2. Si es riesgo medio/alto, verificar flag force
force, _ := request.Params.Arguments["force"].(bool)

// 3. Si no hay force y es riesgoso, retornar error descriptivo
if impact.IsRisky && !force {
	return &mcp.CallToolResult{
		Content: []interface{}{
			mcp.NewTextContent(fmt.Sprintf(
				"⚠️ RIESGO %s DETECTADO\n" +
				"Se modificará %.1f%% del archivo (%d ocurrencias)\n" +
				"Factores de riesgo:\n%s\n\n" +
				"Opciones:\n" +
				"1. Usa analyze_edit primero para ver preview\n" +
				"2. Confirma con force: true si estás seguro",
				impact.RiskLevel,
				impact.ChangePercentage,
				impact.Occurrences,
				strings.Join(impact.RiskFactors, "\n")
			)),
		},
		IsError: true,
	}, nil
}

// 4. Crear backup automático (siempre)
backupID, err := engine.backupManager.CreateBackup(path, "edit_file")
if err != nil {
	return nil, fmt.Errorf("no se pudo crear backup: %v", err)
}

// 5. Ejecutar operación
// ...

// 6. Mantener backup (no eliminar)
// Antes: defer os.Remove(backupPath) → ELIMINAR ESTO
// Ahora: Mantener por maxAgeDays (default: 7 días)
```

### Componente 3: Nuevas Herramientas MCP

**Tool 1: `list_backups`**
```go
listBackupsTool := mcp.NewTool("list_backups",
	mcp.WithDescription("Lista backups disponibles con metadata detallada"),
	mcp.WithNumber("limit", 
		mcp.Required(),
		mcp.Description("Máximo número de backups a retornar (default: 20)")),
	mcp.WithString("filter_operation",
		mcp.Description("Filtrar por tipo de operación: edit, delete, batch, all")),
	mcp.WithString("filter_path",
		mcp.Description("Filtrar por ruta de archivo (substring match)")),
	mcp.WithNumber("newer_than_hours",
		mcp.Description("Solo backups creados en las últimas N horas")),
)

// Retorna:
{
	"total": 45,
	"returned": 20,
	"backups": [
		{
			"backup_id": "20241203-153045-abc123",
			"timestamp": "2024-12-03T15:30:45Z",
			"operation": "batch_operations",
			"files_count": 12,
			"total_size": "2.3MB",
			"age": "2 hours ago",
			"files_preview": ["src/main.go", "src/utils.go", "..."]
		}
	],
	"backup_location": "C:\\Users\\DAVID\\AppData\\Local\\Temp\\mcp-batch-backups"
}
```

**Tool 2: `restore_backup`**
```go
restoreBackupTool := mcp.NewTool("restore_backup",
	mcp.WithDescription("Restaura archivo(s) desde un backup"),
	mcp.WithString("backup_id", mcp.Required(),
		mcp.Description("ID del backup (de list_backups)")),
	mcp.WithString("file_path",
		mcp.Description("Archivo específico a restaurar (opcional, default: todos)")),
	mcp.WithBoolean("preview",
		mcp.Description("Si true, solo muestra diff sin restaurar (default: false)")),
	mcp.WithBoolean("force",
		mcp.Description("Sobrescribir sin confirmación si archivo cambió (default: false)")),
)

// Retorna (preview=true):
{
	"preview_mode": true,
	"files_to_restore": [
		{
			"file": "src/main.go",
			"backup_content": "...",
			"current_content": "...",
			"diff": "--- backup\n+++ current\n...",
			"changes": {
				"lines_added": 5,
				"lines_removed": 3,
				"lines_modified": 12
			}
		}
	]
}

// Retorna (preview=false):
{
	"restored": true,
	"files_restored": 12,
	"backup_created": "20241203-160000-xyz789", // Backup del estado actual antes de restaurar
	"errors": []
}
```

**Tool 3: `compare_with_backup`**
```go
compareBackupTool := mcp.NewTool("compare_with_backup",
	mcp.WithDescription("Compara archivo actual vs backup específico"),
	mcp.WithString("backup_id", mcp.Required()),
	mcp.WithString("file_path", mcp.Required()),
	mcp.WithString("format", 
		mcp.Description("Formato del diff: unified, side-by-side, summary (default: unified)")),
)

// Retorna:
{
	"file": "src/main.go",
	"backup_id": "20241203-153045-abc123",
	"backup_timestamp": "2024-12-03T15:30:45Z",
	"backup_size": 12345,
	"current_size": 12567,
	"size_difference": "+222 bytes",
	"diff": "--- backup (20241203-153045)\n+++ current\n@@ -1,10 +1,12 @@\n...",
	"statistics": {
		"lines_added": 15,
		"lines_removed": 8,
		"lines_modified": 23,
		"similarity": 92.5
	}
}
```

**Tool 4: `cleanup_backups`**
```go
cleanupBackupsTool := mcp.NewTool("cleanup_backups",
	mcp.WithDescription("Elimina backups antiguos para liberar espacio"),
	mcp.WithNumber("older_than_days",
		mcp.Description("Eliminar backups más antiguos que N días (default: 7)")),
	mcp.WithBoolean("dry_run",
		mcp.Description("Si true, solo muestra qué se eliminaría (default: true)")),
)

// Retorna:
{
	"dry_run": true,
	"backups_to_delete": 12,
	"space_to_free": "45.2MB",
	"oldest_backup": "20241126-083045-old123",
	"newest_to_delete": "20241201-153045-xyz456",
	"kept_backups": 33
}
```

**Tool 5: `get_backup_info`**
```go
backupInfoTool := mcp.NewTool("get_backup_info",
	mcp.WithDescription("Obtiene información detallada de un backup específico"),
	mcp.WithString("backup_id", mcp.Required()),
)

// Retorna:
{
	"backup_id": "20241203-153045-abc123",
	"timestamp": "2024-12-03T15:30:45Z",
	"operation": "batch_operations",
	"user_context": "Batch rename: old_name → new_name (47 files)",
	"files": [
		{
			"original_path": "C:\\__REPOS\\project\\src\\main.go",
			"size": 12345,
			"hash": "sha256:abc123...",
			"modified_time": "2024-12-03T15:29:30Z"
		}
	],
	"total_size": "2.3MB",
	"total_files": 47,
	"backup_location": "C:\\Users\\DAVID\\AppData\\Local\\Temp\\mcp-batch-backups\\20241203-153045-abc123"
}
```

---

## 📝 Cambios en Tools Existentes

### 1. `edit_file` / `recovery_edit` / `intelligent_edit`

**Cambios:**
```go
// ANTES:
func editFileHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path := request.Params.Arguments["path"].(string)
	oldText := request.Params.Arguments["old_text"].(string)
	newText := request.Params.Arguments["new_text"].(string)
	
	result, err := engine.EditFile(path, oldText, newText)
	// ...
}

// DESPUÉS:
func editFileHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path := request.Params.Arguments["path"].(string)
	oldText := request.Params.Arguments["old_text"].(string)
	newText := request.Params.Arguments["new_text"].(string)
	force, _ := request.Params.Arguments["force"].(bool)  // NUEVO
	
	// 1. NUEVO: Calcular impacto
	content, _ := os.ReadFile(path)
	impact := engine.CalculateChangeImpact(string(content), oldText, newText)
	
	// 2. NUEVO: Validar riesgo
	if impact.IsRisky && !force {
		return &mcp.CallToolResult{
			Content: []interface{}{
				mcp.NewTextContent(fmt.Sprintf(
					"⚠️ RIESGO %s: %.1f%% del archivo cambiará (%d ocurrencias)\n" +
					"Usa analyze_edit para preview o force: true para confirmar",
					impact.RiskLevel, impact.ChangePercentage, impact.Occurrences,
				)),
			},
			IsError: true,
		}, nil
	}
	
	// 3. NUEVO: Crear backup persistente
	backupID, err := engine.backupManager.CreateBackup(path, "edit_file")
	if err != nil {
		return nil, fmt.Errorf("backup failed: %v", err)
	}
	
	// 4. Ejecutar edición
	result, err := engine.EditFile(path, oldText, newText)
	if err != nil {
		return nil, err
	}
	
	// 5. NUEVO: Incluir backup_id en respuesta
	return &mcp.CallToolResult{
		Content: []interface{}{
			mcp.NewTextContent(fmt.Sprintf(
				"✅ File edited successfully\n" +
				"Replaced %d occurrence(s)\n" +
				"Lines affected: %d\n" +
				"🔒 Backup created: %s\n" +
				"   Restore with: restore_backup(\"%s\")",
				result.ReplacementCount,
				result.LinesAffected,
				backupID,
				backupID,
			)),
		},
	}, nil
}
```

**Nuevos parámetros:**
- `force` (boolean, optional): Bypass validación de riesgo
- Retorna `backup_id` en la respuesta

### 2. `batch_operations`

**Cambios:**
```go
// Modificar BatchRequest para incluir:
type BatchRequest struct {
	Operations   []FileOperation `json:"operations"`
	Atomic       bool            `json:"atomic"`
	CreateBackup bool            `json:"create_backup"` // Ya existe
	ValidateOnly bool            `json:"validate_only"` // Ya existe
	Force        bool            `json:"force"`         // NUEVO
}

// En ExecuteBatch:
// 1. Calcular impacto total de todas las operaciones
totalImpact := calculateBatchImpact(request.Operations)

// 2. Si impacto alto y no force, retornar warning
if totalImpact.IsRisky && !request.Force {
	return BatchResult{
		Success: false,
		ValidationOnly: true,
		Errors: []string{
			fmt.Sprintf(
				"⚠️ BATCH RISK HIGH: %d files affected, %.1f%% total changes\n" +
				"Use validate_only: true first or force: true to confirm",
				totalImpact.FilesAffected,
				totalImpact.AverageChangePercentage,
			),
		},
	}
}

// 3. Crear backup único para todo el batch
if request.CreateBackup {
	backupID, err := batchManager.CreateBatchBackup(request.Operations)
	result.BackupID = backupID
}
```

### 3. `delete_file` / `soft_delete_file`

**Cambios:**
```go
// SIEMPRE crear backup antes de eliminar
backupID, err := engine.backupManager.CreateBackup(path, "delete_file")

// Opción: Mover a .trash en lugar de eliminar permanentemente
// (similar a soft_delete pero más integrado)
trashPath := filepath.Join(
	engine.backupManager.backupDir,
	backupID,
	"files",
	filepath.Base(path),
)
```

### 4. `analyze_edit` (mejorar)

**Cambios:**
```go
// Agregar análisis de impacto en el dry-run
analysis.ChangeImpact = engine.CalculateChangeImpact(content, oldText, newText)

// En la respuesta, incluir:
// - Risk level
// - % de archivo afectado
// - Número de ocurrencias
// - Recomendaciones específicas
```

---

## 🔧 Configuración Necesaria

### 1. Variables de Entorno

Agregar a `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "filesystem-ultra": {
      "command": "C:\\MCPs\\clone\\mcp-filesystem-go-ultra\\mcp-filesystem-ultra.exe",
      "args": [
        "--allowed-paths=C:\\__REPOS",
        "--compact-mode",
        "--backup-dir=C:\\Users\\DAVID\\AppData\\Local\\Temp\\mcp-batch-backups",
        "--backup-max-age=7",
        "--backup-max-count=100"
      ],
      "env": {
        "ALLOWED_PATHS": "C:\\__REPOS;C:\\Users\\DAVID\\AppData\\Local\\Temp\\mcp-batch-backups",
        "MCP_BACKUP_DIR": "C:\\Users\\DAVID\\AppData\\Local\\Temp\\mcp-batch-backups",
        "MCP_BACKUP_MAX_AGE_DAYS": "7",
        "MCP_BACKUP_MAX_COUNT": "100"
      }
    }
  }
}
```

### 2. Nuevos Flags de Línea de Comandos

Agregar en `main.go`:

```go
var (
	// ... flags existentes ...
	
	// Nuevos flags para backup
	backupDir      = flag.String("backup-dir", "", "Directory for backup storage")
	backupMaxAge   = flag.Int("backup-max-age", 7, "Max age of backups in days")
	backupMaxCount = flag.Int("backup-max-count", 100, "Max number of backups to keep")
	
	// Nuevos flags para protección
	riskThresholdMedium   = flag.Float64("risk-threshold-medium", 30.0, "% change for medium risk")
	riskThresholdHigh     = flag.Float64("risk-threshold-high", 50.0, "% change for high risk")
	riskOccurrencesMedium = flag.Int("risk-occurrences-medium", 50, "Occurrences for medium risk")
	riskOccurrencesHigh   = flag.Int("risk-occurrences-high", 100, "Occurrences for high risk")
)
```

### 3. Configuración en Configuration struct

```go
type Configuration struct {
	// ... campos existentes ...
	
	// Backup settings
	BackupDir      string
	BackupMaxAge   int
	BackupMaxCount int
	
	// Risk thresholds
	RiskThresholdMedium   float64
	RiskThresholdHigh     float64
	RiskOccurrencesMedium int
	RiskOccurrencesHigh   int
}
```

---

## 📂 Estructura de Archivos a Crear/Modificar

### Archivos Nuevos a Crear:

```
core/
├── backup_manager.go         (NUEVO) - Sistema completo de backups
└── impact_analyzer.go         (NUEVO) - Análisis de riesgo e impacto

docs/
└── BUG10_RESOLUTION.md        (NUEVO) - Documentación de resolución

guides/
└── BACKUP_RECOVERY_GUIDE.md   (NUEVO) - Guía de usuario para backups
```

### Archivos a Modificar:

```
main.go                        - Agregar 5 nuevas tools + configuración
core/edit_operations.go        - Integrar BackupManager + ChangeImpact
core/batch_operations.go       - Integrar validación de riesgo
core/file_operations.go        - Backup en delete operations
core/engine.go                 - Agregar backupManager field
go.mod                         - Posible nueva dependencia: diff library
```

### Tamaño Estimado del Código:

- `backup_manager.go`: ~600 líneas
- `impact_analyzer.go`: ~300 líneas
- Modificaciones en archivos existentes: ~400 líneas
- Nuevas tools en `main.go`: ~500 líneas
- Tests: ~800 líneas

**Total estimado: ~2,600 líneas de código**

---

## 🧪 Plan de Testing

### Tests Unitarios Necesarios:

**1. `tests/backup_manager_test.go`**
```go
// Test cases:
- TestCreateBackup
- TestListBackups
- TestRestoreBackup
- TestCompareBackup
- TestCleanupOldBackups
- TestBackupMetadata
- TestConcurrentBackups
- TestBackupWithLargeFiles
```

**2. `tests/impact_analyzer_test.go`**
```go
// Test cases:
- TestCalculateChangeImpact_LowRisk
- TestCalculateChangeImpact_MediumRisk
- TestCalculateChangeImpact_HighRisk
- TestCalculateChangeImpact_CriticalRisk
- TestMultipleOccurrences
- TestLargeFileImpact
```

**3. `tests/bug10_integration_test.go`**
```go
// Test cases:
- TestEditWithBackup
- TestBatchOperationsWithBackup
- TestRestoreAfterFailedEdit
- TestRiskValidationPreventsEdit
- TestForceBypassValidation
- TestBackupAccessibleByMCP
```

### Escenarios de Usuario a Validar:

1. **Escenario 1: Edit simple con backup**
   ```
   edit_file("test.go", "old", "new")
   → Backup creado automáticamente
   → list_backups() muestra el backup
   → compare_with_backup() muestra diff
   → restore_backup() recupera el original
   ```

2. **Escenario 2: Edit riesgoso bloqueado**
   ```
   edit_file("big.go", "func", "function") // 200 ocurrencias
   → Error: "RIESGO HIGH: 45% del archivo cambiará"
   → analyze_edit() muestra preview
   → edit_file(..., force: true) ejecuta con confirmación
   ```

3. **Escenario 3: Batch operations con rollback**
   ```
   batch_operations([op1, op2, op3], atomic: true)
   → Backup de todos los archivos
   → Op1 ✅, Op2 ✅, Op3 ❌
   → Rollback automático
   → restore_backup() disponible
   ```

4. **Escenario 4: Limpieza de backups antiguos**
   ```
   list_backups() → 150 backups
   cleanup_backups(older_than_days: 7, dry_run: true)
   → "Se eliminarían 45 backups (120MB)"
   cleanup_backups(older_than_days: 7, dry_run: false)
   → "Eliminados 45 backups"
   ```

---

## 📅 Fases de Desarrollo

### FASE 1: Sistema de Backup Mejorado (2-3 días)
**Prioridad:** ALTA

**Tareas:**
1. ✅ Crear `core/backup_manager.go` con:
   - BackupManager struct
   - CreateBackup() con metadata
   - ListBackups() con filtros
   - GetBackupInfo()
   - SaveMetadata() / LoadMetadata()
2. ✅ Modificar `core/edit_operations.go`:
   - Integrar BackupManager
   - Eliminar `defer os.Remove(backupPath)`
   - Retornar backup_id en resultado
3. ✅ Agregar configuración en `main.go`:
   - Flags de línea de comandos
   - Inicialización de BackupManager
   - Pasar a UltraFastEngine
4. ✅ Tests: `backup_manager_test.go`

**Entregable:**
- Backups persistentes en ubicación accesible
- Metadata completa con timestamps
- Backups no se eliminan automáticamente

### FASE 2: Protección Anti-Sobrescritura (1-2 días)
**Prioridad:** ALTA

**Tareas:**
1. ✅ Crear `core/impact_analyzer.go`:
   - ChangeImpact struct
   - CalculateChangeImpact() function
   - Risk level detection
2. ✅ Modificar tools en `main.go`:
   - Agregar parámetro `force` a edit tools
   - Validación de riesgo antes de ejecutar
   - Mensajes de error descriptivos
3. ✅ Actualizar `analyze_edit`:
   - Incluir análisis de impacto
   - Recomendaciones específicas
4. ✅ Tests: `impact_analyzer_test.go`

**Entregable:**
- Validación automática de cambios riesgosos
- Mensajes claros de advertencia
- Opción force para bypass consciente

### FASE 3: Herramientas de Restauración (2-3 días)
**Prioridad:** MEDIA

**Tareas:**
1. ✅ Implementar funciones en `backup_manager.go`:
   - RestoreBackup()
   - CompareBackup()
   - CleanupOldBackups()
2. ✅ Agregar 5 nuevas tools en `main.go`:
   - list_backups
   - restore_backup
   - compare_with_backup
   - cleanup_backups
   - get_backup_info
3. ✅ Librería de diff:
   - Evaluar: "github.com/sergi/go-diff/diffmatchpatch"
   - Implementar formato unified diff
4. ✅ Tests: `bug10_integration_test.go`

**Entregable:**
- 5 nuevas herramientas MCP funcionales
- Sistema completo de recuperación
- Diffs legibles y útiles

### FASE 4: Integración y Documentación (1 día)
**Prioridad:** MEDIA

**Tareas:**
1. ✅ Actualizar documentación:
   - guides/BACKUP_RECOVERY_GUIDE.md
   - docs/BUG10_RESOLUTION.md
   - Actualizar README.md con nuevas tools
2. ✅ Ejemplos en `examples/`:
   - backup_usage.json
   - risk_validation.json
3. ✅ Actualizar `get_help()` tool:
   - Nuevo topic: "backup"
   - Ejemplos de uso
4. ✅ CHANGELOG.md v3.8.0

**Entregable:**
- Documentación completa
- Ejemplos de uso
- Guía de migración

### FASE 5: Testing y Validación (1-2 días)
**Prioridad:** ALTA

**Tareas:**
1. ✅ Tests de integración completos
2. ✅ Testing con Claude Desktop:
   - Verificar ALLOWED_PATHS
   - Probar todas las nuevas tools
   - Validar escenarios de usuario
3. ✅ Performance testing:
   - Impacto en velocidad de edición
   - Tamaño de backups con archivos grandes
4. ✅ Limpieza y optimización de código

**Entregable:**
- Sistema probado y funcional
- Sin degradación de performance
- Listo para producción

---

## ⚡ Impacto en Performance

### Análisis de Costos:

**Operación de Backup:**
- Costo adicional: ~5-10ms por archivo pequeño (<100KB)
- Costo con archivos grandes: ~50ms por 1MB
- Mitigación: Backup en goroutine paralela

**Validación de Impacto:**
- Costo adicional: ~1-3ms (análisis de strings)
- Solo ocurre en ediciones, no en lectura
- Negligible comparado con I/O de disco

**Almacenamiento:**
- Sin compresión: ~1:1 del tamaño original
- Con compresión (opcional): ~0.3:1 (texto)
- Limpieza automática: 7 días default

### Optimizaciones Propuestas:

1. **Backup asíncrono:**
   ```go
   go func() {
       backupID, _ := backupManager.CreateBackup(path, operation)
       // No bloquea la respuesta al usuario
   }()
   ```

2. **Cache de metadata:**
   ```go
   // Evitar re-escanear directorio en cada list_backups()
   type BackupManager struct {
       metadataCache map[string]*BackupMetadata
       cacheMutex    sync.RWMutex
       lastScan      time.Time
   }
   ```

3. **Compresión opcional:**
   ```go
   // Para archivos grandes, comprimir con gzip
   if fileSize > 1*1024*1024 { // >1MB
       compressBackup(backupPath)
   }
   ```

---

## 🚨 Riesgos y Mitigaciones

### Riesgo 1: Espacio en Disco
**Problema:** Backups pueden consumir mucho espacio

**Mitigaciones:**
- Limpieza automática después de 7 días
- Límite de 100 backups máximo
- Tool `cleanup_backups` para control manual
- Warning cuando espacio disponible < 1GB

### Riesgo 2: ALLOWED_PATHS
**Problema:** Si backup_dir no está en ALLOWED_PATHS, MCP no puede acceder

**Mitigaciones:**
- Documentación clara en configuración
- Validación al inicio: verificar que backup_dir esté accesible
- Error descriptivo si no está configurado correctamente

### Riesgo 3: Performance
**Problema:** Backup puede ralentizar operaciones

**Mitigaciones:**
- Backup asíncrono donde sea posible
- Solo crear backup si archivo cambió realmente
- Cache de metadata para evitar I/O innecesario

### Riesgo 4: Falsos Positivos en Riesgo
**Problema:** Validación puede bloquear operaciones legítimas

**Mitigaciones:**
- Thresholds configurables
- Flag `force` para bypass
- Mensajes claros explicando por qué es riesgoso
- `analyze_edit` para preview antes de forzar

---

## ✅ Criterios de Aceptación

Para considerar Bug10 como **RESUELTO**, se deben cumplir:

1. ✅ **Backups accesibles:**
   - Backups en `C:\Users\DAVID\AppData\Local\Temp\mcp-batch-backups`
   - Ubicación incluida en ALLOWED_PATHS
   - `list_backups()` funciona desde Claude Desktop

2. ✅ **Protección anti-sobrescritura:**
   - Ediciones >30% del archivo muestran warning
   - Ediciones >50 ocurrencias requieren `force: true`
   - `analyze_edit` muestra preview completo

3. ✅ **Herramientas de restauración:**
   - `list_backups()` funcional
   - `restore_backup()` recupera archivos correctamente
   - `compare_with_backup()` muestra diff legible
   - `cleanup_backups()` gestiona espacio

4. ✅ **Metadata completa:**
   - Timestamp
   - Operación que lo creó
   - Tamaño de archivo
   - Hash para integridad

5. ✅ **Sin degradación de performance:**
   - Ediciones <20ms más lentas con backup
   - Cache hit rate >95% mantenido
   - No bloquea operaciones normales

6. ✅ **Documentación:**
   - Guía de usuario para recovery
   - Ejemplos de uso de todas las tools
   - Configuración documentada

---

## 📊 Métricas de Éxito

Después de la implementación, medir:

1. **Tasa de recuperación:**
   - Objetivo: >90% de recuperaciones exitosas
   - Métrica: `restore_backup` success rate

2. **Prevención de pérdidas:**
   - Objetivo: 0 pérdidas de código por sobrescritura accidental
   - Métrica: User reports de pérdida de datos

3. **Uso de backups:**
   - Objetivo: >30% de usuarios usan `list_backups` al menos 1x/semana
   - Métrica: Tool invocation count

4. **Performance:**
   - Objetivo: <10ms overhead promedio
   - Métrica: Edit operation latency P95

5. **Espacio en disco:**
   - Objetivo: <500MB promedio para usuario típico
   - Métrica: Total backup directory size

---

## 🎯 Conclusión

### Recomendación: **✅ PROCEDER CON IMPLEMENTACIÓN**

**Justificación:**
1. ✅ Arquitectura existente permite implementación sin refactoring mayor
2. ✅ Beneficio claro: previene pérdida de código
3. ✅ Impacto en performance es mínimo y mitigable
4. ✅ Todas las fases son incrementales y testables
5. ✅ No rompe compatibilidad con tools existentes

**Esfuerzo estimado:** 7-11 días de desarrollo

**Complejidad:** Media (requiere testing cuidadoso pero no hay bloqueos técnicos)

**Valor para el usuario:** **ALTO** - Resuelve problema real de pérdida de código

---

## 📋 Próximos Pasos

1. ✅ **Revisión de este plan** - Confirmar aprobación
2. ⏳ **Setup de entorno** - Configurar backup_dir, ALLOWED_PATHS
3. ⏳ **Fase 1** - Implementar BackupManager
4. ⏳ **Fase 2** - Agregar validación de riesgo
5. ⏳ **Fase 3** - Implementar tools de recuperación
6. ⏳ **Testing** - Validación completa con Claude Desktop
7. ⏳ **Release v3.8.0** - Deploy a producción

---

**Autor:** GitHub Copilot  
**Fecha de creación:** 3 de Diciembre de 2025  
**Última actualización:** 3 de Diciembre de 2025  
**Estado:** ✅ LISTO PARA REVISIÓN
