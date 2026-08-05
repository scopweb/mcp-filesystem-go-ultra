package core

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ChunkingConfig holds configuration for intelligent chunking
type ChunkingConfig struct {
	MaxChunkSize   int  // Max size per chunk in bytes
	OverlapSize    int  // Overlap between chunks for context
	MaxConcurrent  int  // Max concurrent chunk operations
	ProgressReport bool // Whether to report progress
	SmartSplit     bool // Whether to split at logical boundaries
}

// ChunkOperation represents a chunked file operation
type ChunkOperation struct {
	ID           string
	TotalChunks  int
	CurrentChunk int
	Status       string
	StartTime    time.Time
	LastUpdate   time.Time
}

// DefaultChunkingConfig returns optimized defaults for Claude Desktop
func DefaultChunkingConfig() *ChunkingConfig {
	return &ChunkingConfig{
		MaxChunkSize:   64 * 1024, // 64KB chunks - mejor rendimiento
		OverlapSize:    512,       // 512 bytes overlap para mejor contexto
		MaxConcurrent:  4,         // Aumentado para mejor paralelismo
		ProgressReport: false,     // Desactivado por defecto
		SmartSplit:     true,
	}
}

// StreamingWriteFile writes large files in intelligent chunks
// ULTRA-FAST: Uses buffered I/O with pooled buffers for optimal performance
func (e *UltraFastEngine) StreamingWriteFile(ctx context.Context, path, content string) error {
	// Normalize path (handles WSL ↔ Windows conversion)
	path = NormalizePath(path)

	// Acquire semaphore for concurrency control
	if err := e.acquireOperation(ctx, "streaming_write"); err != nil {
		return err
	}
	start := time.Now()
	defer e.releaseOperation("streaming_write", start)

	// Check context before proceeding
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("operation cancelled: %w", err)
	}

	// Check if path is allowed (security + access control)
	if !e.IsPathAllowed(path) {
		return e.AccessDeniedError("streaming_write", path)
	}

	// Quick path for small files - use MediumFileThreshold for cutoff
	if len(content) <= MediumFileThreshold {
		return e.WriteFileContent(ctx, path, content)
	}

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

	// Log only for very large files (>5MB) to reduce overhead
	if totalSize > LargeFileThreshold && !e.config.CompactMode {
		slog.Info("Starting streaming write", "path", path, "size", formatSize(int64(totalSize)), "chunks", totalChunks)
	}

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Execute pre-write hooks (without full content for large files to avoid memory blowup)
	if e.hookManager != nil && e.hookManager.IsEnabled() {
		workingDir, _ := os.Getwd()
		hookCtx := &HookContext{
			Event:      HookPreWrite,
			ToolName:   "streaming_write_file",
			FilePath:   path,
			Operation:  "streaming_write",
			Timestamp:  time.Now(),
			WorkingDir: workingDir,
			Metadata:   map[string]interface{}{"size": totalSize, "is_large": true},
		}
		if _, err := e.hookManager.ExecuteHooks(ctx, HookPreWrite, hookCtx); err != nil {
			return fmt.Errorf("pre-write hook denied streaming write: %w", err)
		}
	}

	// Create temp file for atomic operation with secure random suffix
	tmpPath := path + ".streaming." + secureRandomSuffix()

	// Preserve original file permissions if file exists, otherwise use 0644
	fileMode := os.FileMode(0644)
	if existingInfo, statErr := os.Stat(path); statErr == nil {
		fileMode = existingInfo.Mode()
	}

	// Open file for writing with O_SYNC for data integrity
	file, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, fileMode)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer func() {
		file.Close()
		if _, err := os.Stat(tmpPath); err == nil {
			os.Remove(tmpPath) // Clean up on error
		}
	}()

	// Use buffered writer with pooled buffer for better performance
	// 64KB buffer provides optimal balance between memory and I/O efficiency
	bufPtr := e.bufferPool.Get().(*[]byte)
	defer e.bufferPool.Put(bufPtr)

	writer := bufio.NewWriterSize(file, len(*bufPtr))

	// Write in chunks with optimized I/O
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

		// Write chunk through buffered writer
		n, err := writer.WriteString(chunk)
		if err != nil {
			return fmt.Errorf("failed to write chunk %d: %w", i+1, err)
		}
		written += n
	}

	// Flush buffered data
	if err := writer.Flush(); err != nil {
		return fmt.Errorf("failed to flush buffer: %w", err)
	}

	// Sync to disk
	if err := file.Sync(); err != nil {
		return fmt.Errorf("failed to sync file: %w", err)
	}
	file.Close()

	// Atomic rename
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("failed to finalize file: %w", err)
	}

	// Invalidate cache
	e.invalidateMutatedPath(path)

	operation.Status = "completed"

	// Log only for very large files (>5MB) and if not in compact mode
	if totalSize > LargeFileThreshold && !e.config.CompactMode {
		elapsed := time.Since(start)
		throughput := float64(totalSize) / elapsed.Seconds() / 1024 / 1024
		slog.Info("Streaming write completed", "path", path, "duration", elapsed, "throughput_mbs", throughput)
	}

	// Auto-sync to Windows if enabled (async, non-blocking)
	if e.autoSyncManager != nil {
		_ = e.autoSyncManager.AfterWrite(path)
	}

	// Post-write hook (best effort, no content for large files)
	if e.hookManager != nil && e.hookManager.IsEnabled() {
		workingDir, _ := os.Getwd()
		hookCtx := &HookContext{
			Event:      HookPostWrite,
			ToolName:   "streaming_write_file",
			FilePath:   path,
			Operation:  "streaming_write",
			Timestamp:  time.Now(),
			WorkingDir: workingDir,
			Metadata:   map[string]interface{}{"size": totalSize, "is_large": true},
		}
		_, _ = e.hookManager.ExecuteHooks(ctx, HookPostWrite, hookCtx)
	}

	return nil
}

