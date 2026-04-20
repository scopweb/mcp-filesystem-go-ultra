package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mcp/filesystem-ultra/core"
)

// auditWrap wraps a tool handler with request normalization and audit logging.
// Normalization: applies data-driven rules (param aliases, type coercions, nested fixes).
// Audit: creates an AuditEntry, injects it into context, logs timing/status/path on completion.
func auditWrap(engine *core.UltraFastEngine, tool string, handler func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		start := time.Now()

		// Create audit entry and inject into context for sub-op annotation
		entry := &core.AuditEntry{
			Timestamp: start,
			Tool:      tool,
			SessionID: engine.CurrentSessionID(),
		}
		ctx = context.WithValue(ctx, core.AuditEntryKey{}, entry)

		// Run normalizer on arguments
		if normalizer := engine.GetNormalizer(); normalizer != nil {
			if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
				result := normalizer.Normalize(tool, args)
				if result.WasModified {
					request.Params.Arguments = result.Args
					entry.Normalizations = result.Applied
				}
			}
		}

		// Validate parameters against tool schema (after normalization)
		if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
			if validationErrs := core.ValidateToolParams(tool, args); len(validationErrs) > 0 {
				entry.DurationMs = time.Since(start).Milliseconds()
				entry.Status = "error"
				entry.Error = "parameter validation failed"
				engine.Audit(*entry)
				return mcp.NewToolResultError("Parameter validation failed:\n• " + strings.Join(validationErrs, "\n• ")), nil
			}
		}

		// Call actual handler
		res, err := handler(ctx, request)

		// Complete audit entry
		entry.DurationMs = time.Since(start).Milliseconds()
		if err != nil {
			entry.Status = "error"
			entry.Error = err.Error()
		} else if res != nil && res.IsError {
			entry.Status = "error"
			// Extract error text from result content
			if len(res.Content) > 0 {
				if tc, ok := res.Content[0].(mcp.TextContent); ok {
					entry.Error = tc.Text
				}
			}
		} else {
			entry.Status = "ok"
		}

		// Extract path and summarize args for logging
		if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
			if p, ok := args["path"].(string); ok {
				entry.Path = p
			}
			entry.Args = summarizeArgs(args)
			// Extract bytes_out from result — excluding the unified diff section
			// to keep the metric representative of actual file bytes, not response text.
			if res != nil && len(res.Content) > 0 {
				if tc, ok := res.Content[0].(mcp.TextContent); ok {
					text := tc.Text
					// Strip unified diff from byte count (starts after "\n\nDiff:\n")
					if idx := strings.Index(text, "\n\nDiff:\n"); idx >= 0 {
						entry.BytesOut = int64(idx)
					} else {
						entry.BytesOut = int64(len(text))
					}
				}
			}
		}

		// ROI: compute token estimates for savings analysis.
		// tokens_consumed = actual tokens used by this tool call (request + response).
		// tokens_baseline = what would be spent without the filesystem server (naive approach).
		// tokens_saved    = max(0, baseline - consumed).
		entry.TokensConsumed = (entry.BytesIn + entry.BytesOut) / 4
		switch tool {
		case "read_file", "read_text_file":
			if entry.FileSize > 0 {
				// Baseline: user would paste the whole file into context.
				entry.TokensBaseline = entry.FileSize / 4
			}
		case "edit_file", "multi_edit", "edit":
			if entry.FileSize > 0 {
				// Baseline: read full file + write full file back.
				entry.TokensBaseline = entry.FileSize * 2 / 4
			}
		case "search_files", "search":
			// Baseline hard to estimate; use 0 (no savings claimed).
		default:
			// write_file, list_directory, etc.: no meaningful baseline difference.
			entry.TokensBaseline = entry.TokensConsumed
		}
		if entry.TokensBaseline > entry.TokensConsumed {
			entry.TokensSaved = entry.TokensBaseline - entry.TokensConsumed
		}

		// Log the audit entry
		engine.Audit(*entry)

		return res, err
	}
}

// summarizeArgs creates a compact map of key arguments for audit logging
func summarizeArgs(args map[string]interface{}) map[string]string {
	summary := make(map[string]string)
	for k, v := range args {
		switch k {
		case "content", "edits_json", "pipeline_json", "request_json", "rename_json":
			// Skip large payloads, just note their presence
			if s, ok := v.(string); ok {
				summary[k] = fmt.Sprintf("(%d chars)", len(s))
			} else {
				summary[k] = "(present)"
			}
		default:
			s := fmt.Sprintf("%v", v)
			if len(s) > 100 {
				s = s[:100] + "..."
			}
			summary[k] = s
		}
	}
	return summary
}
