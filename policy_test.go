package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestReadOnly_BlocksWrite(t *testing.T) {
	dir := t.TempDir()
	reg := newHelpTestRegistry(t, dir)
	reg.engine.GetConfig().ReadOnly = true
	p := filepath.Join(dir, "a.txt")
	h := reg.handlers["write_file"]
	res, err := h(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{
		Name: "write_file", Arguments: map[string]any{"path": p, "content": "x"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(resultText(t, res), "READ_ONLY") {
		t.Fatalf("got %s", resultText(t, res))
	}
}

func TestSecretPath_DeniedWithoutFlag(t *testing.T) {
	dir := t.TempDir()
	env := filepath.Join(dir, ".env")
	_ = os.WriteFile(env, []byte("K=1\n"), 0644)
	engine := newHelpTestRegistry(t, dir).engine
	if engine.IsPathAllowed(env) {
		t.Fatal(".env must be denied by default")
	}
}

func TestSecretPath_AllowedWithFlag(t *testing.T) {
	dir := t.TempDir()
	env := filepath.Join(dir, ".env")
	_ = os.WriteFile(env, []byte("K=1\n"), 0644)
	reg := newHelpTestRegistry(t, dir)
	reg.engine.GetConfig().AllowSecrets = true
	if !reg.engine.IsPathAllowed(env) {
		t.Fatal("--allow-secrets should permit .env")
	}
}

func TestGetFileInfo_MIMEAndTokens(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(p, []byte("hello world"), 0644)
	reg := newHelpTestRegistry(t, dir)
	info, err := reg.engine.GetFileInfo(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(info, "token_estimate") && !strings.Contains(info, "tok") {
		t.Fatalf("missing token estimate: %s", info)
	}
	if !strings.Contains(strings.ToLower(info), "text") && !strings.Contains(info, "MIME") && !strings.Contains(info, "octet") {
		t.Fatalf("missing mime: %s", info)
	}
}

func TestGitStatus_NotMutating(t *testing.T) {
	if toolIsMutating("git", map[string]interface{}{"action": "status"}) {
		t.Fatal("git status is read-only")
	}
	if !toolIsMutating("git", map[string]interface{}{"action": "commit"}) {
		t.Fatal("git commit is mutating")
	}
	if !toolIsMutating("write_file", nil) {
		t.Fatal("write_file mutating")
	}
}
