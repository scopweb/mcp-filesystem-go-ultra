package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// BackupMetadata contiene información sobre un backup individual de archivo
type BackupMetadata struct {
	OriginalPath string    `json:"original_path"`
	BackupPath   string    `json:"backup_path"`
	Size         int64     `json:"size"`
	Hash         string    `json:"hash"`
	ModifiedTime time.Time `json:"modified_time"`
}

// BackupInfo contiene información sobre un backup completo
type BackupInfo struct {
	BackupID    string           `json:"backup_id"`
	Timestamp   time.Time        `json:"timestamp"`
	Operation   string           `json:"operation"`
	UserContext string           `json:"user_context"`
	Files       []BackupMetadata `json:"files"`
	TotalSize   int64            `json:"total_size"`
}

// BackupManager gestiona todos los backups del sistema
type BackupManager struct {
	backupDir     string
	maxBackups    int
	maxAgeDays    int
	mutex         sync.RWMutex
	metadataCache map[string]*BackupInfo
	cacheLastScan time.Time
}

// NewBackupManager crea un nuevo BackupManager
func NewBackupManager(backupDir string, maxBackups int, maxAgeDays int) (*BackupManager, error) {
	if backupDir == "" {
		// Default location accessible by MCP
		backupDir = filepath.Join(os.TempDir(), "mcp-batch-backups")
	}

	// Crear directorio si no existe
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create backup directory: %w", err)
	}

	bm := &BackupManager{
		backupDir:     backupDir,
		maxBackups:    maxBackups,
		maxAgeDays:    maxAgeDays,
		metadataCache: make(map[string]*BackupInfo),
	}

	// Cargar cache inicial
	if err := bm.refreshCache(); err != nil {
		// No es crítico si falla el cache
		fmt.Printf("Warning: failed to refresh backup cache: %v\n", err)
	}

	return bm, nil
}

// CreateBackup crea un backup de un archivo con metadata completa
func (bm *BackupManager) CreateBackup(path string, operation string) (string, error) {
	return bm.CreateBackupWithContext(path, operation, "")
}

// CreateBackupWithContext crea un backup con contexto adicional
func (bm *BackupManager) CreateBackupWithContext(path string, operation string, userContext string) (string, error) {
	bm.mutex.Lock()
	defer bm.mutex.Unlock()

	// Verificar que el archivo existe
	fileInfo, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("source file not found: %w", err)
	}

	// Generar ID único para este backup
	backupID := generateBackupID()
	backupBaseDir := filepath.Join(bm.backupDir, backupID)

	// Crear directorio del backup
	backupFilesDir := filepath.Join(backupBaseDir, "files")
	if err := os.MkdirAll(backupFilesDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create backup directory: %w", err)
	}

	// Calcular ruta relativa para mantener estructura
	fileName := filepath.Base(path)
	backupFilePath := filepath.Join(backupFilesDir, fileName)

	// Copiar archivo y calcular hash
	hash, err := copyFileWithHash(path, backupFilePath)
	if err != nil {
		os.RemoveAll(backupBaseDir) // Limpiar en caso de error
		return "", fmt.Errorf("failed to copy file: %w", err)
	}

	// Crear metadata
	metadata := BackupMetadata{
		OriginalPath: path,
		BackupPath:   filepath.Join("files", fileName),
		Size:         fileInfo.Size(),
		Hash:         hash,
		ModifiedTime: fileInfo.ModTime(),
	}

	backupInfo := BackupInfo{
		BackupID:    backupID,
		Timestamp:   time.Now(),
		Operation:   operation,
		UserContext: userContext,
		Files:       []BackupMetadata{metadata},
		TotalSize:   fileInfo.Size(),
	}

	// Guardar metadata
	if err := bm.saveBackupMetadata(backupBaseDir, &backupInfo); err != nil {
		os.RemoveAll(backupBaseDir)
		return "", fmt.Errorf("failed to save metadata: %w", err)
	}

	// Actualizar cache
	bm.metadataCache[backupID] = &backupInfo

	// Limpiar backups antiguos si es necesario
	go bm.cleanupIfNeeded()

	return backupID, nil
}

