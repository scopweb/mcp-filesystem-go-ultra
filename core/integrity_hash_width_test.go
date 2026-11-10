package core

import (
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The integrity hash is rendered as the first 8 characters of the CRC32. A
// CRC32 below 0x10000000 formats to 7 hex digits without padding, and the
// [:8] slices in the tool layer then panic ("slice bounds out of range [:8]
// with length 7"). The hash must always be 8 hex characters.
func TestVerifyFileIntegrityHashIsAlwaysEightChars(t *testing.T) {
	dir := t.TempDir()
	e := testEngine(t, dir)
	for i := 0; i < 10000; i++ {
		content := []byte(strings.Repeat("x", 100) + strconv.Itoa(i))
		if len(fmt.Sprintf("%x", crc32.ChecksumIEEE(content))) != 7 {
			continue
		}
		path := filepath.Join(dir, "Program.cs")
		if err := os.WriteFile(path, content, 0644); err != nil {
			t.Fatal(err)
		}
		inv := e.VerifyFileIntegrity(path, 10)
		if inv == nil {
			t.Fatal("no integrity result")
		}
		if len(inv.Hash) != 8 {
			t.Fatalf("integrity hash must be 8 hex chars, got %q (crc32=%x)", inv.Hash, crc32.ChecksumIEEE(content))
		}
		return
	}
	t.Fatal("could not construct a seven-digit CRC32 fixture")
}
