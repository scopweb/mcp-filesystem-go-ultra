package core

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// FeedbackStatus indicates whether an operation was correct or not.
type FeedbackStatus string

const (
	FeedbackOK   FeedbackStatus = "ok"
	FeedbackWarn FeedbackStatus = "warn"
	FeedbackKO   FeedbackStatus = "ko"
)

// PatternID identifies a detected anti-pattern.
type PatternID string

const (
	PatternTruncation       PatternID = "truncation"        // write < 50% of existing content
	PatternInflationLoop    PatternID = "inflation_loop"     // write > 3x of existing content
	PatternStaleRead        PatternID = "stale_read"         // edit without prior read in session
	PatternFullRewrite      PatternID = "full_rewrite"       // write_file on large existing file
	PatternRepeatedOldText  PatternID = "repeated_old_text"  // same old_text fails 2+ times
	PatternLargeNewText     PatternID = "large_new_text"     // new_text > 80% of file size (use write_file instead)
)

// FeedbackSignal is returned by the detector to the tool handler.
type FeedbackSignal struct {
	Status     FeedbackStatus `json:"status"`
	Pattern    PatternID      `json:"pattern,omitempty"`
	Message    string         `json:"message"`
	Suggestion string         `json:"suggestion,omitempty"`
	BlockOp    bool           `json:"block_op"` // true = operation should not proceed
}

// OK returns a FeedbackSignal indicating no issue.
func OK() *FeedbackSignal {
	return &FeedbackSignal{Status: FeedbackOK, BlockOp: false}
}

// sessionState tracks per-session operation history for reinforcement detection.
type sessionState struct {
	mu sync.Mutex

	// Map of path -> number of times old_text failed to match
	failedOldText map[string]map[string]int

	// Map of path -> last read timestamp (to detect edits without prior read)
	lastRead map[string]time.Time
}

var globalSession = &sessionState{
	failedOldText: make(map[string]map[string]int),
	lastRead:      make(map[string]time.Time),
}

// RecordRead marks a file as recently read (resets stale-read detection).
func RecordRead(path string) {
	globalSession.mu.Lock()
	defer globalSession.mu.Unlock()
	globalSession.lastRead[path] = time.Now()
}

// RecordFailedOldText increments the failure counter for an old_text on a path.
func RecordFailedOldText(path, oldText string) {
	globalSession.mu.Lock()
	defer globalSession.mu.Unlock()
	if globalSession.failedOldText[path] == nil {
		globalSession.failedOldText[path] = make(map[string]int)
	}
	// Key by first 60 chars to avoid huge map keys
	key := oldText
	if len(key) > 60 {
		key = key[:60]
	}
	globalSession.failedOldText[path][key]++
}

// ResetFailedOldText clears the counter after a successful edit.
func ResetFailedOldText(path, oldText string) {
	globalSession.mu.Lock()
	defer globalSession.mu.Unlock()
	if globalSession.failedOldText[path] != nil {
		key := oldText
		if len(key) > 60 {
			key = key[:60]
		}
		delete(globalSession.failedOldText[path], key)
	}
}

// --- Detectors ---

// CheckWriteOp evaluates a write_file operation before it executes.
// existingSize = 0 means file does not exist (new file — no checks needed).
func CheckWriteOp(path string, newContent string, existingSize int64) *FeedbackSignal {
	if existingSize == 0 {
		return OK()
	}

	newSize := int64(len(newContent))

	// Truncation: new < 50% of existing
	if newSize > 0 && newSize < existingSize/2 {
		return &FeedbackSignal{
			Status:  FeedbackKO,
			Pattern: PatternTruncation,
			BlockOp: true,
			Message: fmt.Sprintf(
				"BLOCKED: new content (%d B) is less than 50%% of existing file (%d B). "+
					"Looks like accidental truncation.",
				newSize, existingSize),
			Suggestion: "Read the full file first, then use edit_file for partial changes. " +
				"To force overwrite: delete_file first, then write_file.",
		}
	}

	// Inflation loop: new > 3x existing
	if existingSize > 10*1024 && newSize > existingSize*3 {
		ratio := newSize / existingSize
		return &FeedbackSignal{
			Status:  FeedbackKO,
			Pattern: PatternInflationLoop,
			BlockOp: true,
			Message: fmt.Sprintf(
				"BLOCKED: new content (%d B) is %dx the existing file (%d B). "+
					"Looks like an inflation loop (content = read + append repeated).",
				newSize, ratio, existingSize),
			Suggestion: "Use edit_file for surgical changes instead of write_file.",
		}
	}

	// Full rewrite warning: writing >10KB to existing file — suggest edit_file
	if existingSize > 10*1024 && newSize > 10*1024 {
		return &FeedbackSignal{
			Status:  FeedbackWarn,
			Pattern: PatternFullRewrite,
			BlockOp: false,
			Message: fmt.Sprintf(
				"Full rewrite of %s (%d B → %d B). This overwrites the entire file.",
				path, existingSize, newSize),
			Suggestion: "Prefer edit_file for targeted changes — saves tokens and avoids accidental data loss.",
		}
	}

	return OK()
}

