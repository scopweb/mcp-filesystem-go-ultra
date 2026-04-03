package core

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ClaudeDesktopOptimizer provides intelligent optimizations for Claude Desktop usage
type ClaudeDesktopOptimizer struct {
	engine *UltraFastEngine
	config *OptimizationConfig
}

// OptimizationConfig holds optimization settings
type OptimizationConfig struct {
	MaxDirectFileSize  int64         // Max size for direct operations
	ChunkSize          int           // Default chunk size
	MaxResponseTime    time.Duration // Max time per operation
	AutoDetectFileType bool          // Auto-detect text vs binary
	ProgressReporting  bool          // Report progress for long operations
	SmartErrorRecovery bool          // Attempt error recovery
}

// BatchOperation represents a single operation in a batch
type BatchOperation struct {
	Type    string // "read", "write", "edit"
	Path    string
	Content string // for write operations
	OldText string // for edit operations
	NewText string // for edit operations
}

// NewClaudeDesktopOptimizer creates a new optimizer
func NewClaudeDesktopOptimizer(engine *UltraFastEngine) *ClaudeDesktopOptimizer {
	return &ClaudeDesktopOptimizer{
		engine: engine,
		config: &OptimizationConfig{
			MaxDirectFileSize:  200 * 1024,       // 200KB (aumentado para mejor rendimiento)
			ChunkSize:          64 * 1024,        // 64KB chunks (aumentado)
			MaxResponseTime:    30 * time.Second, // 30s max
			AutoDetectFileType: true,
			ProgressReporting:  false, // Desactivado por defecto para reducir overhead
			SmartErrorRecovery: true,
		},
	}
}

// IntelligentWrite automatically chooses the best write strategy
func (o *ClaudeDesktopOptimizer) IntelligentWrite(ctx context.Context, path, content string) error {
	// Normalize path first (handles WSL ↔ Windows conversion)
	path = NormalizePath(path)

	// Bug #19: Detect potential file truncation — warn when overwriting a file
	// with significantly less content. This catches LLMs that rewrite entire files
	// after only reading a partial range, potentially losing data.
	if info, err := os.Stat(path); err == nil {
		existingSize := info.Size()
		newSize := int64(len(content))
		// If existing file is >1KB and new content is <50% of existing size, warn
		if existingSize > 1024 && newSize > 0 && newSize < existingSize/2 {
			AppendSubOp(ctx, "truncation_blocked")
			// Create backup before potentially destructive overwrite
			if o.engine.backupManager != nil {
				backupID, backupErr := o.engine.backupManager.CreateBackup(path, "truncation_protection")
				if backupErr == nil {
					return fmt.Errorf("WRITE BLOCKED — new content (%d bytes) is less than 50%% of existing file (%d bytes). "+
						"This looks like accidental file truncation. Use mcp_edit for partial changes, or read the FULL file first. "+
						"Backup created: %s. To force overwrite, use write_base64 or delete_file first then mcp_write",
						newSize, existingSize, backupID)
				}
			}
			return fmt.Errorf("WRITE BLOCKED — new content (%d bytes) is less than 50%% of existing file (%d bytes). "+
				"This looks like accidental file truncation. Use mcp_edit for partial changes, or read the FULL file first",
				newSize, existingSize)
		}
	}

	size := int64(len(content))

	// Auto-select strategy sin logging excesivo
	if size <= o.config.MaxDirectFileSize {
		AppendSubOp(ctx, "direct_write")
		return o.engine.WriteFileContent(ctx, path, content)
	} else {
		AppendSubOp(ctx, "streaming_write")
		return o.engine.StreamingWriteFile(ctx, path, content)
	}
}

// IntelligentRead automatically chooses the best read strategy
func (o *ClaudeDesktopOptimizer) IntelligentRead(ctx context.Context, path string) (string, error) {
	// Normalize path first (handles WSL ↔ Windows conversion)
	path = NormalizePath(path)

	// Check file size first
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}

	size := info.Size()
	// Log solo si debug mode y archivo grande
	if size > 5*1024*1024 && !o.engine.config.CompactMode {
		slog.Info("Intelligent read", "path", path, "size", formatSize(size))
	}

	// Auto-select strategy
	if size <= o.config.MaxDirectFileSize {
		AppendSubOp(ctx, "direct_read")
		return o.engine.ReadFileContent(ctx, path)
	} else {
		AppendSubOp(ctx, "chunked_read")
		return o.engine.ChunkedReadFile(ctx, path, o.config.ChunkSize)
	}
}

