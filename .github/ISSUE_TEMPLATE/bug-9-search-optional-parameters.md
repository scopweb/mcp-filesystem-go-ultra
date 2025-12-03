# Bug #9: Optional search parameters not exposed in MCP tool definitions

**Status:** ✅ RESOLVED in v3.7.1

## Problem
The `smart_search` and `advanced_text_search` tools supported advanced optional parameters internally (in `core/search_operations.go`), but these parameters were **NOT exposed** in the MCP tool definitions. This prevented Claude Desktop from using these powerful search capabilities.

## Impact
- Claude Desktop could not perform content searches with `smart_search` (only filename matching)
- Claude Desktop could not filter searches by file type
- Claude Desktop could not use case-sensitive or whole-word search options
- Claude Desktop could not request context lines around matches
- This forced multiple tool calls and increased token usage significantly

## Symptoms
```json
// Claude Desktop could ONLY do this:
{
  "tool": "smart_search",
  "arguments": {
    "path": "./src",
    "pattern": "TODO"
  }
}
// Parameters like include_content, file_types were ignored
```

## Root Cause
In `main.go`:

### Before (Bug):
```go
// Tool definition - missing optional parameters
smartSearchTool := mcp.NewTool("smart_search",
    mcp.WithString("path", mcp.Required()),
    mcp.WithString("pattern", mcp.Required()),
    // ❌ No optional parameters exposed!
)

// Handler - parameters hardcoded
engineReq := localmcp.CallToolRequest{
    Arguments: map[string]interface{}{
        "include_content": false,      // ❌ Hardcoded
        "file_types": []interface{}{}  // ❌ Hardcoded
    }
}
```

The code in `core/search_operations.go` (lines 27-39) was ready to accept these parameters, but they were never exposed to Claude Desktop through the MCP interface.

## Solution ✅

### 1. Exposed Optional Parameters in Tool Definitions

**`smart_search` (main.go, lines 443-449):**
```go
smartSearchTool := mcp.NewTool("smart_search",
    mcp.WithDescription("Search files by name/content"),
    mcp.WithString("path", mcp.Required()),
    mcp.WithString("pattern", mcp.Required()),
    mcp.WithBoolean("include_content", mcp.Description("Include file content search (default: false)")),
    mcp.WithString("file_types", mcp.Description("Comma-separated file extensions (e.g., '.go,.txt')")),
)
```

**`advanced_text_search` (main.go, lines 493-500):**
```go
advancedTextSearchTool := mcp.NewTool("advanced_text_search",
    mcp.WithDescription("Advanced text search with context"),
    mcp.WithString("path", mcp.Required()),
    mcp.WithString("pattern", mcp.Required()),
    mcp.WithBoolean("case_sensitive", mcp.Description("Case sensitive search (default: false)")),
    mcp.WithBoolean("whole_word", mcp.Description("Match whole words only (default: false)")),
    mcp.WithBoolean("include_context", mcp.Description("Include context lines (default: false)")),
    mcp.WithNumber("context_lines", mcp.Description("Number of context lines (default: 3)")),
)
```

### 2. Updated Handlers to Extract Parameters

**`smart_search` handler:**
```go
// Extract optional parameters from request
includeContent := false
fileTypes := []interface{}{}

if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
    if ic, ok := args["include_content"].(bool); ok {
        includeContent = ic
    }
    if ft, ok := args["file_types"].(string); ok && ft != "" {
        parts := strings.Split(ft, ",")
        for _, part := range parts {
            fileTypes = append(fileTypes, strings.TrimSpace(part))
        }
    }
}
```

**`advanced_text_search` handler:**
```go
// Extract optional parameters
caseSensitive := false
wholeWord := false
includeContext := false
contextLines := 3

if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
    if cs, ok := args["case_sensitive"].(bool); ok {
        caseSensitive = cs
    }
    if ww, ok := args["whole_word"].(bool); ok {
        wholeWord = ww
    }
    if ic, ok := args["include_context"].(bool); ok {
        includeContext = ic
    }
    if cl, ok := args["context_lines"].(float64); ok {
        contextLines = int(cl)
    }
}
```

