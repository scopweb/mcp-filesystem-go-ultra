package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mcp/filesystem-ultra/cache"
	"github.com/mcp/filesystem-ultra/core"
)

func TestReadFileResource_TextBinaryLimitAndDeny(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	cacheInstance, err := cache.NewIntelligentCache(1024 * 1024)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := core.NewUltraFastEngine(&core.Config{
		Cache:        cacheInstance,
		AllowedPaths: []string{dir},
		ParallelOps:  2,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { engine.Close() })

	textPath := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(textPath, []byte("hola"), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := readFileResource(context.Background(), engine, "file:///"+filepath.ToSlash(textPath))
	if err != nil {
		t.Fatal(err)
	}
	text, ok := got[0].(mcp.TextResourceContents)
	if !ok || text.Text != "hola" || !strings.Contains(text.MIMEType, "text/plain") {
		t.Fatalf("%#v", got[0])
	}

	binPath := filepath.Join(dir, "bin.dat")
	if err := os.WriteFile(binPath, []byte{0xff, 0xfe, 0x00, 0x01}, 0644); err != nil {
		t.Fatal(err)
	}
	got, err = readFileResource(context.Background(), engine, "file:///"+filepath.ToSlash(binPath))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got[0].(mcp.BlobResourceContents); !ok {
		t.Fatalf("binary must be a blob, got %T", got[0])
	}

	big := filepath.Join(dir, "big.bin")
	if err := os.WriteFile(big, []byte("0123456789"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := readFileLimited(context.Background(), big, 4); err == nil {
		t.Fatal("over-limit read must fail during the read")
	}
	exact, err := readFileLimited(context.Background(), big, 10)
	if err != nil || string(exact) != "0123456789" {
		t.Fatalf("exact limit: %q %v", exact, err)
	}

	outsideFile := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("nope"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := readFileResource(context.Background(), engine, "file:///"+filepath.ToSlash(outsideFile)); err == nil {
		t.Fatal("outside file must be denied")
	}
}

func TestReadLimited_Cancelled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := readFileLimited(ctx, path, 100); err == nil {
		t.Fatal("cancelled read must fail")
	}
}
