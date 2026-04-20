package main

import (
	"bufio"
	"context"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"math"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed static/*
var staticFiles embed.FS

// BackupInfo mirrors core.BackupInfo for reading metadata files
type BackupInfo struct {
	BackupID    string           `json:"backup_id"`
	Timestamp   time.Time        `json:"timestamp"`
	Operation   string           `json:"operation"`
	UserContext string           `json:"user_context"`
	Files       []BackupMetadata `json:"files"`
	TotalSize   int64            `json:"total_size"`
}

// BackupMetadata mirrors core.BackupMetadata
type BackupMetadata struct {
	OriginalPath string    `json:"original_path"`
	BackupPath   string    `json:"backup_path"`
	Size         int64     `json:"size"`
	Hash         string    `json:"hash"`
	ModifiedTime time.Time `json:"modified_time"`
}

// BatchRawMetadata is the minimal format used by batch backups
type BatchRawMetadata struct {
	CreatedAt  string `json:"created_at"`
	Operations int    `json:"operations"`
	Timestamp  string `json:"timestamp"`
}

// backupCache caches the unified backup list with a TTL
type backupCache struct {
	mu      sync.RWMutex
	data    []BackupInfo
	updated time.Time
	ttl     time.Duration
}

var bkCache = &backupCache{ttl: 30 * time.Second}

func (c *backupCache) get() ([]BackupInfo, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.data != nil && time.Since(c.updated) < c.ttl {
		return c.data, true
	}
	return nil, false
}

func (c *backupCache) set(data []BackupInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data = data
	c.updated = time.Now()
}

// loadUnifiedBackup reads a backup directory and normalizes both normal and batch formats
func loadUnifiedBackup(backupDir, dirName string) (*BackupInfo, error) {
	metaPath := filepath.Join(backupDir, dirName, "metadata.json")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, err
	}

	// Try normal backup format first
	var info BackupInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, err
	}

	// If backup_id is set, it's a normal backup
	if info.BackupID != "" {
		return &info, nil
	}

	// Try batch format
	var batch BatchRawMetadata
	if err := json.Unmarshal(data, &batch); err != nil {
		return nil, err
	}

	// Parse timestamp from batch metadata
	var ts time.Time
	if batch.CreatedAt != "" {
		ts, _ = time.Parse(time.RFC3339Nano, batch.CreatedAt)
	}
	if ts.IsZero() && batch.Timestamp != "" {
		ts, _ = time.Parse(time.RFC3339Nano, batch.Timestamp)
		if ts.IsZero() {
			ts, _ = time.Parse("2006-01-02T15:04:05", batch.Timestamp)
		}
	}
	if ts.IsZero() {
		ts = time.Now() // fallback
	}

	// Scan directory for op-N-filename files
	batchDir := filepath.Join(backupDir, dirName)
	dirEntries, _ := os.ReadDir(batchDir)
	var files []BackupMetadata
	var totalSize int64
	for _, de := range dirEntries {
		if de.IsDir() || de.Name() == "metadata.json" {
			continue
		}
		fi, err := de.Info()
		if err != nil {
			continue
		}
		sz := fi.Size()
		totalSize += sz
		files = append(files, BackupMetadata{
			OriginalPath: de.Name(),
			BackupPath:   de.Name(),
			Size:         sz,
			ModifiedTime: fi.ModTime(),
		})
	}

	return &BackupInfo{
		BackupID:  dirName,
		Timestamp: ts,
		Operation: "batch",
		Files:     files,
		TotalSize: totalSize,
	}, nil
}

// loadAllBackups scans the backup directory and returns all backups (cached)
func loadAllBackups(backupDir string) []BackupInfo {
	if backupDir == "" {
		return nil
	}
	if cached, ok := bkCache.get(); ok {
		return cached
	}

	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return nil
	}

	var backups []BackupInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := loadUnifiedBackup(backupDir, entry.Name())
		if err != nil {
			continue
		}
		backups = append(backups, *info)
	}

	sort.Slice(backups, func(i, j int) bool {
		return backups[i].Timestamp.After(backups[j].Timestamp)
	})

	bkCache.set(backups)
	return backups
}

func main() {
	logDir := flag.String("log-dir", "", "Directory containing MCP server logs (required)")
	proxyLogDir := flag.String("proxy-log-dir", "", "Directory containing proxy logs (proxy.jsonl)")
	backupDir := flag.String("backup-dir", "", "Directory containing MCP server backups")
	port := flag.Int("port", 9100, "HTTP port to listen on")
	flag.Parse()

	if *logDir == "" {
		fmt.Fprintln(os.Stderr, "Error: --log-dir is required")
		flag.Usage()
		os.Exit(1)
	}

	mux := http.NewServeMux()

	// API endpoints
	mux.HandleFunc("/api/health", handleHealth)
	mux.HandleFunc("/api/metrics", metricsHandler(*logDir))
	mux.HandleFunc("/api/operations", operationsHandler(*logDir))
	mux.HandleFunc("/api/operations/live", operationsSSEHandler(*logDir))
	mux.HandleFunc("/api/backups", backupsHandler(*backupDir))
	mux.HandleFunc("/api/backups/search", backupSearchHandler(*backupDir))
	mux.HandleFunc("/api/backups/search-content", backupContentSearchHandler(*backupDir))
	mux.HandleFunc("/api/backups/detail/", backupDetailHandler(*backupDir))
	mux.HandleFunc("/api/backups/file/", backupFileHandler(*backupDir))
	mux.HandleFunc("/api/stats", statsHandler(*logDir))
	mux.HandleFunc("/api/normalizer", normalizerHandler(*logDir))
	mux.HandleFunc("/api/error-patterns", errorPatternsHandler(*logDir))
	mux.HandleFunc("/api/proxy-stats", proxyStatsHandler(*proxyLogDir))
	mux.HandleFunc("/api/roi", roiHandler(*logDir))

	// Serve embedded static files
	staticFS, _ := fs.Sub(staticFiles, "static")
	mux.Handle("/", http.FileServer(http.FS(staticFS)))

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("Dashboard starting on http://localhost%s", addr)
	log.Printf("  Log dir:    %s", *logDir)
	if *backupDir != "" {
		log.Printf("  Backup dir: %s", *backupDir)
	}
	if *proxyLogDir != "" {
		log.Printf("  Proxy logs: %s", *proxyLogDir)
	}
	log.Fatal(http.ListenAndServe(addr, mux))
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "time": time.Now().Format(time.RFC3339)})
}

func metricsHandler(logDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
		data, err := os.ReadFile(filepath.Join(logDir, "metrics.json"))
		if err != nil {
			json.NewEncoder(w).Encode(map[string]string{"error": "no metrics available yet"})
			return
		}
		w.Write(data)
	}
}

func operationsHandler(logDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")

		limit := 100
		if l := r.URL.Query().Get("limit"); l != "" {
			fmt.Sscanf(l, "%d", &limit)
		}
		if limit > 1000 {
			limit = 1000
		}

		logPath := filepath.Join(logDir, "operations.jsonl")
		data, err := os.ReadFile(logPath)
		if err != nil {
			json.NewEncoder(w).Encode([]interface{}{})
			return
		}

		lines := strings.Split(strings.TrimSpace(string(data)), "\n")

		// Return last N lines (most recent operations)
		start := 0
		if len(lines) > limit {
			start = len(lines) - limit
		}
		recent := lines[start:]

		// Reverse to show newest first
		for i, j := 0, len(recent)-1; i < j; i, j = i+1, j-1 {
			recent[i], recent[j] = recent[j], recent[i]
		}

		w.Write([]byte("["))
		first := true
		for _, line := range recent {
			if strings.TrimSpace(line) == "" {
				continue
			}
			if !first {
				w.Write([]byte(","))
			}
			w.Write([]byte(line))
			first = false
		}
		w.Write([]byte("]"))
	}
}

func operationsSSEHandler(logDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "SSE not supported", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		logPath := filepath.Join(logDir, "operations.jsonl")

		// Get initial file size to only send new entries
		var offset int64
		if info, err := os.Stat(logPath); err == nil {
			offset = info.Size()
		}

		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
				info, err := os.Stat(logPath)
				if err != nil || info.Size() <= offset {
					continue
				}

				f, err := os.Open(logPath)
				if err != nil {
					continue
				}

				f.Seek(offset, 0)
				buf := make([]byte, info.Size()-offset)
				n, _ := f.Read(buf)
				f.Close()

				if n > 0 {
					offset = info.Size()
					lines := strings.Split(strings.TrimSpace(string(buf[:n])), "\n")
					for _, line := range lines {
						if strings.TrimSpace(line) != "" {
							fmt.Fprintf(w, "data: %s\n\n", line)
						}
					}
					flusher.Flush()
				}
			}
		}
	}
}

func backupsHandler(backupDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		backups := loadAllBackups(backupDir)
		if backups == nil {
			backups = []BackupInfo{}
		}
		json.NewEncoder(w).Encode(backups)
	}
}

// BackupSearchResponse is the response for /api/backups/search
type BackupSearchResponse struct {
	Total      int          `json:"total"`
	Offset     int          `json:"offset"`
	Limit      int          `json:"limit"`
	Operations []string     `json:"operations"`
	Results    []BackupInfo `json:"results"`
}

func backupSearchHandler(backupDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		q := strings.ToLower(r.URL.Query().Get("q"))
		operation := r.URL.Query().Get("operation")
		preset := r.URL.Query().Get("preset")
		fromStr := r.URL.Query().Get("from")
		toStr := r.URL.Query().Get("to")
		limitStr := r.URL.Query().Get("limit")
		offsetStr := r.URL.Query().Get("offset")

		limit := 50
		if v, err := strconv.Atoi(limitStr); err == nil && v > 0 && v <= 200 {
			limit = v
		}
		offset := 0
		if v, err := strconv.Atoi(offsetStr); err == nil && v >= 0 {
			offset = v
		}

		// Date range from preset
		var fromTime, toTime time.Time
		now := time.Now()
		switch preset {
		case "today":
			fromTime = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		case "24h":
			fromTime = now.Add(-24 * time.Hour)
		case "7d":
			fromTime = now.Add(-7 * 24 * time.Hour)
		case "30d":
			fromTime = now.Add(-30 * 24 * time.Hour)
		}
		// Custom date range overrides preset
		if fromStr != "" {
			if t, err := time.Parse("2006-01-02", fromStr); err == nil {
				fromTime = t
			}
		}
		if toStr != "" {
			if t, err := time.Parse("2006-01-02", toStr); err == nil {
				toTime = t.Add(24*time.Hour - time.Second) // end of day
			}
		}

		all := loadAllBackups(backupDir)

		// Collect distinct operations
		opSet := map[string]bool{}
		for _, b := range all {
			if b.Operation != "" {
				opSet[b.Operation] = true
			}
		}
		var operations []string
		for op := range opSet {
			operations = append(operations, op)
		}
		sort.Strings(operations)

		// Filter
		var filtered []BackupInfo
		for _, b := range all {
			// Operation filter
			if operation != "" && b.Operation != operation {
				continue
			}
			// Date range
			if !fromTime.IsZero() && b.Timestamp.Before(fromTime) {
				continue
			}
			if !toTime.IsZero() && b.Timestamp.After(toTime) {
				continue
			}
			// Text search: match against backup_id, operation, user_context, file names
			if q != "" {
				match := strings.Contains(strings.ToLower(b.BackupID), q) ||
					strings.Contains(strings.ToLower(b.Operation), q) ||
					strings.Contains(strings.ToLower(b.UserContext), q)
				if !match {
					for _, f := range b.Files {
						if strings.Contains(strings.ToLower(f.OriginalPath), q) ||
							strings.Contains(strings.ToLower(f.BackupPath), q) {
							match = true
							break
						}
					}
				}
				if !match {
					continue
				}
			}
			filtered = append(filtered, b)
		}

		total := len(filtered)

		// Pagination
		if offset > len(filtered) {
			offset = len(filtered)
		}
		end := offset + limit
		if end > len(filtered) {
			end = len(filtered)
		}
		page := filtered[offset:end]
		if page == nil {
			page = []BackupInfo{}
		}

		json.NewEncoder(w).Encode(BackupSearchResponse{
			Total:      total,
			Offset:     offset,
			Limit:      limit,
			Operations: operations,
			Results:    page,
		})
	}
}

// ContentMatch represents a single match found inside a backup file
type ContentMatch struct {
	BackupID string `json:"backup_id"`
	FileName string `json:"file_name"`
	FilePath string `json:"file_path"`
	Line     int    `json:"line"`
	Context  string `json:"context"`
}

func backupContentSearchHandler(backupDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		q := r.URL.Query().Get("q")
		if q == "" {
			json.NewEncoder(w).Encode(map[string]interface{}{"error": "q parameter required", "matches": []interface{}{}})
			return
		}

		maxResults := 20
		if v, err := strconv.Atoi(r.URL.Query().Get("max_results")); err == nil && v > 0 && v <= 100 {
			maxResults = v
		}
		maxFileSize := int64(1024 * 1024) // 1MB default

		// 10-second timeout
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		qLower := strings.ToLower(q)
		all := loadAllBackups(backupDir)

		var matches []ContentMatch
		done := false

		for _, b := range all {
			if done {
				break
			}
			for _, f := range b.Files {
				if done {
					break
				}
				select {
				case <-ctx.Done():
					done = true
					break
				default:
				}

				// Determine file path on disk
				var filePath string
				if b.Operation == "batch" {
					filePath = filepath.Join(backupDir, b.BackupID, f.BackupPath)
				} else {
					filePath = filepath.Join(backupDir, b.BackupID, f.BackupPath)
				}

				info, err := os.Stat(filePath)
				if err != nil || info.Size() > maxFileSize {
					continue
				}

				file, err := os.Open(filePath)
				if err != nil {
					continue
				}

				scanner := bufio.NewScanner(file)
				scanner.Buffer(make([]byte, 256*1024), 256*1024)
				var lines []string
				for scanner.Scan() {
					lines = append(lines, scanner.Text())
				}
				file.Close()

				for i, line := range lines {
					if strings.Contains(strings.ToLower(line), qLower) {
						// Build context: 2 lines before + match + 2 lines after
						start := i - 2
						if start < 0 {
							start = 0
						}
						end := i + 3
						if end > len(lines) {
							end = len(lines)
						}
						var ctxLines []string
						for j := start; j < end; j++ {
							prefix := "  "
							if j == i {
								prefix = "> "
							}
							ctxLines = append(ctxLines, fmt.Sprintf("%s%d: %s", prefix, j+1, lines[j]))
						}

						matches = append(matches, ContentMatch{
							BackupID: b.BackupID,
							FileName: f.OriginalPath,
							FilePath: f.BackupPath,
							Line:     i + 1,
							Context:  strings.Join(ctxLines, "\n"),
						})

						if len(matches) >= maxResults {
							done = true
							break
						}
					}
				}
			}
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"query":   q,
			"total":   len(matches),
			"matches": matches,
		})
	}
}

var safeIDRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// backupDetailHandler returns full metadata for a single backup: /api/backups/detail/{id}
func backupDetailHandler(backupDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		id := strings.TrimPrefix(r.URL.Path, "/api/backups/detail/")
		if id == "" || !safeIDRegex.MatchString(id) {
			http.Error(w, `{"error":"invalid backup id"}`, http.StatusBadRequest)
			return
		}

		info, err := loadUnifiedBackup(backupDir, id)
		if err != nil {
			http.Error(w, `{"error":"backup not found"}`, http.StatusNotFound)
			return
		}

		// Enrich each file with existence check and actual backup path
		type FileDetail struct {
			BackupMetadata
			Exists     bool   `json:"Exists"`
			BackupFull string `json:"BackupFull"`
			ViewURL    string `json:"ViewURL"`
		}

		type DetailResponse struct {
			BackupInfo
			FileDetails []FileDetail `json:"FileDetails"`
			BackupPath  string       `json:"BackupPath"`
		}

		var details []FileDetail
		for _, f := range info.Files {
			// For batch backups, files are directly in the backup dir
			// For normal backups, files are in the files/ subdir
			fullPath := filepath.Join(backupDir, id, f.BackupPath)
			_, statErr := os.Stat(fullPath)
			details = append(details, FileDetail{
				BackupMetadata: f,
				Exists:         statErr == nil,
				BackupFull:     fullPath,
				ViewURL:        fmt.Sprintf("/api/backups/file/%s/%s", id, filepath.Base(f.BackupPath)),
			})
		}

		resp := DetailResponse{
			BackupInfo:  *info,
			FileDetails: details,
			BackupPath:  filepath.Join(backupDir, id),
		}

		json.NewEncoder(w).Encode(resp)
	}
}

// backupFileHandler serves the actual backup file content: /api/backups/file/{id}/{filename}
func backupFileHandler(backupDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Parse: /api/backups/file/{id}/{filename}
		rest := strings.TrimPrefix(r.URL.Path, "/api/backups/file/")
		parts := strings.SplitN(rest, "/", 2)
		if len(parts) != 2 {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}

		id, fileName := parts[0], parts[1]
		if !safeIDRegex.MatchString(id) || strings.Contains(fileName, "..") || strings.ContainsAny(fileName, `/\`) {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}

		// Try files/ subdir first (normal backups), then direct (batch backups)
		filePath := filepath.Join(backupDir, id, "files", fileName)
		f, err := os.Open(filePath)
		if err != nil {
			filePath = filepath.Join(backupDir, id, fileName)
			f, err = os.Open(filePath)
			if err != nil {
				http.Error(w, "file not found", http.StatusNotFound)
				return
			}
		}
		defer f.Close()

		info, _ := f.Stat()

		// Determine content type — default to plain text for code files
		ext := filepath.Ext(fileName)
		ct := mime.TypeByExtension(ext)
		if ct == "" {
			ct = "text/plain; charset=utf-8"
		}

		// If ?download=true, force download
		if r.URL.Query().Get("download") == "true" {
			w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", fileName))
		}

		w.Header().Set("Content-Type", ct)
		if info != nil {
			w.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size()))
		}
		io.Copy(w, f)
	}
}

// NormalizationApplied mirrors core.NormalizationApplied
type NormalizationApplied struct {
	RuleID string `json:"rule_id"`
	Type   string `json:"type"`
	Param  string `json:"param"`
	From   string `json:"from,omitempty"`
	To     string `json:"to,omitempty"`
}

// AuditEntry mirrors the core audit entry for parsing operations.jsonl
type AuditEntry struct {
	Timestamp      time.Time              `json:"ts"`
	Tool           string                 `json:"tool"`
	Path           string                 `json:"path,omitempty"`
	DurationMs     int64                  `json:"duration_ms"`
	BytesIn        int64                  `json:"bytes_in,omitempty"`
	BytesOut       int64                  `json:"bytes_out,omitempty"`
	Status         string                 `json:"status"`
	Error          string                 `json:"error,omitempty"`
	RiskLevel      string                 `json:"risk,omitempty"`
	FileSize       int64                  `json:"file_size,omitempty"`
	Args           map[string]string      `json:"args,omitempty"`
	LinesChanged   int                    `json:"lines_changed,omitempty"`
	Matches        int                    `json:"matches,omitempty"`
	CacheHit       *bool                  `json:"cache_hit,omitempty"`
	Normalizations []NormalizationApplied `json:"norms,omitempty"`
	FeedbackPattern string `json:"feedback_pattern,omitempty"`
	FeedbackStatus  string `json:"feedback_status,omitempty"`
	// ROI / savings fields (v4.3.3+)
	SessionID      string `json:"session_id,omitempty"`
	FileLinesTotal int    `json:"file_lines_total,omitempty"`
	LinesRead      int    `json:"lines_read,omitempty"`
	TokensConsumed int64  `json:"tokens_consumed,omitempty"`
	TokensBaseline int64  `json:"tokens_baseline,omitempty"`
	TokensSaved    int64  `json:"tokens_saved,omitempty"`
}

type ToolStats struct {
	Count        int64   `json:"count"`
	Errors       int64   `json:"errors"`
	AvgMs        float64 `json:"avg_ms"`
	MinMs        int64   `json:"min_ms"`
	MaxMs        int64   `json:"max_ms"`
	P95Ms        int64   `json:"p95_ms"`
	TotalBytes   int64   `json:"total_bytes"`
	TotalBytesIn int64   `json:"total_bytes_in"`
	ErrorRate    float64 `json:"error_rate"`
	TotalLines   int64   `json:"total_lines_changed"`
	TotalMatches int64   `json:"total_matches"`
	AvgFileSize  float64 `json:"avg_file_size"`
}

type HourBucket struct {
	Hour  string `json:"hour"`
	Count int64  `json:"count"`
}

type TopFile struct {
	Path  string `json:"path"`
	Count int64  `json:"count"`
}

type StatsResponse struct {
	TotalOps        int64                 `json:"total_ops"`
	TotalErrors     int64                 `json:"total_errors"`
	ErrorRate       float64               `json:"error_rate"`
	AvgDurationMs   float64               `json:"avg_duration_ms"`
	TotalBytesOut   int64                 `json:"total_bytes_out"`
	TotalBytesIn    int64                 `json:"total_bytes_in"`
	TokensEstimate  int64                 `json:"tokens_estimate"`
	TimeSpan        string                `json:"time_span"`
	ByTool          map[string]*ToolStats `json:"by_tool"`
	ByHour          []HourBucket          `json:"by_hour"`
	TopFiles        []TopFile             `json:"top_files"`
	ByRisk          map[string]int64      `json:"by_risk"`
	SlowestOps      []AuditEntry          `json:"slowest_ops"`
	TotalLines      int64                 `json:"total_lines_changed"`
	TotalMatches    int64                 `json:"total_matches"`
}

func statsHandler(logDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		logPath := filepath.Join(logDir, "operations.jsonl")
		data, err := os.ReadFile(logPath)
		if err != nil {
			json.NewEncoder(w).Encode(StatsResponse{ByTool: map[string]*ToolStats{}, ByRisk: map[string]int64{}})
			return
		}

		lines := strings.Split(strings.TrimSpace(string(data)), "\n")

		byTool := map[string]*ToolStats{}
		byRisk := map[string]int64{}
		fileCounts := map[string]int64{}
		hourCounts := map[string]int64{}
		toolDurations := map[string][]int64{} // for P95

		var totalOps, totalErrors, totalBytesOut, totalBytesIn, totalDuration, totalLines, totalMatches int64
		toolFileSizes := map[string][]int64{} // for avg file size per tool
		var earliest, latest time.Time
		var allEntries []AuditEntry

		for _, line := range lines {
			if strings.TrimSpace(line) == "" {
				continue
			}
			var e AuditEntry
			if err := json.Unmarshal([]byte(line), &e); err != nil {
				continue
			}

			allEntries = append(allEntries, e)
			totalOps++
			totalDuration += e.DurationMs
			totalBytesOut += e.BytesOut

			if earliest.IsZero() || e.Timestamp.Before(earliest) {
				earliest = e.Timestamp
			}
			if latest.IsZero() || e.Timestamp.After(latest) {
				latest = e.Timestamp
			}

			// By tool
			ts, ok := byTool[e.Tool]
			if !ok {
				ts = &ToolStats{MinMs: math.MaxInt64}
				byTool[e.Tool] = ts
			}
			ts.Count++
			ts.TotalBytes += e.BytesOut
			ts.TotalBytesIn += e.BytesIn
			ts.TotalLines += int64(e.LinesChanged)
			ts.TotalMatches += int64(e.Matches)
			if e.FileSize > 0 {
				toolFileSizes[e.Tool] = append(toolFileSizes[e.Tool], e.FileSize)
			}
			totalBytesIn += e.BytesIn
			totalLines += int64(e.LinesChanged)
			totalMatches += int64(e.Matches)
			if e.DurationMs < ts.MinMs {
				ts.MinMs = e.DurationMs
			}
			if e.DurationMs > ts.MaxMs {
				ts.MaxMs = e.DurationMs
			}
			toolDurations[e.Tool] = append(toolDurations[e.Tool], e.DurationMs)

			if e.Status != "ok" {
				totalErrors++
				ts.Errors++
			}

			// By path
			if e.Path != "" {
				fileCounts[e.Path]++
			}

			// By risk
			if e.RiskLevel != "" {
				byRisk[e.RiskLevel]++
			}

			// By hour
			h := e.Timestamp.Format("2006-01-02 15:00")
			hourCounts[h]++
		}

		// Compute averages, error rates, P95
		for tool, ts := range byTool {
			if ts.Count > 0 {
				ts.ErrorRate = float64(ts.Errors) / float64(ts.Count) * 100
			}
			durations := toolDurations[tool]
			if len(durations) > 0 {
				var sum int64
				for _, d := range durations {
					sum += d
				}
				ts.AvgMs = float64(sum) / float64(len(durations))

				sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
				p95idx := int(float64(len(durations)) * 0.95)
				if p95idx >= len(durations) {
					p95idx = len(durations) - 1
				}
				ts.P95Ms = durations[p95idx]
			}
			if ts.MinMs == math.MaxInt64 {
				ts.MinMs = 0
			}
			if sizes, ok := toolFileSizes[tool]; ok && len(sizes) > 0 {
				var sum int64
				for _, s := range sizes {
					sum += s
				}
				ts.AvgFileSize = float64(sum) / float64(len(sizes))
			}
		}

		// Top 15 files
		type pathCount struct {
			path  string
			count int64
		}
		var pcs []pathCount
		for p, c := range fileCounts {
			pcs = append(pcs, pathCount{p, c})
		}
		sort.Slice(pcs, func(i, j int) bool { return pcs[i].count > pcs[j].count })
		var topFiles []TopFile
		for i, pc := range pcs {
			if i >= 15 {
				break
			}
			topFiles = append(topFiles, TopFile{Path: pc.path, Count: pc.count})
		}

		// Hours sorted
		var hours []string
		for h := range hourCounts {
			hours = append(hours, h)
		}
		sort.Strings(hours)
		// Keep last 48 hours max
		if len(hours) > 48 {
			hours = hours[len(hours)-48:]
		}
		var byHour []HourBucket
		for _, h := range hours {
			byHour = append(byHour, HourBucket{Hour: h, Count: hourCounts[h]})
		}

		// Top 10 slowest
		sort.Slice(allEntries, func(i, j int) bool { return allEntries[i].DurationMs > allEntries[j].DurationMs })
		var slowest []AuditEntry
		for i, e := range allEntries {
			if i >= 10 {
				break
			}
			slowest = append(slowest, e)
		}

		// Time span
		var timeSpan string
		if !earliest.IsZero() && !latest.IsZero() {
			dur := latest.Sub(earliest)
			if dur.Hours() >= 24 {
				timeSpan = fmt.Sprintf("%.1f days", dur.Hours()/24)
			} else {
				timeSpan = fmt.Sprintf("%.1f hours", dur.Hours())
			}
		}

		var avgDuration float64
		if totalOps > 0 {
			avgDuration = float64(totalDuration) / float64(totalOps)
		}

		resp := StatsResponse{
			TotalOps:       totalOps,
			TotalErrors:    totalErrors,
			ErrorRate:      func() float64 { if totalOps > 0 { return float64(totalErrors) / float64(totalOps) * 100 }; return 0 }(),
			AvgDurationMs:  avgDuration,
			TotalBytesOut:  totalBytesOut,
			TotalBytesIn:   totalBytesIn,
			TokensEstimate: (totalBytesOut + totalBytesIn) / 4,
			TimeSpan:       timeSpan,
			ByTool:         byTool,
			ByHour:         byHour,
			TopFiles:       topFiles,
			ByRisk:         byRisk,
			SlowestOps:     slowest,
			TotalLines:     totalLines,
			TotalMatches:   totalMatches,
		}

		json.NewEncoder(w).Encode(resp)
	}
}

// ProxyLogEntry mirrors the proxy's log format
type ProxyLogEntry struct {
	Timestamp  time.Time `json:"ts"`
	Model      string    `json:"model,omitempty"`
	Tool       string    `json:"tool"`
	Path       string    `json:"path,omitempty"`
	BytesIn    int64     `json:"bytes_in"`
	BytesOut   int64     `json:"bytes_out"`
	TokensIn   int64     `json:"tokens_in"`
	TokensOut  int64     `json:"tokens_out"`
	DurationMs int64     `json:"duration_ms"`
	Status     string    `json:"status"`
	Error      string    `json:"error,omitempty"`
}

type ModelStats struct {
	Count     int64   `json:"count"`
	Errors    int64   `json:"errors"`
	TokensIn  int64   `json:"tokens_in"`
	TokensOut int64   `json:"tokens_out"`
	AvgMs     float64 `json:"avg_ms"`
	ErrorRate float64 `json:"error_rate"`
}

type ProxyToolStats struct {
	Count     int64   `json:"count"`
	TokensIn  int64   `json:"tokens_in"`
	TokensOut int64   `json:"tokens_out"`
	AvgMs     float64 `json:"avg_ms"`
	Errors    int64   `json:"errors"`
	ErrorRate float64 `json:"error_rate"`
}

type ProxyStatsResponse struct {
	TotalCalls     int64                      `json:"total_calls"`
	TotalTokensIn  int64                      `json:"total_tokens_in"`
	TotalTokensOut int64                      `json:"total_tokens_out"`
	TotalTokens    int64                      `json:"total_tokens"`
	TotalErrors    int64                      `json:"total_errors"`
	ErrorRate      float64                    `json:"error_rate"`
	AvgDurationMs  float64                    `json:"avg_duration_ms"`
	TimeSpan       string                     `json:"time_span"`
	ByModel        map[string]*ModelStats     `json:"by_model"`
	ByTool         map[string]*ProxyToolStats `json:"by_tool"`
	ByHour         []HourBucket               `json:"by_hour"`
	TopFiles       []TopFile                  `json:"top_files"`
}

func proxyStatsHandler(proxyLogDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		emptyResp := ProxyStatsResponse{ByModel: map[string]*ModelStats{}, ByTool: map[string]*ProxyToolStats{}}
		if proxyLogDir == "" {
			json.NewEncoder(w).Encode(emptyResp)
			return
		}

		logPath := filepath.Join(proxyLogDir, "proxy.jsonl")
		data, err := os.ReadFile(logPath)
		if err != nil {
			json.NewEncoder(w).Encode(emptyResp)
			return
		}

		lines := strings.Split(strings.TrimSpace(string(data)), "\n")

		byModel := map[string]*ModelStats{}
		byTool := map[string]*ProxyToolStats{}
		fileCounts := map[string]int64{}
		hourCounts := map[string]int64{}
		toolDurations := map[string][]int64{}
		modelDurations := map[string][]int64{}

		var totalCalls, totalTokensIn, totalTokensOut, totalErrors, totalDuration int64
		var earliest, latest time.Time

		for _, line := range lines {
			if strings.TrimSpace(line) == "" {
				continue
			}
			var e ProxyLogEntry
			if err := json.Unmarshal([]byte(line), &e); err != nil {
				continue
			}

			totalCalls++
			totalTokensIn += e.TokensIn
			totalTokensOut += e.TokensOut
			totalDuration += e.DurationMs

			if earliest.IsZero() || e.Timestamp.Before(earliest) {
				earliest = e.Timestamp
			}
			if latest.IsZero() || e.Timestamp.After(latest) {
				latest = e.Timestamp
			}

			modelKey := e.Model
			if modelKey == "" {
				modelKey = "unknown"
			}
			ms, ok := byModel[modelKey]
			if !ok {
				ms = &ModelStats{}
				byModel[modelKey] = ms
			}
			ms.Count++
			ms.TokensIn += e.TokensIn
			ms.TokensOut += e.TokensOut
			modelDurations[modelKey] = append(modelDurations[modelKey], e.DurationMs)
			if e.Status != "ok" {
				totalErrors++
				ms.Errors++
			}

			ts, ok := byTool[e.Tool]
			if !ok {
				ts = &ProxyToolStats{}
				byTool[e.Tool] = ts
			}
			ts.Count++
			ts.TokensIn += e.TokensIn
			ts.TokensOut += e.TokensOut
			toolDurations[e.Tool] = append(toolDurations[e.Tool], e.DurationMs)
			if e.Status != "ok" {
				ts.Errors++
			}

			if e.Path != "" {
				fileCounts[e.Path]++
			}
			h := e.Timestamp.Format("2006-01-02 15:00")
			hourCounts[h]++
		}

		for model, ms := range byModel {
			if ms.Count > 0 {
				ms.ErrorRate = float64(ms.Errors) / float64(ms.Count) * 100
			}
			if durations := modelDurations[model]; len(durations) > 0 {
				var sum int64
				for _, d := range durations {
					sum += d
				}
				ms.AvgMs = float64(sum) / float64(len(durations))
			}
		}
		for tool, ts := range byTool {
			if ts.Count > 0 {
				ts.ErrorRate = float64(ts.Errors) / float64(ts.Count) * 100
			}
			if durations := toolDurations[tool]; len(durations) > 0 {
				var sum int64
				for _, d := range durations {
					sum += d
				}
				ts.AvgMs = float64(sum) / float64(len(durations))
			}
		}

		type pc struct {
			path  string
			count int64
		}
		var pcs []pc
		for p, c := range fileCounts {
			pcs = append(pcs, pc{p, c})
		}
		sort.Slice(pcs, func(i, j int) bool { return pcs[i].count > pcs[j].count })
		var topFiles []TopFile
		for i, p := range pcs {
			if i >= 15 {
				break
			}
			topFiles = append(topFiles, TopFile{Path: p.path, Count: p.count})
		}

		var hours []string
		for h := range hourCounts {
			hours = append(hours, h)
		}
		sort.Strings(hours)
		if len(hours) > 48 {
			hours = hours[len(hours)-48:]
		}
		var byHour []HourBucket
		for _, h := range hours {
			byHour = append(byHour, HourBucket{Hour: h, Count: hourCounts[h]})
		}

		var timeSpan string
		if !earliest.IsZero() && !latest.IsZero() {
			dur := latest.Sub(earliest)
			if dur.Hours() >= 24 {
				timeSpan = fmt.Sprintf("%.1f days", dur.Hours()/24)
			} else {
				timeSpan = fmt.Sprintf("%.1f hours", dur.Hours())
			}
		}

		var avgDuration float64
		if totalCalls > 0 {
			avgDuration = float64(totalDuration) / float64(totalCalls)
		}

		json.NewEncoder(w).Encode(ProxyStatsResponse{
			TotalCalls:     totalCalls,
			TotalTokensIn:  totalTokensIn,
			TotalTokensOut: totalTokensOut,
			TotalTokens:    totalTokensIn + totalTokensOut,
			TotalErrors:    totalErrors,
			ErrorRate: func() float64 {
				if totalCalls > 0 {
					return float64(totalErrors) / float64(totalCalls) * 100
				}
				return 0
			}(),
			AvgDurationMs: avgDuration,
			TimeSpan:      timeSpan,
			ByModel:       byModel,
			ByTool:        byTool,
			ByHour:        byHour,
			TopFiles:      topFiles,
		})
	}
}

// --- Normalizer Status endpoint ---

func normalizerHandler(logDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")

		statsPath := filepath.Join(logDir, "normalizer_stats.json")
		data, err := os.ReadFile(statsPath)
		if err != nil {
			// No stats file yet — return empty stats
			json.NewEncoder(w).Encode(map[string]interface{}{
				"total_processed":  0,
				"total_normalized": 0,
				"by_tool":          map[string]int{},
				"by_rule":          map[string]int{},
				"recent":           []interface{}{},
			})
			return
		}
		w.Write(data)
	}
}

// --- Error Patterns endpoint ---

// ErrorPattern represents a recurring error pattern detected in audit logs
type ErrorPattern struct {
	Pattern       string        `json:"pattern"`
	Tool          string        `json:"tool"`
	Count         int64         `json:"count"`
	FirstSeen     time.Time     `json:"first_seen"`
	LastSeen      time.Time     `json:"last_seen"`
	Trend         string        `json:"trend"`
	SampleErrors  []string      `json:"sample_errors"`
	SuggestedRule *SuggestedRule `json:"suggested_rule,omitempty"`
}

// SuggestedRule is a normalizer rule suggestion based on error patterns
type SuggestedRule struct {
	Type   string `json:"type"`
	From   string `json:"from"`
	To     string `json:"to"`
	Reason string `json:"reason"`
}

// errorPatternNormalizers replace variable parts of error messages with placeholders
var errorPatternNormalizers = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(?:\/[\w.\-]+)+(?:\.\w+)?`),         // file paths
	regexp.MustCompile(`\b\d+\b`),                                // numbers
	regexp.MustCompile(`"[^"]{20,}"`),                             // long quoted strings
	regexp.MustCompile(`'[^']{20,}'`),                             // long single-quoted strings
}

func normalizeErrorMessage(msg string) string {
	result := msg
	result = errorPatternNormalizers[0].ReplaceAllString(result, "<PATH>")
	result = errorPatternNormalizers[1].ReplaceAllString(result, "<N>")
	result = errorPatternNormalizers[2].ReplaceAllString(result, "<STR>")
	result = errorPatternNormalizers[3].ReplaceAllString(result, "<STR>")
	return result
}

func suggestRule(tool, pattern string, samples []string) *SuggestedRule {
	lp := strings.ToLower(pattern)

	// Parameter not found patterns
	if strings.Contains(lp, "required") || strings.Contains(lp, "not found") || strings.Contains(lp, "missing") {
		for _, s := range samples {
			ls := strings.ToLower(s)
			// Look for known param aliases
			if strings.Contains(ls, "old_str") || strings.Contains(ls, "new_str") {
				return &SuggestedRule{Type: "param_alias", From: "old_str", To: "old_text", Reason: "Client sending old_str instead of old_text"}
			}
			if strings.Contains(ls, "type") && tool == "batch_operations" {
				return &SuggestedRule{Type: "nested_alias", From: "type", To: "action", Reason: "Client sending 'type' instead of 'action' in pipeline steps"}
			}
		}
	}

	// JSON parse errors
	if strings.Contains(lp, "json") && (strings.Contains(lp, "invalid") || strings.Contains(lp, "parse") || strings.Contains(lp, "unmarshal")) {
		return &SuggestedRule{Type: "json_accept_both", From: "raw_array", To: "json_string", Reason: "Client may be sending raw JSON array instead of JSON string"}
	}

	// Type coercion errors
	if strings.Contains(lp, "bool") || strings.Contains(lp, "boolean") || strings.Contains(lp, "string") && strings.Contains(lp, "expected") {
		return &SuggestedRule{Type: "type_coerce", From: "string", To: "bool", Reason: "Client sending string where bool expected"}
	}

	return nil
}

func errorPatternsHandler(logDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")

		opsPath := filepath.Join(logDir, "operations.jsonl")
		f, err := os.Open(opsPath)
		if err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"patterns":        []interface{}{},
				"total_errors":    0,
				"unique_patterns": 0,
				"with_suggestions": 0,
			})
			return
		}
		defer f.Close()

		type patternData struct {
			count    int64
			first    time.Time
			last     time.Time
			tool     string
			samples  []string
			recent   []time.Time // last 20 timestamps for trend
		}

		patterns := make(map[string]*patternData) // key: "tool:pattern"
		totalErrors := int64(0)

		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
		for scanner.Scan() {
			var entry AuditEntry
			if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
				continue
			}
			if entry.Status != "error" || entry.Error == "" {
				continue
			}
			totalErrors++

			normalized := normalizeErrorMessage(entry.Error)
			key := entry.Tool + ":" + normalized

			pd, ok := patterns[key]
			if !ok {
				pd = &patternData{
					first: entry.Timestamp,
					last:  entry.Timestamp,
					tool:  entry.Tool,
				}
				patterns[key] = pd
			}
			pd.count++
			if entry.Timestamp.Before(pd.first) {
				pd.first = entry.Timestamp
			}
			if entry.Timestamp.After(pd.last) {
				pd.last = entry.Timestamp
			}
			if len(pd.samples) < 3 {
				pd.samples = append(pd.samples, entry.Error)
			}
			pd.recent = append(pd.recent, entry.Timestamp)
			if len(pd.recent) > 20 {
				pd.recent = pd.recent[len(pd.recent)-20:]
			}
		}

		// Build result
		result := make([]ErrorPattern, 0, len(patterns))
		withSuggestions := 0

		for key, pd := range patterns {
			parts := strings.SplitN(key, ":", 2)
			pattern := parts[1]

			// Compute trend from recent timestamps
			trend := "stable"
			if len(pd.recent) >= 4 {
				mid := len(pd.recent) / 2
				firstHalf := pd.recent[:mid]
				secondHalf := pd.recent[mid:]
				if len(firstHalf) > 0 && len(secondHalf) > 0 {
					firstSpan := firstHalf[len(firstHalf)-1].Sub(firstHalf[0])
					secondSpan := secondHalf[len(secondHalf)-1].Sub(secondHalf[0])
					if firstSpan > 0 && secondSpan > 0 {
						firstRate := float64(len(firstHalf)) / firstSpan.Seconds()
						secondRate := float64(len(secondHalf)) / secondSpan.Seconds()
						if secondRate > firstRate*1.5 {
							trend = "increasing"
						} else if secondRate < firstRate*0.5 {
							trend = "decreasing"
						}
					}
				}
			}

			suggested := suggestRule(pd.tool, pattern, pd.samples)
			if suggested != nil {
				withSuggestions++
			}

			result = append(result, ErrorPattern{
				Pattern:       pattern,
				Tool:          pd.tool,
				Count:         pd.count,
				FirstSeen:     pd.first,
				LastSeen:      pd.last,
				Trend:         trend,
				SampleErrors:  pd.samples,
				SuggestedRule: suggested,
			})
		}

		// Sort by count descending
		sort.Slice(result, func(i, j int) bool {
			return result[i].Count > result[j].Count
		})

		// Limit to 50
		if len(result) > 50 {
			result = result[:50]
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"patterns":         result,
			"total_errors":     totalErrors,
			"unique_patterns":  len(patterns),
			"with_suggestions": withSuggestions,
		})
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// ROI / Savings handler
// ──────────────────────────────────────────────────────────────────────────────

type SessionROI struct {
	SessionID      string    `json:"session_id"`
	FirstOp        time.Time `json:"first_op"`
	LastOp         time.Time `json:"last_op"`
	DurationMin    float64   `json:"duration_min"`
	OpsCount       int64     `json:"ops_count"`
	TokensConsumed int64     `json:"tokens_consumed"`
	TokensBaseline int64     `json:"tokens_baseline"`
	TokensSaved    int64     `json:"tokens_saved"`
	SavingsPct     float64   `json:"savings_pct"`
	Errors         int64     `json:"errors"`
}

type ToolROI struct {
	Tool           string  `json:"tool"`
	OpsCount       int64   `json:"ops_count"`
	TokensConsumed int64   `json:"tokens_consumed"`
	TokensBaseline int64   `json:"tokens_baseline"`
	TokensSaved    int64   `json:"tokens_saved"`
	SavingsPct     float64 `json:"savings_pct"`
	AvgSavedPerOp  float64 `json:"avg_saved_per_op"`
}

type TopSavingOp struct {
	Timestamp      time.Time `json:"ts"`
	Tool           string    `json:"tool"`
	Path           string    `json:"path,omitempty"`
	TokensConsumed int64     `json:"tokens_consumed"`
	TokensBaseline int64     `json:"tokens_baseline"`
	TokensSaved    int64     `json:"tokens_saved"`
	FileSize       int64     `json:"file_size,omitempty"`
}

type ROIResponse struct {
	// Global totals
	TotalOps       int64   `json:"total_ops"`
	TokensConsumed int64   `json:"tokens_consumed"`
	TokensBaseline int64   `json:"tokens_baseline"`
	TokensSaved    int64   `json:"tokens_saved"`
	SavingsPct     float64 `json:"savings_pct"`
	// Range efficiency (read_file range vs full file)
	RangeReadOps   int64   `json:"range_read_ops"`
	RangeReadPct   float64 `json:"range_read_pct"` // % of reads that used range
	AvgReadPct     float64 `json:"avg_read_pct"`   // avg % of file actually read
	// Sessions
	SessionCount   int64        `json:"session_count"`
	Sessions       []SessionROI `json:"sessions"`
	// By tool
	ByTool         []ToolROI    `json:"by_tool"`
	// Top savings operations
	TopSavings     []TopSavingOp `json:"top_savings"`
	// Anti-patterns detected
	AntiPatterns   map[string]int64 `json:"anti_patterns"`
	TimeSpan       string           `json:"time_span"`
}

func roiHandler(logDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		logPath := filepath.Join(logDir, "operations.jsonl")
		data, err := os.ReadFile(logPath)
		if err != nil {
			json.NewEncoder(w).Encode(ROIResponse{})
			return
		}
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")

		// Parse all entries
		var entries []AuditEntry
		for _, line := range lines {
			if strings.TrimSpace(line) == "" {
				continue
			}
			var e AuditEntry
			if json.Unmarshal([]byte(line), &e) == nil {
				entries = append(entries, e)
			}
		}

		if len(entries) == 0 {
			json.NewEncoder(w).Encode(ROIResponse{})
			return
		}

		// Global accumulators
		var totalConsumed, totalBaseline, totalSaved int64
		var rangeReadOps int64
		var sumReadPct float64
		var readOpsWithLineInfo int64
		antiPatterns := map[string]int64{}
		toolMap := map[string]*ToolROI{}
		sessionMap := map[string]*SessionROI{}
		var topSavings []TopSavingOp
		var earliest, latest time.Time

		for i := range entries {
			e := &entries[i]

			// Time range
			if earliest.IsZero() || e.Timestamp.Before(earliest) {
				earliest = e.Timestamp
			}
			if latest.IsZero() || e.Timestamp.After(latest) {
				latest = e.Timestamp
			}

			// Anti-patterns
			if e.FeedbackPattern != "" {
				antiPatterns[e.FeedbackPattern]++
			}

			// Token accumulators — only count ops with ROI data (v4.3.3+)
			if e.TokensConsumed > 0 || e.TokensBaseline > 0 {
				totalConsumed += e.TokensConsumed
				totalBaseline += e.TokensBaseline
				totalSaved += e.TokensSaved

				// By tool
				tr, ok := toolMap[e.Tool]
				if !ok {
					tr = &ToolROI{Tool: e.Tool}
					toolMap[e.Tool] = tr
				}
				tr.OpsCount++
				tr.TokensConsumed += e.TokensConsumed
				tr.TokensBaseline += e.TokensBaseline
				tr.TokensSaved += e.TokensSaved

				// Top savings
				if e.TokensSaved > 0 {
					topSavings = append(topSavings, TopSavingOp{
						Timestamp:      e.Timestamp,
						Tool:           e.Tool,
						Path:           e.Path,
						TokensConsumed: e.TokensConsumed,
						TokensBaseline: e.TokensBaseline,
						TokensSaved:    e.TokensSaved,
						FileSize:       e.FileSize,
					})
				}
			}

			// Range read efficiency
			if (e.Tool == "read_file" || e.Tool == "read_text_file") && e.FileLinesTotal > 0 && e.LinesRead > 0 {
				readOpsWithLineInfo++
				pct := float64(e.LinesRead) / float64(e.FileLinesTotal) * 100
				sumReadPct += pct
				if e.LinesRead < e.FileLinesTotal {
					rangeReadOps++
				}
			}

			// Session aggregation
			if e.SessionID != "" {
				sr, ok := sessionMap[e.SessionID]
				if !ok {
					sr = &SessionROI{SessionID: e.SessionID, FirstOp: e.Timestamp, LastOp: e.Timestamp}
					sessionMap[e.SessionID] = sr
				}
				sr.OpsCount++
				sr.TokensConsumed += e.TokensConsumed
				sr.TokensBaseline += e.TokensBaseline
				sr.TokensSaved += e.TokensSaved
				if e.Timestamp.Before(sr.FirstOp) {
					sr.FirstOp = e.Timestamp
				}
				if e.Timestamp.After(sr.LastOp) {
					sr.LastOp = e.Timestamp
				}
				if e.Status == "error" {
					sr.Errors++
				}
			}
		}

		// Compute tool savings %
		byTool := make([]ToolROI, 0, len(toolMap))
		for _, tr := range toolMap {
			if tr.OpsCount > 0 {
				tr.AvgSavedPerOp = float64(tr.TokensSaved) / float64(tr.OpsCount)
			}
			if tr.TokensBaseline > 0 {
				tr.SavingsPct = float64(tr.TokensSaved) / float64(tr.TokensBaseline) * 100
			}
			byTool = append(byTool, *tr)
		}
		sort.Slice(byTool, func(i, j int) bool { return byTool[i].TokensSaved > byTool[j].TokensSaved })

		// Compute session stats
		sessions := make([]SessionROI, 0, len(sessionMap))
		for _, sr := range sessionMap {
			sr.DurationMin = sr.LastOp.Sub(sr.FirstOp).Minutes()
			if sr.TokensBaseline > 0 {
				sr.SavingsPct = float64(sr.TokensSaved) / float64(sr.TokensBaseline) * 100
			}
			sessions = append(sessions, *sr)
		}
		sort.Slice(sessions, func(i, j int) bool { return sessions[i].FirstOp.After(sessions[j].FirstOp) })
		if len(sessions) > 20 {
			sessions = sessions[:20]
		}

		// Top savings ops (top 10)
		sort.Slice(topSavings, func(i, j int) bool { return topSavings[i].TokensSaved > topSavings[j].TokensSaved })
		if len(topSavings) > 10 {
			topSavings = topSavings[:10]
		}

		// Global %
		var savingsPct float64
		if totalBaseline > 0 {
			savingsPct = float64(totalSaved) / float64(totalBaseline) * 100
		}

		// Range read stats
		var avgReadPct, rangeReadPct float64
		if readOpsWithLineInfo > 0 {
			avgReadPct = sumReadPct / float64(readOpsWithLineInfo)
			rangeReadPct = float64(rangeReadOps) / float64(readOpsWithLineInfo) * 100
		}

		timeSpan := ""
		if !earliest.IsZero() {
			d := latest.Sub(earliest)
			switch {
			case d < time.Hour:
				timeSpan = fmt.Sprintf("%.0f min", d.Minutes())
			case d < 24*time.Hour:
				timeSpan = fmt.Sprintf("%.1f h", d.Hours())
			default:
				timeSpan = fmt.Sprintf("%.1f days", d.Hours()/24)
			}
		}

		json.NewEncoder(w).Encode(ROIResponse{
			TotalOps:       int64(len(entries)),
			TokensConsumed: totalConsumed,
			TokensBaseline: totalBaseline,
			TokensSaved:    totalSaved,
			SavingsPct:     math.Round(savingsPct*10) / 10,
			RangeReadOps:   rangeReadOps,
			RangeReadPct:   math.Round(rangeReadPct*10) / 10,
			AvgReadPct:     math.Round(avgReadPct*10) / 10,
			SessionCount:   int64(len(sessionMap)),
			Sessions:       sessions,
			ByTool:         byTool,
			TopSavings:     topSavings,
			AntiPatterns:   antiPatterns,
			TimeSpan:       timeSpan,
		})
	}
}