// ChunkedReadFile reads large files in chunks with optimized I/O
// ULTRA-FAST: Uses pre-allocated buffer and efficient reading
func (e *UltraFastEngine) ChunkedReadFile(ctx context.Context, path string, maxChunkSize int) (string, error) {
	// Normalize path (handles WSL ↔ Windows conversion)
	path = NormalizePath(path)

	// Acquire semaphore for concurrency control
	if err := e.acquireOperation(ctx, "chunked_read"); err != nil {
		return "", err
	}
	start := time.Now()
	defer e.releaseOperation("chunked_read", start)

	// Check context before proceeding
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("operation cancelled: %w", err)
	}

	// Check if path is allowed (access control) - must check before any read path
	if len(e.config.AllowedPaths) > 0 {
		if !e.IsPathAllowed(path) {
			return "", e.AccessDeniedError("chunked_read", path)
		}
	}

	// Get file info
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("file stat error: %w", err)
	}

	fileSize := info.Size()

	// Quick path for small files
	if fileSize <= int64(maxChunkSize) {
		return e.ReadFileContent(ctx, path)
	}

	// Log only for very large files and if not in compact mode
	if fileSize > LargeFileThreshold && !e.config.CompactMode {
		slog.Info("Chunked read started", "path", path, "size", formatSize(fileSize))
	}

	// Open file
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// Use pooled buffer for reading
	bufPtr := e.bufferPool.Get().(*[]byte)
	defer e.bufferPool.Put(bufPtr)
	buffer := *bufPtr

	// Use buffered reader for better I/O performance
	reader := bufio.NewReaderSize(file, len(buffer))

	// Pre-allocate result with exact size for zero-copy
	var result strings.Builder
	result.Grow(int(fileSize))

	// Read through buffered reader into pre-allocated builder
	for {
		n, err := reader.Read(buffer)
		if n > 0 {
			result.Write(buffer[:n])
		}
		if err != nil {
			break
		}
	}

	// Log only for very large files and if not in compact mode
	if fileSize > LargeFileThreshold && !e.config.CompactMode {
		elapsed := time.Since(start)
		throughput := float64(fileSize) / elapsed.Seconds() / 1024 / 1024
		slog.Info("Chunked read completed", "path", path, "duration", elapsed, "throughput_mbs", throughput)
	}

	return result.String(), nil
}

// SmartEditFile intelligently edits files based on size
// WARNING: On Windows with Claude Desktop, changes may not persist to Windows filesystem.
// Use WriteFileContent with complete file content for guaranteed persistence.
// See: guides/WINDOWS_FILESYSTEM_PERSISTENCE.md
func (e *UltraFastEngine) SmartEditFile(ctx context.Context, path, oldText, newText string, force bool, maxFileSize int64) (*EditResult, error) {
	// Normalize path (handles WSL ↔ Windows conversion)
	path = NormalizePath(path)

	// Check context before proceeding
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("operation cancelled: %w", err)
	}

	// Check if path is allowed (access control)
	if len(e.config.AllowedPaths) > 0 {
		if !e.IsPathAllowed(path) {
			return nil, e.AccessDeniedError("smart_edit", path)
		}
	}

	// Get file info first
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("file stat error: %w", err)
	}

	fileSize := info.Size()

	// For very large files, use different strategy
	if fileSize > maxFileSize {
		return e.streamingEditLargeFile(ctx, path, oldText, newText, force)
	}

	// Use regular edit for smaller files (EditFile acquires its own semaphore)
	return e.EditFile(ctx, path, oldText, newText, force, false, false)
}

