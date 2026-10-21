package mcpserver

// project_map_test.go — contract and safety of the project_map tool.
//
// project_map is registered as a STABLE tool (explicit exception to the
// experimental policy): it must ship a complete outputSchema, work under
// --readonly and --compact-mode, never leave the allowed roots, and never
// reveal what the file-security policy hides.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/mcp/filesystem-ultra/cache"
	"github.com/mcp/filesystem-ultra/core"
)

func projectMapWrite(t *testing.T, root, name, body string) string {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

func callProjectMap(t *testing.T, reg *toolRegistry, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := callProjectMapCtx(t, reg, context.Background(), args)
	if err != nil {
		t.Fatalf("project_map: %v", err)
	}
	return res
}

// callProjectMapCtx invokes the tool through the registered MCP handler so
// the test exercises the same path a client uses.
func callProjectMapCtx(t *testing.T, reg *toolRegistry, ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	t.Helper()
	st, ok := reg.server.ListTools()["project_map"]
	if !ok {
		t.Fatal("project_map not registered")
	}
	if st.Handler == nil {
		t.Fatal("project_map has no handler")
	}
	return st.Handler(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{Name: "project_map", Arguments: args},
	})
}

func TestProjectMap_RegisteredInEveryProfile(t *testing.T) {
	for _, profile := range []toolProfile{profileStrict, profileAgent, profileUltra} {
		reg := newProfileRegistry(t, t.TempDir(), registerOpts{Profile: profile})
		st, ok := reg.server.ListTools()["project_map"]
		if !ok {
			t.Fatalf("%s profile does not register project_map", profile)
		}
		if st.Tool.RawOutputSchema == nil && st.Tool.OutputSchema.Type == "" {
			t.Fatalf("%s profile: project_map must declare an outputSchema", profile)
		}
		ann := st.Tool.Annotations
		if ann.ReadOnlyHint == nil || !*ann.ReadOnlyHint {
			t.Fatalf("%s profile: readOnlyHint must be true", profile)
		}
		if ann.DestructiveHint == nil || *ann.DestructiveHint {
			t.Fatalf("%s profile: destructiveHint must be false", profile)
		}
		if ann.IdempotentHint == nil || !*ann.IdempotentHint {
			t.Fatalf("%s profile: idempotentHint must be true", profile)
		}
	}
}

func TestProjectMap_StableNotExperimental(t *testing.T) {
	reg := newProfileRegistry(t, t.TempDir(), registerOpts{Profile: profileStrict})
	st := reg.server.ListTools()["project_map"]
	if strings.Contains(st.Tool.Description, "[EXPERIMENTAL") {
		t.Fatalf("project_map must be stable: %s", st.Tool.Description)
	}
	if _, experimental := experimentalFeatures["project_map"]; experimental {
		t.Fatal("project_map must not be listed as experimental")
	}
}

func TestProjectMap_RunsUnderStrictProfile(t *testing.T) {
	dir := t.TempDir()
	projectMapWrite(t, dir, "go.mod", "module x\n")
	projectMapWrite(t, dir, "pkg/a.go", "package pkg\n")
	reg := newProfileRegistry(t, dir, registerOpts{Profile: profileStrict})

	res := callProjectMap(t, reg, map[string]any{"path": dir})
	if res.IsError {
		t.Fatalf("project_map failed: %s", resultText(t, res))
	}
	payload := structuredPayload(t, res)
	if payload["summary_scope"] != core.ProjectMapSummaryScope {
		t.Fatalf("summary_scope=%v", payload["summary_scope"])
	}
	summary, _ := payload["summary"].(map[string]any)
	if summary == nil || summary["visited_files"] == nil {
		t.Fatalf("summary=%v", payload["summary"])
	}
}

