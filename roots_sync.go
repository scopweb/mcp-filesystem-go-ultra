package main

import (
	"context"
	"log"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/mcp/filesystem-ultra/core"
)

// rootsRequestTimeout caps server→client roots/list. Claude Desktop does not
// implement that method (-32601). A synchronous wait inside the initialized
// notification handler deadlocks stdio (tools/list never answered).
const rootsRequestTimeout = 2 * time.Second

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
	ctx, cancel := context.WithTimeout(ctx, rootsRequestTimeout)
	defer cancel()
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

func (r *rootsSync) applyFromClientAsync(ctx context.Context) {
	go r.applyFromClient(ctx)
}

// refreshClientRoots reconsults the MCP client's roots/list when a session is present.
var refreshClientRoots func(ctx context.Context)

func registerRootsSync(s *server.MCPServer, engine *core.UltraFastEngine, cli []string, mode core.RootsMode) {
	syncer := &rootsSync{server: s, engine: engine, mode: mode, cli: append([]string(nil), cli...)}
	refreshClientRoots = syncer.applyFromClient
	s.AddNotificationHandler("notifications/initialized", func(ctx context.Context, _ mcp.JSONRPCNotification) {
		syncer.applyFromClientAsync(ctx)
	})
	s.AddNotificationHandler(string(mcp.MethodNotificationRootsListChanged), func(ctx context.Context, _ mcp.JSONRPCNotification) {
		syncer.applyFromClientAsync(ctx)
	})
}
