package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/mcp/filesystem-ultra/internal/benchclock"
)

type jsonRPCMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   json.RawMessage `json:"error,omitempty"`
}

type callToolParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

type initializeParams struct {
	ClientInfo struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"clientInfo"`
}

type ProxyLogEntry struct {
	Timestamp  time.Time `json:"ts"`
	Model      string    `json:"model,omitempty"`
	Client     string    `json:"client,omitempty"`
	Tool       string    `json:"tool"`
	Path       string    `json:"path,omitempty"`
	BytesIn    int64     `json:"bytes_in"`
	BytesOut   int64     `json:"bytes_out"`
	TokensIn   int64     `json:"tokens_in"`
	TokensOut  int64     `json:"tokens_out"`
	DurationMs int64     `json:"duration_ms"`
	DurationNs int64     `json:"duration_ns"`
	Status     string    `json:"status"`
	Error      string    `json:"error,omitempty"`
	RequestID  string    `json:"request_id,omitempty"`
}

type pendingCall struct {
	entry ProxyLogEntry
	start int64
	rawID json.RawMessage
	timer *time.Timer
}

type config struct {
	model       string
	logDir      string
	callTimeout time.Duration
	idleTimeout time.Duration
	reapStale   bool
	target      []string
	childEnv    []string
}

func main() {
	model := flag.String("model", "", "Model name to tag in logs (e.g. opus-4, sonnet-4)")
	logDir := flag.String("log-dir", "", "Directory for proxy logs (required)")
	timeout := flag.Duration("timeout", 0, "Kill child after this duration of inactivity with pending requests (0 = no idle kill)")
	callTimeout := flag.Duration("call-timeout", 60*time.Second, "Per tools/call timeout; return an MCP error if the child does not respond (0 = wait forever)")
	reapStale := flag.Bool("reap-stale", true, "On start, terminate old-hash and orphaned instances of the same logical server")
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: mcp-proxy [--model NAME] [--log-dir DIR] [--timeout DURATION] [--call-timeout DURATION] [--reap-stale=false] -- <command> [args...]")
		fmt.Fprintln(os.Stderr, "  The target MCP server command follows after --")
		os.Exit(1)
	}

	if *logDir == "" {
		fmt.Fprintln(os.Stderr, "Error: --log-dir is required")
		os.Exit(1)
	}

	if args[0] == "--" {
		args = args[1:]
	}
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Error: no target command specified")
		os.Exit(1)
	}

	os.Exit(runProxy(config{
		model:       *model,
		logDir:      *logDir,
		callTimeout: *callTimeout,
		idleTimeout: *timeout,
		reapStale:   *reapStale,
		target:      args,
	}, os.Stdin, os.Stdout, os.Stderr))
}

func runProxy(cfg config, clientIn io.Reader, clientOut io.Writer, errOut io.Writer) int {
	log.SetOutput(errOut)
	log.Printf("mcp-proxy: model=%q log-dir=%q call-timeout=%v idle-timeout=%v reap-stale=%v target=%v",
		cfg.model, cfg.logDir, cfg.callTimeout, cfg.idleTimeout, cfg.reapStale, cfg.target)

	if cfg.reapStale && len(cfg.target) > 0 {
		reapStaleChildren(cfg.target[0])
	}

	logger, err := newProxyLogger(cfg.logDir)
	if err != nil {
		fmt.Fprintf(errOut, "Failed to init logger: %v\n", err)
		return 1
	}
	defer logger.Close()

	cmd, childStdin, childStdout, cleanup, err := startChild(cfg.target, errOut, cfg.childEnv)
	if err != nil {
		fmt.Fprintf(errOut, "Failed to start child: %v\n", err)
		return 1
	}
	defer cleanup()

	var mu sync.Mutex
	pending := map[string]*pendingCall{}
	timedOutIDs := map[string]struct{}{}
	detectedClient := ""
	done := make(chan struct{})
	var closeDone sync.Once
	shut := func() { closeDone.Do(func() { close(done) }) }

	var outMu sync.Mutex
	writeClient := func(line []byte) {
		outMu.Lock()
		defer outMu.Unlock()
		_, _ = clientOut.Write(line)
		_, _ = clientOut.Write([]byte("\n"))
	}

	failPending := func(status, msg string) {
		mu.Lock()
		left := make([]*pendingCall, 0, len(pending))
		for id, pc := range pending {
			if pc.timer != nil {
				pc.timer.Stop()
			}
			delete(pending, id)
			left = append(left, pc)
		}
		mu.Unlock()
		for _, pc := range left {
			pc.entry.setDuration(benchclock.Since(pc.start))
			pc.entry.Status = status
			pc.entry.Error = msg
			logger.Log(pc.entry)
			writeClient(toolErrorReply(pc.rawID, msg))
		}
	}

	onCallTimeout := func(reqID string) {
		mu.Lock()
		pc, ok := pending[reqID]
		if ok {
			delete(pending, reqID)
			timedOutIDs[reqID] = struct{}{}
		}
		mu.Unlock()
		if !ok {
			return
		}
		msg := fmt.Sprintf("proxy: %s timed out after %s (child did not respond)", pc.entry.Tool, cfg.callTimeout)
		log.Printf("mcp-proxy: %s", msg)
		pc.entry.setDuration(benchclock.Since(pc.start))
		pc.entry.Status = "timeout"
		pc.entry.Error = msg
		logger.Log(pc.entry)
		writeClient(toolErrorReply(pc.rawID, msg))
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		select {
		case <-sigChan:
			log.Printf("mcp-proxy: received signal, shutting down child")
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			shut()
		case <-done:
		}
	}()

	go func() {
		scanner := bufio.NewScanner(clientIn)
		scanner.Buffer(make([]byte, 10*1024*1024), 10*1024*1024)
		for scanner.Scan() {
			select {
			case <-done:
				return
			default:
			}

			line := scanner.Bytes()

			var msg jsonRPCMessage
			// Invalid JSON still passes through to the server for protocol errors.
			_ = json.Unmarshal(line, &msg)

			switch msg.Method {
			case "initialize":
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
						Model:     cfg.model,
						Client:    client,
						Tool:      params.Name,
						BytesIn:   int64(len(argsBytes)),
						TokensIn:  int64(len(argsBytes)) / 4,
						RequestID: reqID,
					}
					if p, ok := params.Arguments["path"].(string); ok {
						entry.Path = p
					}

					pc := &pendingCall{entry: entry, start: benchclock.Now(), rawID: append(json.RawMessage(nil), msg.ID...)}
					mu.Lock()
					pending[reqID] = pc
					if cfg.callTimeout > 0 {
						id := reqID
						pc.timer = time.AfterFunc(cfg.callTimeout, func() { onCallTimeout(id) })
					}
					mu.Unlock()
				}
			}
			// Register before forwarding so a fast child cannot outrun pending.
			if _, err := childStdin.Write(append(append([]byte(nil), line...), '\n')); err != nil {
				log.Printf("mcp-proxy: child stdin write: %v", err)
				shut()
				return
			}
		}
		if err := scanner.Err(); err != nil {
			log.Printf("mcp-proxy: stdin scanner error: %v", err)
		}
		_ = childStdin.Close()
		shut()
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if cfg.idleTimeout > 0 {
		go func() {
			ticker := time.NewTicker(cfg.idleTimeout)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					mu.Lock()
					hasPending := len(pending) > 0
					mu.Unlock()
					if hasPending {
						log.Printf("mcp-proxy: idle timeout reached (%v) with pending requests, killing child", cfg.idleTimeout)
						if cmd.Process != nil {
							_ = cmd.Process.Kill()
						}
						cancel()
						return
					}
				case <-ctx.Done():
					return
				case <-done:
					return
				}
			}
		}()
	}

	scanner := bufio.NewScanner(childStdout)
	scanner.Buffer(make([]byte, 10*1024*1024), 10*1024*1024)
	for scanner.Scan() {
		if ctx.Err() != nil {
			break
		}
		line := scanner.Bytes()
		var msg jsonRPCMessage
		if err := json.Unmarshal(line, &msg); err == nil && msg.ID != nil && msg.Method == "" {
			reqID := extractID(msg.ID)

			mu.Lock()
			if _, late := timedOutIDs[reqID]; late {
				delete(timedOutIDs, reqID)
				mu.Unlock()
				continue
			}
			pc, ok := pending[reqID]
			if ok {
				if pc.timer != nil {
					pc.timer.Stop()
				}
				delete(pending, reqID)
			}
			mu.Unlock()

			if ok {
				pc.entry.setDuration(benchclock.Since(pc.start))
				pc.entry.BytesOut = int64(len(line))
				pc.entry.TokensOut = int64(len(line)) / 4

				if len(msg.Error) > 0 && string(msg.Error) != "null" {
					pc.entry.Status = "error"
					var errObj struct {
						Message string `json:"message"`
					}
					if json.Unmarshal(msg.Error, &errObj) == nil {
						pc.entry.Error = errObj.Message
					}
				} else {
					pc.entry.Status = "ok"
					var result struct {
						IsError bool `json:"isError"`
					}
					if json.Unmarshal(msg.Result, &result) == nil && result.IsError {
						pc.entry.Status = "error"
						var resultFull struct {
							Content []struct {
								Type string `json:"type"`
								Text string `json:"text"`
							} `json:"content"`
						}
						if json.Unmarshal(msg.Result, &resultFull) == nil && len(resultFull.Content) > 0 {
							pc.entry.Error = resultFull.Content[0].Text
						}
					}
				}

				logger.Log(pc.entry)
			}
			writeClient(line)
		} else {
			writeClient(line)
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("mcp-proxy: scanner error: %v", err)
	}

	cancel()
	shut()
	failPending("error", "proxy: child closed stdout before responding")

	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	_ = cmd.Wait()
	return 0
}

func (e *ProxyLogEntry) setDuration(d time.Duration) {
	e.DurationNs = d.Nanoseconds()
	e.DurationMs = d.Milliseconds()
}

func toolErrorReply(rawID json.RawMessage, msg string) []byte {
	type content struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	payload := struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  struct {
			IsError bool      `json:"isError"`
			Content []content `json:"content"`
		} `json:"result"`
	}{
		JSONRPC: "2.0",
		ID:      rawID,
	}
	payload.Result.IsError = true
	payload.Result.Content = []content{{Type: "text", Text: msg}}
	b, err := json.Marshal(payload)
	if err != nil {
		return []byte(`{"jsonrpc":"2.0","id":null,"result":{"isError":true,"content":[{"type":"text","text":"proxy: timeout"}]}}`)
	}
	return b
}

func extractID(raw json.RawMessage) string {
	if raw == nil {
		return ""
	}
	s := strings.Trim(string(raw), `"`)
	return s
}

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

	if l.written >= 10*1024*1024 {
		l.rotate()
	}
}

func (l *proxyLogger) rotate() {
	l.file.Close()
	ts := time.Now().Format("20060102-150405")
	rotated := filepath.Join(l.logDir, fmt.Sprintf("proxy-%s.jsonl", ts))
	os.Rename(l.logPath, rotated)

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
