package core

import (
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
	Type        string                 `json:"type"`        // write, edit, move, delete, create_dir, copy
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
	backupDir     string
	maxBackups    int
	mutex         sync.Mutex
	currentBackup string
}

// NewBatchOperationManager crea un nuevo manager de operaciones batch
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

	for i, op := range operations {
		switch op.Type {
		case "write":
			if op.Path == "" {
				errors = append(errors, fmt.Sprintf("Op %d: path is required for write", i))
			}
			// Validar que el directorio padre existe o se puede crear
			dir := filepath.Dir(op.Path)
			if _, err := os.Stat(dir); os.IsNotExist(err) {
				errors = append(errors, fmt.Sprintf("Op %d: parent directory does not exist: %s", i, dir))
			}

		case "edit":
			if op.Path == "" {
				errors = append(errors, fmt.Sprintf("Op %d: path is required for edit", i))
			}
			if op.OldText == "" && op.NewText == "" {
				errors = append(errors, fmt.Sprintf("Op %d: old_text or new_text required for edit", i))
			}
			// Validar que el archivo existe
			if _, err := os.Stat(op.Path); os.IsNotExist(err) {
				errors = append(errors, fmt.Sprintf("Op %d: file does not exist: %s", i, op.Path))
			}

		case "move":
			if op.Source == "" || op.Destination == "" {
				errors = append(errors, fmt.Sprintf("Op %d: source and destination required for move", i))
			}
			// Validar que el source existe
			if _, err := os.Stat(op.Source); os.IsNotExist(err) {
				errors = append(errors, fmt.Sprintf("Op %d: source does not exist: %s", i, op.Source))
			}
			// Validar que el destino no existe
			if _, err := os.Stat(op.Destination); err == nil {
				errors = append(errors, fmt.Sprintf("Op %d: destination already exists: %s", i, op.Destination))
			}

		case "copy":
			if op.Source == "" || op.Destination == "" {
				errors = append(errors, fmt.Sprintf("Op %d: source and destination required for copy", i))
			}
			// Validar que el source existe
			if _, err := os.Stat(op.Source); os.IsNotExist(err) {
				errors = append(errors, fmt.Sprintf("Op %d: source does not exist: %s", i, op.Source))
			}

		case "delete":
			if op.Path == "" {
				errors = append(errors, fmt.Sprintf("Op %d: path is required for delete", i))
			}
			// Validar que el archivo existe
			if _, err := os.Stat(op.Path); os.IsNotExist(err) {
				errors = append(errors, fmt.Sprintf("Op %d: file does not exist: %s", i, op.Path))
			}

		case "create_dir":
			if op.Path == "" {
				errors = append(errors, fmt.Sprintf("Op %d: path is required for create_dir", i))
			}
			// Validar que el directorio no existe
			if _, err := os.Stat(op.Path); err == nil {
				errors = append(errors, fmt.Sprintf("Op %d: directory already exists: %s", i, op.Path))
			}

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

	case "edit":
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
		case "write", "edit":
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

// createBackup crea un backup de todos los archivos afectados
func (m *BatchOperationManager) createBackup(operations []FileOperation) (string, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// Crear directorio de backup con timestamp
	timestamp := time.Now().Format("20060102-150405")
	backupPath := filepath.Join(m.backupDir, fmt.Sprintf("batch-%s", timestamp))

	if err := os.MkdirAll(backupPath, 0755); err != nil {
		return "", err
	}

	// Guardar metadatos del batch
	metadata := map[string]interface{}{
		"timestamp":  timestamp,
		"operations": len(operations),
		"created_at": time.Now().Format(time.RFC3339),
	}

	metadataJSON, _ := json.MarshalIndent(metadata, "", "  ")
	os.WriteFile(filepath.Join(backupPath, "metadata.json"), metadataJSON, 0644)

	// Hacer backup de archivos afectados
	for i, op := range operations {
		var sourceFile string

		switch op.Type {
		case "write", "edit", "delete":
			sourceFile = op.Path
		case "move":
			sourceFile = op.Source
		}

		if sourceFile != "" {
			if _, err := os.Stat(sourceFile); err == nil {
				// El archivo existe, hacer backup
				backupFile := filepath.Join(backupPath, fmt.Sprintf("op-%d-%s", i, filepath.Base(sourceFile)))
				if err := copyFile(sourceFile, backupFile); err != nil {
					return "", fmt.Errorf("failed to backup %s: %w", sourceFile, err)
				}
			}
		}
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
	err := os.WriteFile(op.Path, []byte(op.Content), 0644)
	if err == nil {
		result.BytesAffected = int64(len(op.Content))
	}
	return err
}

func (m *BatchOperationManager) executeEdit(op FileOperation, result *OperationResult) error {
	content, err := os.ReadFile(op.Path)
	if err != nil {
		return err
	}

	original := string(content)
	newContent := strings.Replace(original, op.OldText, op.NewText, 1)
	if newContent == original {
		return fmt.Errorf("old_text not found in file: %s", op.Path)
	}

	err = os.WriteFile(op.Path, []byte(newContent), 0644)
	if err == nil {
		result.BytesAffected = int64(len(newContent) - len(original))
	}
	return err
}

func (m *BatchOperationManager) executeMove(op FileOperation, result *OperationResult) error {
	info, err := os.Stat(op.Source)
	if err != nil {
		return err
	}

	err = os.Rename(op.Source, op.Destination)
	if err == nil {
		result.BytesAffected = info.Size()
	}
	return err
}

func (m *BatchOperationManager) executeCopy(op FileOperation, result *OperationResult) error {
	info, err := os.Stat(op.Source)
	if err != nil {
		return err
	}

	err = copyFile(op.Source, op.Destination)
	if err == nil {
		result.BytesAffected = info.Size()
	}
	return err
}

func (m *BatchOperationManager) executeDelete(op FileOperation, result *OperationResult) error {
	info, err := os.Stat(op.Path)
	if err != nil {
		return err
	}

	err = os.Remove(op.Path)
	if err == nil {
		result.BytesAffected = info.Size()
	}
	return err
}

func (m *BatchOperationManager) executeCreateDir(op FileOperation, result *OperationResult) error {
	return os.MkdirAll(op.Path, 0755)
}

// cleanOldBackups limpia backups antiguos manteniendo solo los últimos N
func (m *BatchOperationManager) cleanOldBackups() {
	entries, err := os.ReadDir(m.backupDir)
	if err != nil {
		return
	}

	if len(entries) <= m.maxBackups {
		return
	}

	// Ordenar por fecha de modificación y eliminar los más antiguos
	for i := 0; i < len(entries)-m.maxBackups; i++ {
		path := filepath.Join(m.backupDir, entries[i].Name())
		os.RemoveAll(path)
	}
}
