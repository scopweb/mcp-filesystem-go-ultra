package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mcp/filesystem-ultra/core"
)

const (
	inspectJSONInputLimit = 16 << 20
	inspectOutputLimit    = 64 << 10
	inspectDumpLimit      = 4096
	inspectScanDefault    = 1 << 20
	inspectScanMaximum    = 16 << 20
	inspectHashPaths      = 64
)

func registerInspectTools(reg *toolRegistry) {
	detail := func() mcp.ToolOption {
		return mcp.WithString("detail", mcp.Enum("summary", "normal", "full"), mcp.Description("Presentation detail; limits always apply. Default normal."))
	}
	common := func(name, description string) []mcp.ToolOption {
		return []mcp.ToolOption{mcp.WithDescription(description), mcp.WithReadOnlyHintAnnotation(true), mcp.WithDestructiveHintAnnotation(false), mcp.WithIdempotentHintAnnotation(true), mcp.WithOpenWorldHintAnnotation(false), detail()}
	}
	options := common("json_query", "json_query — Query UTF-8 JSON with standard JMESPath (not jq). Ultra only. Arrays are paginated after evaluation; default 100, max 1000 items. 16 MiB input, bounded query complexity, 64 KiB response. continuation contains next-call arguments including expected_hash. Related: read_file, file_bytes.")
	options = append(options,
		mcp.WithString("path", mcp.Required(), mcp.Description("JSON file inside allowed paths")),
		mcp.WithString("query", mcp.Description("Standard JMESPath expression; omitted or empty selects the document")),
		mcp.WithString("output_format", mcp.Enum("json", "table", "csv", "text")),
		mcp.WithArray("columns", mcp.WithStringItems(), mcp.Description("Ordered dot-separated field paths for table/csv")),
		mcp.WithNumber("limit", mcp.Description("Array page size: 1..1000, default 100")),
		mcp.WithNumber("offset", mcp.Description("0-based result offset; non-arrays require 0")),
		mcp.WithString("expected_hash", mcp.Description("OCC token from the previous response; included in continuation")),
	)
	reg.addTool(mcp.NewTool("json_query", options...), auditWrap(reg.engine, "json_query", func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return inspectJSONQuery(ctx, reg.engine, req.GetArguments()), nil
	}), `json_query(path:"C:/repo/lot.json", query:"problemes[?mena == 'text']", limit:100)`)
	options = common("file_bytes", "file_bytes — Inspect BOM, encoding, EOL, Unicode/invisible characters and raw hex bytes. Ultra only. Diagnostic prefix defaults to 1 MiB; dump defaults to 512 bytes (max 4096). Statistics explicitly report partial scans. Byte offsets are 0-based; lines/Unicode columns 1-based. Related: read_file, file_hash.")
	options = append(options,
		mcp.WithString("path", mcp.Required()),
		mcp.WithString("mode", mcp.Enum("encoding", "bytes", "all"), mcp.Description("Default encoding")),
		mcp.WithNumber("start_line"), mcp.WithNumber("end_line"), mcp.WithNumber("byte_offset"),
		mcp.WithNumber("end_byte_offset", mcp.Description("Exclusive byte-range end; continuation uses it to preserve a selected line range")),
		mcp.WithNumber("max_bytes", mcp.Description("Dump bytes: 1..4096, default 512")),
		mcp.WithNumber("max_scan_bytes", mcp.Description("Diagnostic prefix bytes: default 1048576, max controlled by --max-inspect-scan-bytes")),
	)
	reg.addTool(mcp.NewTool("file_bytes", options...), auditWrap(reg.engine, "file_bytes", func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return inspectFileBytes(ctx, reg.engine, req.GetArguments()), nil
	}), `file_bytes(path:"C:/repo/file.cs", mode:"all", max_bytes:512)`)
	options = common("file_hash", "file_hash — Streaming SHA-256 of original file bytes (BOM/EOL included). Ultra only. path or paths (max 64); compare:true requires two paths and directly compares bytes, reporting the first difference. Detected concurrent changes fail the whole operation. SHA-256 is not the read_file OCC token. Related: diff_files, file_bytes.")
	options = append(options, mcp.WithString("path"), mcp.WithArray("paths", mcp.WithStringItems()), mcp.WithBoolean("compare"))
	reg.addTool(mcp.NewTool("file_hash", options...), auditWrap(reg.engine, "file_hash", func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return inspectFileHash(ctx, reg.engine, req.GetArguments()), nil
	}), `file_hash(paths:["C:/repo/baseline.txt","C:/repo/generated.txt"], compare:true)`)
}

func inspectFailure(code, path, message string) *mcp.CallToolResult {
	return pathErrorResult(code, message, path, nil, "Correct the arguments or inspect the file before retrying")
}

func inspectInt(args map[string]any, key string, fallback, minimum, maximum int) (int, error) {
	raw, ok := args[key]
	if !ok {
		return fallback, nil
	}
	// Requests decoded by MCP use float64. Tests and internal callers may use integers.
	var n int
	switch v := raw.(type) {
	case float64:
		if v < float64(minimum) || v > float64(maximum) || v != float64(int(v)) {
			return 0, fmt.Errorf("%s must be an integer in %d..%d", key, minimum, maximum)
		}
		n = int(v)
	case int:
		n = v
	default:
		return 0, fmt.Errorf("%s must be an integer", key)
	}
	if n < minimum || n > maximum {
		return 0, fmt.Errorf("%s must be in %d..%d", key, minimum, maximum)
	}
	return n, nil
}

