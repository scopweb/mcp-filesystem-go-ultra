package core

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// HookEvent represents the different hook execution points
type HookEvent string

const (
	// File operation hooks
	HookPreWrite    HookEvent = "pre-write"    // Before writing a file
	HookPostWrite   HookEvent = "post-write"   // After writing a file
	HookPreEdit     HookEvent = "pre-edit"     // Before editing a file
	HookPostEdit    HookEvent = "post-edit"    // After editing a file
	HookPreDelete   HookEvent = "pre-delete"   // Before deleting a file
	HookPostDelete  HookEvent = "post-delete"  // After deleting a file
	HookPreCreate   HookEvent = "pre-create"   // Before creating a directory
	HookPostCreate  HookEvent = "post-create"  // After creating a directory
	HookPreMove     HookEvent = "pre-move"     // Before moving a file
	HookPostMove    HookEvent = "post-move"    // After moving a file
	HookPreCopy     HookEvent = "pre-copy"     // Before copying a file
	HookPostCopy    HookEvent = "post-copy"    // After copying a file
)

// HookDecision represents the decision made by a hook
type HookDecision string

const (
	HookAllow    HookDecision = "allow"    // Allow the operation to proceed
	HookDeny     HookDecision = "deny"     // Deny the operation
	HookContinue HookDecision = "continue" // Continue with next hook
)

// HookType represents the type of hook command
type HookType string

const (
	HookTypeCommand HookType = "command" // Shell command
	HookTypeScript  HookType = "script"  // Script file
)

// Hook represents a single hook configuration
type Hook struct {
	Type        HookType `json:"type"`               // Type of hook (command or script)
	Command     string   `json:"command"`            // Command to execute
	Script      string   `json:"script,omitempty"`   // Script path (if type is script)
	Timeout     int      `json:"timeout,omitempty"`  // Timeout in seconds (default: 60)
	FailOnError bool     `json:"failOnError"`        // If true, operation fails if hook fails
	Description string   `json:"description"`        // Human-readable description
	Enabled     bool     `json:"enabled"`            // Whether this hook is enabled
}

// HookMatcher represents a matcher for specific tools/operations
type HookMatcher struct {
	Pattern string  `json:"pattern"` // Pattern to match (exact, regex, or wildcard)
	Hooks   []*Hook `json:"hooks"`   // Hooks to execute for this pattern
}

// HookConfig represents the complete hook configuration
type HookConfig struct {
	Hooks map[HookEvent][]*HookMatcher `json:"hooks"`
}

// HookContext contains information passed to hooks
type HookContext struct {
	Event         HookEvent              `json:"event"`          // The hook event
	ToolName      string                 `json:"tool_name"`      // Name of the tool being executed
	FilePath      string                 `json:"file_path"`      // Path of the file being operated on
	Operation     string                 `json:"operation"`      // Operation being performed
	Content       string                 `json:"content,omitempty"` // File content (for write/edit)
	OldContent    string                 `json:"old_content,omitempty"` // Previous content (for edit)
	NewContent    string                 `json:"new_content,omitempty"` // New content (for edit)
	SourcePath    string                 `json:"source_path,omitempty"` // Source path (for move/copy)
	DestPath      string                 `json:"dest_path,omitempty"` // Destination path (for move/copy)
	Timestamp     time.Time              `json:"timestamp"`      // Timestamp of the operation
	WorkingDir    string                 `json:"working_dir"`    // Current working directory
	Metadata      map[string]interface{} `json:"metadata,omitempty"` // Additional metadata
}

// HookResult represents the result of a hook execution
type HookResult struct {
	Decision          HookDecision           `json:"decision"`                     // Decision (allow/deny/continue)
	Reason            string                 `json:"reason,omitempty"`             // Reason for the decision
	ModifiedContent   string                 `json:"modified_content,omitempty"`   // Modified content (e.g., formatted)
	AdditionalContext string                 `json:"additional_context,omitempty"` // Additional context to add
	Metadata          map[string]interface{} `json:"metadata,omitempty"`           // Additional metadata
	Stdout            string                 `json:"stdout,omitempty"`             // Command stdout
	Stderr            string                 `json:"stderr,omitempty"`             // Command stderr
	ExitCode          int                    `json:"exit_code"`                    // Command exit code
	Duration          time.Duration          `json:"duration"`                     // Execution duration
}

// HookManager manages hook execution
type HookManager struct {
	config      *HookConfig
	configMutex sync.RWMutex
	enabled     bool
	debugMode   bool
}

// NewHookManager creates a new hook manager
func NewHookManager() *HookManager {
	return &HookManager{
		config: &HookConfig{
			Hooks: make(map[HookEvent][]*HookMatcher),
		},
		enabled:   false,
		debugMode: false,
	}
}

// LoadConfig loads hook configuration from a JSON file
func (hm *HookManager) LoadConfig(configPath string) error {
	hm.configMutex.Lock()
	defer hm.configMutex.Unlock()

	// Read config file
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read hook config: %w", err)
	}

	// Parse JSON
	var config HookConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse hook config: %w", err)
	}

	hm.config = &config
	hm.enabled = true

	log.Printf("🪝 Loaded hook configuration from %s", configPath)
	return nil
}

