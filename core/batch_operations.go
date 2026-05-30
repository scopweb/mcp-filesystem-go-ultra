package core

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// FileOperation representa una operación individual en un batch
type FileOperation struct {
	Type        string                 `json:"type"`        // write, edit, search_and_replace, move, delete, create_dir, copy
	Path        string                 `json:"path"`        // Ruta principal
	Source      string                 `json:"source"`      // Para move/copy
	Destination string                 `json:"destination"` // Para move/copy
	Content     string                 `json:"content"`     // Para write
	OldText     string                 `json:"old_text"`    // Para edit
	NewText     string                 `json:"new_text"`    // Para edit
	Options     map[string]interface{} `json:"options"`     // Opciones adicionales
}

// BatchRequest representa una solicitud de operaciones en batch
type BatchRequest struct {
	Operations   []FileOperation `json:"operations"`
	Atomic       bool            `json:"atomic"`        // Si es true, se hace rollback en caso de error
	CreateBackup bool            `json:"create_backup"` // Crear backup antes de ejecutar
	ValidateOnly bool            `json:"validate_only"` // Solo validar, no ejecutar
	Force        bool            `json:"force"`         // Bypass risk validation warnings
}

// BatchResult representa el resultado de ejecutar un batch
type BatchResult struct {
	Success        bool              `json:"success"`
	TotalOps       int               `json:"total_operations"`
	CompletedOps   int               `json:"completed_operations"`
	FailedOps      int               `json:"failed_operations"`
	Results        []OperationResult `json:"results"`
	BackupPath     string            `json:"backup_path,omitempty"`
	BackupID       string            `json:"backup_id,omitempty"` // New: ID from BackupManager
	RollbackDone   bool              `json:"rollback_done"`
	ExecutionTime  string            `json:"execution_time"`
	ValidationOnly bool              `json:"validation_only"`
	Errors         []string          `json:"errors,omitempty"`
	RiskLevel      string            `json:"risk_level,omitempty"`   // New: Batch risk assessment
	RiskWarning    string            `json:"risk_warning,omitempty"` // New: Risk warning message
}

// OperationResult representa el resultado de una operación individual
type OperationResult struct {
	Index         int    `json:"index"`
	Type          string `json:"type"`
	Path          string `json:"path"`
	Success       bool   `json:"success"`
	Error         string `json:"error,omitempty"`
	Skipped       bool   `json:"skipped,omitempty"`
	BytesAffected int64  `json:"bytes_affected,omitempty"`
}

// BatchOperationManager maneja operaciones en batch con soporte para rollback
type BatchOperationManager struct {
	backupDir       string          // Local backup directory (for tests or when no shared manager)
	backupManager   *BackupManager  // Shared backup manager (统一backup系统)
	maxBackups      int
	mutex           sync.Mutex
	currentBackup   string
	engine          *UltraFastEngine // Reference to engine for intelligent edit (Bug #18)
}

// NewBatchOperationManager creates a new batch operation manager
// backupDir is kept for test compatibility; use SetBackupManager for shared backup system
func NewBatchOperationManager(backupDir string, maxBackups int) *BatchOperationManager {
	if backupDir == "" {
		backupDir = filepath.Join(os.TempDir(), "mcp-batch-backups")
	}
	os.MkdirAll(backupDir, 0755)
	return &BatchOperationManager{
		backupDir:   backupDir,
		maxBackups: maxBackups,
	}
}

// SetBackupManager sets the shared backup manager for unified backup system
// Call this in production to use the engine's BackupManager
func (m *BatchOperationManager) SetBackupManager(manager *BackupManager) {
	m.backupManager = manager
}

// SetEngine sets the engine reference for intelligent edit support in batch operations.
func (m *BatchOperationManager) SetEngine(engine *UltraFastEngine) {
	m.engine = engine
}

