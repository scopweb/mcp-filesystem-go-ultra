// Temporary MCP package - will be replaced with proper SDK
package mcp

import "context"

// Temporary MCP types for compilation
type CallToolRequest struct {
	Arguments map[string]interface{} `json:"arguments"`
}

type CallToolResponse struct {
	Content []TextContent `json:"content"`
}

type TextContent struct {
	Text string `json:"text"`
}

type CallToolParams struct {
	Arguments map[string]interface{} `json:"arguments"`
}

type CallToolResult struct {
	Content []Content `json:"content"`
}

type Content interface {
	// Content marker interface
}

type ServerSession struct {
	// Placeholder for server session
}

type Tool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type Server struct {
	// Placeholder for server
}

type Implementation struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type ServerOptions struct {
	// Placeholder for server options
}

type Transport interface {
	// Transport interface
}

type StdioTransport struct {
	// Placeholder for stdio transport
}

// Temporary functions
func NewServer(impl *Implementation, opts *ServerOptions) *Server {
	return &Server{}
}

func NewStdioTransport() *StdioTransport {
	return &StdioTransport{}
}

func (s *Server) Connect(ctx context.Context, transport Transport) (*ServerSession, error) {
	return &ServerSession{}, nil
}

func AddTool(s *Server, t *Tool, h func(context.Context, *ServerSession, *CallToolParams) (*CallToolResult, error)) {
	// Placeholder implementation
}

// Make TextContent implement Content interface
func (tc *TextContent) content() {}
