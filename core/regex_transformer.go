package core

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// TransformMode defines how patterns are applied
type TransformMode int

const (
	ModeSequential TransformMode = iota // Apply patterns in order
	ModeParallel                        // Apply patterns independently (all to original)
)

// TransformPattern represents a single regex transformation
type TransformPattern struct {
	Pattern     string         // Regex pattern
	Replacement string         // Replacement string (supports $1, $2, etc.)
	Limit       int            // Max replacements (-1 for all)
	compiled    *regexp.Regexp // Compiled regex (internal cache)
}

// RegexTransformConfig holds configuration for regex transformations
type RegexTransformConfig struct {
	FilePath      string             // File to transform
	Patterns      []TransformPattern // Patterns to apply
	Mode          TransformMode      // Sequential or parallel
	CaseSensitive bool               // Case-sensitive matching
	MultiLine     bool               // Multiline regex mode
	CreateBackup  bool               // Create backup before transformation
	DryRun        bool               // Validate without applying
}

// PatternResult holds results for a single pattern
type PatternResult struct {
	Pattern       string // Pattern used
	Replacements  int    // Number of replacements made
	Lines         []int  // Line numbers affected (if tracked)
	Error         string // Error if pattern failed
}

// RegexTransformResult holds transformation results
type RegexTransformResult struct {
	Success           bool            // Whether transformation succeeded
	FilePath          string          // File that was transformed
	PatternsApplied   int             // Number of patterns successfully applied
	TotalReplacements int             // Total replacements across all patterns
	LinesAffected     int             // Unique lines affected
	Duration          time.Duration   // Processing duration
	BackupID          string          // Backup ID if created
	Details           []PatternResult // Per-pattern results
	Errors            []string        // Any errors encountered
	Mode              string          // Mode used (sequential, parallel)
}

// RegexTransformer handles advanced regex transformations
// This is a standalone module that delegates to LargeFileProcessor
type RegexTransformer struct {
	processor *LargeFileProcessor // File processor for handling large files
}

// NewRegexTransformer creates a new regex transformer
func NewRegexTransformer(engine *UltraFastEngine) *RegexTransformer {
	return &RegexTransformer{
		processor: NewLargeFileProcessor(engine),
	}
}

// Transform applies regex transformations to a file
func (rt *RegexTransformer) Transform(ctx context.Context, config RegexTransformConfig) (*RegexTransformResult, error) {
	startTime := time.Now()

	result := &RegexTransformResult{
		FilePath: config.FilePath,
		Details:  make([]PatternResult, 0, len(config.Patterns)),
	}

	// Set mode string
	if config.Mode == ModeSequential {
		result.Mode = "sequential"
	} else {
		result.Mode = "parallel"
	}

	// Validate and compile all patterns first
	if err := rt.compilePatterns(&config); err != nil {
		result.Errors = append(result.Errors, err.Error())
		return result, err
	}

	// Create processing function based on mode
	var processFunc ProcessorFunc
	if config.Mode == ModeSequential {
		processFunc = rt.createSequentialProcessor(config, result)
	} else {
		processFunc = rt.createParallelProcessor(config, result)
	}

	// Use LargeFileProcessor to handle the actual file processing
	procConfig := ProcessingConfig{
		InputPath:    config.FilePath,
		OutputPath:   config.FilePath,
		Mode:         ModeAuto, // Let processor decide based on file size
		ProcessFunc:  processFunc,
		CreateBackup: config.CreateBackup,
		DryRun:       config.DryRun,
	}

	procResult, err := rt.processor.ProcessFile(ctx, procConfig)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		return result, err
	}

	// Populate result from processing result
	result.Success = procResult.Success
	result.BackupID = procResult.BackupID
	result.Duration = time.Since(startTime)
	result.LinesAffected = procResult.TransformedLines

	// Calculate patterns applied
	for _, detail := range result.Details {
		if detail.Error == "" {
			result.PatternsApplied++
			result.TotalReplacements += detail.Replacements
		}
	}

	return result, nil
}

// compilePatterns validates and compiles all regex patterns
func (rt *RegexTransformer) compilePatterns(config *RegexTransformConfig) error {
	for i := range config.Patterns {
		pattern := config.Patterns[i].Pattern

		// Add case-insensitive flag if needed
		if !config.CaseSensitive {
			if !strings.HasPrefix(pattern, "(?i)") {
				pattern = "(?i)" + pattern
			}
		}

		// Add multiline flags if needed
		if config.MultiLine {
			if !strings.HasPrefix(pattern, "(?m)") && !strings.HasPrefix(pattern, "(?s)") {
				pattern = "(?s)" + pattern // (?s) makes . match newlines
			}
		}

		// Compile pattern
		compiled, err := regexp.Compile(pattern)
		if err != nil {
			return fmt.Errorf("invalid pattern '%s': %w", config.Patterns[i].Pattern, err)
		}

		config.Patterns[i].compiled = compiled
	}

	return nil
}