// CreateBatchBackup crea un backup de múltiples archivos
func (bm *BackupManager) CreateBatchBackup(paths []string, operation string, userContext string) (string, error) {
	bm.mutex.Lock()
	defer bm.mutex.Unlock()

	if len(paths) == 0 {
		return "", fmt.Errorf("no files to backup")
	}

	// Generar ID único
	backupID := generateBackupID()
	backupBaseDir := filepath.Join(bm.backupDir, backupID)
	backupFilesDir := filepath.Join(backupBaseDir, "files")

	if err := os.MkdirAll(backupFilesDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create backup directory: %w", err)
	}

	var files []BackupMetadata
	var totalSize int64

	// Copiar cada archivo
	for _, path := range paths {
		fileInfo, err := os.Stat(path)
		if err != nil {
			fmt.Printf("Warning: skipping file %s: %v\n", path, err)
			continue
		}

		fileName := filepath.Base(path)
		backupFilePath := filepath.Join(backupFilesDir, fileName)

		hash, err := copyFileWithHash(path, backupFilePath)
		if err != nil {
			fmt.Printf("Warning: failed to backup %s: %v\n", path, err)
			continue
		}

		files = append(files, BackupMetadata{
			OriginalPath: path,
			BackupPath:   filepath.Join("files", fileName),
			Size:         fileInfo.Size(),
			Hash:         hash,
			ModifiedTime: fileInfo.ModTime(),
		})

		totalSize += fileInfo.Size()
	}

	if len(files) == 0 {
		os.RemoveAll(backupBaseDir)
		return "", fmt.Errorf("no files were backed up")
	}

	// Crear metadata
	backupInfo := BackupInfo{
		BackupID:    backupID,
		Timestamp:   time.Now(),
		Operation:   operation,
		UserContext: userContext,
		Files:       files,
		TotalSize:   totalSize,
	}

	if err := bm.saveBackupMetadata(backupBaseDir, &backupInfo); err != nil {
		os.RemoveAll(backupBaseDir)
		return "", fmt.Errorf("failed to save metadata: %w", err)
	}

	bm.metadataCache[backupID] = &backupInfo

	go bm.cleanupIfNeeded()

	return backupID, nil
}

