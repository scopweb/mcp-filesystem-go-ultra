package mcpserver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/server"
	"github.com/mcp/filesystem-ultra/cache"
	"github.com/mcp/filesystem-ultra/core"
)

func TestAuthTrace_SchemaAndContract(t *testing.T) {
	reg := newHelpTestRegistry(t, t.TempDir())
	registered := reg.server.ListTools()
	for _, name := range []string{"write_file", "edit_file", "multi_edit", "apply_patch", "batch_operations"} {
		st, ok := registered[name]
		if !ok {
			t.Fatalf("%s missing", name)
		}
		props := st.Tool.InputSchema.Properties
		if _, ok := props["path"]; !ok && name != "batch_operations" {
			t.Fatalf("%s lost existing properties", name)
		}
		for _, key := range []string{"mandate_id", "policy_version", "strength", "ticket_id", "scope_note"} {
			raw, ok := props[key]
			if !ok {
				t.Errorf("%s missing %s", name, key)
				continue
			}
			pm, _ := raw.(map[string]any)
			if pm["type"] != "string" {
				t.Errorf("%s.%s type %v", name, key, pm["type"])
			}
		}
		strength, _ := props["strength"].(map[string]any)
		if !enumContains(strength["enum"], "explicit") || !enumContains(strength["enum"], "inferred") {
			t.Errorf("%s strength enum %#v", name, strength["enum"])
		}
		c, ok := core.Contract(name)
		if !ok {
			t.Fatalf("no contract %s", name)
		}
		if _, ok := c.Params["mandate_id"]; !ok {
			t.Errorf("contract %s missing mandate_id", name)
		}
		if len(c.Params["strength"].Enum) != 2 {
			t.Errorf("contract %s strength enum %#v", name, c.Params["strength"].Enum)
		}
	}
	help := callHelp(t, reg, map[string]interface{}{"tool": "write_file"})
	if !strings.Contains(resultText(t, help), "mandate_id") {
		t.Fatal("help(write_file) missing mandate_id")
	}
}

func enumContains(raw any, want string) bool {
	switch v := raw.(type) {
	case []string:
		for _, s := range v {
			if s == want {
				return true
			}
		}
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok && s == want {
				return true
			}
		}
	}
	return false
}

func newLoggedRegistry(t *testing.T, dir, logDir string) *toolRegistry {
	t.Helper()
	c, err := cache.NewIntelligentCache(4 * 1024 * 1024)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := core.NewUltraFastEngine(&core.Config{
		Cache: c, AllowedPaths: []string{dir}, ParallelOps: 2,
		CompactMode: true, LogDir: logDir,
	})
	if err != nil {
		c.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { engine.Close() })
	reg := &toolRegistry{
		server:         server.NewMCPServer("test", "0.0.0"),
		engine:         engine,
		handlers:       make(map[string]toolHandler),
		regexTransform: core.NewRegexTransformer(engine),
	}
	registerCoreTools(reg)
	registerBatchTools(reg)
	registerPatchTools(reg)
	registerTraceTools(reg)
	return reg
}

func readAudit(t *testing.T, logDir string) []core.AuditEntry {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(logDir, "operations.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var out []core.AuditEntry
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e core.AuditEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("audit line: %v\n%s", err, line)
		}
		out = append(out, e)
	}
	return out
}

func traceArgs() map[string]any {
	return map[string]any{
		"mandate_id":     "m-1",
		"policy_version": "pol-2026.11",
		"strength":       "explicit",
		"ticket_id":      "tkt-9",
		"scope_note":     "worker scope",
	}
}

func assertTrace(t *testing.T, m map[string]any, strength, notice string) {
	t.Helper()
	want := map[string]string{
		"mandate_id":     "m-1",
		"policy_version": "pol-2026.11",
		"strength":       strength,
		"ticket_id":      "tkt-9",
		"scope_note":     "worker scope",
	}
	for k, v := range want {
		if m[k] != v {
			t.Errorf("%s=%v want %s", k, m[k], v)
		}
	}
	if notice == "" {
		if _, ok := m["auth_notice"]; ok {
			t.Errorf("unexpected auth_notice %v", m["auth_notice"])
		}
		return
	}
	if m["auth_notice"] != notice {
		t.Errorf("auth_notice=%v", m["auth_notice"])
	}
}