// createSequentialProcessor creates a processor that applies patterns in sequence
func (rt *RegexTransformer) createSequentialProcessor(config RegexTransformConfig, result *RegexTransformResult) ProcessorFunc {
	return func(content string, metadata ProcessMetadata) (string, error) {
		current := content

		// Apply each pattern to the result of the previous one
		for _, pattern := range config.Patterns {
			patternResult := PatternResult{
				Pattern: pattern.Pattern,
			}

			// Apply transformation
			var transformed string
			if pattern.Limit == -1 || pattern.Limit == 0 {
				// Replace all occurrences
				replacementCount := 0
				transformed = pattern.compiled.ReplaceAllStringFunc(current, func(match string) string {
					replacementCount++
					return expandReplacement(pattern.Replacement, match, pattern.compiled)
				})
				patternResult.Replacements = replacementCount
			} else {
				// Replace limited number
				replacementCount := 0
				transformed = pattern.compiled.ReplaceAllStringFunc(current, func(match string) string {
					if replacementCount < pattern.Limit {
						replacementCount++
						return expandReplacement(pattern.Replacement, match, pattern.compiled)
					}
					return match
				})
				patternResult.Replacements = replacementCount
			}

			current = transformed
			result.Details = append(result.Details, patternResult)
		}

		return current, nil
	}
}

// createParallelProcessor creates a processor that applies all patterns to the original content
func (rt *RegexTransformer) createParallelProcessor(config RegexTransformConfig, result *RegexTransformResult) ProcessorFunc {
	return func(content string, metadata ProcessMetadata) (string, error) {
		current := content

		// Apply each pattern independently to the original content
		// Then merge results (later patterns take precedence on conflicts)
		for _, pattern := range config.Patterns {
			patternResult := PatternResult{
				Pattern: pattern.Pattern,
			}

			// Apply transformation to current state
			var transformed string
			if pattern.Limit == -1 || pattern.Limit == 0 {
				// Replace all occurrences
				replacementCount := 0
				transformed = pattern.compiled.ReplaceAllStringFunc(current, func(match string) string {
					replacementCount++
					return expandReplacement(pattern.Replacement, match, pattern.compiled)
				})
				patternResult.Replacements = replacementCount
			} else {
				// Replace limited number
				replacementCount := 0
				transformed = pattern.compiled.ReplaceAllStringFunc(current, func(match string) string {
					if replacementCount < pattern.Limit {
						replacementCount++
						return expandReplacement(pattern.Replacement, match, pattern.compiled)
					}
					return match
				})
				patternResult.Replacements = replacementCount
			}

			current = transformed
			result.Details = append(result.Details, patternResult)
		}

		return current, nil
	}
}

// expandReplacement expands a replacement string with capture groups
func expandReplacement(replacement string, match string, re *regexp.Regexp) string {
	// Use the original regex's submatches
	matches := re.FindStringSubmatchIndex(match)
	if matches == nil {
		return replacement
	}

	// Build result by expanding $n references using Expand
	result := []byte{}
	result = re.Expand(result, []byte(replacement), []byte(match), matches)

	return string(result)
}

// TransformWithCustomFunction applies a custom transformation function
// This is a convenience method for non-regex transformations
func (rt *RegexTransformer) TransformWithCustomFunction(
	ctx context.Context,
	filePath string,
	transformFunc func(content string) (string, error),
	createBackup bool,
	dryRun bool,
) (*ProcessingResult, error) {
	// Wrap custom function in ProcessorFunc
	processFunc := func(content string, metadata ProcessMetadata) (string, error) {
		return transformFunc(content)
	}

	// Use processor directly
	config := ProcessingConfig{
		InputPath:    filePath,
		OutputPath:   filePath,
		Mode:         ModeAuto,
		ProcessFunc:  processFunc,
		CreateBackup: createBackup,
		DryRun:       dryRun,
	}

	return rt.processor.ProcessFile(ctx, config)
}

// QuickTransform is a convenience method for simple single-pattern transformations
func (rt *RegexTransformer) QuickTransform(
	ctx context.Context,
	filePath string,
	pattern string,
	replacement string,
	caseSensitive bool,
) (*RegexTransformResult, error) {
	config := RegexTransformConfig{
		FilePath: filePath,
		Patterns: []TransformPattern{
			{
				Pattern:     pattern,
				Replacement: replacement,
				Limit:       -1, // Replace all
			},
		},
		Mode:          ModeSequential,
		CaseSensitive: caseSensitive,
		CreateBackup:  true,
		DryRun:        false,
	}

	return rt.Transform(ctx, config)
}