// IntelligentEdit automatically chooses the best edit strategy
// Note: On Windows with Claude Desktop, changes may not persist.
// For guaranteed persistence, use IntelligentWrite with complete file content instead.
// See: guides/WINDOWS_FILESYSTEM_PERSISTENCE.md
func (o *ClaudeDesktopOptimizer) IntelligentEdit(ctx context.Context, path, oldText, newText string, force bool) (*EditResult, error) {
	// Normalize path first (handles WSL ↔ Windows conversion)
	path = NormalizePath(path)

	// Analyze file first
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	size := info.Size()
	// Log solo si debug mode y archivo grande
	if size > 5*1024*1024 && !o.engine.config.CompactMode {
		slog.Info("Intelligent edit", "path", path, "size", formatSize(size))
	}

	// Auto-select strategy
	if size <= o.config.MaxDirectFileSize {
		AppendSubOp(ctx, "direct_edit")
		return o.engine.EditFile(ctx, path, oldText, newText, force, false)
	} else {
		AppendSubOp(ctx, "smart_edit_large")
		return o.engine.SmartEditFile(ctx, path, oldText, newText, force, o.config.MaxDirectFileSize)
	}
}

// GetOptimizationSuggestion provides suggestions for Claude Desktop usage
func (o *ClaudeDesktopOptimizer) GetOptimizationSuggestion(ctx context.Context, path string) (string, error) {
	// Normalize path first (handles WSL ↔ Windows conversion)
	path = NormalizePath(path)

	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}

	size := info.Size()
	ext := strings.ToLower(filepath.Ext(path))

	// Compact mode: minimal suggestion
	if o.engine.config.CompactMode {
		var strategy, warning string
		if size < 50*1024 {
			strategy = "direct"
		} else if size < 500*1024 {
			strategy = "intelligent"
		} else if size < 5*1024*1024 {
			strategy = "streaming"
			warning = " (use streaming ops)"
		} else {
			strategy = "chunked"
			warning = " (very large)"
		}
		return fmt.Sprintf("%s: %s, strategy:%s%s", filepath.Base(path), formatSize(size), strategy, warning), nil
	}

	// Verbose mode: detailed suggestion
	var suggestion strings.Builder
	suggestion.WriteString(fmt.Sprintf("🧠 Claude Desktop Optimization Suggestion for: %s\n", filepath.Base(path)))
	suggestion.WriteString(fmt.Sprintf("File size: %s\n\n", formatSize(size)))

	// Size-based recommendations
	if size < 10*1024 {
		suggestion.WriteString("✅ OPTIMAL: Use any operation directly\n")
		suggestion.WriteString("• All operations will be fast\n")
		suggestion.WriteString("• No special handling needed\n")
	} else if size < 50*1024 {
		suggestion.WriteString("✅ GOOD: Direct operations recommended\n")
		suggestion.WriteString("• Use regular read_file, write_file, edit_file\n")
		suggestion.WriteString("• Response time: <2 seconds\n")
	} else if size < 500*1024 {
		suggestion.WriteString("⚠️ LARGE: Use intelligent operations\n")
		suggestion.WriteString("• Recommended: intelligent_write, intelligent_read, intelligent_edit\n")
		suggestion.WriteString("• Alternative: chunked_read_file, streaming_write_file\n")
		suggestion.WriteString("• Response time: 2-10 seconds with progress\n")
	} else if size < 5*1024*1024 {
		suggestion.WriteString("🚨 VERY LARGE: Use streaming operations only\n")
		suggestion.WriteString("• MUST use: streaming_write_file, chunked_read_file, smart_edit_file\n")
		suggestion.WriteString("• DO NOT use: regular read_file or write_file\n")
		suggestion.WriteString("• Response time: 10-60 seconds with progress\n")
	} else {
		suggestion.WriteString("🚫 EXTREMELY LARGE: Consider alternative approach\n")
		suggestion.WriteString("• File too large for direct editing\n")
		suggestion.WriteString("• Recommended: Use search operations to find specific sections\n")
		suggestion.WriteString("• Consider: Breaking into smaller files\n")
	}

	// File type recommendations
	suggestion.WriteString("\n📄 File Type Analysis:\n")
	if textExtensionsMap[ext] {
		suggestion.WriteString("• Type: Text file (fully editable)\n")
		suggestion.WriteString("• All operations supported\n")
	} else if binaryExtensionsMap[ext] {
		suggestion.WriteString("• Type: Binary file\n")
		suggestion.WriteString("• Recommended: Read-only operations\n")
		suggestion.WriteString("• Editing may corrupt file\n")
	} else {
		suggestion.WriteString("• Type: Unknown (treating as text)\n")
		suggestion.WriteString("• Test with small operations first\n")
	}

	// Claude Desktop specific tips
	suggestion.WriteString("\n💡 Claude Desktop Tips:\n")
	suggestion.WriteString("• Files >50KB may cause timeouts with regular operations\n")
	suggestion.WriteString("• Use intelligent_* functions for automatic optimization\n")
	suggestion.WriteString("• Progress reporting helps with large files\n")
	suggestion.WriteString("• Smart edit handles large files better than regular edit\n")

	return suggestion.String(), nil
}

