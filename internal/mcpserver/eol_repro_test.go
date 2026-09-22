package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestEOLReproAppend(t *testing.T) {
	for _, original := range []string{"\ufeffhead\r\nkeep\r\n", "\ufeffhead\r\nkeep\ntail\r\n"} {
		t.Run(strings.ReplaceAll(original, "\n", "LF"), func(t *testing.T) {
			dir := t.TempDir()
			reg := buildEditRegistry(t, dir, false)
			path := filepath.Join(dir, "f.txt")
			if err := os.WriteFile(path, []byte(original), 0600); err != nil { t.Fatal(err) }
			var req mcp.CallToolRequest
			req.Params.Arguments = map[string]interface{}{"path": path, "mode": "append", "content": "new\nextra\n"}
			r, err := reg.writeFileHandler(context.Background(), req)
			if err != nil { t.Fatal(err) }
			if r.IsError { t.Fatal(resultText(t, r)) }
			got := fileBytes(t, path)
			want := original + "new\r\nextra\r\n"
			t.Logf("disk=%q", got)
			if string(got) != want { t.Errorf("got %q want %q", got, want) }
		})
	}
}