// ListBackups lista backups con filtros opcionales
func (bm *BackupManager) ListBackups(limit int, filterOperation string, filterPath string, newerThanHours int) ([]BackupInfo, error) {
	bm.mutex.RLock()
	defer bm.mutex.RUnlock()

	// Refrescar cache si es necesario
	if time.Since(bm.cacheLastScan) > 5*time.Minute {
		bm.mutex.RUnlock()
		bm.mutex.Lock()
		bm.refreshCache()
		bm.mutex.Unlock()
		bm.mutex.RLock()
	}

	var results []BackupInfo
	cutoffTime := time.Now().Add(-time.Duration(newerThanHours) * time.Hour)

	for _, info := range bm.metadataCache {
		// Aplicar filtros
		if filterOperation != "" && filterOperation != "all" && info.Operation != filterOperation {
			continue
		}

		if newerThanHours > 0 && info.Timestamp.Before(cutoffTime) {
			continue
		}

		if filterPath != "" {
			matchFound := false
			for _, file := range info.Files {
				if strings.Contains(strings.ToLower(file.OriginalPath), strings.ToLower(filterPath)) {
					matchFound = true
					break
				}
			}
			if !matchFound {
				continue
			}
		}

		results = append(results, *info)
	}

	// Ordenar por timestamp descendente (más recientes primero)
	sort.Slice(results, func(i, j int) bool {
		return results[i].Timestamp.After(results[j].Timestamp)
	})

	// Aplicar límite
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

// GetBackupInfo obtiene información de un backup específico
func (bm *BackupManager) GetBackupInfo(backupID string) (*BackupInfo, error) {
	bm.mutex.RLock()
	defer bm.mutex.RUnlock()

	// Buscar en cache
	if info, exists := bm.metadataCache[backupID]; exists {
		return info, nil
	}

	// Cargar desde disco si no está en cache
	backupDir := filepath.Join(bm.backupDir, backupID)
	info, err := bm.loadBackupMetadata(backupDir)
	if err != nil {
		return nil, fmt.Errorf("backup not found: %w", err)
	}

	return info, nil
}

// RestoreBackup restaura archivos desde un backup
func (bm *BackupManager) RestoreBackup(backupID string, specificFile string, createBackup bool) ([]string, error) {
	// Obtener información del backup
	info, err := bm.GetBackupInfo(backupID)
	if err != nil {
		return nil, err
	}

	backupBaseDir := filepath.Join(bm.backupDir, backupID)
	var restoredFiles []string

	// Si se especificó un archivo, restaurar solo ese
	if specificFile != "" {
		for _, file := range info.Files {
			if file.OriginalPath == specificFile {
				if createBackup {
					// Crear backup del estado actual antes de restaurar
					_, err := bm.CreateBackup(file.OriginalPath, "pre_restore")
					if err != nil {
						return nil, fmt.Errorf("failed to create pre-restore backup: %w", err)
					}
				}

				backupFilePath := filepath.Join(backupBaseDir, file.BackupPath)
				if err := copyFile(backupFilePath, file.OriginalPath); err != nil {
					return nil, fmt.Errorf("failed to restore %s: %w", file.OriginalPath, err)
				}
				restoredFiles = append(restoredFiles, file.OriginalPath)
				break
			}
		}

		if len(restoredFiles) == 0 {
			return nil, fmt.Errorf("file %s not found in backup", specificFile)
		}
	} else {
		// Restaurar todos los archivos
		for _, file := range info.Files {
			if createBackup {
				// Solo crear backup si el archivo existe actualmente
				if _, err := os.Stat(file.OriginalPath); err == nil {
					bm.CreateBackup(file.OriginalPath, "pre_restore")
				}
			}

			backupFilePath := filepath.Join(backupBaseDir, file.BackupPath)

			// Asegurar que el directorio destino existe
			destDir := filepath.Dir(file.OriginalPath)
			if err := os.MkdirAll(destDir, 0755); err != nil {
				fmt.Printf("Warning: failed to create directory for %s: %v\n", file.OriginalPath, err)
				continue
			}

			if err := copyFile(backupFilePath, file.OriginalPath); err != nil {
				fmt.Printf("Warning: failed to restore %s: %v\n", file.OriginalPath, err)
				continue
			}
			restoredFiles = append(restoredFiles, file.OriginalPath)
		}
	}

	return restoredFiles, nil
}

// CompareWithBackup compara un archivo actual con su versión en el backup
func (bm *BackupManager) CompareWithBackup(backupID string, filePath string) (string, error) {
	info, err := bm.GetBackupInfo(backupID)
	if err != nil {
		return "", err
	}

	backupBaseDir := filepath.Join(bm.backupDir, backupID)

	// Buscar el archivo en el backup
	var backupFile *BackupMetadata
	for i := range info.Files {
		if info.Files[i].OriginalPath == filePath {
			backupFile = &info.Files[i]
			break
		}
	}

	if backupFile == nil {
		return "", fmt.Errorf("file %s not found in backup", filePath)
	}

	// Leer archivo del backup
	backupContent, err := os.ReadFile(filepath.Join(backupBaseDir, backupFile.BackupPath))
	if err != nil {
		return "", fmt.Errorf("failed to read backup file: %w", err)
	}

	// Leer archivo actual (si existe)
	var currentContent []byte
	if _, err := os.Stat(filePath); err == nil {
		currentContent, err = os.ReadFile(filePath)
		if err != nil {
			return "", fmt.Errorf("failed to read current file: %w", err)
		}
	}

	// Generar diff simple
	diff := generateSimpleDiff(string(backupContent), string(currentContent), backupFile.OriginalPath)

	return diff, nil
}

// CleanupOldBackups elimina backups antiguos según maxAgeDays
func (bm *BackupManager) CleanupOldBackups(olderThanDays int, dryRun bool) (int, int64, error) {
	bm.mutex.Lock()
	defer bm.mutex.Unlock()

	cutoffTime := time.Now().AddDate(0, 0, -olderThanDays)
	deletedCount := 0
	var freedSpace int64

	entries, err := os.ReadDir(bm.backupDir)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to read backup directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		backupID := entry.Name()
		backupPath := filepath.Join(bm.backupDir, backupID)

		// Cargar metadata
		info, err := bm.loadBackupMetadata(backupPath)
		if err != nil {
			continue
		}

		// Verificar antigüedad
		if info.Timestamp.Before(cutoffTime) {
			freedSpace += info.TotalSize

			if !dryRun {
				if err := os.RemoveAll(backupPath); err != nil {
					fmt.Printf("Warning: failed to delete backup %s: %v\n", backupID, err)
					continue
				}
				delete(bm.metadataCache, backupID)
			}

			deletedCount++
		}
	}

	return deletedCount, freedSpace, nil
}

// GetBackupPath retorna la ruta completa de un backup
func (bm *BackupManager) GetBackupPath(backupID string) string {
	return filepath.Join(bm.backupDir, backupID)
}

// saveBackupMetadata guarda la metadata de un backup
func (bm *BackupManager) saveBackupMetadata(backupDir string, info *BackupInfo) error {
	metadataPath := filepath.Join(backupDir, "metadata.json")
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(metadataPath, data, 0644)
}

