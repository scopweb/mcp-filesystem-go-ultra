package mcpserver

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mcp/filesystem-ultra/core"
)

func TestMultiEditPresentationPanicKeepsReceipt(t *testing.T) {
	result := &core.MultiEditResult{SuccessfulEdits: 1, TotalEdits: 1, BackupID: "full-undo-id"}
	res, err := func() (response *mcp.CallToolResult, responseErr error) {
		defer recoverMultiEditPresentation("x.csproj", result, &response, &responseErr)
		panic("synthetic rendering failure")
	}()
	if err != nil || res == nil || !res.IsError {
		t.Fatalf("expected presentation error receipt: %v, %v", res, err)
	}
	sc := res.StructuredContent.(map[string]any)
	body := resultText(t, res)
	if sc["status"] != "applied" || sc["backup_id"] != result.BackupID || !strings.Contains(body, "MODIFIED") || !strings.Contains(body, "UNDO:"+result.BackupID) {
		t.Fatalf("lost committed state: %v", sc)
	}
}

func TestWriteFileBase64AppendRegression(t *testing.T) {
	for _, field := range []string{"content_base64", "content"} {
		t.Run(field, func(t *testing.T) {
			dir := t.TempDir()
			reg := buildEditRegistry(t, dir, false)
			path := filepath.Join(dir, "x.bin")
			var want []byte
			for i, size := range []int{9999, 9999, 3892} {
				block := make([]byte, size)
				for j := range block {
					block[j] = byte(j*31 + i)
				}
				want = append(want, block...)
				res := callWriteFile(t, reg, map[string]any{"path": path, "encoding": "base64", "mode": "append", field: base64.StdEncoding.EncodeToString(block)})
				if res.IsError {
					t.Fatalf("append: %s", resultText(t, res))
				}
				got, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if len(got) != len(want) || sha256.Sum256(got) != sha256.Sum256(want) {
					t.Fatalf("append %d: got %d bytes, want %d with matching SHA-256", i, len(got), len(want))
				}
				sc := res.StructuredContent.(map[string]any)
				if sc["bytes_written"] != len(want) || sc["verified"] != true {
					t.Fatalf("incorrect evidence: %v", sc)
				}
				if !strings.Contains(resultText(t, res), fmt.Sprintf("appended %d B, total %d B", size, len(want))) {
					t.Fatalf("missing append sizes: %s", resultText(t, res))
				}
			}
			if len(want) != 23890 {
				t.Fatal("invalid fixture")
			}
		})
	}
}

func TestWriteFileTruncationStateRegression(t *testing.T) {
	for _, compact := range []bool{false, true} {
		t.Run(fmt.Sprint(compact), func(t *testing.T) {
			dir := t.TempDir()
			reg := buildEditRegistry(t, dir, compact)
			path := filepath.Join(dir, "Program.cs")
			before, after := bytes.Repeat([]byte("a"), 4529), strings.Repeat("b", 1350)
			if err := os.WriteFile(path, before, 0644); err != nil {
				t.Fatal(err)
			}
			res := callWriteFile(t, reg, map[string]any{"path": path, "content": after})
			body := resultText(t, res)
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if res.IsError || string(got) != after {
				t.Fatalf("expected backed-up write: %s", body)
			}
			sc := res.StructuredContent.(map[string]any)
			if strings.Contains(body+fmt.Sprint(sc["feedback"]), "BLOCKED") || !strings.Contains(body, "WARNING") || !strings.Contains(body, "WRITTEN") {
				t.Fatalf("contradictory response: %s", body)
			}
			id, _ := sc["backup_id"].(string)
			if id == "" || !strings.Contains(body, id) {
				t.Fatalf("missing backup: %v", sc)
			}
			if _, err := reg.engine.GetBackupManager().GetBackupInfo(id); err != nil {
				t.Fatalf("backup does not exist: %v", err)
			}
		})
	}
}

func TestMultiEditShortTextRegression(t *testing.T) {
	dir := t.TempDir()
	reg := newMultiEditRegistry(t, dir)
	check := func(t *testing.T, name, before, old, replacement string) {
		t.Helper()
		path := filepath.Join(dir, name+".csproj")
		if err := os.WriteFile(path, []byte(before), 0644); err != nil {
			t.Fatal(err)
		}
		edits := []any{map[string]any{"old_text": old, "new_text": replacement}}
		res := callMultiEditHandler(t, reg, path, "", map[string]any{"edits": edits, "diff_format": "full"})
		if res.IsError {
			t.Fatalf("multi_edit: %s", resultText(t, res))
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != strings.Replace(before, old, replacement, 1) {
			t.Fatalf("incorrect edit: %q", got)
		}
		sc := res.StructuredContent.(map[string]any)
		id, _ := sc["backup_id"].(string)
		if id == "" || !strings.Contains(resultText(t, res), "UNDO:"+id) {
			t.Fatalf("missing undo: %s", resultText(t, res))
		}
	}
	for lines := 1; lines <= 3; lines++ {
		for n := 1; n <= 10; n++ {
			name := fmt.Sprintf("lines%d_len%d", lines, n)
			t.Run(name, func(t *testing.T) {
				old, replacement := strings.Repeat("a", n), strings.Repeat("b", 11-n)
				check(t, name, old+strings.Repeat("\nz", lines-1), old, replacement)
			})
		}
	}
	t.Run("short_crc32", func(t *testing.T) {
		old, replacement := "<Version>0.2.0</Version>", "<Version>0.2.1</Version>"
		// A high-risk csproj edit with a seven-digit CRC32, not a short old_text.
		for i := 0; i < 10000; i++ {
			before := fmt.Sprintf("<Project><!--%s%d-->\n%s\n</Project>", strings.Repeat("x", 100), i, old)
			after := strings.Replace(before, old, replacement, 1)
			if len(fmt.Sprintf("%x", crc32.ChecksumIEEE([]byte(after)))) == 7 {
				check(t, "short_crc32", before, old, replacement)
				return
			}
		}
		t.Fatal("could not construct short CRC32 fixture")
	})
}