// executeHooksForOperation runs pre/post hooks for batch operations when an engine is available.
// This ensures hooks are respected even when using the batch manager's low-level execution path.
// Returns error only if a hook denied the operation.
func (m *BatchOperationManager) executeHooksForOperation(ctx context.Context, event HookEvent, op FileOperation) error {
	if m.engine == nil || m.engine.hookManager == nil || !m.engine.hookManager.IsEnabled() {
		return nil
	}

	workingDir, _ := os.Getwd()
	hookCtx := &HookContext{
		Event:      event,
		ToolName:   "batch_" + string(op.Type), // e.g. "batch_write", "batch_edit"
		FilePath:   op.Path,
		Operation:  string(op.Type),
		SourcePath: op.Source,
		DestPath:   op.Destination,
		Content:    op.Content,
		Timestamp:  time.Now(),
		WorkingDir: workingDir,
	}

	_, err := m.engine.hookManager.ExecuteHooks(ctx, event, hookCtx)
	return err
}

// ExecuteBatch ejecuta un batch de operaciones
func (m *BatchOperationManager) ExecuteBatch(request BatchRequest) BatchResult {
	startTime := time.Now()

	result := BatchResult{
		TotalOps:       len(request.Operations),
		Results:        make([]OperationResult, 0, len(request.Operations)),
		ValidationOnly: request.ValidateOnly,
	}

	// Paso 1: Validar todas las operaciones
	validationErrors := m.validateOperations(request.Operations)
	if len(validationErrors) > 0 {
		result.Success = false
		result.Errors = validationErrors
		result.ExecutionTime = time.Since(startTime).String()
		return result
	}

	// Si solo es validación, retornar aquí
	if request.ValidateOnly {
		result.Success = true
		result.ExecutionTime = time.Since(startTime).String()
		for i, op := range request.Operations {
			result.Results = append(result.Results, OperationResult{
				Index:   i,
				Type:    op.Type,
				Path:    op.Path,
				Success: true,
			})
		}
		return result
	}

	// Paso 2: Crear backup si se solicita
	if request.CreateBackup {
		backupPath, err := m.createBackup(request.Operations)
		if err != nil {
			result.Success = false
			result.Errors = []string{fmt.Sprintf("Failed to create backup: %v", err)}
			result.ExecutionTime = time.Since(startTime).String()
			return result
		}
		result.BackupPath = backupPath
		m.currentBackup = backupPath
	}

	// Paso 3: Ejecutar operaciones
	rollbackInfo := make([]rollbackData, 0, len(request.Operations))

	for i, op := range request.Operations {
		opResult := OperationResult{
			Index: i,
			Type:  op.Type,
			Path:  op.Path,
		}

		// Guardar información para rollback
		var rbData rollbackData
		if request.Atomic {
			rbData = m.prepareRollback(op)
		}

		// Ejecutar operación
		err := m.executeOperation(op, &opResult)

		if err != nil {
			opResult.Success = false
			opResult.Error = err.Error()
			result.FailedOps++

			// Si es atómico y falla, hacer rollback
			if request.Atomic {
				result.RollbackDone = true
				m.rollback(rollbackInfo)
				result.Success = false
				result.Errors = []string{fmt.Sprintf("Operation %d failed, rollback completed: %v", i, err)}
				result.Results = append(result.Results, opResult)
				result.ExecutionTime = time.Since(startTime).String()
				return result
			}
		} else {
			opResult.Success = true
			result.CompletedOps++
			if request.Atomic {
				rollbackInfo = append(rollbackInfo, rbData)
			}
		}

		result.Results = append(result.Results, opResult)
	}

	result.Success = result.FailedOps == 0
	result.ExecutionTime = time.Since(startTime).String()

	return result
}