// streamingEditLargeFile handles editing of very large files in two
// constant-memory passes (perf v4.5.30): the old implementation read the
// whole file (ChunkedReadFile), normalized it (2nd copy), replaced (3rd
// copy) and wrote — peaking at ~3x file size in RAM for 5-50MB files.
//
// Pass 1 streams the file counting occurrences/lines/bytes for risk
// assessment + backup. Pass 2 streams again writing the replaced content
// to a temp file + atomic rename. Peak memory is one 64KB window plus the
// needle length, regardless of file size.
func (e *UltraFastEngine) streamingEditLargeFile(ctx context.Context, path, oldText, newText string, force bool) (*EditResult, error) {
	// Log solo si no estamos en compact mode
	if !e.config.CompactMode {
		slog.Info("Large file edit started", "path", path, "mode", "streaming")
	}

	if oldText == "" {
		return nil, &ValidationError{Field: "old_text", Message: "must not be empty (streaming edit would expand the whole file)"}
	}

	// Normalize search/replace text to LF so CRLF files match (Bug #23).
	// File content is normalized window-by-window during both passes.
	// NOTE: as before, the replaced output is written with LF endings.
	oldText = normalizeLineEndings(oldText)
	newText = normalizeLineEndings(newText)

	hold := len(oldText) - 1
	if hold < 0 {
		hold = 0
	}

	// Pass 1: streaming stats (occurrences, lines, normalized length).
	occurrences := 0
	totalLines := 0
	normalizedLen := 0
	err := e.forEachNormalizedWindow(ctx, path, hold, func(window string, limit int, last bool) (int, error) {
		pos := 0
		for {
			idx := strings.Index(window[pos:], oldText)
			if idx < 0 {
				break
			}
			abs := pos + idx
			if abs >= limit {
				break // match starts in the held tail — counted in the next window
			}
			occurrences++
			pos = abs + len(oldText)
		}
		consumed := limit
		if pos > consumed {
			consumed = pos // a match starting before limit may extend past it
		}
		totalLines += strings.Count(window[:consumed], "\n")
		normalizedLen += consumed
		return consumed, nil
	})
	if err != nil {
		return nil, err
	}

	if occurrences == 0 {
		return &EditResult{
			ReplacementCount: 0,
			MatchConfidence:  "no-match",
			LinesAffected:    0,
		}, nil
	}

	// Risk assessment + backup for large file edits (Bug #16)
	impact := calculateChangeImpactFromStats(totalLines+1, normalizedLen, occurrences, oldText, newText, e.riskThresholds)

	var backupID string
	if e.backupManager != nil && impact.IsRisky {
		backupID, err = e.backupManager.CreateBackupWithContext(path, "streaming_edit",
			fmt.Sprintf("Streaming edit: %d occurrences, %.1f%% change, risk=%s",
				impact.Occurrences, impact.ChangePercentage, impact.RiskLevel))
		if err != nil {
			return nil, fmt.Errorf("could not create backup: %w", err)
		}
	}

	// Bug #22: streaming edit NEVER blocks — backup is already created, data is safe.
	// Risk warning is appended to the result instead.

	// Pass 2: streaming replace → temp file → atomic rename.
	if err := e.executeStreamingReplace(ctx, path, oldText, newText, hold); err != nil {
		return nil, err
	}

	result := &EditResult{
		ReplacementCount: occurrences,
		MatchConfidence:  "high",
		LinesAffected:    strings.Count(oldText, "\n") + 1,
		BackupID:         backupID,
	}

	// Attach risk warning for any risky operation (Bug #22: always warn, never block)
	if impact.IsRisky {
		result.RiskWarning = impact.FormatRiskNotice(backupID, path)
	}

	return result, nil
}

