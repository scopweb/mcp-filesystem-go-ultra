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
	Type        string                 `json:"type"`        // write, edit, search_and_replace, move, delete, create_dir, copy, extract
	Path        string                 `json:"path"`        // Ruta principal
	Source      string                 `json:"source"`      // Para move/copy/extract (archivo origen)
	Destination string                 `json:"destination"` // Para move/copy/extract (archivo destino)
	Content     string                 `json:"content"`     // Para write
	OldText     string                 `json:"old_text"`    // Para edit
	NewText     string                 `json:"new_text"`    // Para edit
	StartLine   int                    `json:"start_line"`  // Para extract: primera línea (1-based, inclusive)
	EndLine     int                    `json:"end_line"`    // Para extract: última línea (1-based, inclusive)
	Append      bool                   `json:"append"`      // Para extract: añadir al destino en vez de sobrescribir
	Options     map[string]interface{} `json:"options"`     // Opciones adicionales
}

// UnmarshalJSON accepts natural aliases for search_and_replace/edit fields
// (parity with multi_edit's old_str/old_string aliases): "search", "find" and
// "pattern" map to old_text; "replace" and "replacement" map to new_text.
// Canonical old_text/new_text take precedence when both are present.
func (op *FileOperation) UnmarshalJSON(data []byte) error {
	type plain FileOperation
	var aux struct {
		plain
		Search      string `json:"search"`
		Find        string `json:"find"`
		Pattern     string `json:"pattern"`
		Replace     string `json:"replace"`
		Replacement string `json:"replacement"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*op = FileOperation(aux.plain)
	if op.OldText == "" {
		for _, alias := range []string{aux.Search, aux.Find, aux.Pattern} {
			if alias != "" {
				op.OldText = alias
				break
			}
		}
	}
	if op.NewText == "" {
		for _, alias := range []string{aux.Replace, aux.Replacement} {
			if alias != "" {
				op.NewText = alias
				break
			}
		}
	}
	return nil
}

// BatchRequest representa una solicitud de operaciones en batch
type BatchRequest struct {
	OperationID   string          `json:"operation_id,omitempty"`
	RetryContract string          `json:"retry_contract,omitempty"`
	Operations    []FileOperation `json:"operations"`
	Atomic        bool            `json:"atomic"`        // Si es true, se hace rollback en caso de error
	CreateBackup  bool            `json:"create_backup"` // Crear backup antes de ejecutar
	ValidateOnly  bool            `json:"validate_only"` // Solo validar, no ejecutar
	Force         bool            `json:"force"`         // Bypass risk validation warnings
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
	RollbackStatus string            `json:"rollback_status,omitempty"`
	RollbackErrors []string          `json:"rollback_errors,omitempty"`
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
	backupDir     string         // Local backup directory (for tests or when no shared manager)
	backupManager *BackupManager // Shared backup manager (统一backup系统)
	maxBackups    int
	mutex         sync.Mutex
	currentBackup string
	engine        *UltraFastEngine // Reference to engine for intelligent edit (Bug #18)
}

// NewBatchOperationManager creates a new batch operation manager
// backupDir is kept for test compatibility; use SetBackupManager for shared backup system
func NewBatchOperationManager(backupDir string, maxBackups int) *BatchOperationManager {
	if backupDir == "" {
		backupDir = filepath.Join(os.TempDir(), "mcp-batch-backups")
	}
	os.MkdirAll(backupDir, 0755)
	return &BatchOperationManager{
		backupDir:  backupDir,
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

// allowedDirsSuffix delegates to the engine's AllowedDirsSuffix so batch
// access-denied errors also list the effective allowed directories. Nil-safe:
// returns "" when no engine is wired (standalone manager use).
func (m *BatchOperationManager) allowedDirsSuffix() string {
	if m.engine == nil {
		return ""
	}
	return m.engine.AllowedDirsSuffix()
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
	return m.ExecuteBatchContext(context.Background(), request)
}

// refreshKnownHashes updates the auto-OCC baseline for files modified by a batch.
// Existing files get their new on-disk hash recorded; removed/moved-away files
// have their baseline cleared. Without this, a file the session had read and
// then modified via batch would wrongly trip auto-OCC on the next edit_file.
func (m *BatchOperationManager) refreshKnownHashes(ops []FileOperation) {
	for _, op := range ops {
		switch op.Type {
		case "write", "edit", "search_and_replace":
			refreshKnownHashPath(op.Path)
		case "extract":
			refreshKnownHashPath(op.Source)
			refreshKnownHashPath(op.Destination)
		case "copy":
			refreshKnownHashPath(op.Destination)
		case "move":
			InvalidateKnownHash(NormalizePath(op.Source))
			refreshKnownHashPath(op.Destination)
		case "delete":
			InvalidateKnownHash(NormalizePath(op.Path))
		}
	}
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
					errors = append(errors, fmt.Sprintf("Op %d: access denied — path '%s' is not in allowed paths%s", i, p, m.allowedDirsSuffix()))
				}
			}
			// Prevent destructive operations on allowed-path roots
			if op.Type == "delete" || op.Type == "move" {
				target := op.Path
				if op.Type == "move" {
					target = op.Source
				}
				if target != "" && m.engine.IsAllowedPathRoot(target) {
					errors = append(errors, fmt.Sprintf("Op %d: access denied — cannot %s allowed-path root '%s'%s", i, op.Type, target, m.allowedDirsSuffix()))
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
			delete(pendingPaths, op.Source)     // ya no estará en el origen

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

		case "extract":
			// extract: move lines [start_line, end_line] from source to destination (point 4)
			if op.Source == "" || op.Destination == "" {
				errors = append(errors, fmt.Sprintf("Op %d: source and destination required for extract", i))
			}
			if op.StartLine < 1 || op.EndLine < op.StartLine {
				errors = append(errors, fmt.Sprintf("Op %d: extract requires start_line>=1 and end_line>=start_line (got %d..%d)", i, op.StartLine, op.EndLine))
			}
			if _, err := os.Stat(op.Source); os.IsNotExist(err) && !pendingPaths[op.Source] {
				errors = append(errors, fmt.Sprintf("Op %d: source does not exist: %s", i, op.Source))
			}
			pendingPaths[op.Destination] = true // el destino existirá tras la op

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
	// Segundo archivo afectado (extract: el destino). Permite revertir una
	// operación que toca dos archivos a la vez (point 4).
	secondPath       string
	secondContent    []byte
	secondWasCreated bool
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

	case "extract":
		// Capture both files so an extract can be fully reverted (point 4).
		rb.originalPath = op.Source
		if content, err := os.ReadFile(op.Source); err == nil {
			rb.content = content
		}
		rb.secondPath = op.Destination
		if content, err := os.ReadFile(op.Destination); err == nil {
			rb.secondContent = content
		} else {
			rb.secondWasCreated = true
		}
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

		case "extract":
			// Restaurar origen y revertir el destino (point 4).
			os.WriteFile(rb.originalPath, rb.content, 0644)
			if rb.secondWasCreated {
				os.Remove(rb.secondPath)
			} else {
				os.WriteFile(rb.secondPath, rb.secondContent, 0644)
			}
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
	backupID := fmt.Sprintf("batch-%s-%s", timestamp, secureRandomSuffix())
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
		case "move", "extract":
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
		m.backupManager.mutex.Lock()
		m.backupManager.metadataCache[backupID] = &info
		m.backupManager.mutex.Unlock()
	}

	// Limpiar backups antiguos
	m.cleanOldBackups()

	return backupPath, nil
}

// executeOperation ejecuta una operación individual
func (m *BatchOperationManager) executeOperationContext(ctx context.Context, op FileOperation, result *OperationResult) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	switch op.Type {
	case "write":
		return m.executeWrite(ctx, op, result)
	case "edit":
		return m.executeEdit(ctx, op, result)
	case "search_and_replace":
		return m.executeSearchAndReplace(ctx, op, result)
	case "move":
		return m.executeMove(ctx, op, result)
	case "copy":
		return m.executeCopy(ctx, op, result)
	case "delete":
		return m.executeDelete(ctx, op, result)
	case "create_dir":
		return m.executeCreateDir(ctx, op, result)
	case "extract":
		return m.executeExtract(ctx, op, result)
	default:
		return fmt.Errorf("unknown operation type: %s", op.Type)
	}
}

func (m *BatchOperationManager) executeWrite(ctx context.Context, op FileOperation, result *OperationResult) error {

	// Pre-write hook (respects user hooks even in batch mode)
	if err := m.executeHooksForOperation(ctx, HookPreWrite, op); err != nil {
		return fmt.Errorf("pre-write hook denied batch write: %w", err)
	}

	// Point 6a: atomic write (temp file + rename) instead of a direct
	// os.WriteFile, so a batch interrupted mid-write never leaves a partial
	// file. Preserve the existing file mode when overwriting.
	if err := m.commitBytes(ctx, op.Path, []byte(op.Content)); err != nil {
		return err
	}

	result.BytesAffected = int64(len(op.Content))

	// Post-write hook (best effort)
	postOp := op
	_ = m.executeHooksForOperation(ctx, HookPostWrite, postOp)

	return nil
}

// executeExtract moves lines [StartLine, EndLine] from Source to Destination
// atomically (point 4). The same computed slice is written to the destination
// and removed from the source, so the bytes written == the bytes deleted by
// construction — closing the drift gap of the old two-step (write dest + delete
// source) workflow. Both writes are atomic (temp + rename); the batch's
// prepareRollback captured both files' prior state so an enclosing atomic batch
// can revert a partial extract.
func (m *BatchOperationManager) executeExtract(ctx context.Context, op FileOperation, result *OperationResult) error {

	if err := m.executeHooksForOperation(ctx, HookPreEdit, op); err != nil {
		return fmt.Errorf("pre-extract hook denied batch extract: %w", err)
	}

	srcBytes, err := os.ReadFile(op.Source)
	if err != nil {
		return err
	}

	removed, remaining, err := ComputeLineRangeDeletion(string(srcBytes), op.StartLine, op.EndLine)
	if err != nil {
		return err
	}

	// Destination content: the SAME removed bytes, optionally appended.
	var destContent []byte
	if op.Append {
		if existing, rerr := os.ReadFile(op.Destination); rerr == nil {
			destContent = append(existing, []byte(removed)...)
		} else if os.IsNotExist(rerr) {
			destContent = []byte(removed)
		} else {
			return rerr
		}
	} else {
		destContent = []byte(removed)
	}

	// Write destination first, then update source. If the source update fails,
	// the enclosing atomic batch rolls back both files.
	if err := m.commitBytes(ctx, op.Destination, destContent); err != nil {
		return fmt.Errorf("extract: writing destination %s: %w", op.Destination, err)
	}

	if err := m.commitBytes(ctx, op.Source, []byte(remaining)); err != nil {
		return fmt.Errorf("extract: updating source %s: %w", op.Source, err)
	}

	if m.engine != nil {
		m.engine.invalidateFileReadCache(op.Source)
		m.engine.invalidateFileReadCache(op.Destination)
	}

	result.BytesAffected = int64(len(removed))
	_ = m.executeHooksForOperation(ctx, HookPostEdit, op)
	return nil
}

func (m *BatchOperationManager) executeEdit(ctx context.Context, op FileOperation, result *OperationResult) error {

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
		editResult, editErr := m.engine.performIntelligentEdit(original, op.OldText, op.NewText, false)
		if editErr != nil || editResult.ReplacementCount == 0 {
			return fmt.Errorf("old_text not found in file: %s. "+
				"ALWAYS read the file with read_file BEFORE editing. "+
				"Copy the exact text from the read result as old_text", op.Path)
		}
		finalContent = editResult.ModifiedContent
	} else {
		// Fallback (with boundary-newline preservation, same as the engine path)
		idx := strings.Index(original, op.OldText)
		if idx < 0 {
			return fmt.Errorf("old_text not found in file: %s. "+
				"ALWAYS read the file with read_file BEFORE editing. "+
				"Copy the exact text from the read result as old_text", op.Path)
		}
		matchEnd := idx + len(op.OldText)
		finalContent = original[:idx] + preserveBoundaryNewline(original, matchEnd, op.OldText, op.NewText) + original[matchEnd:]
	}

	err = m.commitBytes(ctx, op.Path, []byte(finalContent))
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

func (m *BatchOperationManager) executeSearchAndReplace(ctx context.Context, op FileOperation, result *OperationResult) error {

	// Pre-write style hook for search_and_replace (treated as edit/write)
	if err := m.executeHooksForOperation(ctx, HookPreWrite, op); err != nil {
		return fmt.Errorf("pre-write hook denied batch search_and_replace: %w", err)
	}

	if m.engine == nil {
		return fmt.Errorf("search_and_replace requires engine (not available in standalone batch mode)")
	}
	var sizeBefore int64
	if info, statErr := os.Stat(op.Path); statErr == nil {
		sizeBefore = info.Size()
	}
	content, err := os.ReadFile(op.Path)
	if err != nil {
		return err
	}
	replacements := strings.Count(string(content), op.OldText)
	if replacements > 0 {
		if err := m.commitBytes(ctx, op.Path, []byte(strings.ReplaceAll(string(content), op.OldText, op.NewText))); err != nil {
			return err
		}
	}
	if replacements == 0 {
		return fmt.Errorf("pattern '%s' not found in %s", op.OldText, op.Path)
	}
	// Report the byte delta (parity with edit), not the replacement count.
	result.BytesAffected = int64(replacements) * int64(len(op.NewText)-len(op.OldText))
	if info, statErr := os.Stat(op.Path); statErr == nil {
		result.BytesAffected = info.Size() - sizeBefore
	}

	// Post-write hook (best effort)
	_ = m.executeHooksForOperation(ctx, HookPostWrite, op)

	return nil
}

func (m *BatchOperationManager) executeMove(ctx context.Context, op FileOperation, result *OperationResult) error {

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

func (m *BatchOperationManager) executeCopy(ctx context.Context, op FileOperation, result *OperationResult) error {

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

func (m *BatchOperationManager) executeDelete(ctx context.Context, op FileOperation, result *OperationResult) error {

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

func (m *BatchOperationManager) executeCreateDir(ctx context.Context, op FileOperation, result *OperationResult) error {

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
	case "move", "copy", "extract":
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