func TestAuthTrace_WriteEchoesAuditsAndOmits(t *testing.T) {
	dir := t.TempDir()
	logDir := filepath.Join(dir, "logs")
	reg := newLoggedRegistry(t, dir, logDir)

	plain := filepath.Join(dir, "plain.txt")
	res := callPatchTool(t, reg, "write_file", map[string]any{"path": plain, "content": "plain"})
	if res.IsError {
		t.Fatalf("plain write: %s", resultText(t, res))
	}
	m, _ := res.StructuredContent.(map[string]any)
	for _, k := range []string{"mandate_id", "policy_version", "strength", "ticket_id", "scope_note", "auth_notice"} {
		if _, ok := m[k]; ok {
			t.Errorf("omitted write leaked %s", k)
		}
	}
	assertPayloadConforms(t, parseSchema(t, writeFileOutputSchema), m)

	traced := filepath.Join(dir, "traced.txt")
	args := traceArgs()
	args["path"] = traced
	args["content"] = "traced"
	args["strength"] = "inferred"
	res = callPatchTool(t, reg, "write_file", args)
	if res.IsError {
		t.Fatalf("traced write: %s", resultText(t, res))
	}
	m, _ = res.StructuredContent.(map[string]any)
	assertTrace(t, m, "inferred", core.AuthNoticeInferred)
	assertPayloadConforms(t, parseSchema(t, writeFileOutputSchema), m)
	raw, err := os.ReadFile(traced)
	if err != nil || string(raw) != "traced" {
		t.Fatalf("write body %q err %v", raw, err)
	}

	var final *core.AuditEntry
	for i := range readAudit(t, logDir) {
		e := readAudit(t, logDir)[i]
		if e.Tool == "write_file" && e.Status == "ok" && e.MandateID == "m-1" {
			final = &e
		}
	}
	if final == nil || final.TicketID != "tkt-9" || final.PolicyVersion != "pol-2026.11" || final.Strength != "inferred" || final.ScopeNote != "worker scope" {
		t.Fatalf("audit missing trace: %+v", final)
	}
}

func TestAuthTrace_EditDryRunAndInferredApply(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	path := filepath.Join(dir, "edit.txt")
	if err := os.WriteFile(path, []byte("alpha\n"), 0644); err != nil {
		t.Fatal(err)
	}
	args := traceArgs()
	args["path"] = path
	args["old_text"] = "alpha"
	args["new_text"] = "beta"
	args["dry_run"] = true
	args["strength"] = "inferred"
	res := callPatchTool(t, reg, "edit_file", args)
	if res.IsError {
		t.Fatalf("dry_run: %s", resultText(t, res))
	}
	m, _ := res.StructuredContent.(map[string]any)
	assertTrace(t, m, "inferred", "")
	if m["status"] != "simulated" {
		t.Fatalf("status %v", m["status"])
	}
	assertPayloadConforms(t, parseSchema(t, editFileOutputSchema), m)
	raw, _ := os.ReadFile(path)
	if string(raw) != "alpha\n" {
		t.Fatalf("dry_run wrote %q", raw)
	}

	delete(args, "dry_run")
	res = callPatchTool(t, reg, "edit_file", args)
	if res.IsError {
		t.Fatalf("apply: %s", resultText(t, res))
	}
	m, _ = res.StructuredContent.(map[string]any)
	assertTrace(t, m, "inferred", core.AuthNoticeInferredDryRun)
	assertPayloadConforms(t, parseSchema(t, editFileOutputSchema), m)
	raw, _ = os.ReadFile(path)
	if string(raw) != "beta\n" {
		t.Fatalf("apply wrote %q", raw)
	}
}

