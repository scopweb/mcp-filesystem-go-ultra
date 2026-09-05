package main

import (
	"context"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/mcp/filesystem-ultra/cache"
	"github.com/mcp/filesystem-ultra/core"
)

type blockingRootsSession struct {
	id string
	ch chan mcp.JSONRPCNotification
	in ListRoots
}

type ListRoots func(ctx context.Context, request mcp.ListRootsRequest) (*mcp.ListRootsResult, error)

func (m *blockingRootsSession) SessionID() string { return m.id }
func (m *blockingRootsSession) NotificationChannel() chan<- mcp.JSONRPCNotification {
	return m.ch
}
func (m *blockingRootsSession) Initialize()       {}
func (m *blockingRootsSession) Initialized() bool { return true }
func (m *blockingRootsSession) ListRoots(ctx context.Context, request mcp.ListRootsRequest) (*mcp.ListRootsResult, error) {
	return m.in(ctx, request)
}

func TestRootsSync_InitializedDoesNotBlockStdio(t *testing.T) {
	dir := t.TempDir()
	cacheInstance, err := cache.NewIntelligentCache(4 * 1024 * 1024)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := core.NewUltraFastEngine(&core.Config{
		Cache: cacheInstance, AllowedPaths: []string{dir}, ParallelOps: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { engine.Close() })

	entered := make(chan struct{})
	s := server.NewMCPServer("test", "0.0.0", server.WithRoots())
	registerRootsSync(s, engine, []string{dir}, core.RootsReplace)

	sess := &blockingRootsSession{
		id: "block",
		ch: make(chan mcp.JSONRPCNotification, 4),
		in: func(ctx context.Context, _ mcp.ListRootsRequest) (*mcp.ListRootsResult, error) {
			close(entered)
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	ctx := s.WithContext(context.Background(), sess)

	start := time.Now()
	_ = s.HandleMessage(ctx, []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("initialized handler blocked stdio for %s", elapsed)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("roots/list was not started in the background")
	}
}

func TestApplyFromClient_TimesOut(t *testing.T) {
	dir := t.TempDir()
	cacheInstance, err := cache.NewIntelligentCache(4 * 1024 * 1024)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := core.NewUltraFastEngine(&core.Config{
		Cache: cacheInstance, AllowedPaths: []string{dir}, ParallelOps: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { engine.Close() })

	s := server.NewMCPServer("test", "0.0.0", server.WithRoots())
	syncer := &rootsSync{server: s, engine: engine, mode: core.RootsReplace, cli: []string{dir}}
	sess := &blockingRootsSession{
		id: "to",
		ch: make(chan mcp.JSONRPCNotification, 4),
		in: func(ctx context.Context, _ mcp.ListRootsRequest) (*mcp.ListRootsResult, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	ctx := s.WithContext(context.Background(), sess)
	start := time.Now()
	syncer.applyFromClient(ctx)
	elapsed := time.Since(start)
	if elapsed < rootsRequestTimeout/2 || elapsed > rootsRequestTimeout+time.Second {
		t.Fatalf("timeout elapsed %s, want ~%s", elapsed, rootsRequestTimeout)
	}
}