// forEachNormalizedWindow streams the file at path in 64KB reads, converts
// line endings to LF window-by-window, and invokes fn for each window.
//
// Windows OVERLAP: fn receives the window and a limit; only matches
// STARTING before limit belong to this call (matches starting in the last
// `hold` bytes may be incomplete and are deferred to the next window).
// fn returns how many leading bytes it consumed; the unconsumed tail is
// carried into the next window. On the final window (last=true) limit ==
// len(window) and fn must consume everything.
//
// A trailing '\r' at a read boundary is held back raw so a "\r\n" pair
// split across reads is normalized to a single '\n' (not "\n\n").
func (e *UltraFastEngine) forEachNormalizedWindow(ctx context.Context, path string, hold int, fn func(window string, limit int, last bool) (consumed int, err error)) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer f.Close()

	bufPtr := e.bufferPool.Get().(*[]byte)
	defer e.bufferPool.Put(bufPtr)
	buf := *bufPtr

	var carry string // normalized unconsumed tail from the previous window
	var rawCR string // raw "\r" held back for a "\r\n" split across reads

	for {
		if err := ctx.Err(); err != nil {
			return &ContextError{Op: "streaming_edit", Details: "operation cancelled during streaming pass"}
		}

		n, rerr := f.Read(buf)
		if n > 0 {
			last := rerr == io.EOF
			data := carry + rawCR + string(buf[:n])
			rawCR = ""
			if !last && strings.HasSuffix(data, "\r") {
				rawCR = "\r"
				data = data[:len(data)-1]
			}
			data = normalizeLineEndings(data)

			limit := len(data)
			if !last {
				limit = len(data) - hold
				if limit < 0 {
					limit = 0
				}
			}

			consumed, fnErr := fn(data, limit, last)
			if fnErr != nil {
				return fnErr
			}
			if consumed < 0 || consumed > len(data) {
				consumed = len(data) // defensive: never lose bytes
			}
			carry = data[consumed:]
		}

		if rerr == io.EOF {
			// Read may report EOF on a follow-up call with n==0 (the file ended
			// exactly at the previous buffer boundary). Drain any pending carry
			// through the callback as the final window so a held match tail is
			// not silently lost.
			if carry != "" || rawCR != "" {
				data := carry + rawCR
				rawCR = ""
				data = normalizeLineEndings(data)
				consumed, fnErr := fn(data, len(data), true)
				if fnErr != nil {
					return fnErr
				}
				if consumed < 0 || consumed > len(data) {
					consumed = len(data)
				}
				carry = data[consumed:]
			}
			break
		}
		if rerr != nil {
			return fmt.Errorf("error reading file: %w", rerr)
		}
	}

	if carry != "" || rawCR != "" {
		return fmt.Errorf("streaming window callback did not consume the final window (lost %d bytes)", len(carry)+len(rawCR))
	}
	return nil
}

// executeStreamingReplace is pass 2 of the large-file edit: it streams the
// file again, writes the LF-normalized content with all occurrences of
// needle replaced to a temp file, and atomically renames it over path.
// Mirrors StreamingWriteFile's durability contract (hooks, file mode
// preservation, buffered writer, sync, rename, cache invalidation, autosync).
func (e *UltraFastEngine) executeStreamingReplace(ctx context.Context, path, needle, replacement string, hold int) error {
	totalSize := int64(0)
	if info, err := os.Stat(path); err == nil {
		totalSize = info.Size()
	}

	// Pre-write hook (no full content for large files)
	if e.hookManager != nil && e.hookManager.IsEnabled() {
		workingDir, _ := os.Getwd()
		hookCtx := &HookContext{
			Event:      HookPreWrite,
			ToolName:   "streaming_edit",
			FilePath:   path,
			Operation:  "streaming_edit",
			Timestamp:  time.Now(),
			WorkingDir: workingDir,
			Metadata:   map[string]interface{}{"size": totalSize, "is_large": true},
		}
		if _, err := e.hookManager.ExecuteHooks(ctx, HookPreWrite, hookCtx); err != nil {
			return fmt.Errorf("pre-write hook denied streaming edit: %w", err)
		}
	}

	tmpPath := path + ".streaming." + secureRandomSuffix()

	fileMode := os.FileMode(0644)
	if existingInfo, statErr := os.Stat(path); statErr == nil {
		fileMode = existingInfo.Mode()
	}

	file, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, fileMode)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	success := false
	defer func() {
		file.Close()
		if !success {
			os.Remove(tmpPath)
		}
	}()

	writer := bufio.NewWriterSize(file, DefaultBufferSize)

	streamErr := e.forEachNormalizedWindow(ctx, path, hold, func(window string, limit int, last bool) (int, error) {
		pos := 0
		for {
			idx := strings.Index(window[pos:], needle)
			if idx < 0 {
				break
			}
			abs := pos + idx
			if abs >= limit {
				break // deferred to the next window
			}
			if _, err := writer.WriteString(window[pos:abs]); err != nil {
				return 0, fmt.Errorf("failed to write replaced content: %w", err)
			}
			if _, err := writer.WriteString(replacement); err != nil {
				return 0, fmt.Errorf("failed to write replaced content: %w", err)
			}
			pos = abs + len(needle)
		}
		consumed := limit
		if pos > consumed {
			consumed = pos
		}
		if pos < consumed {
			if _, err := writer.WriteString(window[pos:consumed]); err != nil {
				return 0, fmt.Errorf("failed to write replaced content: %w", err)
			}
		}
		return consumed, nil
	})
	if streamErr != nil {
		return streamErr
	}

	if err := writer.Flush(); err != nil {
		return fmt.Errorf("failed to flush buffer: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("failed to sync file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("failed to finalize file: %w", err)
	}
	success = true

	// Invalidate cache
	e.invalidateMutatedPath(path)

	// Auto-sync to Windows if enabled (async, non-blocking)
	if e.autoSyncManager != nil {
		_ = e.autoSyncManager.AfterWrite(path)
	}

	// Post-write hook (best effort, no content for large files)
	if e.hookManager != nil && e.hookManager.IsEnabled() {
		workingDir, _ := os.Getwd()
		hookCtx := &HookContext{
			Event:      HookPostWrite,
			ToolName:   "streaming_edit",
			FilePath:   path,
			Operation:  "streaming_edit",
			Timestamp:  time.Now(),
			WorkingDir: workingDir,
			Metadata:   map[string]interface{}{"size": totalSize, "is_large": true},
		}
		_, _ = e.hookManager.ExecuteHooks(ctx, HookPostWrite, hookCtx)
	}

	return nil
}

