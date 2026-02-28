package core

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"time"
)

// ProcessingMode defines how a file should be processed
type ProcessingMode int

const (
	ModeAuto         ProcessingMode = iota // Auto-select based on file size
	ModeLineByLine                         // Process line by line
	ModeChunkByChunk                       // Process in chunks
	ModeFullFile                           // Load entire file in memory
)

// ProcessMetadata provides context during processing
type ProcessMetadata struct {
	LineNumber   int    // Current line number (for line mode)
	ChunkIndex   int    // Current chunk index (for chunk mode)
	TotalLines   int    // Total lines (if known)
	TotalChunks  int    // Total chunks (if known)
	FilePath     string // File being processed
	TotalSize    int    // Total file size in bytes
}

// ProcessorFunc is a function that processes content
type ProcessorFunc func(content string, metadata ProcessMetadata) (string, error)

// ProcessingConfig holds configuration for large file processing
type ProcessingConfig struct {
	InputPath    string        // Source file path
	OutputPath   string        // Destination file path (empty = overwrite)
	Mode         ProcessingMode // Processing mode
	ChunkSize    int           // Size of each chunk in bytes (0 = default)
	ProcessFunc  ProcessorFunc // Function to process content
	CreateBackup bool          // Create backup before processing
	DryRun       bool          // Validate without applying changes
}

// ProcessingResult holds results of file processing
type ProcessingResult struct {
	Success          bool          // Whether processing succeeded
	InputPath        string        // Input file path
	OutputPath       string        // Output file path
	BytesProcessed   int64         // Total bytes processed
	LinesProcessed   int           // Total lines processed
	ChunksProcessed  int           // Total chunks processed
	TransformedLines int           // Lines that were transformed
	Duration         time.Duration // Processing duration
	BackupID         string        // Backup ID if created
	Errors           []string      // Any errors encountered
	Mode             string        // Mode used (in-memory, streaming)
}

// LargeFileProcessor handles processing of large files
// This is a standalone module that doesn't modify UltraFastEngine
type LargeFileProcessor struct {
	engine      *UltraFastEngine // Reference to engine for infrastructure access
	maxMemoryMB int              // Maximum memory to use (MB)
	chunkSizeKB int              // Default chunk size (KB)
}

// NewLargeFileProcessor creates a new large file processor
func NewLargeFileProcessor(engine *UltraFastEngine) *LargeFileProcessor {
	return &LargeFileProcessor{
		engine:      engine,
		maxMemoryMB: 50,  // Conservative memory limit
		chunkSizeKB: 128, // Optimal for 2-10MB files
	}
}

// ProcessFile is the main entry point for file processing
func (p *LargeFileProcessor) ProcessFile(ctx context.Context, config ProcessingConfig) (*ProcessingResult, error) {
	startTime := time.Now()

	// Normalize path
	config.InputPath = NormalizePath(config.InputPath)
	if config.OutputPath != "" {
		config.OutputPath = NormalizePath(config.OutputPath)
	} else {
		config.OutputPath = config.InputPath
	}

	// Validate input file exists
	info, err := os.Stat(config.InputPath)
	if err != nil {
		return nil, &PathError{
			Op:   "process_file",
			Path: config.InputPath,
			Err:  err,
		}
	}

	// Check context before starting
	if err := ctx.Err(); err != nil {
		return nil, &ContextError{
			Op:      "process_file",
			Details: "operation cancelled before start",
		}
	}

	fileSize := info.Size()
	result := &ProcessingResult{
		InputPath:  config.InputPath,
		OutputPath: config.OutputPath,
	}

	// Auto-select mode based on file size
	mode := config.Mode
	if mode == ModeAuto {
		if fileSize <= 10*1024*1024 { // <= 10MB
			mode = ModeFullFile
		} else {
			mode = ModeLineByLine
		}
	}

	// Set default chunk size if not specified
	if config.ChunkSize == 0 {
		config.ChunkSize = p.chunkSizeKB * 1024
	}

	// Process based on selected mode
	var processingErr error
	switch mode {
	case ModeFullFile:
		result.Mode = "in-memory"
		processingErr = p.processInMemory(ctx, config, result)
	case ModeLineByLine:
		result.Mode = "streaming-line"
		processingErr = p.processLineByLine(ctx, config, result)
	case ModeChunkByChunk:
		result.Mode = "streaming-chunk"
		processingErr = p.processChunkByChunk(ctx, config, result)
	default:
		return nil, fmt.Errorf("unsupported processing mode: %v", mode)
	}

	// Set final result fields
	result.Duration = time.Since(startTime)
	result.Success = processingErr == nil
	if processingErr != nil {
		result.Errors = append(result.Errors, processingErr.Error())
	}

	return result, processingErr
}

