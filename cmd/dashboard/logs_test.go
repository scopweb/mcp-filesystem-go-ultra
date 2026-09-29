package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuditLinesCorrelatesLifecycle(t *testing.T) {
	raw := []byte(strings.Join([]string{
		`{"tool":"edit_file","status":"in_progress","req_id":"done"}`,
		`{"tool":"edit_file","status":"ok","req_id":"done","duration_ms":12}`,
		`{"tool":"read_file","status":"in_progress","req_id":"open"}`,
		`{"tool":"batch_operations","status":"ok","req_id":"pipe","sub_op":"step:1/1:find:search"}`,
		`{"tool":"write_file","status":"warn","req_id":"warned"}`,
		`not-json`,
		`{"status":"ok"}`,
	}, "\n"))

	view := auditLines(raw, true)
	if len(view) != 4 {
		t.Fatalf("operation view = %d lines, want 4: %v", len(view), view)
	}
	if strings.Contains(strings.Join(view, "\n"), `"status":"in_progress","req_id":"done"`) ||
		!strings.Contains(view[0], `"status":"ok"`) {
		t.Fatalf("completed start leaked or completion missing: %v", view)
	}
	if !strings.Contains(strings.Join(view, "\n"), `"req_id":"open"`) {
		t.Fatal("unfinished call must remain visible")
	}

	stats := auditLines(raw, false)
	if len(stats) != 2 {
		t.Fatalf("aggregate lines = %d, want completion + warning: %v", len(stats), stats)
	}
	joined := strings.Join(stats, "\n")
	if strings.Contains(joined, "in_progress") || strings.Contains(joined, "step:") {
		t.Fatalf("aggregates include lifecycle noise: %s", joined)
	}
}

func TestStreamAuditLineDropsStarts(t *testing.T) {
	if streamAuditLine(`{"status":"in_progress","tool":"read_file"}`) {
		t.Fatal("live stream must not emit start breadcrumbs")
	}
	if !streamAuditLine(`{"status":"ok","tool":"read_file"}`) {
		t.Fatal("completion should stream")
	}
	if streamAuditLine(`not-json`) {
		t.Fatal("invalid line should not stream")
	}
}

func TestLogTailSurvivesPartialLinesAndReplacement(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "operations.jsonl")
	if err := os.WriteFile(path, []byte(`{"tool":"read_file","status":"ok"}`+"\n{\"tool\":\"next\""), 0600); err != nil {
		t.Fatal(err)
	}
	var tail logTail
	got := tail.read(path)
	if len(got) != 1 || !strings.Contains(got[0], "read_file") {
		t.Fatalf("first read: %v", got)
	}
	if again := tail.read(path); len(again) != 0 {
		t.Fatalf("partial line emitted early: %v", again)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(",\"status\":\"ok\"}\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	got = tail.read(path)
	if len(got) != 1 || !strings.Contains(got[0], `"tool":"next"`) {
		t.Fatalf("completed partial = %v", got)
	}

	if err := os.WriteFile(path, []byte(`{"tool":"after-rotation","status":"ok"}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got = tail.read(path)
	if len(got) != 1 || !strings.Contains(got[0], "after-rotation") {
		t.Fatalf("replacement not followed: %v", got)
	}
}

func TestStatsDoesNotCountBreadcrumbsAsErrors(t *testing.T) {
	dir := t.TempDir()
	raw := strings.Join([]string{
		`{"ts":"2026-09-29T12:00:00Z","tool":"edit_file","status":"in_progress","req_id":"a","duration_ms":0}`,
		`{"ts":"2026-09-29T12:00:01Z","tool":"edit_file","status":"ok","req_id":"a","duration_ms":12}`,
		`{"ts":"2026-09-29T12:00:02Z","tool":"read_file","status":"in_progress","req_id":"b","duration_ms":0}`,
		`{"ts":"2026-09-29T12:00:03Z","tool":"batch_operations","status":"ok","req_id":"c","sub_op":"step:1/2:find:search","duration_ms":4}`,
		`{"ts":"2026-09-29T12:00:04Z","tool":"batch_operations","status":"warn","req_id":"c","duration_ms":9}`,
		`{"ts":"2026-09-29T12:00:05Z","tool":"write_file","status":"error","req_id":"d","duration_ms":3,"error":"denied"}`,
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, "operations.jsonl"), []byte(raw+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	statsHandler(dir)(rr, httptest.NewRequest(http.MethodGet, "/api/stats", nil))
	var resp StatsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.TotalOps != 3 || resp.TotalErrors != 1 {
		t.Fatalf("ops=%d errors=%d, want 3 and 1", resp.TotalOps, resp.TotalErrors)
	}
	if resp.ByTool["edit_file"].Count != 1 || resp.ByTool["batch_operations"].Count != 1 {
		t.Fatalf("tool counts = %+v", resp.ByTool)
	}
}

func TestProxyStatsUsesClientAndNanoseconds(t *testing.T) {
	dir := t.TempDir()
	line := `{"ts":"2026-09-29T12:00:00Z","model":"grok","client":"opencode/1","tool":"read_file","bytes_in":8,"bytes_out":8,"tokens_in":2,"tokens_out":2,"duration_ms":0,"duration_ns":2500000,"status":"ok"}`
	if err := os.WriteFile(filepath.Join(dir, "proxy.jsonl"), []byte(line+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	proxyStatsHandler(dir)(rr, httptest.NewRequest(http.MethodGet, "/api/proxy-stats", nil))
	var resp ProxyStatsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Available || resp.TotalCalls != 1 || resp.AvgDurationMs < 2 || resp.AvgDurationMs > 3 {
		t.Fatalf("%+v", resp)
	}
	if _, ok := resp.ByModel["opencode/1 / grok"]; !ok {
		t.Fatalf("client/model key missing: %+v", resp.ByModel)
	}
}