## New Capabilities

### `smart_search` - New Optional Parameters:
- ✅ `include_content` (boolean): Search within file content, not just filenames
- ✅ `file_types` (string): Filter by comma-separated extensions

**Example:**
```json
{
  "tool": "smart_search",
  "arguments": {
    "path": "./src",
    "pattern": "TODO",
    "include_content": true,
    "file_types": ".go,.js"
  }
}
```

### `advanced_text_search` - New Optional Parameters:
- ✅ `case_sensitive` (boolean): Case-sensitive search
- ✅ `whole_word` (boolean): Match whole words only
- ✅ `include_context` (boolean): Include context lines
- ✅ `context_lines` (number): Number of context lines to show

**Example:**
```json
{
  "tool": "advanced_text_search",
  "arguments": {
    "path": "./src",
    "pattern": "function",
    "case_sensitive": true,
    "whole_word": true,
    "include_context": true,
    "context_lines": 5
  }
}
```

## Benefits

### Efficiency Improvement
**Before (Bug #9):**
```
User: "Find all .go files with function 'ParseConfig'"
Claude:
  1. smart_search(pattern="ParseConfig")          # Only finds filenames
  2. list_directory to find .go files             # Separate call
  3. read_file on each .go file                   # Multiple calls
  4. Manually grep for the pattern                # Token-heavy
  → 5+ tool calls, thousands of tokens
```

**After (Bug #9 Resolved):**
```
User: "Find all .go files with function 'ParseConfig'"
Claude:
  1. smart_search(pattern="ParseConfig", include_content=true, file_types=".go")
  → 1 tool call, direct results ✅
```

### Token Reduction
- **Eliminates multiple read_file calls**: Save 90-95% tokens
- **Direct filtering**: No need to process and filter results manually
- **Targeted search**: Only searches relevant file types

## Files Modified

1. **main.go**
   - Lines 443-449: `smart_search` tool definition
   - Lines 450-481: `smart_search` handler updated
   - Lines 493-500: `advanced_text_search` tool definition  
   - Lines 501-539: `advanced_text_search` handler updated

2. **README.md**
   - Lines 542-570: Updated `smart_search` documentation
   - Lines 572-600: Updated `advanced_text_search` documentation
   - Updated version to 3.7.1

3. **CHANGELOG.md**
   - Added v3.7.1 entry with complete bug description

4. **tests/bug9_test.go** (NEW - 285 lines)
   - `TestSmartSearchWithIncludeContent`: Validates include_content parameter
   - `TestSmartSearchWithFileTypes`: Validates file_types filtering
   - `TestAdvancedTextSearchCaseSensitive`: Validates case_sensitive parameter
   - `TestAdvancedTextSearchWithContext`: Validates include_context and context_lines

5. **docs/BUG9_RESOLUTION.md** (NEW - 290 lines)
   - Complete technical documentation
   - Before/after comparisons
   - Usage examples

## Test Results ✅

All tests passing:
```
=== RUN   TestSmartSearchWithIncludeContent
--- PASS: TestSmartSearchWithIncludeContent (0.05s)
=== RUN   TestSmartSearchWithFileTypes
--- PASS: TestSmartSearchWithFileTypes (0.01s)
=== RUN   TestAdvancedTextSearchCaseSensitive
--- PASS: TestAdvancedTextSearchCaseSensitive (0.01s)
=== RUN   TestAdvancedTextSearchWithContext
--- PASS: TestAdvancedTextSearchWithContext (0.01s)
PASS
```

## Backward Compatibility ✅
- All parameters are optional with sensible defaults
- Existing code continues to work without changes
- No breaking changes

## Resolution Timeline
- **Discovered**: December 3, 2025
- **Fixed**: December 3, 2025
- **Released**: v3.7.1
- **Status**: ✅ Production Ready

## Related Issues
- Related to token optimization efforts (v3.0 - v3.7)
- Part of making Claude Desktop more efficient

## Credits
Identified through analysis of Claude Desktop usage patterns and comparison with internal capabilities of `core/search_operations.go`.