// GetFileAnalysis provides intelligent analysis for large files
func (e *UltraFastEngine) GetFileAnalysis(ctx context.Context, path string) (string, error) {
	// Normalize path (handles WSL ↔ Windows conversion)
	path = NormalizePath(path)
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
	if textExtensionsMap[ext] {
		analysis.WriteString("Type: Text file (editable)\n")
	} else if binaryExtensionsMap[ext] {
		analysis.WriteString("Type: Binary file (read-only recommended)\n")
	} else {
		analysis.WriteString("Type: Unknown (treat as text)\n")
	}

	return analysis.String(), nil
}

// binaryExtensionsMap provides O(1) lookup for known binary extensions.
// Used by plan-mode analysis and by isTextCandidate to pre-filter content
// searches during the walk without any I/O. Note: .svg is intentionally
// absent — it is XML text and must stay searchable.
var binaryExtensionsMap = map[string]bool{
	// Executables / libraries
	".exe": true, ".dll": true, ".so": true, ".dylib": true, ".com": true,
	".sys": true, ".drv": true, ".ocx": true, ".cpl": true, ".scr": true,
	".o": true, ".obj": true, ".a": true, ".lib": true, ".pdb": true,
	".class": true, ".pyc": true, ".pyo": true, ".wasm": true,
	// Images
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".bmp": true,
	".ico": true, ".icns": true, ".webp": true, ".tiff": true, ".tif": true,
	".psd": true, ".ai": true, ".eps": true, ".raw": true, ".heic": true,
	// Audio / video
	".mp3": true, ".wav": true, ".flac": true, ".ogg": true, ".m4a": true,
	".aac": true, ".wma": true,
	".mp4": true, ".mkv": true, ".avi": true, ".mov": true, ".wmv": true,
	".webm": true, ".flv": true,
	// Archives / packages
	".zip": true, ".tar": true, ".gz": true, ".tgz": true, ".bz2": true,
	".xz": true, ".7z": true, ".rar": true, ".jar": true, ".war": true,
	".nupkg": true, ".snupkg": true, ".msi": true, ".cab": true,
	".deb": true, ".rpm": true, ".apk": true, ".ipa": true,
	// Documents (binary formats)
	".pdf": true, ".doc": true, ".docx": true, ".xls": true, ".xlsx": true,
	".ppt": true, ".pptx": true, ".odt": true, ".ods": true, ".odp": true,
	// Fonts
	".ttf": true, ".otf": true, ".woff": true, ".woff2": true, ".eot": true,
	// Databases / binary data
	".db": true, ".sqlite": true, ".sqlite3": true, ".mdb": true, ".accdb": true,
	".dat": true, ".bin": true, ".dump": true, ".parquet": true,
	// Disk images / certs / misc
	".iso": true, ".img": true, ".dmg": true, ".vmdk": true,
	".cer": true, ".crt": true, ".pfx": true, ".p12": true,
}
