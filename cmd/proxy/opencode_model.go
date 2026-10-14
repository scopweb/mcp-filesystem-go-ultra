package main

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// opencodeModels reads the model of the assistant turn that owns a tool call.
// It never writes the database and never copies tool arguments into the log.
type opencodeModels struct {
	path     string
	prefix   string
	fallback string

	mu      sync.Mutex
	db      *sql.DB
	openErr error
}

func newOpenCodeModels(path, prefix, fallback string) *opencodeModels {
	return &opencodeModels{path: path, prefix: prefix, fallback: fallback}
}

func defaultOpenCodeDB() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	p := filepath.Join(home, ".local", "share", "opencode", "opencode.db")
	st, err := os.Stat(p)
	if err != nil || st.IsDir() {
		return ""
	}
	return p
}

func (r *opencodeModels) tag(entry *ProxyLogEntry) {
	if r == nil || entry == nil || r.path == "" {
		return
	}
	if !strings.Contains(strings.ToLower(entry.Client), "opencode") {
		return
	}
	model, source := r.resolve(entry.Tool, entry.Timestamp)
	entry.ModelSource = source
	if source == "opencode" && model != "" {
		entry.Model = model
	}
}

func (r *opencodeModels) resolve(tool string, at time.Time) (string, string) {
	if tool == "" || at.IsZero() {
		return r.fallback, "unmatched"
	}
	db, err := r.conn()
	if err != nil {
		return r.fallback, "unavailable"
	}
	from := at.Add(-3 * time.Second).UnixMilli()
	to := at.Add(1 * time.Second).UnixMilli()
	sessionFrom := at.Add(-6 * time.Hour).UnixMilli()

	rows, err := db.Query(`
		SELECT p.data, m.data
		FROM part p
		JOIN message m ON m.id = p.message_id
		WHERE p.session_id IN (
			SELECT id FROM session
			WHERE time_updated >= ?
			ORDER BY time_updated DESC
			LIMIT 6
		)
		  AND p.time_created >= ?
		  AND p.time_created <= ?
		ORDER BY p.time_created DESC
		LIMIT 40`, sessionFrom, from, to)
	if err != nil {
		return r.fallback, "unavailable"
	}
	defer rows.Close()

	seen := map[string]struct{}{}
	for rows.Next() {
		var partRaw, msgRaw string
		if err := rows.Scan(&partRaw, &msgRaw); err != nil {
			return r.fallback, "unavailable"
		}
		name := partTool(partRaw)
		if !toolMatches(name, tool, r.prefix) {
			continue
		}
		model := messageModel(msgRaw)
		if model == "" {
			continue
		}
		seen[model] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return r.fallback, "unavailable"
	}
	if len(seen) == 1 {
		for model := range seen {
			return model, "opencode"
		}
	}
	if len(seen) > 1 {
		return r.fallback, "ambiguous"
	}
	return r.fallback, "unmatched"
}

func (r *opencodeModels) conn() (*sql.DB, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.db != nil || r.openErr != nil {
		return r.db, r.openErr
	}
	db, err := sql.Open("sqlite", sqliteReadOnlyDSN(r.path))
	if err != nil {
		r.openErr = err
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetConnMaxLifetime(0)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		r.openErr = err
		return nil, err
	}
	r.db = db
	return db, nil
}

func (r *opencodeModels) Close() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.db != nil {
		_ = r.db.Close()
		r.db = nil
	}
}

func sqliteReadOnlyDSN(path string) string {
	return "file:" + filepath.ToSlash(path) + "?mode=ro&_pragma=busy_timeout(200)&_pragma=query_only(1)"
}

func toolMatches(stored, mcpTool, prefix string) bool {
	if stored == "" || mcpTool == "" {
		return false
	}
	if prefix != "" {
		return stored == prefix+"_"+mcpTool
	}
	return stored == mcpTool || strings.HasSuffix(stored, "_"+mcpTool)
}

func partTool(raw string) string {
	var part struct {
		Type string `json:"type"`
		Tool string `json:"tool"`
	}
	if err := json.Unmarshal([]byte(raw), &part); err != nil || part.Type != "tool" {
		return ""
	}
	return part.Tool
}

func messageModel(raw string) string {
	var msg struct {
		ProviderID string `json:"providerID"`
		ModelID    string `json:"modelID"`
	}
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		return ""
	}
	provider, model := cleanModelToken(msg.ProviderID), cleanModelToken(msg.ModelID)
	if model == "" {
		return ""
	}
	if provider == "" {
		return model
	}
	return provider + "/" + model
}

func cleanModelToken(s string) string {
	if s == "" || len(s) > 80 {
		return ""
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.' || r == '_' || r == '-':
		default:
			return ""
		}
	}
	return s
}
