package core

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink not permitted in this environment: %v", err)
	}
}

func assertAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("path must not exist outside the root: %s (err=%v)", path, err)
	}
}

func TestCopyFile_DanglingSymlinkDestDoesNotEscape(t *testing.T) {
	engine, cleanup := setupTestEngine(t)
	defer cleanup()
	ctx := context.Background()
	allowed := engine.config.AllowedPaths[0]
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "copy.txt")

	src := createTestFile(t, allowed, "src.txt", "secret")
	link := filepath.Join(allowed, "evil")
	symlinkOrSkip(t, outsideFile, link)

	err := engine.CopyFile(ctx, src, link)
	if err == nil {
		t.Fatal("CopyFile through a dangling symlink must fail")
	}
	assertAbsent(t, outsideFile)
}

func TestCopyFile_SymlinkDestInsideDoesNotFollow(t *testing.T) {
	engine, cleanup := setupTestEngine(t)
	defer cleanup()
	ctx := context.Background()
	allowed := engine.config.AllowedPaths[0]

	src := createTestFile(t, allowed, "src.txt", "new")
	target := createTestFile(t, allowed, "real.txt", "original")
	link := filepath.Join(allowed, "alias.txt")
	symlinkOrSkip(t, target, link)

	if err := engine.CopyFile(ctx, src, link); err == nil {
		t.Fatal("CopyFile must not follow an in-root symlink destination")
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "original" {
		t.Fatalf("in-root target was modified: %q", got)
	}
}

func TestCopyFile_SymlinkDestOutsideExistingUnchanged(t *testing.T) {
	engine, cleanup := setupTestEngine(t)
	defer cleanup()
	ctx := context.Background()
	allowed := engine.config.AllowedPaths[0]
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "kept.txt")
	if err := os.WriteFile(outsideFile, []byte("kept"), 0644); err != nil {
		t.Fatal(err)
	}
	src := createTestFile(t, allowed, "src.txt", "overwrite")
	link := filepath.Join(allowed, "out-link")
	symlinkOrSkip(t, outsideFile, link)

	if err := engine.CopyFile(ctx, src, link); err == nil {
		t.Fatal("CopyFile must not follow a symlink to an outside file")
	}
	got, err := os.ReadFile(outsideFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "kept" {
		t.Fatalf("outside file was modified: %q", got)
	}
}

func TestCopyFile_IntermediateDanglingSymlinkDoesNotEscape(t *testing.T) {
	engine, cleanup := setupTestEngine(t)
	defer cleanup()
	ctx := context.Background()
	allowed := engine.config.AllowedPaths[0]
	outside := t.TempDir()
	outsideDir := filepath.Join(outside, "nested")
	link := filepath.Join(allowed, "via")
	symlinkOrSkip(t, outsideDir, link)

	src := createTestFile(t, allowed, "src.txt", "secret")
	dest := filepath.Join(link, "copy.txt")
	err := engine.CopyFile(ctx, src, dest)
	if err == nil {
		t.Fatal("CopyFile through an intermediate dangling symlink must fail")
	}
	assertAbsent(t, filepath.Join(outsideDir, "copy.txt"))
	assertAbsent(t, outsideDir)
}

func TestCopyFile_MissingAndExistingDest(t *testing.T) {
	engine, cleanup := setupTestEngine(t)
	defer cleanup()
	ctx := context.Background()
	allowed := engine.config.AllowedPaths[0]
	src := createTestFile(t, allowed, "src.txt", "ok")
	dest := filepath.Join(allowed, "fresh.txt")
	if err := engine.CopyFile(ctx, src, dest); err != nil {
		t.Fatalf("copy to missing dest: %v", err)
	}
	if err := engine.CopyFile(ctx, src, dest); err == nil {
		t.Fatal("copy onto an existing file must fail")
	}
}

func TestCopyDirectory_DanglingSymlinkDestDoesNotEscape(t *testing.T) {
	engine, cleanup := setupTestEngine(t)
	defer cleanup()
	ctx := context.Background()
	allowed := engine.config.AllowedPaths[0]
	outside := t.TempDir()
	outsideDir := filepath.Join(outside, "copied-dir")

	srcDir := filepath.Join(allowed, "src-dir")
	if err := os.Mkdir(srcDir, 0755); err != nil {
		t.Fatal(err)
	}
	createTestFile(t, srcDir, "a.txt", "a")
	link := filepath.Join(allowed, "dir-link")
	symlinkOrSkip(t, outsideDir, link)

	if err := engine.CopyFile(ctx, srcDir, link); err == nil {
		t.Fatal("directory copy through a dangling symlink must fail")
	}
	assertAbsent(t, outsideDir)
	assertAbsent(t, filepath.Join(outsideDir, "a.txt"))
}

func TestBatchCopy_DanglingSymlinkDestDoesNotEscape(t *testing.T) {
	engine, cleanup := setupTestEngine(t)
	defer cleanup()
	allowed := engine.config.AllowedPaths[0]
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "batch.txt")
	src := createTestFile(t, allowed, "src.txt", "secret")
	link := filepath.Join(allowed, "batch-evil")
	symlinkOrSkip(t, outsideFile, link)

	m := NewBatchOperationManager(t.TempDir(), 5)
	m.SetEngine(engine)
	result := m.ExecuteBatch(BatchRequest{Operations: []FileOperation{{
		Type: "copy", Source: src, Destination: link,
	}}})
	if result.Success {
		t.Fatal("batch copy through a dangling symlink must fail")
	}
	assertAbsent(t, outsideFile)
}

func TestPipelineCopy_DanglingSymlinkDestDoesNotEscape(t *testing.T) {
	engine, cleanup := setupTestEngine(t)
	defer cleanup()
	allowed := engine.config.AllowedPaths[0]
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "piped.txt")
	src := createTestFile(t, allowed, "held.txt", "secret")
	destDir := filepath.Join(allowed, "out")
	if err := os.Mkdir(destDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Pipeline joins destination + basename, so the symlink is that joined path.
	symlinkOrSkip(t, outsideFile, filepath.Join(destDir, "held.txt"))

	result, err := NewPipelineExecutor(engine).Execute(context.Background(), PipelineRequest{
		Name: "copy-symlink",
		Steps: []PipelineStep{{
			ID:     "copy",
			Action: "copy",
			Params: map[string]interface{}{"files": []string{src}, "destination": destDir},
		}},
	})
	if err == nil && result != nil && result.Success {
		t.Fatal("pipeline copy through a dangling symlink must fail")
	}
	assertAbsent(t, outsideFile)
}
