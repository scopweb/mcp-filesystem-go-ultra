package core

import (
	"fmt"
	"os"
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
	PatternTruncation        PatternID = "truncation"         // write < 50% of existing content
	PatternInflationLoop     PatternID = "inflation_loop"     // write > 3x of existing content
	PatternStaleRead         PatternID = "stale_read"         // edit without prior read in session
	PatternFullRewrite       PatternID = "full_rewrite"       // write_file on large existing file
	PatternRepeatedOldText   PatternID = "repeated_old_text"  // same old_text fails 2+ times
	PatternLargeNewText      PatternID = "large_new_text"     // new_text > 80% of file size (use write_file instead)
	PatternAccidentalRewrite PatternID = "accidental_rewrite" // edit_file: small old_text + large new_text with file content remaining (2026-06-11)
	PatternExternalChange    PatternID = "external_change"    // file changed on disk since the session last read/wrote it (auto-OCC, new point 4)
)

// FeedbackSignal is returned by the detector to the tool handler.
type FeedbackSignal struct {
	Status     FeedbackStatus `json:"status"`
	Pattern    PatternID      `json:"pattern,omitempty"`
	Message    string         `json:"message"`
	Suggestion string         `json:"suggestion,omitempty"`
	BlockOp    bool           `json:"block_op"` // true = operation should not proceed

	// Downgraded is set to true by ApplyAdaptiveWriteBlock (and any future
	// adaptive downgraders) when a blocking signal was turned into a
	// non-blocking warn because a safety backup was created. Tool handlers
	// can use this to choose response formatting (e.g., force verbose mode
	// so the restore command is literal, not compact). Omitted from JSON
	// when false to preserve backward compatibility.
	Downgraded bool `json:"downgraded,omitempty"`
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

	// Map of path -> content_hash the session last saw (via read or its own
	// write/edit). Used by auto-OCC (new point 4) to detect a file that changed
	// on disk externally since the client last saw it, even when the caller did
	// not pass expected_hash.
	knownHash map[string]string

	// Map of path -> true once the STALE_READ warning has been emitted for
	// that file in this session. The warning is informational; repeating it
	// on every edit of the same file is pure noise (issue: it fires whenever
	// the pre-edit read was done with a non-filesystem-ultra tool, so the
	// "was read" tracking never sees the read). Cleared by RecordRead /
	// RecordReadHash so a genuine post-read staleness can warn again.
	staleWarned map[string]bool
}

var globalSession = &sessionState{
	failedOldText: make(map[string]map[string]int),
	lastRead:      make(map[string]time.Time),
	knownHash:     make(map[string]string),
	staleWarned:   make(map[string]bool),
}

// autoOCCMode controls automatic optimistic-concurrency checking (new point 4):
// "off" disables it, "warn" (default) emits a non-blocking warning when the file
// changed on disk since the session last saw it, "block" turns that into a hard
// error. Set from the --auto-occ CLI flag.
var autoOCCMode = "warn"

// SetAutoOCCMode configures auto-OCC. Unknown values fall back to "warn".
func SetAutoOCCMode(mode string) {
	switch mode {
	case "off", "warn", "block":
		autoOCCMode = mode
	default:
		autoOCCMode = "warn"
	}
}

// RecordRead marks a file as recently read (resets stale-read detection).
func RecordRead(path string) {
	globalSession.mu.Lock()
	defer globalSession.mu.Unlock()
	globalSession.lastRead[path] = time.Now()
	delete(globalSession.staleWarned, path)
}

// RecordReadHash records the content_hash the session observed for a file on a
// read, and refreshes its last-seen timestamp. Together with RecordWriteHash
// this is the "known hash" auto-OCC compares against (new point 4).
func RecordReadHash(path, hash string) {
	if hash == "" {
		return
	}
	globalSession.mu.Lock()
	defer globalSession.mu.Unlock()
	globalSession.knownHash[path] = hash
	globalSession.lastRead[path] = time.Now()
	delete(globalSession.staleWarned, path)
}