// validateOperations valida que todas las operaciones sean ejecutables
func (m *BatchOperationManager) validateOperations(operations []FileOperation) []string {
	errors := make([]string, 0)

	// Track paths that will be created/available by earlier ops in this batch
	// so that write→edit, create_dir→write, copy→edit chains validate correctly.
	pendingPaths := make(map[string]bool)

	for i, op := range operations {
		// Security: enforce allowed-paths on every path in the operation.
		// Without this check, batch operations bypass --allowed-paths access control.
		if m.engine != nil && len(m.engine.config.AllowedPaths) > 0 {
			for _, p := range m.collectPaths(op) {
				if p != "" && !m.engine.IsPathAllowed(p) {
					errors = append(errors, fmt.Sprintf("Op %d: access denied — path '%s' is not in allowed paths", i, p))
				}
			}
			// Prevent destructive operations on allowed-path roots
			if op.Type == "delete" || op.Type == "move" {
				target := op.Path
				if op.Type == "move" {
					target = op.Source
				}
				if target != "" && m.engine.IsAllowedPathRoot(target) {
					errors = append(errors, fmt.Sprintf("Op %d: access denied — cannot %s allowed-path root '%s'", i, op.Type, target))
				}
			}
		}

		switch op.Type {
		case "write":
			if op.Path == "" {
				errors = append(errors, fmt.Sprintf("Op %d: path is required for write", i))
			}
			// Validar que el directorio padre existe o será creado por una op anterior del batch
			dir := filepath.Dir(op.Path)
			if _, err := os.Stat(dir); os.IsNotExist(err) && !pendingPaths[dir] {
				errors = append(errors, fmt.Sprintf("Op %d: parent directory does not exist: %s", i, dir))
			}
			pendingPaths[op.Path] = true // este archivo existirá tras la op

		case "edit":
			if op.Path == "" {
				errors = append(errors, fmt.Sprintf("Op %d: path is required for edit", i))
			}
			if op.OldText == "" && op.NewText == "" {
				errors = append(errors, fmt.Sprintf("Op %d: old_text or new_text required for edit", i))
			}
			// Permitir si una op anterior del batch crea el archivo
			if _, err := os.Stat(op.Path); os.IsNotExist(err) && !pendingPaths[op.Path] {
				errors = append(errors, fmt.Sprintf("Op %d: file does not exist: %s", i, op.Path))
			}

		case "search_and_replace":
			if op.Path == "" {
				errors = append(errors, fmt.Sprintf("Op %d: path is required for search_and_replace", i))
			}
			if op.OldText == "" {
				errors = append(errors, fmt.Sprintf("Op %d: old_text (pattern) is required for search_and_replace", i))
			}
			if _, err := os.Stat(op.Path); os.IsNotExist(err) && !pendingPaths[op.Path] {
				errors = append(errors, fmt.Sprintf("Op %d: path does not exist: %s", i, op.Path))
			}

		case "move":
			if op.Source == "" || op.Destination == "" {
				errors = append(errors, fmt.Sprintf("Op %d: source and destination required for move", i))
			}
			if _, err := os.Stat(op.Source); os.IsNotExist(err) && !pendingPaths[op.Source] {
				errors = append(errors, fmt.Sprintf("Op %d: source does not exist: %s", i, op.Source))
			}
			// Validar que el destino no existe ni será creado por una op anterior
			if _, err := os.Stat(op.Destination); err == nil && !pendingPaths[op.Destination] {
				errors = append(errors, fmt.Sprintf("Op %d: destination already exists: %s", i, op.Destination))
			}
			pendingPaths[op.Destination] = true // el archivo existirá en el destino
			delete(pendingPaths, op.Source)      // ya no estará en el origen

		case "copy":
			if op.Source == "" || op.Destination == "" {
				errors = append(errors, fmt.Sprintf("Op %d: source and destination required for copy", i))
			}
			if _, err := os.Stat(op.Source); os.IsNotExist(err) && !pendingPaths[op.Source] {
				errors = append(errors, fmt.Sprintf("Op %d: source does not exist: %s", i, op.Source))
			}
			pendingPaths[op.Destination] = true // la copia existirá

		case "delete":
			if op.Path == "" {
				errors = append(errors, fmt.Sprintf("Op %d: path is required for delete", i))
			}
			if _, err := os.Stat(op.Path); os.IsNotExist(err) && !pendingPaths[op.Path] {
				errors = append(errors, fmt.Sprintf("Op %d: file does not exist: %s", i, op.Path))
			}
			delete(pendingPaths, op.Path) // ya no estará disponible

		case "create_dir":
			if op.Path == "" {
				errors = append(errors, fmt.Sprintf("Op %d: path is required for create_dir", i))
			}
			// Validar que el directorio no existe ya (en disco ni pendiente)
			if _, err := os.Stat(op.Path); err == nil {
				errors = append(errors, fmt.Sprintf("Op %d: directory already exists: %s", i, op.Path))
			}
			pendingPaths[op.Path] = true // el directorio existirá tras la op

		default:
			errors = append(errors, fmt.Sprintf("Op %d: unknown operation type: %s", i, op.Type))
		}
	}

	return errors
}