// processInMemory loads the entire file into memory and processes it
// Optimal for 2-10MB files
func (p *LargeFileProcessor) processInMemory(ctx context.Context, config ProcessingConfig, result *ProcessingResult) error {
	// Create backup if requested
	if config.CreateBackup && !config.DryRun {
		backupID, err := p.engine.backupManager.CreateBackup(config.InputPath, "large_file_processing")
		if err != nil {
			return fmt.Errorf("failed to create backup: %w", err)
		}
		result.BackupID = backupID
	}

	// Read entire file using existing ChunkedReadFile (which handles large files efficiently)
	content, err := p.engine.ChunkedReadFile(ctx, config.InputPath, 0)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	result.BytesProcessed = int64(len(content))

	// Check context after read
	if err := ctx.Err(); err != nil {
		return &ContextError{
			Op:      "process_in_memory",
			Details: "operation cancelled after read",
		}
	}

	// Process content with provided function
	metadata := ProcessMetadata{
		FilePath:  config.InputPath,
		TotalSize: len(content),
	}

	processed, err := config.ProcessFunc(content, metadata)
	if err != nil {
		return fmt.Errorf("processing function failed: %w", err)
	}

	// Check if content actually changed
	if processed != content {
		result.TransformedLines = 1 // Mark as transformed
	}

	// Write result if not dry run
	if !config.DryRun {
		// Use existing StreamingWriteFile for atomic write
		err = p.engine.StreamingWriteFile(ctx, config.OutputPath, processed)
		if err != nil {
			return fmt.Errorf("failed to write file: %w", err)
		}
	}

	return nil
}

// processLineByLine processes the file line by line
// More memory efficient for very large files (>10MB)
func (p *LargeFileProcessor) processLineByLine(ctx context.Context, config ProcessingConfig, result *ProcessingResult) error {
	// Create backup if requested
	if config.CreateBackup && !config.DryRun {
		backupID, err := p.engine.backupManager.CreateBackup(config.InputPath, "large_file_processing")
		if err != nil {
			return fmt.Errorf("failed to create backup: %w", err)
		}
		result.BackupID = backupID
	}

	// Open input file
	inputFile, err := os.Open(config.InputPath)
	if err != nil {
		return fmt.Errorf("failed to open input file: %w", err)
	}
	defer inputFile.Close()

	// Create temp output file
	tempPath := config.OutputPath + ".tmp." + fmt.Sprintf("%d", time.Now().UnixNano())
	outputFile, err := os.Create(tempPath)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer func() {
		outputFile.Close()
		// Clean up temp file if we're exiting with error
		if !result.Success && !config.DryRun {
			os.Remove(tempPath)
		}
	}()

	// Create buffered reader and writer
	scanner := bufio.NewScanner(inputFile)
	// Increase buffer size for very long lines
	buf := make([]byte, 1024*1024)         // 1MB buffer
	scanner.Buffer(buf, 64*1024*1024) // Max 64MB for very long lines

	writer := bufio.NewWriter(outputFile)
	defer writer.Flush()

	lineNumber := 0
	for scanner.Scan() {
		lineNumber++

		// Check context periodically
		if lineNumber%1000 == 0 {
			if err := ctx.Err(); err != nil {
				return &ContextError{
					Op:      "process_line_by_line",
					Details: fmt.Sprintf("cancelled at line %d", lineNumber),
				}
			}
		}

		line := scanner.Text()
		result.BytesProcessed += int64(len(line)) + 1 // +1 for newline

		// Process line
		metadata := ProcessMetadata{
			LineNumber: lineNumber,
			FilePath:   config.InputPath,
		}

		processedLine, err := config.ProcessFunc(line, metadata)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: %v", lineNumber, err))
			// Continue with original line on error
			processedLine = line
		}

		// Track if line was transformed
		if processedLine != line {
			result.TransformedLines++
		}

		// Write processed line
		if !config.DryRun {
			if lineNumber > 1 {
				writer.WriteString("\n")
			}
			writer.WriteString(processedLine)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading file: %w", err)
	}

	result.LinesProcessed = lineNumber

	// Flush and close files before rename
	if !config.DryRun {
		if err := writer.Flush(); err != nil {
			return fmt.Errorf("failed to flush writer: %w", err)
		}

		// Explicitly close both files before rename (Windows requires this)
		if err := outputFile.Close(); err != nil {
			return fmt.Errorf("failed to close output file: %w", err)
		}
		if err := inputFile.Close(); err != nil {
			return fmt.Errorf("failed to close input file: %w", err)
		}

		// Atomic rename from temp to final
		if err := os.Rename(tempPath, config.OutputPath); err != nil {
			return fmt.Errorf("failed to rename temp file: %w", err)
		}

		// Invalidate cache
		p.engine.cache.InvalidateFile(config.OutputPath)
	}

	return nil
}