// RecordWriteHash records the content_hash of a file AFTER the session wrote or
// edited it. Critical for auto-OCC correctness: without it, the next edit would
// see the post-edit file differ from the last *read* hash and raise a false
// external-change warning. By tracking the session's own writes, auto-OCC only
// fires on changes the session did not make.
func RecordWriteHash(path, hash string) {
	if hash == "" {
		return
	}
	globalSession.mu.Lock()
	defer globalSession.mu.Unlock()
	globalSession.knownHash[path] = hash
	globalSession.lastRead[path] = time.Now()
}

// InvalidateKnownHash drops the auto-OCC baseline for a path (e.g. after a file
// is deleted or moved away). A later edit then has no baseline and won't warn,
// rather than comparing against a hash that no longer applies.
func InvalidateKnownHash(path string) {
	globalSession.mu.Lock()
	defer globalSession.mu.Unlock()
	delete(globalSession.knownHash, path)
	delete(globalSession.lastRead, path)
}

// refreshKnownHashPath re-reads a single path and records its current hash as
// the session baseline, or clears the baseline if the file is gone. Shared by
// batch and pipeline so a session's own multi-file writes don't later trip
// auto-OCC as external changes (new point 4).
func refreshKnownHashPath(path string) {
	if path == "" {
		return
	}
	if data, err := os.ReadFile(path); err == nil {
		RecordWriteHash(NormalizePath(path), contentHashFNV(string(data)))
	} else {
		InvalidateKnownHash(NormalizePath(path))
	}
}

// RefreshKnownHashes refreshes the auto-OCC baseline for a set of paths a
// session-initiated operation just wrote (batch or pipeline). Files that no
// longer exist have their baseline cleared.
func RefreshKnownHashes(paths []string) {
	for _, p := range paths {
		refreshKnownHashPath(p)
	}
}

