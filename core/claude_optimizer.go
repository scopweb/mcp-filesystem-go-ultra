package core

import (
	"context"
	"fmt"
	"log"
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
	MaxDirectFileSize    int64         // Max size for direct operations
	ChunkSize           int           // Default chunk size
	MaxResponseTime     time.Duration // Max time per operation
	AutoDetectFileType  bool          // Auto-detect text vs binary
	ProgressReporting   bool          // Report progress for long operations
	SmartErrorRecovery  bool          // Attempt error recovery
}

// BatchOperation represents a single operation in a batch
type BatchOperation struct {
	Type     string // "read", "write", "edit"
	Path     string
	Content  string // for write operations
	OldText  string // for edit operations  
	NewText  string // for edit operations
}

// NewClaudeDesktopOptimizer creates a new optimizer
func NewClaudeDesktopOptimizer(engine *UltraFastEngine) *ClaudeDesktopOptimizer {
	return &ClaudeDesktopOptimizer{
		engine: engine,
		config: &OptimizationConfig{
			MaxDirectFileSize:   50 * 1024,      // 50KB
			ChunkSize:          32 * 1024,       // 32KB chunks
			MaxResponseTime:    30 * time.Second, // 30s max
			AutoDetectFileType: true,
			ProgressReporting:  true,
			SmartErrorRecovery: true,
		},
	}
}

// IntelligentWrite automatically chooses the best write strategy
func (o *ClaudeDesktopOptimizer) IntelligentWrite(ctx context.Context, path, content string) error {
	size := int64(len(content))
	
	// Log the decision process
	log.Printf("🧠 IntelligentWrite: %s (%s)", path, formatSize(size))
	
	// Auto-select strategy
	if size <= o.config.MaxDirectFileSize {
		log.Printf("📝 Using direct write (small file)")
		return o.engine.WriteFileContent(ctx, path, content)
	} else {
		log.Printf("🚀 Using streaming write (large file)")
		return o.engine.StreamingWriteFile(ctx, path, content)
	}
}

// IntelligentRead automatically chooses the best read strategy
func (o *ClaudeDesktopOptimizer) IntelligentRead(ctx context.Context, path string) (string, error) {
	// Check file size first
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	
	size := info.Size()
	log.Printf("🧠 IntelligentRead: %s (%s)", path, formatSize(size))
	
	// Auto-select strategy
	if size <= o.config.MaxDirectFileSize {
		log.Printf("📖 Using direct read (small file)")
		return o.engine.ReadFileContent(ctx, path)
	} else {
		log.Printf("📚 Using chunked read (large file)")
		return o.engine.ChunkedReadFile(ctx, path, o.config.ChunkSize)
	}
}

// IntelligentEdit automatically chooses the best edit strategy
func (o *ClaudeDesktopOptimizer) IntelligentEdit(ctx context.Context, path, oldText, newText string) (*EditResult, error) {
	// Analyze file first
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	
	size := info.Size()
	log.Printf("🧠 IntelligentEdit: %s (%s)", path, formatSize(size))
	
	// Auto-select strategy
	if size <= o.config.MaxDirectFileSize {
		log.Printf("✏️ Using direct edit (small file)")
		return o.engine.EditFile(path, oldText, newText)
	} else {
		log.Printf("⚡ Using smart edit (large file)")
		return o.engine.SmartEditFile(ctx, path, oldText, newText, o.config.MaxDirectFileSize)
	}
}

// GetOptimizationSuggestion provides suggestions for Claude Desktop usage
func (o *ClaudeDesktopOptimizer) GetOptimizationSuggestion(ctx context.Context, path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}

	size := info.Size()
	ext := strings.ToLower(filepath.Ext(path))
	
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
	if isTextFile(ext) {
		suggestion.WriteString("• Type: Text file (fully editable)\n")
		suggestion.WriteString("• All operations supported\n")
	} else if isBinaryFile(ext) {
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
			_, err = o.IntelligentEdit(ctx, op.Path, op.OldText, op.NewText)
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

// AutoRecoveryEdit attempts to recover from edit failures
func (o *ClaudeDesktopOptimizer) AutoRecoveryEdit(ctx context.Context, path, oldText, newText string) (*EditResult, error) {
	// First attempt - direct edit
	result, err := o.IntelligentEdit(ctx, path, oldText, newText)
	if err == nil {
		return result, nil
	}
	
	log.Printf("🔄 Edit failed, attempting recovery: %v", err)
	
	if !o.config.SmartErrorRecovery {
		return nil, err
	}
	
	// Recovery attempt 1: Normalize whitespace
	normalizedOldText := strings.TrimSpace(oldText)
	normalizedOldText = strings.ReplaceAll(normalizedOldText, "\r\n", "\n")
	normalizedOldText = strings.ReplaceAll(normalizedOldText, "\r", "\n")
	
	if normalizedOldText != oldText {
		log.Printf("🔄 Recovery attempt 1: Normalized whitespace")
		result, err = o.IntelligentEdit(ctx, path, normalizedOldText, newText)
		if err == nil {
			return result, nil
		}
	}
	
	// Recovery attempt 2: Fuzzy match (remove extra spaces)
	fuzzyOldText := strings.Join(strings.Fields(oldText), " ")
	if fuzzyOldText != oldText && fuzzyOldText != normalizedOldText {
		log.Printf("🔄 Recovery attempt 2: Fuzzy match")
		result, err = o.IntelligentEdit(ctx, path, fuzzyOldText, newText)
		if err == nil {
			return result, nil
		}
	}
	
	// Recovery attempt 3: Line-by-line search
	lines := strings.Split(oldText, "\n")
	if len(lines) > 1 {
		log.Printf("🔄 Recovery attempt 3: Line-by-line search")
		content, readErr := o.IntelligentRead(ctx, path)
		if readErr == nil {
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line != "" && strings.Contains(content, line) {
					result, err = o.IntelligentEdit(ctx, path, line, newText)
					if err == nil {
						log.Printf("✅ Recovery successful with line: %s", line[:min(50, len(line))])
						return result, nil
					}
				}
			}
		}
	}
	
	log.Printf("❌ All recovery attempts failed")
	return nil, fmt.Errorf("edit failed even after recovery attempts: %v", err)
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

// min helper function
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
