//go:build e2e

package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

type e6Metrics struct {
	Task      string `json:"task"`
	Success   bool   `json:"success"`
	Calls     int    `json:"calls"`
	Conflicts int    `json:"conflicts"`
	Retries   int    `json:"retries"`
	WrongMods int    `json:"wrong_mods"`
	Millis    int64  `json:"duration_ms"`
}

func TestE2E_E6_AgentEval(t *testing.T) {
	exe := buildServer(t)
	dir := t.TempDir()
	c := startClient(t, exe, dir)
	var all []e6Metrics

	all = append(all, e6Task(t, "localized_edit", func(m *e6Metrics) {
		path := filepath.Join(dir, "local.txt")
		m.Calls++
		call(t, c, "write_file", map[string]any{"path": path, "content": "alpha beta\n"})
		m.Calls++
		call(t, c, "edit_file", map[string]any{"path": path, "old_text": "beta", "new_text": "BETA"})
		b, err := os.ReadFile(path)
		if err != nil || string(b) != "alpha BETA\n" {
			m.WrongMods++
			t.Fatalf("localized: %q (%v)", b, err)
		}
	}))

	all = append(all, e6Task(t, "multifile_refactor", func(m *e6Metrics) {
		a := filepath.Join(dir, "a.go")
		b := filepath.Join(dir, "b.go")
		m.Calls++
		call(t, c, "write_file", map[string]any{"path": a, "content": "package p\nvar Token = 1\n"})
		m.Calls++
		call(t, c, "write_file", map[string]any{"path": b, "content": "package p\nvar Token = 2\n"})
		m.Calls++
		call(t, c, "project_replace", map[string]any{
			"path": dir, "find": "Token", "replace": "Flag", "file_types": ".go", "force": true,
		})
		ab, _ := os.ReadFile(a)
		bb, _ := os.ReadFile(b)
		if !strings.Contains(string(ab), "Flag") || !strings.Contains(string(bb), "Flag") {
			m.WrongMods++
			t.Fatalf("refactor a=%q b=%q", ab, bb)
		}
		if strings.Contains(string(ab), "Token") || strings.Contains(string(bb), "Token") {
			m.WrongMods++
			t.Fatalf("old token remains a=%q b=%q", ab, bb)
		}
	}))

	all = append(all, e6Task(t, "stale_hash_recovery", func(m *e6Metrics) {
		path := filepath.Join(dir, "occ.txt")
		m.Calls++
		call(t, c, "write_file", map[string]any{"path": path, "content": "v1\n"})
		m.Calls++
		res := call(t, c, "read_file", map[string]any{"path": path})
		sc, _ := res.StructuredContent.(map[string]any)
		hash, _ := sc["content_hash"].(string)
		if hash == "" {
			t.Fatal("no content_hash")
		}
		if err := os.WriteFile(path, []byte("v1-ext\n"), 0644); err != nil {
			t.Fatal(err)
		}
		m.Calls++
		_ = callErr(t, c, "edit_file", map[string]any{
			"path": path, "old_text": "v1", "new_text": "v2", "expected_hash": hash,
		})
		m.Conflicts++
		m.Calls++
		res = call(t, c, "read_file", map[string]any{"path": path})
		sc, _ = res.StructuredContent.(map[string]any)
		hash, _ = sc["content_hash"].(string)
		m.Calls++
		call(t, c, "edit_file", map[string]any{
			"path": path, "old_text": "v1-ext", "new_text": "v2", "expected_hash": hash,
		})
		got, _ := os.ReadFile(path)
		if string(got) != "v2\n" {
			m.WrongMods++
			t.Fatalf("recovered: %q", got)
		}
	}))

	all = append(all, e6Task(t, "concurrent_writer_reviewer", func(m *e6Metrics) {
		path := filepath.Join(dir, "race.txt")
		m.Calls++
		call(t, c, "write_file", map[string]any{"path": path, "content": "base\n"})
		m.Calls++
		res := call(t, c, "read_file", map[string]any{"path": path})
		sc, _ := res.StructuredContent.(map[string]any)
		hash, _ := sc["content_hash"].(string)
		m.Calls++
		call(t, c, "edit_file", map[string]any{"path": path, "old_text": "base", "new_text": "writer"})
		m.Calls++
		_ = callErr(t, c, "edit_file", map[string]any{
			"path": path, "old_text": "base", "new_text": "reviewer", "expected_hash": hash,
		})
		m.Conflicts++
		got, _ := os.ReadFile(path)
		if string(got) != "writer\n" {
			m.WrongMods++
			t.Fatalf("lost write: %q", got)
		}
	}))

	all = append(all, e6Task(t, "resume_lost_response", func(m *e6Metrics) {
		src := filepath.Join(dir, "retry-src.txt")
		dst := filepath.Join(dir, "retry-dst.txt")
		if err := os.WriteFile(src, []byte("first\nsecond\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, []byte("prefix\n"), 0644); err != nil {
			t.Fatal(err)
		}
		ops := []map[string]any{{
			"type": "extract", "source": src, "destination": dst,
			"start_line": 1, "end_line": 1, "append": true,
		}}
		m.Calls++
		probe := callErr(t, c, "batch_operations", map[string]any{
			"request_json": e3JSON(t, map[string]any{
				"retry_contract": "e3-v1", "operation_id": "probe", "atomic": true, "operations": ops,
			}),
		})
		pre := e3RetryPrefixRe.FindStringSubmatch(probe)
		if len(pre) != 2 {
			t.Fatalf("no retry prefix:\n%s", probe)
		}
		payload := e3JSON(t, map[string]any{
			"retry_contract": "e3-v1", "operation_id": pre[1] + ":e6-once", "atomic": true, "operations": ops,
		})
		for i := 0; i < 2; i++ {
			m.Calls++
			call(t, c, "batch_operations", map[string]any{"request_json": payload})
			if i == 1 {
				m.Retries++
			}
		}
		gotSrc, _ := os.ReadFile(src)
		gotDst, _ := os.ReadFile(dst)
		if string(gotSrc) != "second\n" || string(gotDst) != "prefix\nfirst\n" {
			m.WrongMods++
			t.Fatalf("retry replay mutated twice src=%q dst=%q", gotSrc, gotDst)
		}
	}))

	all = append(all, e6Task(t, "large_repo_search", func(m *e6Metrics) {
		root := filepath.Join(dir, "big")
		if err := os.MkdirAll(root, 0755); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 40; i++ {
			name := filepath.Join(root, "f"+strconv.Itoa(i)+".go")
			body := "package p\n"
			if i%7 == 0 {
				body += "func Marker() {}\n"
			}
			if err := os.WriteFile(name, []byte(body), 0644); err != nil {
				t.Fatal(err)
			}
		}
		m.Calls++
		res := call(t, c, "search_files", map[string]any{
			"path": root, "pattern": "Marker", "include_content": true, "file_types": ".go", "output_format": "json",
		})
		sc, _ := res.StructuredContent.(map[string]any)
		n := 0
		switch v := sc["match_count"].(type) {
		case int:
			n = v
		case float64:
			n = int(v)
		}
		if n == 0 {
			t.Fatalf("expected Marker hits: %#v", sc)
		}
	}))

	raw, _ := json.Marshal(all)
	t.Logf("e6_eval_metrics %s", raw)
	for _, m := range all {
		if !m.Success || m.WrongMods > 0 {
			t.Errorf("%s failed success=%v wrong=%d", m.Task, m.Success, m.WrongMods)
		}
	}
}

func e6Task(t *testing.T, name string, fn func(*e6Metrics)) e6Metrics {
	t.Helper()
	m := e6Metrics{Task: name}
	start := time.Now()
	fn(&m)
	m.Millis = time.Since(start).Milliseconds()
	m.Success = m.WrongMods == 0
	return m
}
