package mcpserver

import (
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mcp/filesystem-ultra/core"
)

// finishRead applies --response-mode. compat keeps the historical text fallback.
// structured keeps each file body once: single-file in structured content,
// batch in files[].content. The text fallback becomes a short summary.
func finishRead(engine *core.UltraFastEngine, payload map[string]any, fullText, summary string, batch bool) (map[string]any, string) {
	if engine == nil || !engine.StructuredResponses() || summary == "" {
		return payload, fullText
	}
	if payload == nil {
		payload = map[string]any{}
	}
	payload["presentation"] = core.ResponseModeStructured
	if batch {
		payload["content"] = summary
	}
	return payload, summary
}

// structuredBodyNotice is the recovery hint for clients that never render
// structuredContent. The body itself stays out of the text block: the summary
// only says where it is and how to get it back.
const structuredBodyNotice = "if your client does not show structuredContent, start the server with --response-mode=compat and restart"

func readSummary(path, hash string, lines int, truncated bool) string {
	flag := ""
	if truncated {
		flag = " truncated"
	}
	if hash == "" {
		return fmt.Sprintf("read %s | %d lines%s | body in structuredContent, not in this text; %s", path, lines, flag, structuredBodyNotice)
	}
	return fmt.Sprintf("read %s | %d lines%s | %s | body in structuredContent, not in this text; %s", path, lines, flag, hash, structuredBodyNotice)
}

func batchReadSummary(files []map[string]any, truncated bool) string {
	ok, failed := 0, 0
	for _, f := range files {
		if _, hasErr := f["error"]; hasErr {
			failed++
		} else {
			ok++
		}
	}
	flag := ""
	if truncated {
		flag = " truncated"
	}
	return fmt.Sprintf("read %d files | ok=%d fail=%d%s | bodies in files[].content of structuredContent, not in this text; %s", len(files), ok, failed, flag, structuredBodyNotice)
}

func presentRead(engine *core.UltraFastEngine, payload map[string]any, fullText, summary string, batch, ok bool) *mcp.CallToolResult {
	payload, text := finishRead(engine, payload, fullText, summary, batch)
	return structuredOrError(ok, payload, text)
}
