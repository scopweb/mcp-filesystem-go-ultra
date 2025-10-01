package core

import (
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ChunkingConfig holds configuration for intelligent chunking
type ChunkingConfig struct {
	MaxChunkSize    int    // Max size per chunk in bytes
	OverlapSize     int    // Overlap between chunks for context
	MaxConcurrent   int    // Max concurrent chunk operations
	ProgressReport  bool   // Whether to report progress
	SmartSplit      bool   // Whether to split at logical boundaries
}

// ChunkOperation represents a chunked file operation
type ChunkOperation struct {
	ID          string
	TotalChunks int
	CurrentChunk int
	Status      string
	StartTime   time.Time
	LastUpdate  time.Time
}

// DefaultChunkingConfig returns optimized defaults for Claude Desktop
func DefaultChunkingConfig() *ChunkingConfig {
	return &ChunkingConfig{
		MaxChunkSize:   32 * 1024, // 32KB chunks - optimal for Claude Desktop
		OverlapSize:    256,       // 256 bytes overlap for context
		MaxConcurrent:  3,         // Conservative for stability
		ProgressReport: true,
		SmartSplit:     true,
	}
}

// StreamingWriteFile writes large files in intelligent chunks
func (e *UltraFastEngine) StreamingWriteFile(ctx context.Context, path, content string) error {
	// Quick path for small files
	if len(content) <= 32*1024 {
		return e.WriteFileContent(ctx, path, content)
	}

	start := time.Now()
	config := DefaultChunkingConfig()
	
	// Calculate chunks
	totalSize := len(content)
	totalChunks := int(math.Ceil(float64(totalSize) / float64(config.MaxChunkSize)))
	
	// Create operation tracking
	opID := fmt.Sprintf("stream_%d", time.Now().UnixNano())
	operation := &ChunkOperation{
		ID:          opID,
		TotalChunks: totalChunks,
		Status:      "starting",
		StartTime:   start,
		LastUpdate:  start,
	}

	log.Printf("🚀 Starting streaming write: %s (%s in %d chunks)", path, formatSize(int64(totalSize)), totalChunks)

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %v", err)
	}

	// Create temp file for atomic operation
	tmpPath := path + ".streaming." + opID
	
	// Open file for writing
	file, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %v", err)
	}
	defer func() {
		file.Close()
		if _, err := os.Stat(tmpPath); err == nil {
			os.Remove(tmpPath) // Clean up on error
		}
	}()

	// Write in chunks with progress reporting
	written := 0
	for i := 0; i < totalChunks; i++ {
		operation.CurrentChunk = i + 1
		operation.Status = "writing"
		operation.LastUpdate = time.Now()

		// Calculate chunk boundaries
		startPos := i * config.MaxChunkSize
		end := startPos + config.MaxChunkSize
		if end > totalSize {
			end = totalSize
		}

		chunk := content[startPos:end]
		
		// Write chunk
		n, err := file.WriteString(chunk)
		if err != nil {
			return fmt.Errorf("failed to write chunk %d: %v", i+1, err)
		}
		written += n

		// Progress report every 10 chunks or for large chunks
		if config.ProgressReport && (i%10 == 0 || len(chunk) > 16*1024) {
			progress := float64(written) / float64(totalSize) * 100
			elapsed := time.Since(start)
			log.Printf("📊 Progress: %.1f%% (%d/%d chunks, %s written, %v elapsed)", 
				progress, i+1, totalChunks, formatSize(int64(written)), elapsed)
		}

		// Small delay to prevent overwhelming Claude Desktop
		if i%5 == 0 && i > 0 {
			time.Sleep(10 * time.Millisecond)
		}
	}

	// Sync and close
	if err := file.Sync(); err != nil {
		return fmt.Errorf("failed to sync file: %v", err)
	}
	file.Close()

	// Atomic rename
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("failed to finalize file: %v", err)
	}

	// Invalidate cache
	e.cache.InvalidateFile(path)

	operation.Status = "completed"
	elapsed := time.Since(start)
	speed := float64(totalSize) / elapsed.Seconds()
	
	log.Printf("✅ Streaming write completed: %s (%s in %v, %.1f KB/s)", 
		path, formatSize(int64(totalSize)), elapsed, speed/1024)

	return nil
}

// ChunkedReadFile reads large files in chunks with progress reporting
func (e *UltraFastEngine) ChunkedReadFile(ctx context.Context, path string, maxChunkSize int) (string, error) {
	// Get file info
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("file stat error: %v", err)
	}

	fileSize := info.Size()
	
	// Quick path for small files
	if fileSize <= int64(maxChunkSize) {
		return e.ReadFileContent(ctx, path)
	}

	start := time.Now()
	log.Printf("🚀 Starting chunked read: %s (%s)", path, formatSize(fileSize))

	// Open file
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %v", err)
	}
	defer file.Close()

	// Read in chunks
	var result strings.Builder
	result.Grow(int(fileSize)) // Pre-allocate capacity

	buffer := make([]byte, maxChunkSize)
	totalRead := int64(0)
	chunkNum := 0

	for {
		n, err := file.Read(buffer)
		if n == 0 {
			break
		}
		
		chunkNum++
		result.Write(buffer[:n])
		totalRead += int64(n)

		// Progress report
		if chunkNum%10 == 0 {
			progress := float64(totalRead) / float64(fileSize) * 100
			log.Printf("📊 Read progress: %.1f%% (%s/%s)", 
				progress, formatSize(totalRead), formatSize(fileSize))
		}

		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			return "", fmt.Errorf("read error: %v", err)
		}
	}

	elapsed := time.Since(start)
	speed := float64(totalRead) / elapsed.Seconds()
	
	log.Printf("✅ Chunked read completed: %s (%s in %v, %.1f MB/s)", 
		path, formatSize(totalRead), elapsed, speed/1024/1024)

	return result.String(), nil
}