func TestProjectMap_WorksUnderReadonly(t *testing.T) {
	dir := t.TempDir()
	projectMapWrite(t, dir, "main.txt", "hello\n")
	c, err := cache.NewIntelligentCache(4 * 1024 * 1024)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := core.NewUltraFastEngine(&core.Config{
		Cache:        c,
		AllowedPaths: []string{dir},
		ParallelOps:  2,
		ReadOnly:     true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { engine.Close() })
	s := server.NewMCPServer("test", "0.0.0")
	reg := &toolRegistry{server: s, engine: engine, handlers: map[string]toolHandler{}, profile: profileStrict}
	registerProjectMapTools(reg)

	res := callProjectMap(t, reg, map[string]any{"path": dir})
	if res.IsError {
		t.Fatalf("project_map must work under --readonly: %s", resultText(t, res))
	}
	policy, _ := structuredPayload(t, res)["policy"].(map[string]any)
	if policy == nil || policy["readonly"] != true {
		t.Fatalf("policy block=%v", policy)
	}
}

func TestProjectMap_DeniedPathIsTypedError(t *testing.T) {
	dir := t.TempDir()
	reg := newProfileRegistry(t, dir, registerOpts{Profile: profileStrict})
	outside := t.TempDir()

	res := callProjectMap(t, reg, map[string]any{"path": outside})
	if !res.IsError {
		t.Fatal("a path outside the allowed roots must fail")
	}
	text := resultText(t, res)
	if !strings.Contains(text, `"code":"NOT_ALLOWED"`) {
		t.Fatalf("error envelope missing NOT_ALLOWED: %s", text)
	}
	var env pathErrorEnvelope
	if err := json.Unmarshal([]byte(text), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Retryable {
		t.Fatal("a denied path is not retryable")
	}
	if env.Error.Suggestion == "" {
		t.Fatal("a denied path must carry a suggestion")
	}
}

func TestProjectMap_DefaultPathIsFirstAllowedRoot(t *testing.T) {
	dir := t.TempDir()
	projectMapWrite(t, dir, "main.txt", "hello\n")
	reg := newProfileRegistry(t, dir, registerOpts{Profile: profileStrict})

	res := callProjectMap(t, reg, map[string]any{})
	if res.IsError {
		t.Fatalf("project_map without path failed: %s", resultText(t, res))
	}
	payload := structuredPayload(t, res)
	roots, _ := payload["roots"].([]string)
	if len(roots) == 0 {
		t.Fatalf("roots=%v", payload["roots"])
	}
	if !strings.Contains(resultText(t, res), "main.txt") {
		t.Fatalf("text fallback lost the tree: %s", resultText(t, res))
	}
}

func TestProjectMap_HiddenPathsAreNotRevealed(t *testing.T) {
	dir := t.TempDir()
	projectMapWrite(t, dir, "main.txt", "hello\n")
	projectMapWrite(t, dir, "secrets/key.txt", "hidden-bytes\n")
	// The policy lives outside the mapped tree so the only visible file is
	// main.txt: visited_files must be 1, never 2.
	cfg := filepath.Join(t.TempDir(), "policy.json")
	if err := os.WriteFile(cfg, []byte(`{
	  "version": 1,
	  "rules": [
	    {"pattern": "**/secrets/**", "level": "hidden"}
	  ]
	}`), 0644); err != nil {
		t.Fatal(err)
	}

	reg := newProfileRegistry(t, dir, registerOpts{Profile: profileStrict})
	policy, err := core.LoadFilePolicy(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := policy.PinConfig(cfg); err != nil {
		t.Fatal(err)
	}
	reg.engine.SetFilePolicy(policy)

	res := callProjectMap(t, reg, map[string]any{"path": dir})
	if res.IsError {
		t.Fatalf("project_map failed: %s", resultText(t, res))
	}
	text := resultText(t, res)
	for _, secret := range []string{"secrets", "key.txt", "hidden-bytes"} {
		if strings.Contains(text, secret) {
			t.Fatalf("hidden path leaked into the text fallback: %s", text)
		}
	}
	raw, err := json.Marshal(structuredPayload(t, res))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"secrets", "key.txt", "hidden-bytes"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("hidden path leaked into structuredContent: %s", raw)
		}
	}
	summary, _ := structuredPayload(t, res)["summary"].(map[string]any)
	if got := summary["visited_files"]; got != 1 {
		t.Fatalf("visited_files=%v, want 1 (hidden files are not counted)", got)
	}
}

func TestProjectMap_SymlinkOutsideRootIsNotFollowed(t *testing.T) {
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0644); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	projectMapWrite(t, dir, "keep.txt", "keep\n")
	link := filepath.Join(dir, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink not permitted in this environment: %v", err)
	}
	reg := newProfileRegistry(t, dir, registerOpts{Profile: profileStrict})

	res := callProjectMap(t, reg, map[string]any{"path": dir, "depth": float64(3)})
	if res.IsError {
		t.Fatalf("project_map failed: %s", resultText(t, res))
	}
	text := resultText(t, res)
	if strings.Contains(text, "secret.txt") {
		t.Fatalf("symlink target leaked: %s", text)
	}
	raw, _ := json.Marshal(structuredPayload(t, res))
	if strings.Contains(string(raw), "secret.txt") {
		t.Fatalf("symlink target leaked into structuredContent: %s", raw)
	}
}