// processChunkByChunk processes the file in chunks
// Good balance between memory and performance
func (p *LargeFileProcessor) processChunkByChunk(ctx context.Context, config ProcessingConfig, result *ProcessingResult) error {
	// Create backup if requested
	if config.CreateBackup && !config.DryRun {
		backupID, err := p.engine.backupManager.CreateBackup(config.InputPath, "large_file_processing")
		if err != nil {
			return fmt.Errorf("failed to create backup: %w", err)
		}
		result.BackupID = backupID
	}

	// Open input file
	inputFile, err := os.Open(config.InputPath)
	if err != nil {
		return fmt.Errorf("failed to open input file: %w", err)
	}
	defer inputFile.Close()

	// Get file size for progress
	fileInfo, _ := inputFile.Stat()
	totalSize := fileInfo.Size()
	totalChunks := int((totalSize + int64(config.ChunkSize) - 1) / int64(config.ChunkSize))

	// Create temp output file
	tempPath := config.OutputPath + ".tmp." + fmt.Sprintf("%d", time.Now().UnixNano())
	outputFile, err := os.Create(tempPath)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer func() {
		outputFile.Close()
		if !result.Success && !config.DryRun {
			os.Remove(tempPath)
		}
	}()

	// Get buffer from pool
	bufPtr := p.engine.bufferPool.Get().(*[]byte)
	defer p.engine.bufferPool.Put(bufPtr)
	buffer := *bufPtr

	writer := bufio.NewWriter(outputFile)
	defer writer.Flush()

	chunkIndex := 0
	for {
		// Check context
		if err := ctx.Err(); err != nil {
			return &ContextError{
				Op:      "process_chunk_by_chunk",
				Details: fmt.Sprintf("cancelled at chunk %d/%d", chunkIndex, totalChunks),
			}
		}

		// Read chunk
		n, err := inputFile.Read(buffer)
		if n > 0 {
			chunk := string(buffer[:n])
			result.BytesProcessed += int64(n)

			// Process chunk
			metadata := ProcessMetadata{
				ChunkIndex:  chunkIndex,
				TotalChunks: totalChunks,
				FilePath:    config.InputPath,
			}

			processedChunk, procErr := config.ProcessFunc(chunk, metadata)
			if procErr != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("chunk %d: %v", chunkIndex, procErr))
				processedChunk = chunk // Use original on error
			}

			// Track if chunk was transformed
			if processedChunk != chunk {
				result.TransformedLines++
			}

			// Write processed chunk
			if !config.DryRun {
				writer.WriteString(processedChunk)
			}

			chunkIndex++
		}

		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			return fmt.Errorf("error reading chunk: %w", err)
		}
	}

	result.ChunksProcessed = chunkIndex

	// Finalize
	if !config.DryRun {
		if err := writer.Flush(); err != nil {
			return fmt.Errorf("failed to flush writer: %w", err)
		}

		// Explicitly close both files before rename (Windows requires this)
		if err := outputFile.Close(); err != nil {
			return fmt.Errorf("failed to close output file: %w", err)
		}
		if err := inputFile.Close(); err != nil {
			return fmt.Errorf("failed to close input file: %w", err)
		}

		// Atomic rename
		if err := os.Rename(tempPath, config.OutputPath); err != nil {
			return fmt.Errorf("failed to rename temp file: %w", err)
		}

		// Invalidate cache
		p.engine.cache.InvalidateFile(config.OutputPath)
	}

	return nil
}
