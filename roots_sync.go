package main

import (
	"context"
	"log"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/mcp/filesystem-ultra/core"
)

type rootsSync struct {
	server *server.MCPServer
	engine *core.UltraFastEngine
	mode   core.RootsMode
	cli    []string
}

func (r *rootsSync) applyFromClient(ctx context.Context) {
	if r.mode == core.RootsIgnore {
		return
	}
	res, err := r.server.RequestRoots(ctx, mcp.ListRootsRequest{})
	if err != nil {
		log.Printf("roots/list skipped: %v", err)
		return
	}
	clientPaths := make([]string, 0, len(res.Roots))
	for _, root := range res.Roots {
		p, err := core.FileURIToPath(root.URI)
		if err != nil {
			log.Printf("skipping root URI %q: %v", root.URI, err)
			continue
		}
		clientPaths = append(clientPaths, core.NormalizePath(p))
	}
	merged, source := core.MergeAllowedPaths(r.cli, clientPaths, r.mode)
	r.engine.SetAllowedPaths(merged, source)
	log.Printf("sandbox roots applied source=%s count=%d", source, len(merged))
}

func registerRootsSync(s *server.MCPServer, engine *core.UltraFastEngine, cli []string, mode core.RootsMode) {
	syncer := &rootsSync{server: s, engine: engine, mode: mode, cli: append([]string(nil), cli...)}
	s.AddNotificationHandler("notifications/initialized", func(ctx context.Context, _ mcp.JSONRPCNotification) {
		syncer.applyFromClient(ctx)
	})
	s.AddNotificationHandler(string(mcp.MethodNotificationRootsListChanged), func(ctx context.Context, _ mcp.JSONRPCNotification) {
		syncer.applyFromClient(ctx)
	})
}