func TestProjectMap_OutputContract(t *testing.T) {
	dir := t.TempDir()
	projectMapWrite(t, dir, "go.mod", "module x\n")
	projectMapWrite(t, dir, "pkg/a.go", "package pkg\n")
	reg := newProfileRegistry(t, dir, registerOpts{Profile: profileStrict})

	res := callProjectMap(t, reg, map[string]any{"path": dir})
	if res.IsError {
		t.Fatalf("project_map failed: %s", resultText(t, res))
	}
	payload := structuredPayload(t, res)
	assertPayloadConforms(t, parseSchema(t, projectMapOutputSchema), payload)
	if msg, _ := payload["message"].(string); msg != textBlock(t, res) {
		t.Fatalf("structured message != text content block\nstructured: %q\ntext: %q", msg, textBlock(t, res))
	}
	if _, ok := payload["lines"]; ok {
		t.Fatal("project_map must not report line counts")
	}
	for _, key := range []string{"roots", "truncated", "limits", "summary_scope", "summary", "structure", "warnings"} {
		if _, ok := payload[key]; !ok {
			t.Errorf("payload missing %q", key)
		}
	}
}

func TestProjectMap_TruncationIsReported(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt", "c.txt", "d.txt"} {
		projectMapWrite(t, dir, name, "x")
	}
	reg := newProfileRegistry(t, dir, registerOpts{Profile: profileStrict})
	reg.engine.GetConfig().ProjectMapMaxEntries = 2

	res := callProjectMap(t, reg, map[string]any{"path": dir})
	if res.IsError {
		t.Fatalf("project_map failed: %s", resultText(t, res))
	}
	payload := structuredPayload(t, res)
	if payload["truncated"] != true {
		t.Fatalf("truncated=%v", payload["truncated"])
	}
	if payload["summary_scope"] != core.ProjectMapSummaryScope {
		t.Fatalf("summary_scope=%v", payload["summary_scope"])
	}
	limits, _ := payload["limits"].(map[string]any)
	if limits["max_entries"] != 2 {
		t.Fatalf("limits=%v", limits)
	}
}

func TestProjectMap_CancelledContextIsReported(t *testing.T) {
	dir := t.TempDir()
	projectMapWrite(t, dir, "a.txt", "x")
	reg := newProfileRegistry(t, dir, registerOpts{Profile: profileStrict})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := callProjectMapCtx(t, reg, ctx, map[string]any{"path": dir})
	if err != nil {
		t.Fatalf("project_map: %v", err)
	}
	if res.IsError {
		t.Fatalf("a cancelled walk must not fail the call: %s", resultText(t, res))
	}
	payload := structuredPayload(t, res)
	if payload["truncated"] != true {
		t.Fatalf("truncated=%v", payload["truncated"])
	}
}

