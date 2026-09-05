package main

import (
	"context"
	"fmt"
	"os"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/mcp/filesystem-ultra/core"
)

func registerFileResources(s *server.MCPServer, engine *core.UltraFastEngine) {
	tmpl := mcp.NewResourceTemplate("file:///{+path}", "host-file",
		mcp.WithTemplateDescription("Read a file under the allowed sandbox roots (file:// URI)."),
	)
	s.AddResourceTemplate(tmpl, func(_ context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
		uri := req.Params.URI
		path, err := core.FileURIToPath(uri)
		if err != nil {
			return nil, err
		}
		path = core.NormalizePath(path)
		if !engine.IsPathAllowed(path) {
			return nil, fmt.Errorf("access denied: %s", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return []mcp.ResourceContents{
			mcp.TextResourceContents{URI: uri, Text: string(data)},
		}, nil
	})
	core.AllowlistChangedHook = func() {
		s.SendNotificationToAllClients(mcp.MethodNotificationResourcesListChanged, nil)
	}
}
