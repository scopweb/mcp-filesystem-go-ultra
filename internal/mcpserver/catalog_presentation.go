package mcpserver

import (
	"maps"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

const relatedToolsMetaKey = "filesystem-ultra/related"

// compactToolPresentation keeps operational guidance in description and schemas.
// Navigation hints belong in metadata; clients may omit them from model context.
// This does not assume that every client filters _meta out of its prompt.
func compactToolPresentation(tool mcp.Tool) mcp.Tool {
	tool.Description = strings.TrimPrefix(tool.Description, tool.Name+" — ")
	tool.Description = strings.ReplaceAll(tool.Description, " (the user's actual disk, e.g. C:\\, D:\\, /mnt/...)", "")
	if description, related, ok := strings.Cut(tool.Description, " Related: "); ok {
		// Copy metadata before adding our key; registration must not mutate a
		// caller-owned map or discard unrelated extensions/progress tokens.
		meta := mcp.Meta{}
		if tool.Meta != nil {
			meta = *tool.Meta
			meta.AdditionalFields = maps.Clone(tool.Meta.AdditionalFields)
		}
		if meta.AdditionalFields == nil {
			meta.AdditionalFields = make(map[string]any)
		}
		if _, exists := meta.AdditionalFields[relatedToolsMetaKey]; !exists {
			meta.AdditionalFields[relatedToolsMetaKey] = related
			tool.Meta = &meta
			tool.Description = description
		}
	}
	return tool
}