// SmartEditFile handles large file editing with intelligent chunking
func (e *UltraFastEngine) SmartEditFile(ctx context.Context, path, oldText, newText string, maxFileSize int64) (*EditResult, error) {
	// Get file info first
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("file stat error: %v", err)
	}

	fileSize := info.Size()
	
	// For very large files, use different strategy
	if fileSize > maxFileSize {
		return e.streamingEditLargeFile(ctx, path, oldText, newText)
	}

	// Use regular edit for smaller files
	return e.EditFile(path, oldText, newText)
}

// streamingEditLargeFile handles editing of very large files
func (e *UltraFastEngine) streamingEditLargeFile(ctx context.Context, path, oldText, newText string) (*EditResult, error) {
	log.Printf("🔧 Large file edit: %s (using streaming approach)", path)
	
	// Read in chunks and process
	const chunkSize = 64 * 1024 // 64KB chunks
	content, err := e.ChunkedReadFile(ctx, path, chunkSize)
	if err != nil {
		return nil, err
	}

	// Perform replacement
	replacements := strings.Count(content, oldText)
	if replacements == 0 {
		return &EditResult{
			ReplacementCount: 0,
			MatchConfidence:  "no-match",
			LinesAffected:    0,
		}, nil
	}

	newContent := strings.ReplaceAll(content, oldText, newText)
	linesAffected := strings.Count(oldText, "\n") + 1

	// Write back using streaming
	err = e.StreamingWriteFile(ctx, path, newContent)
	if err != nil {
		return nil, err
	}

	return &EditResult{
		ReplacementCount: replacements,
		MatchConfidence:  "high",
		LinesAffected:    linesAffected,
	}, nil
}

// GetFileAnalysis provides intelligent analysis for large files
func (e *UltraFastEngine) GetFileAnalysis(ctx context.Context, path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}

	fileSize := info.Size()
	
	var analysis strings.Builder
	analysis.WriteString(fmt.Sprintf("📊 File Analysis: %s\n", filepath.Base(path)))
	analysis.WriteString(fmt.Sprintf("Size: %s\n", formatSize(fileSize)))
	
	// Determine optimal strategy
	if fileSize < 32*1024 {
		analysis.WriteString("Strategy: Direct operation (small file)\n")
	} else if fileSize < 1024*1024 {
		analysis.WriteString("Strategy: Standard chunking (medium file)\n")
	} else if fileSize < 10*1024*1024 {
		analysis.WriteString("Strategy: Large file streaming (recommended)\n")
		analysis.WriteString("Recommendation: Use streaming operations for best performance\n")
	} else {
		analysis.WriteString("Strategy: Very large file - use chunked operations only\n")
		analysis.WriteString("⚠️  Warning: This file is very large. Consider chunked operations.\n")
		analysis.WriteString("Recommendation: Read in chunks, edit specific sections, or use search operations\n")
	}

	// File type detection
	ext := strings.ToLower(filepath.Ext(path))
	if isTextFile(ext) {
		analysis.WriteString("Type: Text file (editable)\n")
	} else if isBinaryFile(ext) {
		analysis.WriteString("Type: Binary file (read-only recommended)\n")
	} else {
		analysis.WriteString("Type: Unknown (treat as text)\n")
	}

	return analysis.String(), nil
}

// isTextFile checks if file extension indicates text file
func isTextFile(ext string) bool {
	textExts := []string{
		".txt", ".md", ".json", ".yaml", ".yml", ".xml", ".html", ".htm",
		".js", ".ts", ".jsx", ".tsx", ".css", ".scss", ".sass", ".less",
		".go", ".py", ".java", ".c", ".cpp", ".h", ".hpp", ".cs", ".php",
		".rb", ".rs", ".swift", ".kt", ".scala", ".sh", ".bash", ".ps1",
		".sql", ".r", ".m", ".pl", ".lua", ".vim", ".emacs", ".gitignore",
		".env", ".config", ".ini", ".toml", ".lock", ".log",
	}
	
	for _, textExt := range textExts {
		if ext == textExt {
			return true
		}
	}
	return false
}

// isBinaryFile checks if file extension indicates binary file
func isBinaryFile(ext string) bool {
	binaryExts := []string{
		".exe", ".dll", ".so", ".dylib", ".a", ".lib", ".obj", ".o",
		".jpg", ".jpeg", ".png", ".gif", ".bmp", ".svg", ".ico", ".webp",
		".mp4", ".avi", ".mov", ".wmv", ".flv", ".mkv", ".webm",
		".mp3", ".wav", ".ogg", ".flac", ".aac", ".wma",
		".zip", ".tar", ".gz", ".7z", ".rar", ".bz2", ".xz",
		".pdf", ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx",
		".db", ".sqlite", ".mdb", ".accdb",
	}
	
	for _, binExt := range binaryExts {
		if ext == binExt {
			return true
		}
	}
	return false
}
