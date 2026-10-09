package mcpserver

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
	"unicode/utf8"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/mcp/filesystem-ultra/core"
)

func registerFileResources(s *server.MCPServer, engine *core.UltraFastEngine) {
	tmpl := mcp.NewResourceTemplate("file:///{+path}", "host-file",
		mcp.WithTemplateDescription("Read a file under the allowed sandbox roots (file:// URI)."),
	)
	s.AddResourceTemplate(tmpl, func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
		return readFileResource(ctx, engine, req.Params.URI)
	})
	core.AllowlistChangedHook = func() {
		s.SendNotificationToAllClients(mcp.MethodNotificationResourcesListChanged, nil)
	}
}

func readFileResource(ctx context.Context, engine *core.UltraFastEngine, uri string) (out []mcp.ResourceContents, err error) {
	start := time.Now()
	entry := core.AuditEntry{
		Timestamp: start,
		Tool:      "resource:file",
		SessionID: engine.CurrentSessionID(),
		Status:    "error",
	}
	defer func() {
		entry.DurationMs = time.Since(start).Milliseconds()
		if err != nil {
			entry.Error = err.Error()
		}
		engine.Audit(entry)
	}()
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	path, err := core.FileURIToPath(uri)
	if err != nil {
		return nil, err
	}
	path = core.NormalizePath(path)
	entry.Path = path
	if !engine.IsPathAllowed(path) {
		return nil, fmt.Errorf("access denied: %s", path)
	}
	if err = engine.Authorize(core.OpRead, path); err != nil {
		return nil, err
	}
	resolved, err := engine.ResolveAndAuthorize("resource", path)
	if err != nil {
		return nil, err
	}
	if err = engine.Authorize(core.OpRead, resolved); err != nil {
		return nil, err
	}
	info, err := os.Lstat(resolved)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("security: refusing to read symlink resource %s", path)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("path is a directory: %s", path)
	}
	data, err := readFileLimited(ctx, resolved, core.VeryLargeFileThreshold)
	if err != nil {
		return nil, err
	}
	entry.BytesOut = int64(len(data))
	entry.FileSize = info.Size()
	if isBinaryResource(data) {
		encodedLen := base64.StdEncoding.EncodedLen(len(data))
		if int64(encodedLen) > core.VeryLargeFileThreshold {
			return nil, fmt.Errorf("binary resource exceeds response limit after base64")
		}
		entry.Status = "ok"
		return []mcp.ResourceContents{mcp.BlobResourceContents{
			URI:      uri,
			MIMEType: http.DetectContentType(data),
			Blob:     base64.StdEncoding.EncodeToString(data),
		}}, nil
	}
	entry.Status = "ok"
	return []mcp.ResourceContents{mcp.TextResourceContents{
		URI:      uri,
		MIMEType: "text/plain; charset=utf-8",
		Text:     string(data),
	}}, nil
}

func isBinaryResource(data []byte) bool {
	if !utf8.Valid(data) {
		return true
	}
	for _, b := range data {
		if b == 0 {
			return true
		}
	}
	return false
}

// readLimited stops after limit+1 bytes. A prior Stat is not the limit.
func readFileLimited(ctx context.Context, path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 32*1024)
	var n int64
	for n <= limit {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		remain := limit + 1 - n
		chunk := tmp
		if int64(len(chunk)) > remain {
			chunk = tmp[:remain]
		}
		nr, rerr := f.Read(chunk)
		n += int64(nr)
		buf = append(buf, chunk[:nr]...)
		if rerr == io.EOF {
			return buf, nil
		}
		if rerr != nil {
			return nil, rerr
		}
		if n > limit {
			return nil, fmt.Errorf("file exceeds resource limit of %d bytes", limit)
		}
	}
	return nil, fmt.Errorf("file exceeds resource limit of %d bytes", limit)
}