// rollbackData contiene información necesaria para revertir una operación
type rollbackData struct {
	operationType string
	originalPath  string
	backupPath    string
	content       []byte
	wasCreated    bool
}

// prepareRollback prepara la información necesaria para revertir una operación
func (m *BatchOperationManager) prepareRollback(op FileOperation) rollbackData {
	rb := rollbackData{
		operationType: op.Type,
	}

	switch op.Type {
	case "write":
		rb.originalPath = op.Path
		// Guardar contenido original si el archivo existe
		if content, err := os.ReadFile(op.Path); err == nil {
			rb.content = content
		} else {
			rb.wasCreated = true
		}

	case "edit", "search_and_replace":
		rb.originalPath = op.Path
		// Guardar contenido original
		if content, err := os.ReadFile(op.Path); err == nil {
			rb.content = content
		}

	case "move":
		rb.originalPath = op.Source
		rb.backupPath = op.Destination

	case "copy":
		rb.originalPath = op.Destination
		rb.wasCreated = true

	case "delete":
		rb.originalPath = op.Path
		// Guardar contenido antes de eliminar
		if content, err := os.ReadFile(op.Path); err == nil {
			rb.content = content
		}

	case "create_dir":
		rb.originalPath = op.Path
		rb.wasCreated = true
	}

	return rb
}

// rollback revierte las operaciones ejecutadas
func (m *BatchOperationManager) rollback(rollbackInfo []rollbackData) {
	// Revertir en orden inverso
	for i := len(rollbackInfo) - 1; i >= 0; i-- {
		rb := rollbackInfo[i]

		switch rb.operationType {
		case "write", "edit", "search_and_replace":
			if rb.wasCreated {
				// El archivo fue creado, eliminarlo
				os.Remove(rb.originalPath)
			} else {
				// Restaurar contenido original
				os.WriteFile(rb.originalPath, rb.content, 0644)
			}

		case "move":
			// Mover de vuelta
			os.Rename(rb.backupPath, rb.originalPath)

		case "copy":
			// Eliminar la copia
			os.Remove(rb.originalPath)

		case "delete":
			// Restaurar archivo eliminado
			os.WriteFile(rb.originalPath, rb.content, 0644)

		case "create_dir":
			// Eliminar directorio creado
			os.Remove(rb.originalPath)
		}
	}
}

// getBackupDir returns the effective backup directory
func (m *BatchOperationManager) getBackupDir() string {
	if m.backupManager != nil {
		return m.backupManager.backupDir
	}
	return m.backupDir
}

// createBackup crea un backup de todos los archivos afectados
func (m *BatchOperationManager) createBackup(operations []FileOperation) (string, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// Crear directorio de backup con timestamp
	timestamp := time.Now().Format("20060102-150405")
	backupID := fmt.Sprintf("batch-%s", timestamp)
	backupPath := filepath.Join(m.getBackupDir(), backupID)

	if err := os.MkdirAll(backupPath, 0755); err != nil {
		return "", err
	}

	// Preparar metadatos compatibles con BackupInfo
	filesBackupDir := filepath.Join(backupPath, "files")
	if err := os.MkdirAll(filesBackupDir, 0755); err != nil {
		return "", err
	}

	var backupMetadatas []BackupMetadata
	var totalSize int64

	// Hacer backup de archivos afectados
	for i, op := range operations {
		var sourceFile string

		switch op.Type {
		case "write", "edit", "search_and_replace", "delete":
			sourceFile = op.Path
		case "move":
			sourceFile = op.Source
		}

		if sourceFile != "" {
			if fileInfo, err := os.Stat(sourceFile); err == nil {
				// El archivo existe, hacer backup
				backupFileName := fmt.Sprintf("op-%d-%s", i, filepath.Base(sourceFile))
				backupFilePath := filepath.Join(filesBackupDir, backupFileName)
				hash, err := copyFileWithHash(sourceFile, backupFilePath)
				if err != nil {
					return "", fmt.Errorf("failed to backup %s: %w", sourceFile, err)
				}
				backupMetadatas = append(backupMetadatas, BackupMetadata{
					OriginalPath: sourceFile,
					BackupPath:   filepath.Join("files", backupFileName),
					Size:         fileInfo.Size(),
					Hash:         hash,
					ModifiedTime: fileInfo.ModTime(),
				})
				totalSize += fileInfo.Size()
			}
		}
	}

	// Guardar metadatos en formato compatible con BackupManager
	info := BackupInfo{
		BackupID:  backupID,
		Timestamp: time.Now(),
		Operation: "batch_operation",
		Files:     backupMetadatas,
		TotalSize: totalSize,
	}

	metadataJSON, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal backup metadata: %w", err)
	}
	if err := os.WriteFile(filepath.Join(backupPath, "metadata.json"), metadataJSON, 0644); err != nil {
		return "", fmt.Errorf("failed to write metadata: %w", err)
	}

	// Registrar en el cache del backup manager si está disponible
	if m.backupManager != nil {
		m.backupManager.metadataCache[backupID] = &info
	}

	// Limpiar backups antiguos
	m.cleanOldBackups()

	return backupPath, nil
}

