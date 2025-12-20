package core

import "fmt"

// PathError represents an error that occurred during a path operation
type PathError struct {
	Op   string // Operation (e.g., "read", "write", "stat")
	Path string // The path that caused the error
	Err  error  // The underlying error
}

func (e *PathError) Error() string {
	return fmt.Sprintf("%s %s: %v", e.Op, e.Path, e.Err)
}

func (e *PathError) Unwrap() error {
	return e.Err
}

// ValidationError represents an error during validation
type ValidationError struct {
	Field   string // Field that failed validation
	Value   string // The value that failed
	Message string // Description of the validation failure
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation failed for %s='%s': %s", e.Field, e.Value, e.Message)
}

// CacheError represents an error in cache operations
type CacheError struct {
	Op  string // Operation that failed
	Key string // Cache key
	Err error  // Underlying error
}

func (e *CacheError) Error() string {
	return fmt.Sprintf("cache %s failed for key '%s': %v", e.Op, e.Key, e.Err)
}

func (e *CacheError) Unwrap() error {
	return e.Err
}

// EditError represents an error during edit operations
type EditError struct {
	Op      string // Operation (e.g., "search", "replace", "validate")
	Path    string // File path
	Details string // Additional details
	Err     error  // Underlying error
}

func (e *EditError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("edit %s on %s: %s (%v)", e.Op, e.Path, e.Details, e.Err)
	}
	return fmt.Sprintf("edit %s on %s: %s", e.Op, e.Path, e.Details)
}

func (e *EditError) Unwrap() error {
	return e.Err
}

// ContextError represents an error related to context (cancellation, timeout)
type ContextError struct {
	Op      string // Operation that was cancelled
	Details string // Additional details
}

func (e *ContextError) Error() string {
	return fmt.Sprintf("context error in %s: %s", e.Op, e.Details)
}