func TestAuthTrace_PatchAndBatchEcho(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	p := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(p, []byte("alpha\nbeta\n"), 0644); err != nil {
		t.Fatal(err)
	}
	patch := "--- a/f.txt\n+++ b/f.txt\n@@ -1,2 +1,2 @@\n alpha\n-beta\n+BETA\n"
	args := traceArgs()
	args["path"] = p
	args["patch"] = patch
	args["dry_run"] = true
	args["strength"] = "inferred"
	res := callPatchTool(t, reg, "apply_patch", args)
	if res.IsError {
		t.Fatalf("patch dry_run: %s", resultText(t, res))
	}
	m, _ := res.StructuredContent.(map[string]any)
	assertTrace(t, m, "inferred", "")
	assertPayloadConforms(t, parseSchema(t, applyPatchOutputSchema), m)
	raw, _ := os.ReadFile(p)
	if string(raw) != "alpha\nbeta\n" {
		t.Fatalf("patch dry_run wrote %q", raw)
	}

	out := filepath.Join(dir, "batch.txt")
	res = callPatchTool(t, reg, "batch_operations", map[string]any{
		"request": map[string]any{
			"operations": []any{map[string]any{"type": "write", "path": out, "content": "batch"}},
		},
		"ticket_id":      "tkt-9",
		"strength":       "explicit",
		"mandate_id":     "m-1",
		"policy_version": "pol-2026.11",
		"scope_note":     "worker scope",
	})
	if res.IsError {
		t.Fatalf("batch: %s", resultText(t, res))
	}
	m, _ = res.StructuredContent.(map[string]any)
	assertTrace(t, m, "explicit", "")
	assertPayloadConforms(t, parseSchema(t, batchOperationsOutputSchema), m)
	raw, err := os.ReadFile(out)
	if err != nil || string(raw) != "batch" {
		t.Fatalf("batch body %q err %v", raw, err)
	}
}

func TestAuthTrace_RejectsBeforeWrite(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	path := filepath.Join(dir, "nope.txt")
	res := callPatchTool(t, reg, "write_file", map[string]any{
		"path": path, "content": "x", "strength": "guessed",
	})
	if !res.IsError {
		t.Fatal("bad strength must be rejected")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("rejected write created a file: %v", err)
	}
	res = callPatchTool(t, reg, "read_file", map[string]any{"path": path, "mandate_id": "m"})
	if !res.IsError || !strings.Contains(resultText(t, res), "unknown parameter") {
		t.Fatalf("read_file should reject mandate_id: %s", resultText(t, res))
	}
}

func TestAuthTrace_MultiEditDryRun(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	path := filepath.Join(dir, "multi.txt")
	if err := os.WriteFile(path, []byte("one\n"), 0644); err != nil {
		t.Fatal(err)
	}
	res := callPatchTool(t, reg, "multi_edit", map[string]any{
		"path":           path,
		"edits":          []any{map[string]any{"old_text": "one", "new_text": "two"}},
		"dry_run":        true,
		"strength":       "inferred",
		"ticket_id":      "tkt-9",
		"mandate_id":     "m-1",
		"policy_version": "pol-2026.11",
		"scope_note":     "worker scope",
	})
	if res.IsError {
		t.Fatalf("multi_edit: %s", resultText(t, res))
	}
	m, _ := res.StructuredContent.(map[string]any)
	assertTrace(t, m, "inferred", "")
	assertPayloadConforms(t, parseSchema(t, multiEditOutputSchema), m)
}

func TestAuthTrace_ValidationErrorStillAuditsTrace(t *testing.T) {
	dir := t.TempDir()
	logDir := filepath.Join(dir, "logs")
	reg := newLoggedRegistry(t, dir, logDir)
	path := filepath.Join(dir, "nope.txt")
	res := callPatchTool(t, reg, "write_file", map[string]any{
		"path":           path,
		"content":        "x",
		"strength":       "guessed",
		"mandate_id":     "m-1",
		"policy_version": "pol-2026.11",
		"ticket_id":      "tkt-9",
		"scope_note":     "worker scope",
	})
	if !res.IsError {
		t.Fatal("bad strength must be rejected")
	}
	if m, ok := res.StructuredContent.(map[string]any); ok {
		if _, present := m["mandate_id"]; present {
			t.Fatal("error receipt must not echo trace")
		}
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("rejected write created a file: %v", err)
	}

	var found *core.AuditEntry
	for _, e := range readAudit(t, logDir) {
		if e.Tool == "write_file" && e.Status == "error" && strings.Contains(e.Error, "strength") {
			found = &e
		}
	}
	if found == nil || found.MandateID != "m-1" || found.TicketID != "tkt-9" || found.PolicyVersion != "pol-2026.11" || found.Strength != "guessed" || found.ScopeNote != "worker scope" {
		t.Fatalf("validation audit dropped trace: %+v", found)
	}
}

