package main

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestOpenCodeModel_ResolvesPrefixedTool(t *testing.T) {
	path := seedOpenCodeDB(t, "xai", "grok-4.7", "filesystem-ultra_read_file")
	at := time.UnixMilli(1_700_000_000_000)
	r := newOpenCodeModels(path, "filesystem-ultra", "Grok 4.6")
	t.Cleanup(r.Close)

	model, source := r.resolve("read_file", at)
	if model != "xai/grok-4.7" || source != "opencode" {
		t.Fatalf("got %s %s", model, source)
	}
	entry := ProxyLogEntry{Client: "opencode/1.19.0", Tool: "read_file", Model: "Grok 4.6", Timestamp: at}
	r.tag(&entry)
	if entry.Model != "xai/grok-4.7" || entry.ModelSource != "opencode" {
		t.Fatalf("tag %+v", entry)
	}
}

func TestOpenCodeModel_DoesNotTagOtherClients(t *testing.T) {
	path := seedOpenCodeDB(t, "xai", "grok-4.7", "filesystem-ultra_read_file")
	at := time.UnixMilli(1_700_000_000_000)
	r := newOpenCodeModels(path, "filesystem-ultra", "opus-4")
	t.Cleanup(r.Close)
	entry := ProxyLogEntry{Client: "claude-desktop/1.0", Tool: "read_file", Model: "opus-4", Timestamp: at}
	r.tag(&entry)
	if entry.Model != "opus-4" || entry.ModelSource != "" {
		t.Fatalf("other client was retagged: %+v", entry)
	}
}

func TestOpenCodeModel_AmbiguousKeepsFallback(t *testing.T) {
	path := seedOpenCodeDB(t, "xai", "grok-4.7", "filesystem-ultra_read_file")
	insertOpenCodeCall(t, path, "ses2", "msg2", "part2", "openai", "gpt-6", "filesystem-ultra_read_file", 1_700_000_000_100)
	at := time.UnixMilli(1_700_000_000_000)
	r := newOpenCodeModels(path, "filesystem-ultra", "Grok 4.6")
	t.Cleanup(r.Close)
	model, source := r.resolve("read_file", at)
	if model != "Grok 4.6" || source != "ambiguous" {
		t.Fatalf("got %s %s", model, source)
	}
}

func TestOpenCodeModel_PrefixIsolatesServers(t *testing.T) {
	path := seedOpenCodeDB(t, "xai", "grok-build-0.1", "filesystem-agents_read_file")
	at := time.UnixMilli(1_700_000_000_000)
	r := newOpenCodeModels(path, "filesystem-ultra", "Grok 4.6")
	t.Cleanup(r.Close)
	model, source := r.resolve("read_file", at)
	if model != "Grok 4.6" || source != "unmatched" {
		t.Fatalf("got %s %s", model, source)
	}
}

// A resolver must reopen an existing database read-only and remain safe when
// several proxy calls resolve their model concurrently after a driver upgrade.
func TestOpenCodeModel_ReadOnlyPersistenceAndConcurrency(t *testing.T) {
	path := seedOpenCodeDB(t, "xai", "grok-4.7", "filesystem-ultra_read_file")
	at := time.UnixMilli(1_700_000_000_000)
	r := newOpenCodeModels(path, "filesystem-ultra", "fallback")
	t.Cleanup(r.Close)
	db, err := r.conn()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DELETE FROM message"); err == nil {
		t.Fatal("resolver connection allowed a write")
	}
	const readers = 16
	done := make(chan bool, readers)
	for range readers {
		go func() {
			model, source := r.resolve("read_file", at)
			done <- model == "xai/grok-4.7" && source == "opencode"
		}()
	}
	for range readers {
		if !<-done {
			t.Error("concurrent read lost the persisted model")
		}
	}
	r.Close()
	reopened := newOpenCodeModels(path, "filesystem-ultra", "fallback")
	t.Cleanup(reopened.Close)
	if model, source := reopened.resolve("read_file", at); model != "xai/grok-4.7" || source != "opencode" {
		t.Fatalf("reopened database: %s %s", model, source)
	}
}
func seedOpenCodeDB(t *testing.T, provider, model, tool string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "opencode.db")
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?_pragma=busy_timeout(1000)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`
		CREATE TABLE session (id TEXT PRIMARY KEY, time_updated INTEGER);
		CREATE TABLE message (id TEXT PRIMARY KEY, session_id TEXT, time_created INTEGER, data TEXT);
		CREATE TABLE part (id TEXT PRIMARY KEY, message_id TEXT, session_id TEXT, time_created INTEGER, data TEXT);
	`)
	if err != nil {
		t.Fatal(err)
	}
	insertOpenCodeCall(t, path, "ses1", "msg1", "part1", provider, model, tool, 1_700_000_000_000)
	return path
}

func insertOpenCodeCall(t *testing.T, path, sessionID, messageID, partID, provider, model, tool string, at int64) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?_pragma=busy_timeout(1000)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	msg := `{"providerID":"` + provider + `","modelID":"` + model + `","role":"assistant"}`
	part := `{"type":"tool","tool":"` + tool + `","state":{"status":"running","input":{"secret":"do-not-log"}}}`
	if _, err := db.Exec(`INSERT INTO session(id, time_updated) VALUES(?, ?)
		ON CONFLICT(id) DO UPDATE SET time_updated=excluded.time_updated`, sessionID, at); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO message(id, session_id, time_created, data) VALUES(?, ?, ?, ?)`, messageID, sessionID, at, msg); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO part(id, message_id, session_id, time_created, data) VALUES(?, ?, ?, ?, ?)`, partID, messageID, sessionID, at, part); err != nil {
		t.Fatal(err)
	}
}
