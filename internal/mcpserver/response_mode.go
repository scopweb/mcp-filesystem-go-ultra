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

// finishBody applies --response-mode to a result whose body is a listing, a
// set of hits or a structural summary.
//
// compat (default) keeps the historical text fallback: the text block carries
// the readable body and structuredContent carries the machine data. structured
// turns the text block into a brief summary so the body exists ONCE, inside
// structuredContent — which is what the flag promises ("brief text; bodies
// stay once in structuredContent").
//
// The summary is stored in structuredContent.message as well, so the
// invariant "message == text content block" keeps holding in every mode.
func finishBody(engine *core.UltraFastEngine, payload map[string]any, fullText, summary string) (map[string]any, string) {
	if engine == nil || !engine.StructuredResponses() || summary == "" {
		return payload, fullText
	}
	if payload == nil {
		payload = map[string]any{}
	}
	payload["presentation"] = core.ResponseModeStructured
	payload["message"] = summary
	return payload, summary
}

// listingSummary is the brief text of a directory listing when the body has
// to stay once, in structuredContent.entries.
func listingSummary(path string, count int, truncated bool) string {
	flag := ""
	if truncated {
		flag = " truncated"
	}
	return fmt.Sprintf("list %s | %d entries%s | entries in structuredContent, not in this text; %s",
		path, count, flag, structuredBodyNotice)
}

// searchBodySummary is the brief text of a search when the hits have to stay
// once, in structuredContent.matches.
func searchBodySummary(message string) string {
	if message == "" {
		return ""
	}
	return message + " | hits in structuredContent.matches, not in this text; " + structuredBodyNotice
}

// projectMapSummary is the brief text of a project map when the skeleton has
// to stay once, in structuredContent.structure and .summary.
func projectMapSummary(path string, files, dirs, entries int) string {
	return fmt.Sprintf("project_map %s | %d entries | %d files | %d dirs | skeleton in structuredContent, not in this text; %s",
		path, entries, files, dirs, structuredBodyNotice)
}