// BatchOptimizedOperations handles multiple files efficiently
func (o *ClaudeDesktopOptimizer) BatchOptimizedOperations(ctx context.Context, operations []BatchOperation) (string, error) {
	start := time.Now()
	var results strings.Builder

	results.WriteString(fmt.Sprintf("🚀 Starting batch operations: %d files\n\n", len(operations)))

	successCount := 0
	errorCount := 0

	for i, op := range operations {
		opStart := time.Now()
		results.WriteString(fmt.Sprintf("📁 [%d/%d] %s: %s\n", i+1, len(operations), op.Type, op.Path))

		var err error
		switch op.Type {
		case "read":
			_, err = o.IntelligentRead(ctx, op.Path)
		case "write":
			err = o.IntelligentWrite(ctx, op.Path, op.Content)
		case "edit":
			_, err = o.IntelligentEdit(ctx, op.Path, op.OldText, op.NewText, false)
		default:
			err = fmt.Errorf("unknown operation type: %s", op.Type)
		}

		elapsed := time.Since(opStart)
		if err != nil {
			results.WriteString(fmt.Sprintf("❌ Error: %v (took %v)\n", err, elapsed))
			errorCount++
		} else {
			results.WriteString(fmt.Sprintf("✅ Success (took %v)\n", elapsed))
			successCount++
		}

		// Small delay between operations to prevent overwhelming Claude Desktop
		if i < len(operations)-1 {
			time.Sleep(100 * time.Millisecond)
		}
	}

	totalElapsed := time.Since(start)
	results.WriteString(fmt.Sprintf("\n📊 Batch Summary:\n"))
	results.WriteString(fmt.Sprintf("✅ Successful: %d\n", successCount))
	results.WriteString(fmt.Sprintf("❌ Failed: %d\n", errorCount))
	results.WriteString(fmt.Sprintf("⏱️ Total time: %v\n", totalElapsed))
	results.WriteString(fmt.Sprintf("⚡ Average per operation: %v\n", totalElapsed/time.Duration(len(operations))))

	return results.String(), nil
}

// AutoRecoveryEdit is a redirected alias for IntelligentEdit.
// It is maintained for backward compatibility with older Claude Desktop versions.
// The original recovery logic was deprecated due to persistence issues on Windows.
// For guaranteed persistence, use IntelligentWrite with complete file content.
// See: guides/WINDOWS_FILESYSTEM_PERSISTENCE.md
func (o *ClaudeDesktopOptimizer) AutoRecoveryEdit(ctx context.Context, path, oldText, newText string, force bool) (*EditResult, error) {
	slog.Warn("Deprecated API called", "function", "recovery_edit", "redirect", "intelligent_edit")
	// This function is now an alias for IntelligentEdit to prevent timeouts and instability.
	return o.IntelligentEdit(ctx, path, oldText, newText, force)
}

// GetPerformanceReport generates a performance report for Claude Desktop
func (o *ClaudeDesktopOptimizer) GetPerformanceReport() string {
	stats := o.engine.GetPerformanceStats()

	var report strings.Builder
	report.WriteString("🚀 Claude Desktop Performance Report\n")
	report.WriteString("=====================================\n\n")
	report.WriteString(stats)
	report.WriteString("\n\n💡 Optimization Tips:\n")
	report.WriteString("• Files <50KB: Use regular operations\n")
	report.WriteString("• Files 50KB-500KB: Use intelligent_* operations\n")
	report.WriteString("• Files >500KB: Use streaming operations\n")
	report.WriteString("• Very large files: Use chunked operations with progress\n")
	report.WriteString("• Always use analyze_file for unknown files\n")

	return report.String()
}