// loadBackupMetadata carga la metadata de un backup
func (bm *BackupManager) loadBackupMetadata(backupDir string) (*BackupInfo, error) {
	metadataPath := filepath.Join(backupDir, "metadata.json")
	data, err := os.ReadFile(metadataPath)
	if err != nil {
		return nil, err
	}

	var info BackupInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, err
	}

	return &info, nil
}

// refreshCache recarga el cache de metadata
func (bm *BackupManager) refreshCache() error {
	entries, err := os.ReadDir(bm.backupDir)
	if err != nil {
		return err
	}

	newCache := make(map[string]*BackupInfo)

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		backupID := entry.Name()
		backupPath := filepath.Join(bm.backupDir, backupID)

		info, err := bm.loadBackupMetadata(backupPath)
		if err != nil {
			continue
		}

		newCache[backupID] = info
	}

	bm.metadataCache = newCache
	bm.cacheLastScan = time.Now()

	return nil
}

// cleanupIfNeeded limpia backups si se exceden los límites
func (bm *BackupManager) cleanupIfNeeded() {
	bm.mutex.Lock()
	defer bm.mutex.Unlock()

	// Contar backups
	if len(bm.metadataCache) <= bm.maxBackups {
		return
	}

	// Eliminar los más antiguos
	var backups []BackupInfo
	for _, info := range bm.metadataCache {
		backups = append(backups, *info)
	}

	sort.Slice(backups, func(i, j int) bool {
		return backups[i].Timestamp.Before(backups[j].Timestamp)
	})

	toDelete := len(backups) - bm.maxBackups
	for i := 0; i < toDelete; i++ {
		backupPath := filepath.Join(bm.backupDir, backups[i].BackupID)
		os.RemoveAll(backupPath)
		delete(bm.metadataCache, backups[i].BackupID)
	}
}

// Funciones auxiliares

func generateBackupID() string {
	timestamp := time.Now().Format("20060102-150405")
	random := fmt.Sprintf("%x", time.Now().UnixNano()%0xFFFFFF)
	return fmt.Sprintf("%s-%s", timestamp, random)
}

func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	return err
}

func copyFileWithHash(src, dst string) (string, error) {
	sourceFile, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return "", err
	}
	defer destFile.Close()

	hash := sha256.New()
	writer := io.MultiWriter(destFile, hash)

	if _, err := io.Copy(writer, sourceFile); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

func generateSimpleDiff(backup, current, filename string) string {
	if current == "" {
		return fmt.Sprintf("File %s does not exist currently (deleted or moved)", filename)
	}

	if backup == current {
		return "No changes detected - files are identical"
	}

	backupLines := strings.Split(backup, "\n")
	currentLines := strings.Split(current, "\n")

	var diff strings.Builder
	diff.WriteString(fmt.Sprintf("=== Comparison for %s ===\n", filename))
	diff.WriteString(fmt.Sprintf("Backup lines: %d\n", len(backupLines)))
	diff.WriteString(fmt.Sprintf("Current lines: %d\n", len(currentLines)))
	diff.WriteString(fmt.Sprintf("Difference: %+d lines\n\n", len(currentLines)-len(backupLines)))

	// Mostrar primeras líneas diferentes
	diff.WriteString("First differences:\n")
	maxLines := 20
	for i := 0; i < maxLines && (i < len(backupLines) || i < len(currentLines)); i++ {
		backupLine := ""
		if i < len(backupLines) {
			backupLine = backupLines[i]
		}
		currentLine := ""
		if i < len(currentLines) {
			currentLine = currentLines[i]
		}

		if backupLine != currentLine {
			diff.WriteString(fmt.Sprintf("Line %d:\n", i+1))
			diff.WriteString(fmt.Sprintf("  - BACKUP:  %s\n", backupLine))
			diff.WriteString(fmt.Sprintf("  + CURRENT: %s\n", currentLine))
		}
	}

	return diff.String()
}

func FormatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func FormatAge(t time.Time) string {
	duration := time.Since(t)

	if duration < time.Minute {
		return "just now"
	}
	if duration < time.Hour {
		minutes := int(duration.Minutes())
		if minutes == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", minutes)
	}
	if duration < 24*time.Hour {
		hours := int(duration.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	}

	days := int(duration.Hours() / 24)
	if days == 1 {
		return "1 day ago"
	}
	return fmt.Sprintf("%d days ago", days)
}