// SetEnabled enables or disables the hook system
func (hm *HookManager) SetEnabled(enabled bool) {
	hm.configMutex.Lock()
	defer hm.configMutex.Unlock()
	hm.enabled = enabled
}

// IsEnabled returns whether the hook system is enabled
func (hm *HookManager) IsEnabled() bool {
	hm.configMutex.RLock()
	defer hm.configMutex.RUnlock()
	return hm.enabled
}

// SetDebugMode enables or disables debug mode
func (hm *HookManager) SetDebugMode(debug bool) {
	hm.configMutex.Lock()
	defer hm.configMutex.Unlock()
	hm.debugMode = debug
}

// ExecuteHooks executes all matching hooks for an event
func (hm *HookManager) ExecuteHooks(ctx context.Context, event HookEvent, hookCtx *HookContext) (*HookResult, error) {
	if !hm.IsEnabled() {
		// Hooks disabled, allow operation
		return &HookResult{Decision: HookAllow}, nil
	}

	hm.configMutex.RLock()
	matchers := hm.config.Hooks[event]
	debugMode := hm.debugMode
	hm.configMutex.RUnlock()

	if len(matchers) == 0 {
		// No hooks configured for this event
		return &HookResult{Decision: HookAllow}, nil
	}

	// Find matching hooks
	var matchedHooks []*Hook
	for _, matcher := range matchers {
		if hm.matchesPattern(matcher.Pattern, hookCtx.ToolName) {
			for _, hook := range matcher.Hooks {
				if hook.Enabled {
					matchedHooks = append(matchedHooks, hook)
				}
			}
		}
	}

	if len(matchedHooks) == 0 {
		// No matching hooks
		return &HookResult{Decision: HookAllow}, nil
	}

	if debugMode {
		log.Printf("🪝 Executing %d hook(s) for event %s on %s", len(matchedHooks), event, hookCtx.FilePath)
	}

	// Execute hooks in parallel (with deduplication)
	results := hm.executeHooksParallel(ctx, matchedHooks, hookCtx)

	// Aggregate results
	return hm.aggregateResults(results, event)
}

// matchesPattern checks if a tool name matches a pattern
func (hm *HookManager) matchesPattern(pattern, toolName string) bool {
	// Exact match
	if pattern == toolName {
		return true
	}

	// Wildcard match (*)
	if pattern == "*" {
		return true
	}

	// Simple wildcard patterns (e.g., "write_*", "*_file")
	if strings.Contains(pattern, "*") {
		return hm.matchesWildcard(pattern, toolName)
	}

	// TODO: Add regex support if needed
	return false
}

// matchesWildcard performs simple wildcard matching
func (hm *HookManager) matchesWildcard(pattern, text string) bool {
	// Simple implementation: split by * and check if all parts are present in order
	parts := strings.Split(pattern, "*")

	currentPos := 0
	for i, part := range parts {
		if part == "" {
			continue
		}

		index := strings.Index(text[currentPos:], part)
		if index == -1 {
			return false
		}

		// For first part, it must be at the beginning (if no leading *)
		if i == 0 && !strings.HasPrefix(pattern, "*") && index != 0 {
			return false
		}

		currentPos += index + len(part)
	}

	// For last part, must be at the end (if no trailing *)
	if !strings.HasSuffix(pattern, "*") && currentPos != len(text) {
		return false
	}

	return true
}

// executeHooksParallel executes multiple hooks in parallel
func (hm *HookManager) executeHooksParallel(ctx context.Context, hooks []*Hook, hookCtx *HookContext) []*HookResult {
	// Deduplicate hooks by command
	uniqueHooks := make(map[string]*Hook)
	for _, hook := range hooks {
		key := hook.Command
		if hook.Type == HookTypeScript {
			key = hook.Script
		}
		if _, exists := uniqueHooks[key]; !exists {
			uniqueHooks[key] = hook
		}
	}

	// Execute in parallel
	results := make([]*HookResult, 0, len(uniqueHooks))
	resultsChan := make(chan *HookResult, len(uniqueHooks))

	var wg sync.WaitGroup
	for _, hook := range uniqueHooks {
		wg.Add(1)
		go func(h *Hook) {
			defer wg.Done()
			result := hm.executeHook(ctx, h, hookCtx)
			resultsChan <- result
		}(hook)
	}

	// Wait for all hooks to complete
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Collect results
	for result := range resultsChan {
		results = append(results, result)
	}

	return results
}

