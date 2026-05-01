package core

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// AuditEntryKey is the context key for passing the audit entry to handlers.
// Used by auditWrap (main.go) and internal engine code to annotate sub-operations.
type AuditEntryKey struct{}

// SetFeedback annotates the current audit entry with a feedback signal.
// Safe to call even when no audit entry is in context (no-op).
func SetFeedback(ctx context.Context, signal *FeedbackSignal) {
	if signal == nil || signal.Status == FeedbackOK {
		return
	}
	if entry, ok := ctx.Value(AuditEntryKey{}).(*AuditEntry); ok {
		entry.FeedbackPattern = string(signal.Pattern)
		entry.FeedbackStatus = string(signal.Status)
		if signal.Status == FeedbackWarn && entry.Status == "ok" {
			entry.Status = "warn"
		}
	}
}

// SetDiffLines annotates the audit entry with the number of diff lines generated.
func SetDiffLines(ctx context.Context, n int) {
	if entry, ok := ctx.Value(AuditEntryKey{}).(*AuditEntry); ok {
		entry.DiffLines = n
	}
}

// SetFileLinesTotal records the total number of lines in the target file.
// Used to compute range-read efficiency ratio in ROI analysis.
func SetFileLinesTotal(ctx context.Context, n int) {
	if entry, ok := ctx.Value(AuditEntryKey{}).(*AuditEntry); ok {
		entry.FileLinesTotal = n
	}
}

// SetLinesRead records how many lines were actually read/affected by this operation.
// Combined with FileLinesTotal, this shows how efficiently the model used range reads.
func SetLinesRead(ctx context.Context, n int) {
	if entry, ok := ctx.Value(AuditEntryKey{}).(*AuditEntry); ok {
		entry.LinesRead = n
	}
}

// SetSubOp annotates the current audit entry's sub-operation via context.
// Safe to call even when no audit entry is in context (no-op).
func SetSubOp(ctx context.Context, subOp string) {
	if entry, ok := ctx.Value(AuditEntryKey{}).(*AuditEntry); ok {
		entry.SubOp = subOp
	}
}

// AppendSubOp appends to the sub-operation field (e.g. "single_file → direct_read").
// If SubOp is empty, sets it; otherwise appends with " → " separator.
func AppendSubOp(ctx context.Context, subOp string) {
	if entry, ok := ctx.Value(AuditEntryKey{}).(*AuditEntry); ok {
		if entry.SubOp == "" {
			entry.SubOp = subOp
		} else {
			entry.SubOp += " → " + subOp
		}
	}
}

// SetBackupID annotates the audit entry with backup chain info.
func SetBackupID(ctx context.Context, backupID, previousBackupID string) {
	if entry, ok := ctx.Value(AuditEntryKey{}).(*AuditEntry); ok {
		entry.BackupID = backupID
		entry.PreviousBackupID = previousBackupID
	}
}

// SetIntegrityStatus annotates the audit entry with file integrity verification result.
func SetIntegrityStatus(ctx context.Context, status, warning string) {
	if entry, ok := ctx.Value(AuditEntryKey{}).(*AuditEntry); ok {
		entry.IntegrityStatus = status
		entry.IntegrityWarn = warning
		if status == "ERROR" && entry.Status == "ok" {
			entry.Status = "warn"
		}
	}
}

// AuditEntry represents a single MCP tool operation log entry
type AuditEntry struct {
	Timestamp    time.Time         `json:"ts"`
	Tool         string            `json:"tool"`
	Path         string            `json:"path,omitempty"`
	DurationMs   int64             `json:"duration_ms"`
	BytesIn      int64             `json:"bytes_in,omitempty"`
	BytesOut     int64             `json:"bytes_out,omitempty"`  // file bytes written/read — excludes diff text
	Status       string            `json:"status"`               // "ok", "warn", or "error"
	Error        string            `json:"error,omitempty"`
	RiskLevel    string            `json:"risk,omitempty"`
	FileSize     int64             `json:"file_size,omitempty"`
	Args         map[string]string `json:"args,omitempty"`
	SubOp        string            `json:"sub_op,omitempty"`
	LinesChanged int               `json:"lines_changed,omitempty"`
	Matches      int               `json:"matches,omitempty"`
	CacheHit       *bool                  `json:"cache_hit,omitempty"`
	Normalizations []NormalizationApplied `json:"norms,omitempty"`
	// Feedback / reinforcement fields
	FeedbackPattern string `json:"feedback_pattern,omitempty"` // e.g. "truncation", "stale_read"
	FeedbackStatus  string `json:"feedback_status,omitempty"`  // "warn" or "ko" (omitted when ok)
	DiffLines       int    `json:"diff_lines,omitempty"`       // number of lines in the unified diff

	// ROI / savings analysis fields
	SessionID      string `json:"session_id,omitempty"`      // groups ops belonging to the same conversation (reset after 5min gap)
	FileLinesTotal int    `json:"file_lines_total,omitempty"` // total lines in the target file (for range-read efficiency)
	LinesRead      int    `json:"lines_read,omitempty"`       // lines actually read/affected (range ops)
	TokensConsumed int64  `json:"tokens_consumed,omitempty"`  // estimated tokens used by this op (bytes/4)
	TokensBaseline int64  `json:"tokens_baseline,omitempty"`  // estimated tokens without filesystem (naive approach)
	TokensSaved    int64  `json:"tokens_saved,omitempty"`     // max(0, tokens_baseline - tokens_consumed)

	// Backup chain tracking (undo step-through)
	BackupID        string `json:"backup_id,omitempty"`          // backup created for this edit
	PreviousBackupID string `json:"previous_backup_id,omitempty"` // parent in undo chain

	// File integrity verification (for HIGH/CRITICAL edits)
	IntegrityStatus string `json:"integrity_status,omitempty"` // "OK", "WARNING", "ERROR"
	IntegrityWarn   string `json:"integrity_warn,omitempty"`    // warning message if verification had issues
}