func TestProjectMap_DetailSummaryTrimsStructure(t *testing.T) {
	dir := t.TempDir()
	projectMapWrite(t, dir, "top.txt", "x")
	projectMapWrite(t, dir, "deep/nested/file.txt", "x")
	reg := newProfileRegistry(t, dir, registerOpts{Profile: profileStrict})

	res := callProjectMap(t, reg, map[string]any{"path": dir, "detail": "summary"})
	if res.IsError {
		t.Fatalf("project_map failed: %s", resultText(t, res))
	}
	structure, _ := structuredPayload(t, res)["structure"].([]core.ProjectMapEntry)
	for _, e := range structure {
		if strings.Contains(strings.TrimSuffix(e.Path, "/"), "/") {
			t.Fatalf("summary detail leaked a nested path: %+v", structure)
		}
	}
}

// TestProjectMap_DetailControlsFiles pins the detail contract: summary is the
// top level, normal is the directory skeleton, full adds the files.
func TestProjectMap_DetailControlsFiles(t *testing.T) {
	dir := t.TempDir()
	projectMapWrite(t, dir, "top.txt", "x")
	projectMapWrite(t, dir, "deep/nested/file.txt", "x")
	reg := newProfileRegistry(t, dir, registerOpts{Profile: profileStrict})

	structureFor := func(detail string) []core.ProjectMapEntry {
		t.Helper()
		args := map[string]any{"path": dir}
		if detail != "" {
			args["detail"] = detail
		}
		res := callProjectMap(t, reg, args)
		if res.IsError {
			t.Fatalf("project_map(%s) failed: %s", detail, resultText(t, res))
		}
		entries, _ := structuredPayload(t, res)["structure"].([]core.ProjectMapEntry)
		return entries
	}

	hasFile := func(entries []core.ProjectMapEntry, name string) bool {
		for _, e := range entries {
			if e.Path == name {
				return true
			}
		}
		return false
	}

	normal := structureFor("normal")
	if hasFile(normal, "top.txt") {
		t.Fatalf("normal detail must not list files: %+v", normal)
	}
	if !hasFile(normal, "deep/") {
		t.Fatalf("normal detail lost the directory: %+v", normal)
	}

	full := structureFor("full")
	if !hasFile(full, "top.txt") {
		t.Fatalf("full detail must list files: %+v", full)
	}
}

func TestProjectMap_HelpDocumentsTheTool(t *testing.T) {
	reg := newProfileRegistry(t, t.TempDir(), registerOpts{Profile: profileStrict})
	text := resultText(t, callHelp(t, reg, map[string]interface{}{"tool": "project_map"}))
	for _, want := range []string{"project_map", "depth", "include_policy", "### Examples"} {
		if !strings.Contains(text, want) {
			t.Errorf("help(project_map) missing %q:\n%s", want, text)
		}
	}
}

// TestProjectMap_CompactModeOmitsEmptyStructureHeader: compact mode drops
// the file lines, so a directory with no subdirectories used to leave a
// dangling "structure:" header with nothing under it.
func TestProjectMap_CompactModeOmitsEmptyStructureHeader(t *testing.T) {
	dir := t.TempDir()
	projectMapWrite(t, dir, "only.txt", "x")
	reg := newProfileRegistry(t, dir, registerOpts{Profile: profileStrict})
	reg.engine.GetConfig().CompactMode = true

	res := callProjectMap(t, reg, map[string]any{"path": dir, "detail": "full"})
	if res.IsError {
		t.Fatalf("project_map failed: %s", resultText(t, res))
	}
	text := resultText(t, res)
	if strings.Contains(text, "structure:") {
		t.Fatalf("compact mode left an empty structure section:\n%s", text)
	}
	// The structured payload keeps the file: compact mode trims the TEXT.
	structure, _ := structuredPayload(t, res)["structure"].([]core.ProjectMapEntry)
	if len(structure) == 0 {
		t.Fatalf("compact mode dropped the structure from structuredContent: %+v", structure)
	}
}
