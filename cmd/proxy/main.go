package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// JSON-RPC message envelope
type jsonRPCMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   json.RawMessage `json:"error,omitempty"`
}

// CallToolParams for extracting tool name and args
type callToolParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
}

// initializeParams for extracting clientInfo from the MCP handshake
type initializeParams struct {
	ClientInfo struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"clientInfo"`
}

// ProxyLogEntry is what we write to the JSONL log
type ProxyLogEntry struct {
	Timestamp  time.Time `json:"ts"`
	Model      string    `json:"model,omitempty"`  // from --model flag (user-specified)
	Client     string    `json:"client,omitempty"` // from MCP initialize clientInfo (auto-detected)
	Tool       string    `json:"tool"`
	Path       string    `json:"path,omitempty"`
	BytesIn    int64     `json:"bytes_in"`
	BytesOut   int64     `json:"bytes_out"`
	TokensIn   int64     `json:"tokens_in"`
	TokensOut  int64     `json:"tokens_out"`
	DurationMs int64     `json:"duration_ms"`
	Status     string    `json:"status"`
	Error      string    `json:"error,omitempty"`
	RequestID  string    `json:"request_id,omitempty"`
}

// pendingCall tracks an in-flight tool call
type pendingCall struct {
	entry ProxyLogEntry
	start time.Time
}

func main() {
	model := flag.String("model", "", "Model name to tag in logs (e.g. opus-4, sonnet-4)")
	logDir := flag.String("log-dir", "", "Directory for proxy logs (required)")
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: mcp-proxy [--model NAME] [--log-dir DIR] -- <command> [args...]")
		fmt.Fprintln(os.Stderr, "  The target MCP server command follows after --")
		os.Exit(1)
	}

	if *logDir == "" {
		fmt.Fprintln(os.Stderr, "Error: --log-dir is required")
		os.Exit(1)
	}

	// Strip leading "--" separator if present
	if args[0] == "--" {
		args = args[1:]
	}
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Error: no target command specified")
		os.Exit(1)
	}

	// Initialize logger
	logger, err := newProxyLogger(*logDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to init logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Close()

	log.SetOutput(os.Stderr)
	log.Printf("mcp-proxy: model=%q log-dir=%q target=%v", *model, *logDir, args)

	// Start child process
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stderr = os.Stderr

	childStdin, err := cmd.StdinPipe()
	if err != nil {
		log.Fatalf("Failed to get child stdin: %v", err)
	}
	childStdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatalf("Failed to get child stdout: %v", err)
	}

	if err := cmd.Start(); err != nil {
		log.Fatalf("Failed to start child: %v", err)
	}

	// Track pending requests: id -> pendingCall
	var mu sync.Mutex
	pending := map[string]*pendingCall{}
	detectedClient := "" // auto-populated from MCP initialize clientInfo

	// Goroutine: relay Claude -> child (stdin), intercept requests
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		scanner.Buffer(make([]byte, 10*1024*1024), 10*1024*1024) // 10MB buffer
		for scanner.Scan() {
			line := scanner.Bytes()
			// Write to child immediately
			childStdin.Write(line)
			childStdin.Write([]byte("\n"))

			// Try to parse as JSON-RPC
			var msg jsonRPCMessage
			if err := json.Unmarshal(line, &msg); err != nil {
				continue
			}

			switch msg.Method {
			case "initialize":
				// Capture clientInfo (e.g. "Claude Desktop/0.9.2") from the MCP handshake.
				// Note: this identifies the MCP client app, NOT the model — the model is not
				// transmitted in the MCP protocol and must be set via --model flag.
				var params initializeParams
				if err := json.Unmarshal(msg.Params, &params); err == nil && params.ClientInfo.Name != "" {
					client := params.ClientInfo.Name
					if params.ClientInfo.Version != "" {
						client += "/" + params.ClientInfo.Version
					}
					mu.Lock()
					detectedClient = client
					mu.Unlock()
					log.Printf("mcp-proxy: client detected from initialize: %q", client)
				}

			case "tools/call":
				var params callToolParams
				if err := json.Unmarshal(msg.Params, &params); err == nil {
					reqID := extractID(msg.ID)
					argsBytes, _ := json.Marshal(params.Arguments)

					mu.Lock()
					client := detectedClient
					mu.Unlock()

					entry := ProxyLogEntry{
						Timestamp: time.Now(),
						Model:     *model,
						Client:    client,
						Tool:      params.Name,
						BytesIn:   int64(len(argsBytes)),
						TokensIn:  int64(len(argsBytes)) / 4,
						RequestID: reqID,
					}

					// Extract path from arguments
					if p, ok := params.Arguments["path"].(string); ok {
						entry.Path = p
					}

					mu.Lock()
					pending[reqID] = &pendingCall{entry: entry, start: time.Now()}
					mu.Unlock()
				}
			}
		}
		childStdin.Close()
	}()

	// Main goroutine: relay child -> Claude (stdout), intercept responses
	scanner := bufio.NewScanner(childStdout)
	scanner.Buffer(make([]byte, 10*1024*1024), 10*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		// Write to Claude immediately
		os.Stdout.Write(line)
		os.Stdout.Write([]byte("\n"))

		// Try to parse as JSON-RPC response
		var msg jsonRPCMessage
		if err := json.Unmarshal(line, &msg); err == nil && msg.ID != nil && msg.Method == "" {
			reqID := extractID(msg.ID)

			mu.Lock()
			pc, ok := pending[reqID]
			if ok {
				delete(pending, reqID)
			}
			mu.Unlock()

			if ok {
				pc.entry.DurationMs = time.Since(pc.start).Milliseconds()
				pc.entry.BytesOut = int64(len(line))
				pc.entry.TokensOut = int64(len(line)) / 4

				if msg.Error != nil && len(msg.Error) > 0 && string(msg.Error) != "null" {
					pc.entry.Status = "error"
					// Extract error message
					var errObj struct{ Message string `json:"message"` }
					if json.Unmarshal(msg.Error, &errObj) == nil {
						pc.entry.Error = errObj.Message
					}
				} else {
					pc.entry.Status = "ok"
					// Check if result contains isError
					var result struct{ IsError bool `json:"isError"` }
					if json.Unmarshal(msg.Result, &result) == nil && result.IsError {
						pc.entry.Status = "error"
					}
				}

				logger.Log(pc.entry)
			}
		}
	}

	// Wait for child to finish
	cmd.Wait()
}

func extractID(raw json.RawMessage) string {
	if raw == nil {
		return ""
	}
	s := strings.Trim(string(raw), `"`)
	return s
}