// CheckEditOp evaluates an edit_file operation before it executes.
func CheckEditOp(path, oldText string, fileSize int64) *FeedbackSignal {
	// Stale read: file not read in this session in the last 10 minutes
	globalSession.mu.Lock()
	lastRead, hasRead := globalSession.lastRead[path]
	failCount := 0
	key := oldText
	if len(key) > 60 {
		key = key[:60]
	}
	if globalSession.failedOldText[path] != nil {
		failCount = globalSession.failedOldText[path][key]
	}
	globalSession.mu.Unlock()

	if failCount >= 2 {
		return &FeedbackSignal{
			Status:  FeedbackKO,
			Pattern: PatternRepeatedOldText,
			BlockOp: false, // warn, don't block — engine will return its own error
			Message: fmt.Sprintf(
				"old_text has failed to match %d times on this file.", failCount),
			Suggestion: "Re-read the file with read_file to get the current exact content, then retry.",
		}
	}

	staleDuration := 10 * time.Minute
	if !hasRead || time.Since(lastRead) > staleDuration {
		return &FeedbackSignal{
			Status:  FeedbackWarn,
			Pattern: PatternStaleRead,
			BlockOp: false,
			Message: "File was not read in the current session before editing.",
			Suggestion: "Use read_file (or read_file with start_line/end_line) to verify current content " +
				"before calling edit_file.",
		}
	}

	// Large new_text relative to file: suggests write_file instead
	newTextSize := int64(len(oldText)) // checked by proxy — not ideal but we don't have newText here
	_ = newTextSize
	_ = fileSize

	return OK()
}

// CheckEditNewText checks if new_text is suspiciously large relative to the file.
func CheckEditNewText(newText string, fileSize int64) *FeedbackSignal {
	if fileSize == 0 {
		return OK()
	}
	newSize := int64(len(newText))
	if newSize > fileSize*8/10 && fileSize > 5*1024 {
		return &FeedbackSignal{
			Status:  FeedbackWarn,
			Pattern: PatternLargeNewText,
			BlockOp: false,
			Message: fmt.Sprintf(
				"new_text (%d B) is >80%% of the file size (%d B). "+
					"You may be replacing most of the file.",
				newSize, fileSize),
			Suggestion: "If replacing the whole file, use write_file instead. " +
				"If patching, narrow old_text to the minimal unique anchor.",
		}
	}
	return OK()
}

// --- Formatting ---

// FormatFeedback appends feedback info to a tool response message.
// It is a no-op for FeedbackOK to keep happy-path responses clean.
func FormatFeedback(signal *FeedbackSignal, existingMsg string) string {
	if signal == nil || signal.Status == FeedbackOK {
		return existingMsg
	}

	var sb strings.Builder
	sb.WriteString(existingMsg)

	prefix := "\n⚠️  "
	if signal.Status == FeedbackKO {
		prefix = "\n🚫 "
	}

	sb.WriteString(prefix)
	sb.WriteString(fmt.Sprintf("[%s] %s", strings.ToUpper(string(signal.Pattern)), signal.Message))
	if signal.Suggestion != "" {
		sb.WriteString("\n   → ")
		sb.WriteString(signal.Suggestion)
	}
	return sb.String()
}

// FormatFeedbackCompact returns a compact single-line feedback tag.
func FormatFeedbackCompact(signal *FeedbackSignal) string {
	if signal == nil || signal.Status == FeedbackOK {
		return ""
	}
	tag := "WARN"
	if signal.Status == FeedbackKO {
		tag = "KO"
	}
	return fmt.Sprintf(" [%s:%s]", tag, signal.Pattern)
}