// executeHook executes a single hook
func (hm *HookManager) executeHook(ctx context.Context, hook *Hook, hookCtx *HookContext) *HookResult {
	startTime := time.Now()

	// Determine timeout
	timeout := 60 * time.Second
	if hook.Timeout > 0 {
		timeout = time.Duration(hook.Timeout) * time.Second
	}

	// Create context with timeout
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Prepare command
	var cmd *exec.Cmd
	if hook.Type == HookTypeScript {
		// Execute script file
		cmd = exec.CommandContext(execCtx, hook.Script)
	} else {
		// Execute shell command
		cmd = exec.CommandContext(execCtx, "cmd", "/C", hook.Command)
	}

	// Set working directory
	if hookCtx.WorkingDir != "" {
		cmd.Dir = hookCtx.WorkingDir
	}

	// Prepare hook context as JSON input
	jsonInput, err := json.Marshal(hookCtx)
	if err != nil {
		return &HookResult{
			Decision: HookContinue,
			Reason:   fmt.Sprintf("Failed to marshal hook context: %v", err),
			ExitCode: -1,
			Duration: time.Since(startTime),
		}
	}

	// Set stdin
	cmd.Stdin = strings.NewReader(string(jsonInput))

	// Capture stdout and stderr
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Execute command
	err = cmd.Run()
	duration := time.Since(startTime)

	// Get exit code
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	// Parse result
	result := hm.parseHookOutput(stdout.String(), stderr.String(), exitCode)
	result.Duration = duration

	// Handle FailOnError
	if hook.FailOnError && exitCode != 0 {
		result.Decision = HookDeny
		if result.Reason == "" {
			result.Reason = fmt.Sprintf("Hook failed with exit code %d", exitCode)
		}
	}

	return result
}

// parseHookOutput parses the output from a hook command
func (hm *HookManager) parseHookOutput(stdout, stderr string, exitCode int) *HookResult {
	result := &HookResult{
		Stdout:   stdout,
		Stderr:   stderr,
		ExitCode: exitCode,
	}

	// Try to parse stdout as JSON first (advanced mode)
	if stdout != "" {
		var jsonResult HookResult
		if err := json.Unmarshal([]byte(stdout), &jsonResult); err == nil {
			// Successfully parsed JSON
			jsonResult.Stdout = stdout
			jsonResult.Stderr = stderr
			jsonResult.ExitCode = exitCode
			return &jsonResult
		}
	}

	// Fall back to exit code interpretation (simple mode)
	switch exitCode {
	case 0:
		result.Decision = HookAllow
	case 2:
		result.Decision = HookDeny
		result.Reason = stderr
	default:
		result.Decision = HookContinue
		if stderr != "" {
			result.Reason = stderr
		}
	}

	return result
}

// aggregateResults combines multiple hook results into a final decision
func (hm *HookManager) aggregateResults(results []*HookResult, event HookEvent) (*HookResult, error) {
	if len(results) == 0 {
		return &HookResult{Decision: HookAllow}, nil
	}

	// If any hook denies, the operation is denied
	for _, result := range results {
		if result.Decision == HookDeny {
			return result, fmt.Errorf("hook denied operation: %s", result.Reason)
		}
	}

	// Aggregate results
	aggregated := &HookResult{
		Decision: HookAllow,
		Metadata: make(map[string]interface{}),
	}

	// Collect modified content (use the last one that provided it)
	for _, result := range results {
		if result.ModifiedContent != "" {
			aggregated.ModifiedContent = result.ModifiedContent
		}
		if result.AdditionalContext != "" {
			aggregated.AdditionalContext += result.AdditionalContext + "\n"
		}
	}

	return aggregated, nil
}

// AddHook adds a hook configuration programmatically
func (hm *HookManager) AddHook(event HookEvent, pattern string, hook *Hook) {
	hm.configMutex.Lock()
	defer hm.configMutex.Unlock()

	// Find or create matcher for this pattern
	matchers := hm.config.Hooks[event]
	var matcher *HookMatcher
	for _, m := range matchers {
		if m.Pattern == pattern {
			matcher = m
			break
		}
	}

	if matcher == nil {
		matcher = &HookMatcher{
			Pattern: pattern,
			Hooks:   []*Hook{},
		}
		matchers = append(matchers, matcher)
		hm.config.Hooks[event] = matchers
	}

	// Add hook
	matcher.Hooks = append(matcher.Hooks, hook)
}

// GetStats returns statistics about hook executions
func (hm *HookManager) GetStats() string {
	hm.configMutex.RLock()
	defer hm.configMutex.RUnlock()

	var stats strings.Builder
	stats.WriteString("🪝 Hook System Statistics\n")
	stats.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	stats.WriteString(fmt.Sprintf("Enabled: %v\n", hm.enabled))
	stats.WriteString(fmt.Sprintf("Events configured: %d\n", len(hm.config.Hooks)))

	for event, matchers := range hm.config.Hooks {
		totalHooks := 0
		enabledHooks := 0
		for _, matcher := range matchers {
			for _, hook := range matcher.Hooks {
				totalHooks++
				if hook.Enabled {
					enabledHooks++
				}
			}
		}
		stats.WriteString(fmt.Sprintf("  %s: %d/%d hooks enabled\n", event, enabledHooks, totalHooks))
	}

	return stats.String()
}