// proxyLogger writes JSONL log entries
type proxyLogger struct {
	mu      sync.Mutex
	file    *os.File
	logDir  string
	logPath string
	written int64
}

func newProxyLogger(logDir string) (*proxyLogger, error) {
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, err
	}
	logPath := filepath.Join(logDir, "proxy.jsonl")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, err
	}
	info, _ := f.Stat()
	written := int64(0)
	if info != nil {
		written = info.Size()
	}
	return &proxyLogger{file: f, logDir: logDir, logPath: logPath, written: written}, nil
}

func (l *proxyLogger) Log(entry ProxyLogEntry) {
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	data = append(data, '\n')

	l.mu.Lock()
	defer l.mu.Unlock()

	n, _ := l.file.Write(data)
	l.written += int64(n)

	// Rotate at 10MB
	if l.written >= 10*1024*1024 {
		l.rotate()
	}
}

func (l *proxyLogger) rotate() {
	l.file.Close()
	ts := time.Now().Format("20060102-150405")
	rotated := filepath.Join(l.logDir, fmt.Sprintf("proxy-%s.jsonl", ts))
	os.Rename(l.logPath, rotated)

	// Keep last 3 rotated files
	pattern := filepath.Join(l.logDir, "proxy-*.jsonl")
	matches, _ := filepath.Glob(pattern)
	if len(matches) > 3 {
		for i := 0; i < len(matches)-3; i++ {
			os.Remove(matches[i])
		}
	}

	f, err := os.OpenFile(l.logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		l.file = nil
		return
	}
	l.file = f
	l.written = 0
}

func (l *proxyLogger) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	return l.file.Close()
}

// flushWriter wraps stdout to ensure immediate writes
type flushWriter struct {
	w io.Writer
}

func (fw *flushWriter) Write(p []byte) (int, error) {
	n, err := fw.w.Write(p)
	if f, ok := fw.w.(*os.File); ok {
		f.Sync()
	}
	return n, err
}