// executeOperation ejecuta una operación individual
func (m *BatchOperationManager) executeOperation(op FileOperation, result *OperationResult) error {
	switch op.Type {
	case "write":
		return m.executeWrite(op, result)
	case "edit":
		return m.executeEdit(op, result)
	case "search_and_replace":
		return m.executeSearchAndReplace(op, result)
	case "move":
		return m.executeMove(op, result)
	case "copy":
		return m.executeCopy(op, result)
	case "delete":
		return m.executeDelete(op, result)
	case "create_dir":
		return m.executeCreateDir(op, result)
	default:
		return fmt.Errorf("unknown operation type: %s", op.Type)
	}
}

func (m *BatchOperationManager) executeWrite(op FileOperation, result *OperationResult) error {
	ctx := context.Background()

	// Pre-write hook (respects user hooks even in batch mode)
	if err := m.executeHooksForOperation(ctx, HookPreWrite, op); err != nil {
		return fmt.Errorf("pre-write hook denied batch write: %w", err)
	}

	err := os.WriteFile(op.Path, []byte(op.Content), 0644)
	if err != nil {
		return err
	}

	result.BytesAffected = int64(len(op.Content))

	// Post-write hook (best effort)
	postOp := op
	_ = m.executeHooksForOperation(ctx, HookPostWrite, postOp)

	return nil
}

func (m *BatchOperationManager) executeEdit(op FileOperation, result *OperationResult) error {
	ctx := context.Background()

	// Pre-edit hook
	if err := m.executeHooksForOperation(ctx, HookPreEdit, op); err != nil {
		return fmt.Errorf("pre-edit hook denied batch edit: %w", err)
	}

	content, err := os.ReadFile(op.Path)
	if err != nil {
		return err
	}

	original := string(content)

	var finalContent string

	// Use performIntelligentEdit when engine is available
	if m.engine != nil {
		editResult, editErr := m.engine.performIntelligentEdit(original, op.OldText, op.NewText)
		if editErr != nil || editResult.ReplacementCount == 0 {
			return fmt.Errorf("old_text not found in file: %s. "+
				"ALWAYS read the file with read_file BEFORE editing. "+
				"Copy the exact text from the read result as old_text", op.Path)
		}
		finalContent = editResult.ModifiedContent
	} else {
		// Fallback
		finalContent = strings.Replace(original, op.OldText, op.NewText, 1)
		if finalContent == original {
			return fmt.Errorf("old_text not found in file: %s. "+
				"ALWAYS read the file with read_file BEFORE editing. "+
				"Copy the exact text from the read result as old_text", op.Path)
		}
	}

	err = os.WriteFile(op.Path, []byte(finalContent), 0644)
	if err != nil {
		return err
	}

	result.BytesAffected = int64(len(finalContent) - len(original))

	// Post-edit hook (best effort)
	postOp := op
	postOp.Content = finalContent
	_ = m.executeHooksForOperation(ctx, HookPostEdit, postOp)

	return nil
}

