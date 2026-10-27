//go:build windows

package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOsRoot_WindowsShortName(t *testing.T) {
	root := t.TempDir()
	longDir := filepath.Join(root, "LongDirectoryNameForOsRoot")
	if err := os.Mkdir(longDir, 0o755); err != nil {
		t.Fatal(err)
	}
	shortDir, ok, detail := distinctShortDir(longDir)
	if !ok {
		t.Skipf("volume did not provide a distinct 8.3 alias: %s", detail)
	}
	if err := os.WriteFile(filepath.Join(longDir, "exists.txt"), []byte("in"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("allowed-long-op-short", func(t *testing.T) {
		assertOsRootShortAlias(t, longDir, shortDir, longDir)
	})
	t.Run("allowed-short-op-long", func(t *testing.T) {
		assertOsRootShortAlias(t, shortDir, longDir, longDir)
	})
}

func assertOsRootShortAlias(t *testing.T, allowed, opDir, longDir string) {
	t.Helper()
	engine, cleanup := setupTestEngine(t)
	defer cleanup()
	engine.SetAllowedPaths([]string{allowed}, AllowedSourceCLI)

	existing := filepath.Join(opDir, "exists.txt")
	info, err := engine.statWithinRoot(existing)
	if err != nil {
		t.Fatalf("stat inside root: %v", err)
	}
	if info.Size() != 2 {
		t.Fatalf("stat size=%d", info.Size())
	}

	_, bases := engine.allowedBases()
	_, rel, err := relWithinBases(existing, bases)
	if err != nil {
		t.Fatalf("relWithinBases: %v", err)
	}
	if strings.Contains(rel, "~") {
		t.Fatalf("os.Root relative uses 8.3 alias: %q", rel)
	}

	created := filepath.Join(opDir, "created.txt")
	if err := engine.WriteFileBytes(context.Background(), created, []byte("ok")); err != nil {
		t.Fatalf("write inside root: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(longDir, "created.txt"))
	if err != nil || string(body) != "ok" {
		t.Fatalf("long path content=%q err=%v", body, err)
	}

	// Missing intermediate directories must still resolve the existing 8.3
	// ancestor before matching the allowed base (as on Windows CI runners).
	nested := filepath.Join(opDir, "MissingParent", "MixedCaseChild", "created.txt")
	if err := engine.WriteFileBytes(context.Background(), nested, []byte("nested")); err != nil {
		t.Fatalf("write under missing parents: %v", err)
	}
	body, err = os.ReadFile(filepath.Join(longDir, "MissingParent", "MixedCaseChild", "created.txt"))
	if err != nil || string(body) != "nested" {
		t.Fatalf("nested long path content=%q err=%v", body, err)
	}
	_, rel, err = relWithinBases(nested, bases)
	if err != nil {
		t.Fatalf("nested relWithinBases: %v", err)
	}
	if rel != filepath.Join("MissingParent", "MixedCaseChild", "created.txt") {
		t.Fatalf("nested relative path=%q", rel)
	}

	outside := filepath.Join(t.TempDir(), "out.txt")
	if err := os.WriteFile(outside, []byte("no"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = engine.statWithinRoot(outside)
	if err == nil || !strings.Contains(err.Error(), "path is outside os.Root bases") {
		t.Fatalf("outside stat = %v, want path is outside os.Root bases", err)
	}
}