func inspectEnum(args map[string]any, key, fallback string, values ...string) (string, error) {
	v, present := args[key]
	if !present {
		return fallback, nil
	}
	s, ok := v.(string)
	if ok {
		for _, allowed := range values {
			if s == allowed {
				return s, nil
			}
		}
	}
	return "", fmt.Errorf("%s must be one of %s", key, strings.Join(values, ", "))
}

func inspectStrings(args map[string]any, key string, maximum int) ([]string, error) {
	raw, present := args[key]
	if !present {
		return nil, nil
	}
	var values []string
	switch v := raw.(type) {
	case []string:
		values = append([]string{}, v...)
	case []any:
		values = make([]string, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("%s must contain strings", key)
			}
			values = append(values, s)
		}
	default:
		return nil, fmt.Errorf("%s must be a native string array", key)
	}
	if len(values) == 0 || len(values) > maximum {
		return nil, fmt.Errorf("%s must have 1..%d items", key, maximum)
	}
	for _, s := range values {
		if strings.TrimSpace(s) == "" {
			return nil, fmt.Errorf("%s contains an empty value", key)
		}
	}
	return values, nil
}

// inspectOpen applies content authorization to both logical and canonical names.
// A second resolution plus SameFile check narrows the path-swap window, following
// the engine's existing path model (not an OS snapshot or a filesystem lock).
func inspectOpen(ctx context.Context, engine *core.UltraFastEngine, path string) (*os.File, string, *mcp.CallToolResult) {
	if err := ctx.Err(); err != nil {
		return nil, path, inspectFailure("CANCELLED", path, err.Error())
	}
	if strings.TrimSpace(path) == "" {
		return nil, path, validationResult("path is required", "path", "non-empty string")
	}
	path = core.NormalizePath(path)
	if denied := enforcePolicy(engine, core.OpRead, path); denied != nil {
		return nil, path, denied
	}
	if core.IsSecretPath(path) && !engine.AllowSecrets() {
		return nil, path, pathErrorResult(errCodeSecretDenied, "secret file content is denied", path, nil, "pass --allow-secrets to read")
	}
	if !engine.IsPathAllowed(path) {
		return nil, path, notAllowedResult(engine, path)
	}
	canonical, err := engine.ResolveAndAuthorize("inspect", path)
	if err != nil {
		return nil, path, mcp.NewToolResultError(formatToolError(err))
	}
	if denied := enforcePolicy(engine, core.OpRead, canonical); denied != nil {
		return nil, path, denied
	}
	if core.IsSecretPath(canonical) && !engine.AllowSecrets() {
		return nil, path, pathErrorResult(errCodeSecretDenied, "secret file content is denied", path, nil, "pass --allow-secrets to read")
	}
	before, err := os.Stat(canonical)
	if err != nil {
		return nil, path, inspectIOError(path, err)
	}
	if !before.Mode().IsRegular() {
		return nil, path, inspectFailure("VALIDATION", path, "expected a regular file")
	}
	f, err := os.Open(canonical)
	if err != nil {
		return nil, path, inspectIOError(path, err)
	}
	after, err := f.Stat()
	resolved, resolveErr := engine.ResolveAndAuthorize("inspect", path)
	if err != nil || resolveErr != nil || resolved != canonical || !os.SameFile(before, after) {
		f.Close()
		return nil, path, inspectFailure("FILE_CHANGED", path, "file changed while opening")
	}
	return f, resolveAbsForResponse(canonical), nil
}

func inspectIOError(path string, err error) *mcp.CallToolResult {
	if errors.Is(err, os.ErrNotExist) {
		return pathErrorResult(errCodeNotFound, err.Error(), path, nil, filesystemMismatchHint)
	}
	return inspectFailure("IO_ERROR", path, err.Error())
}

type inspectContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r inspectContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if len(p) > core.DefaultBufferSize {
		p = p[:core.DefaultBufferSize]
	}
	return r.reader.Read(p)
}

func inspectStable(f *os.File, before os.FileInfo, path string) error {
	after, err := f.Stat()
	if err != nil {
		return err
	}
	named, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !os.SameFile(before, after) || !os.SameFile(after, named) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return fmt.Errorf("file changed during inspection")
	}
	return nil
}

func inspectResponseLimit(engine *core.UltraFastEngine) int {
	limit := inspectOutputLimit
	if n := engine.GetConfig().MaxResponseSize; n > 0 && n < int64(limit) {
		limit = int(n)
	}
	return limit
}

// Measure the actual MCP response, including text duplication in compat mode.
func inspectResponse(engine *core.UltraFastEngine, payload map[string]any, body, summary, detail string) (*mcp.CallToolResult, bool) {
	text := body
	if detail == "summary" {
		text = summary
	}
	payload, text = finishRead(engine, payload, text, summary, false)
	result := mcp.NewToolResultStructured(payload, text)
	encoded, err := json.Marshal(result)
	return result, err == nil && len(encoded) <= inspectResponseLimit(engine)
}