func (m *BatchOperationManager) executeSearchAndReplace(op FileOperation, result *OperationResult) error {
	ctx := context.Background()

	// Pre-write style hook for search_and_replace (treated as edit/write)
	if err := m.executeHooksForOperation(ctx, HookPreWrite, op); err != nil {
		return fmt.Errorf("pre-write hook denied batch search_and_replace: %w", err)
	}

	if m.engine == nil {
		return fmt.Errorf("search_and_replace requires engine (not available in standalone batch mode)")
	}
	replacements, err := m.engine.searchAndReplaceInFile(op.Path, op.OldText, op.NewText, true, false)
	if err != nil {
		return err
	}
	if replacements == 0 {
		return fmt.Errorf("pattern '%s' not found in %s", op.OldText, op.Path)
	}
	result.BytesAffected = int64(replacements)

	// Post-write hook (best effort)
	_ = m.executeHooksForOperation(ctx, HookPostWrite, op)

	return nil
}

func (m *BatchOperationManager) executeMove(op FileOperation, result *OperationResult) error {
	ctx := context.Background()

	if err := m.executeHooksForOperation(ctx, HookPreMove, op); err != nil {
		return fmt.Errorf("pre-move hook denied batch move: %w", err)
	}

	info, err := os.Stat(op.Source)
	if err != nil {
		return err
	}

	err = os.Rename(op.Source, op.Destination)
	if err != nil {
		return err
	}

	result.BytesAffected = info.Size()

	_ = m.executeHooksForOperation(ctx, HookPostMove, op)
	return nil
}

func (m *BatchOperationManager) executeCopy(op FileOperation, result *OperationResult) error {
	ctx := context.Background()

	if err := m.executeHooksForOperation(ctx, HookPreCopy, op); err != nil {
		return fmt.Errorf("pre-copy hook denied batch copy: %w", err)
	}

	info, err := os.Stat(op.Source)
	if err != nil {
		return err
	}

	err = copyFile(op.Source, op.Destination)
	if err != nil {
		return err
	}

	result.BytesAffected = info.Size()

	_ = m.executeHooksForOperation(ctx, HookPostCopy, op)
	return nil
}

func (m *BatchOperationManager) executeDelete(op FileOperation, result *OperationResult) error {
	ctx := context.Background()

	// Pre-delete hook
	if err := m.executeHooksForOperation(ctx, HookPreDelete, op); err != nil {
		return fmt.Errorf("pre-delete hook denied batch delete: %w", err)
	}

	info, err := os.Stat(op.Path)
	if err != nil {
		return err
	}

	err = os.Remove(op.Path)
	if err != nil {
		return err
	}

	result.BytesAffected = info.Size()

	// Post-delete hook (best effort)
	_ = m.executeHooksForOperation(ctx, HookPostDelete, op)

	return nil
}

func (m *BatchOperationManager) executeCreateDir(op FileOperation, result *OperationResult) error {
	ctx := context.Background()

	if err := m.executeHooksForOperation(ctx, HookPreCreate, op); err != nil {
		return fmt.Errorf("pre-create hook denied batch create_dir: %w", err)
	}

	err := os.MkdirAll(op.Path, 0755)
	if err != nil {
		return err
	}

	_ = m.executeHooksForOperation(ctx, HookPostCreate, op)
	return nil
}

// collectPaths returns all filesystem paths referenced by a single operation.
func (m *BatchOperationManager) collectPaths(op FileOperation) []string {
	switch op.Type {
	case "move", "copy":
		return []string{op.Source, op.Destination}
	default:
		return []string{op.Path}
	}
}

// cleanOldBackups limpia backups antiguos manteniendo solo los últimos N
func (m *BatchOperationManager) cleanOldBackups() {
	entries, err := os.ReadDir(m.getBackupDir())
	if err != nil {
		return
	}

	if len(entries) <= m.maxBackups {
		return
	}

	// Ordenar por fecha de modificación y eliminar los más antiguos
	backupDir := m.getBackupDir()
	for i := 0; i < len(entries)-m.maxBackups; i++ {
		path := filepath.Join(backupDir, entries[i].Name())
		os.RemoveAll(path)
	}
}