// CheckAutoOCC compares the file's current on-disk hash against the hash the
// session last saw (new point 4). It returns a signal only when ALL hold:
//   - auto-OCC is enabled (mode != "off"),
//   - the session has a known hash for this path that is still fresh (read/written
//     within the stale window — an old hash is not a reliable baseline),
//   - the on-disk hash differs from the known hash (an external change).
//
// BlockOp follows the mode: false for "warn" (default, non-blocking), true for
// "block". Returns OK() otherwise. This complements explicit expected_hash: it
// catches lost updates even when the caller did not opt into OCC.
func CheckAutoOCC(path, diskHash string) *FeedbackSignal {
	if autoOCCMode == "off" || diskHash == "" {
		return OK()
	}
	globalSession.mu.Lock()
	known, hasKnown := globalSession.knownHash[path]
	lastRead, hasRead := globalSession.lastRead[path]
	globalSession.mu.Unlock()

	if !hasKnown || !hasRead || time.Since(lastRead) > 10*time.Minute {
		return OK()
	}
	if known == diskHash {
		return OK()
	}
	return &FeedbackSignal{
		Status:  FeedbackWarn,
		Pattern: PatternExternalChange,
		BlockOp: autoOCCMode == "block",
		Message: fmt.Sprintf(
			"file changed on disk since this session last saw it (known hash %s, on-disk %s) — another process may have modified it.",
			known, diskHash),
		Suggestion: "Re-read the file with read_file to get the current content (and content_hash) before editing, " +
			"or pass expected_hash to control concurrency explicitly.",
	}
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
// expectedHashProvided (optional) tells the detector the caller passed an
// explicit expected_hash — cryptographic proof of a prior read — in which
// case the STALE_READ warning is suppressed entirely.
func CheckEditOp(path, oldText string, fileSize int64, expectedHashProvided ...bool) *FeedbackSignal {
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
		// Suppress when the caller passed expected_hash: that token could only
		// come from a prior read_file response, so the file WAS read — just
		// possibly outside this tool family's tracking.
		if len(expectedHashProvided) > 0 && expectedHashProvided[0] {
			return OK()
		}
		// Emit at most once per file per session. The warning exists to catch
		// genuine stale-read loops; repeating it on every edit of a file the
		// user read with a different tool is noise.
		globalSession.mu.Lock()
		alreadyWarned := globalSession.staleWarned[path]
		if !alreadyWarned {
			globalSession.staleWarned[path] = true
		}
		globalSession.mu.Unlock()
		if alreadyWarned {
			return OK()
		}
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

// CheckEditRewrite detects the "accidental full-file rewrite" anti-pattern in
// edit_file. It triggers when ALL THREE conditions hold:
//  1. newText is disproportionately larger than oldText (newText > 2× oldText)
//  2. The file has substantial content remaining after the matched oldText
//     (fileSize - oldText > 50% of fileSize)
//  3. newText is substantial in absolute AND relative terms
//     (newSize > 500 bytes AND newSize > 50% of fileSize)
//
// This pattern was observed in production on 2026-06-11: a 15-line header
// was replaced with the full 150-line file via edit_file, producing a
// 298-line result with the SP duplicated. The model intended to rewrite
// the file but the tool's exact-match semantics only swapped the header,
// leaving the rest of the old file concatenated below new_text.
//
// The check is BLOCKING by default. The caller (tool handler) must pass the
// DEDICATED allow_rewrite=true flag to bypass (point 5) — NOT force. force is
// reserved for the risk-threshold bypass; keeping the two separate prevents a
// risky-but-intended edit (forced through on risk) from also silently disabling
// rewrite protection. The recommended fix for a genuine full-file rewrite is
// write_file; allow_rewrite is only for the rare case where edit semantics are
// truly wanted on a near-total rewrite.
//
// Signal 3 prevents false positives on legitimate small edits where the
// ratio is high simply because old_text was tiny (e.g., expanding a
// 19-byte TODO comment to 68 bytes in a 5 KB file: ratio 3.6x but newText
// is just 1.4% of the file — clearly not a "rewrite").
func CheckEditRewrite(oldText, newText string, fileSize int64) *FeedbackSignal {
	oldSize := int64(len(oldText))
	newSize := int64(len(newText))

	// No signal if either side is empty or file is tiny (avoid noise on
	// micro-edits and brand-new files where this check is meaningless).
	if oldSize == 0 || newSize == 0 || fileSize < 1024 {
		return OK()
	}

	// Signal 1: newText must be disproportionately larger than oldText.
	// Threshold: newSize > 2 * oldSize. Legitimate large refactors tend to
	// keep old_text and new_text similar in length (rename, restructure).
	if newSize < oldSize*2 {
		return OK()
	}

	// Signal 3 (run before signal 2 to short-circuit cheaply): newText
	// must be substantial in absolute terms AND relative to the file.
	// - Absolute: > 500 bytes (filters micro-edits where ratio is high
	//   only because old_text was tiny, e.g., expanding a one-line comment).
	// - Relative: > 50% of fileSize (filters "replace 300 B with 900 B
	//   in a 2 KB file" — ratio 3x but new_text is just 45% of the file,
	//   not a rewrite).
	if newSize < 500 {
		return OK()
	}
	if newSize < fileSize/2 {
		return OK()
	}

	// Signal 2: file must have substantial content beyond the matched block.
	// Threshold: more than 50% of the file is outside the match. This
	// distinguishes "replace a header with the whole file" (remaining ≈
	// 90% of file) from "swap two large functions" (remaining ≈ 0% once
	// old_text is found).
	remaining := fileSize - oldSize
	if remaining < fileSize/2 {
		return OK()
	}

	// All three signals fire: this looks like an accidental full-file rewrite.
	ratio := float64(newSize) / float64(oldSize)
	return &FeedbackSignal{
		Status:  FeedbackKO,
		Pattern: PatternAccidentalRewrite,
		BlockOp: true,
		Message: fmt.Sprintf(
			"BLOCKED: looks like an accidental full-file rewrite. "+
				"old_text=%d B (%d%% of file), new_text=%d B (%.1fx old_text, %d%% of file), "+
				"file remaining after match=%d B (%d%%). "+
				"edit_file only swaps the matched block — the rest of the file "+
				"will be concatenated below new_text.",
			oldSize, 100*oldSize/fileSize, newSize, ratio, 100*newSize/fileSize,
			remaining, 100*remaining/fileSize),
		Suggestion: "If you want to rewrite the whole file, use write_file(path, content=<full new file>). " +
			"If you want a targeted edit, narrow old_text to a minimal unique anchor " +
			"(e.g., a single function signature). " +
			"Only if you genuinely intend edit semantics on a near-total rewrite, pass allow_rewrite:true " +
			"(a safety backup will be created). Note: force:true does NOT bypass this guard.",
	}
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
