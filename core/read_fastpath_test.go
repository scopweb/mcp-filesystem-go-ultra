package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// read_fastpath_test.go — behaviour of the read path after the fast-path
// change: the path is resolved ONCE and the OCC hash is memoized with the
// cached bytes. These tests pin the properties that change must not break:
// the lock/OCC identity, the secret denylist (including through symlinks) and
// the allowlist.

// TestContentHashFNVBytesMatchesString pins that the byte-slice hash is the
// same token as the string hash: the OCC token is a public contract.
func TestContentHashFNVBytesMatchesString(t *testing.T) {
	for _, in := range []string{"", "a", "hello world", strings.Repeat("x", 100000)} {
		if got, want := contentHashFNVBytes([]byte(in)), contentHashFNV(in); got != want {
			t.Fatalf("hash mismatch for %d bytes: bytes=%s string=%s", len(in), got, want)
		}
	}
}

// TestReadFileSnapshot_CanonMatchesCanonicalPath pins the lock/OCC identity:
// the canonical path of a snapshot must stay the same value the rest of the
// engine computes, or the per-file lock would key on two different names.
func TestReadFileSnapshot_CanonMatchesCanonicalPath(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(target, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	e := newTestEngine(dir)
	t.Cleanup(func() { _ = e.Close() })

	cases := []struct {
		name string
		path string
	}{
		{"regular", target},
		{"nested", filepath.Join(dir, "sub", "..", "file.txt")},
	}
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(target, link); err == nil {
		cases = append(cases, struct {
			name string
			path string
		}{"symlink", link})
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			snap, err := e.ReadFileSnapshot(context.Background(), tc.path)
			if err != nil {
				t.Fatal(err)
			}
			if want := CanonicalPath(tc.path); snap.Canon != want {
				t.Fatalf("canon mismatch: got %q want %q", snap.Canon, want)
			}
		})
	}
}

// TestReadFileSnapshot_CachesHash pins that the OCC token is memoized with
// the bytes: a cache hit must not need to hash the file again.
func TestReadFileSnapshot_CachesHash(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "data.bin")
	content := strings.Repeat("payload\n", 5000)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	e := newTestEngine(dir)
	t.Cleanup(func() { _ = e.Close() })

	first, err := e.ReadFileSnapshot(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	want := contentHashFNVBytes([]byte(content))
	if first.Hash != want {
		t.Fatalf("hash mismatch: got %s want %s", first.Hash, want)
	}

	// The cache entry must carry the hash.
	_, meta, hit := e.cache.GetFileFreshWithMeta(p)
	if !hit {
		t.Fatal("expected a cache hit after a read")
	}
	if meta.Hash != want {
		t.Fatalf("cached hash mismatch: got %q want %q", meta.Hash, want)
	}

	// A second read is served from the cache with the same token.
	second, err := e.ReadFileSnapshot(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if second.Hash != first.Hash {
		t.Fatalf("hash changed between reads: %s vs %s", first.Hash, second.Hash)
	}
}

// TestReadFileSnapshot_SecretSymlinkDenied pins the denylist through a
// symlink: a link with an ordinary name pointing at a secret file must not
// return the secret, and a link NAMED like a secret must stay denied even
// when it points at an ordinary file.
func TestReadFileSnapshot_SecretSymlinkDenied(t *testing.T) {
	dir := t.TempDir()
	secret := filepath.Join(dir, ".env")
	if err := os.WriteFile(secret, []byte("API_KEY=supersecretvalue\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ordinary := filepath.Join(dir, "config.txt")
	if err := os.WriteFile(ordinary, []byte("not a secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e := newTestEngine(dir)
	t.Cleanup(func() { _ = e.Close() })

	linkToSecret := filepath.Join(dir, "link.txt")
	if err := os.Symlink(secret, linkToSecret); err != nil {
		t.Skipf("symlink not permitted: %v", err)
	}
	secretNamedLink := filepath.Join(dir, ".env.link")
	if err := os.Symlink(ordinary, secretNamedLink); err != nil {
		t.Skipf("symlink not permitted: %v", err)
	}

	t.Run("link-to-secret", func(t *testing.T) {
		snap, err := e.ReadFileSnapshot(context.Background(), linkToSecret)
		if err == nil {
			t.Fatalf("reading a symlink to a secret file must be denied, got %d bytes", len(snap.Bytes))
		}
		if strings.Contains(string(snap.Bytes), "supersecretvalue") {
			t.Fatal("secret content leaked")
		}
	})
	t.Run("secret-named-link", func(t *testing.T) {
		if _, err := e.ReadFileSnapshot(context.Background(), secretNamedLink); err == nil {
			t.Fatal("a link named .env must stay denied")
		}
	})
}

// TestReadFileSnapshot_LegitimateSymlinkStillReads pins that resolving once
// does not break a legitimate symlink inside the allowlist.
func TestReadFileSnapshot_LegitimateSymlinkStillReads(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.txt")
	if err := os.WriteFile(target, []byte("payload\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink not permitted: %v", err)
	}
	e := newTestEngine(dir)
	t.Cleanup(func() { _ = e.Close() })

	snap, err := e.ReadFileSnapshot(context.Background(), link)
	if err != nil {
		t.Fatalf("reading a legitimate symlink failed: %v", err)
	}
	if string(snap.Bytes) != "payload\n" {
		t.Fatalf("unexpected content: %q", snap.Bytes)
	}
}

// TestReadFileSnapshot_OutsideAllowedDenied pins the allowlist after the
// change: a file outside the roots is still refused.
func TestReadFileSnapshot_OutsideAllowedDenied(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	e := newTestEngine(root)
	t.Cleanup(func() { _ = e.Close() })

	if _, err := e.ReadFileSnapshot(context.Background(), outside); err == nil {
		t.Fatal("reading outside the allowed paths must be denied")
	}
}
