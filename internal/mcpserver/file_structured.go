package mcpserver

import (
	"os"
	"path/filepath"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mcp/filesystem-ultra/core"
)

func withMessage(payload map[string]any, text string) *mcp.CallToolResult {
	payload["message"] = text
	return mcp.NewToolResultStructured(payload, text)
}

func fileInfoFields(engine *core.UltraFastEngine, path string) map[string]any {
	item := map[string]any{"path": path, "status": "ok"}
	info, err := os.Stat(path)
	if err != nil {
		return item
	}
	kind := "file"
	if info.IsDir() {
		kind = "dir"
	}
	item["name"] = info.Name()
	item["type"] = kind
	if engine.ProtectionLevel(path) >= core.LevelProtected {
		return item
	}
	item["size"] = info.Size()
	item["mode"] = info.Mode().String()
	item["modified"] = info.ModTime().UTC().Format(time.RFC3339)
	if !info.IsDir() {
		item["mime"] = mimeByExt(path)
	}
	return item
}

func mimeByExt(path string) string {
	switch filepath.Ext(path) {
	case ".txt", ".md":
		return "text/plain"
	case ".json":
		return "application/json"
	case ".go":
		return "text/x-go"
	default:
		return "application/octet-stream"
	}
}

func fileInfoSingle(engine *core.UltraFastEngine, path, text string) *mcp.CallToolResult {
	payload := fileInfoFields(engine, path)
	payload["status"] = "ok"
	return withMessage(payload, text)
}

func fileInfoBatch(engine *core.UltraFastEngine, paths []string, errs []error, text string) *mcp.CallToolResult {
	files := make([]map[string]any, 0, len(paths))
	status := "ok"
	for i, p := range paths {
		if errs[i] != nil {
			status = "partial"
			files = append(files, map[string]any{"path": p, "status": "error", "error": errs[i].Error()})
			continue
		}
		files = append(files, fileInfoFields(engine, p))
	}
	return withMessage(map[string]any{"status": status, "files": files}, text)
}

func appliedFile(text string, fields map[string]any) *mcp.CallToolResult {
	if fields == nil {
		fields = map[string]any{}
	}
	fields["status"] = "applied"
	return withMessage(fields, text)
}