// MetricsSnapshot is the periodic metrics dump written to metrics.json
type MetricsSnapshot struct {
	UpdatedAt    time.Time          `json:"updated_at"`
	OpsTotal     int64              `json:"ops_total"`
	OpsPerSec    float64            `json:"ops_per_sec"`
	CacheHitRate float64            `json:"cache_hit_rate"`
	MemoryMB     float64            `json:"memory_mb"`
	Reads        int64              `json:"reads"`
	Writes       int64              `json:"writes"`
	Lists        int64              `json:"lists"`
	Searches     int64              `json:"searches"`
	Edits        MetricsEditSummary `json:"edits"`
}

// MetricsEditSummary contains edit-specific telemetry
type MetricsEditSummary struct {
	Total    int64   `json:"total"`
	Targeted int64   `json:"targeted"`
	Rewrites int64   `json:"rewrites"`
	AvgBytes float64 `json:"avg_bytes"`
}

const (
	maxLogFileSize   = 10 * 1024 * 1024    // 10MB rotation threshold
	maxLogRetention  = 10 * 24 * time.Hour // keep rotated files for 10 days
)

// AuditLogger writes structured JSON Lines to an operations log file
type AuditLogger struct {
	mu      sync.Mutex
	file    *os.File
	logDir  string
	logPath string
	written int64
}

// NewAuditLogger creates a new audit logger writing to the given directory
func NewAuditLogger(logDir string) (*AuditLogger, error) {
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("create log dir: %w", err)
	}

	logPath := filepath.Join(logDir, "operations.jsonl")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, fmt.Errorf("open log file: %w", err)
	}

	// Get current file size for rotation tracking
	info, _ := f.Stat()
	written := int64(0)
	if info != nil {
		written = info.Size()
	}

	return &AuditLogger{
		file:    f,
		logDir:  logDir,
		logPath: logPath,
		written: written,
	}, nil
}

// Log writes an audit entry as a JSON line
func (a *AuditLogger) Log(entry AuditEntry) {
	if a == nil || a.file == nil {
		return
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	data = append(data, '\n')

	a.mu.Lock()
	defer a.mu.Unlock()

	n, _ := a.file.Write(data)
	a.written += int64(n)

	if a.written >= maxLogFileSize {
		a.rotate()
	}
}

// rotate closes the current log file and renames it with a timestamp suffix
func (a *AuditLogger) rotate() {
	a.file.Close()

	// Rename current to timestamped
	ts := time.Now().Format("20060102-150405")
	rotated := filepath.Join(a.logDir, fmt.Sprintf("operations-%s.jsonl", ts))
	os.Rename(a.logPath, rotated)

	// Clean up old rotated files (keep last maxLogFiles)
	a.cleanOldLogs()

	// Open new file
	f, err := os.OpenFile(a.logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		a.file = nil
		return
	}
	a.file = f
	a.written = 0
}

// cleanOldLogs removes rotated log files older than maxLogRetention
func (a *AuditLogger) cleanOldLogs() {
	pattern := filepath.Join(a.logDir, "operations-*.jsonl")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-maxLogRetention)
	for _, path := range matches {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			os.Remove(path)
		}
	}
}

// WriteMetricsSnapshot writes the current metrics to metrics.json atomically
func WriteMetricsSnapshot(logDir string, snapshot MetricsSnapshot) error {
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}

	path := filepath.Join(logDir, "metrics.json")
	tmpPath := path + ".tmp"

	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// Close closes the audit log file
func (a *AuditLogger) Close() error {
	if a == nil || a.file == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.file.Close()
}