func TestAuthTrace_SessionSetOverrideAndClear(t *testing.T) {
	dir := t.TempDir()
	logDir := filepath.Join(dir, "logs")
	reg := newLoggedRegistry(t, dir, logDir)

	res := callPatchTool(t, reg, "set_trace", map[string]any{
		"mandate_id":     "m-sess",
		"policy_version": "pol-sess",
		"strength":       "explicit",
		"ticket_id":      "t-sess",
		"scope_note":     "session scope",
	})
	if res.IsError {
		t.Fatalf("set_trace: %s", resultText(t, res))
	}
	m, _ := res.StructuredContent.(map[string]any)
	if m["status"] != "set" || m["ticket_id"] != "t-sess" || m["mandate_id"] != "m-sess" {
		t.Fatalf("set receipt %#v", m)
	}
	assertPayloadConforms(t, parseSchema(t, setTraceOutputSchema), m)

	res = callPatchTool(t, reg, "set_trace", map[string]any{"strength": "guessed", "ticket_id": "nope"})
	if !res.IsError {
		t.Fatal("bad set_trace must be rejected")
	}

	plain := filepath.Join(dir, "plain.txt")
	res = callPatchTool(t, reg, "write_file", map[string]any{"path": plain, "content": "plain"})
	if res.IsError {
		t.Fatalf("session write: %s", resultText(t, res))
	}
	m, _ = res.StructuredContent.(map[string]any)
	if m["ticket_id"] != "t-sess" || m["mandate_id"] != "m-sess" || m["strength"] != "explicit" || m["policy_version"] != "pol-sess" || m["scope_note"] != "session scope" {
		t.Fatalf("session echo %#v", m)
	}
	if _, ok := m["auth_notice"]; ok {
		t.Fatalf("explicit session must not nag: %v", m["auth_notice"])
	}

	over := filepath.Join(dir, "over.txt")
	res = callPatchTool(t, reg, "write_file", map[string]any{
		"path": over, "content": "over", "ticket_id": "t-over", "strength": "inferred",
	})
	if res.IsError {
		t.Fatalf("override write: %s", resultText(t, res))
	}
	m, _ = res.StructuredContent.(map[string]any)
	if m["ticket_id"] != "t-over" || m["strength"] != "inferred" || m["mandate_id"] != "m-sess" {
		t.Fatalf("override echo %#v", m)
	}
	if m["auth_notice"] != core.AuthNoticeInferred {
		t.Fatalf("auth_notice=%v", m["auth_notice"])
	}

	res = callPatchTool(t, reg, "set_trace", map[string]any{})
	if res.IsError {
		t.Fatalf("clear: %s", resultText(t, res))
	}
	m, _ = res.StructuredContent.(map[string]any)
	if m["status"] != "cleared" {
		t.Fatalf("clear receipt %#v", m)
	}
	if _, ok := m["ticket_id"]; ok {
		t.Fatal("cleared receipt echoed ticket_id")
	}

	after := filepath.Join(dir, "after.txt")
	res = callPatchTool(t, reg, "write_file", map[string]any{"path": after, "content": "after"})
	if res.IsError {
		t.Fatalf("after clear: %s", resultText(t, res))
	}
	m, _ = res.StructuredContent.(map[string]any)
	for _, k := range []string{"mandate_id", "policy_version", "strength", "ticket_id", "scope_note", "auth_notice"} {
		if _, ok := m[k]; ok {
			t.Errorf("cleared session leaked %s", k)
		}
	}

	var sessionOK, overrideOK, clearOK bool
	for _, e := range readAudit(t, logDir) {
		if e.Tool != "write_file" || e.Status != "ok" {
			continue
		}
		switch e.TicketID {
		case "t-sess":
			sessionOK = e.MandateID == "m-sess" && e.Strength == "explicit"
		case "t-over":
			overrideOK = e.MandateID == "m-sess" && e.Strength == "inferred"
		case "":
			if strings.HasSuffix(e.Path, "after.txt") {
				clearOK = e.MandateID == "" && e.Strength == ""
			}
		}
	}
	if !sessionOK || !overrideOK || !clearOK {
		t.Fatalf("audit session=%v override=%v clear=%v", sessionOK, overrideOK, clearOK)
	}
}
