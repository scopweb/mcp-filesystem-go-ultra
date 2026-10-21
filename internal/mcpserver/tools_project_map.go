package mcpserver

// tools_project_map.go — project_map: cheap structural orientation.
//
// project_map replaces the "list_directory + directory_tree + guess" round
// trips an agent spends on its first call with ONE bounded, metadata-only
// walk. It answers "what shape does this project have?", not "which files
// matter for my task" (that is context_pack / search_files).
//
// The tool is registered as a STABLE tool in every profile (strict, agent,
// ultra) and declares a full outputSchema: the payload is the interop
// contract for third-party MCP clients.

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mcp/filesystem-ultra/core"
)

// projectMapMaxTextLines caps the structure lines of the text fallback. The
// full skeleton stays in structuredContent.
const projectMapMaxTextLines = 120

// projectMapTextLevel returns how deep an entry sits below the root (1 =
// direct child of the root). It only drives the indentation of the text.
func projectMapTextLevel(path string) int {
	trimmed := strings.TrimSuffix(path, "/")
	if trimmed == "" {
		return 0
	}
	return strings.Count(trimmed, "/") + 1
}

func registerProjectMapTools(reg *toolRegistry) {
	engine := reg.engine

	tool := mcp.NewTool("project_map",
		mcp.WithTitleAnnotation("Project Map"),
		mcp.WithRawOutputSchema(projectMapOutputSchema),
		mcp.WithDescription("project_map — Cheap structural orientation for a project: directory skeleton with child counts, "+
			"file-extension histogram, largest files and key files (go.mod, README, Dockerfile, *.csproj, CI workflows). "+
			"Metadata only — never reads file contents. Bounded on entries, wall time and metadata bytes; when a limit is hit, "+
			"truncated=true and summary_scope=visited_entries. Not code selection: use search_files or context_pack for that. "+
			"path defaults to the first effective allowed root. Related: directory_tree, list_directory, search_files, read_file."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithString("path", mcp.Description("Directory to map. Default: the first effective allowed root.")),
		mcp.WithNumber("depth", mcp.Description("Levels below the root to expand. Default 2, max 3.")),
		mcp.WithString("detail", mcp.Description("summary|normal|full. summary: the top level only. normal: directories only (the cheap orientation). full: directories and files."), mcp.Enum("summary", "normal", "full")),
		mcp.WithBoolean("include_policy", mcp.Description("Include the public policy block (profile, roots_mode, auto_occ, readonly, notes). Default true.")),
	)

	reg.addTool(tool, auditWrap(engine, "project_map", func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, _ := request.Params.Arguments.(map[string]interface{})
		if args == nil {
			args = map[string]interface{}{}
		}
		return projectMap(ctx, reg, args)
	}))
}

// projectMap answers one structural orientation request.
func projectMap(ctx context.Context, reg *toolRegistry, args map[string]interface{}) (*mcp.CallToolResult, error) {
	engine := reg.engine

	detail := parseDetailArg(args)
	depth := core.ProjectMapDefaultDepth
	warnings := []string{}
	if v, ok := args["depth"].(float64); ok {
		depth = int(v)
		if depth < 1 {
			return validationResult("depth must be >= 1", "depth", "1..3"), nil
		}
		if depth > core.ProjectMapMaxDepth {
			depth = core.ProjectMapMaxDepth
			warnings = append(warnings, fmt.Sprintf("depth clamped to %d", core.ProjectMapMaxDepth))
		}
	}
	includePolicy := true
	if v, ok := args["include_policy"].(bool); ok {
		includePolicy = v
	}

	path, errRes := projectMapPath(engine, args)
	if errRes != nil {
		return errRes, nil
	}
	path = core.NormalizePath(path)
	if !engine.IsPathAllowed(path) {
		// Same envelope as every other tool: NOT_ALLOWED, retryable=false,
		// with the roots in details. A denied path never leaks content.
		return notAllowedResult(engine, path), nil
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return pathErrorResult(errCodeNotFound, "path does not exist", path, nil, filesystemMismatchHint), nil
		}
		return mcp.NewToolResultError(formatToolError(err)), nil
	}
	if !info.IsDir() {
		return validationResult("path must be a directory", "path", "directory"), nil
	}

	result, err := engine.ProjectMap(ctx, path, core.ProjectMapOpts{
		Depth:  depth,
		Detail: detail,
	})
	if err != nil {
		return mcp.NewToolResultError(formatToolError(err)), nil
	}

	result.Warnings = append(result.Warnings, warnings...)
	payload := projectMapPayload(reg, path, result, detail, includePolicy)
	text := projectMapText(engine, path, result)
	payload["message"] = text
	return mcp.NewToolResultStructured(payload, text), nil
}

// projectMapPath resolves the requested path, defaulting to the first
// effective allowed root.
func projectMapPath(engine *core.UltraFastEngine, args map[string]interface{}) (string, *mcp.CallToolResult) {
	if raw, ok := args["path"].(string); ok && strings.TrimSpace(raw) != "" {
		return raw, nil
	}
	roots := engine.ListedAllowedPaths()
	if len(roots) == 0 {
		return "", pathErrorResult(errCodeRootsEmpty,
			"no allowed roots: pass path explicitly or start the server with --allowed-paths",
			"", nil, suggestionNotAllowed)
	}
	return roots[0], nil
}

// projectMapPayload builds the structured answer.
func projectMapPayload(reg *toolRegistry, path string, res core.ProjectMapResult, detail string, includePolicy bool) map[string]any {
	byExtension := res.Summary.ByExtension
	largest := res.Summary.LargestFiles
	keyFiles := res.Summary.KeyFiles

	switch detail {
	case detailSummary:
		largest = projectMapCapFiles(largest, 3)
		keyFiles = projectMapCap(keyFiles, 10)
		byExtension = projectMapTopExtensions(byExtension, 8)
	case detailNormal:
		largest = projectMapCapFiles(largest, 10)
		keyFiles = projectMapCap(keyFiles, 20)
	}

	payload := map[string]any{
		"roots":         res.Roots,
		"truncated":     res.Truncated,
		"limits":        projectMapLimits(res.Limits),
		"summary_scope": res.SummaryScope,
		"summary": map[string]any{
			"visited_files": res.Summary.VisitedFiles,
			"visited_dirs":  res.Summary.VisitedDirs,
			"by_extension":  byExtension,
			"largest_files": largest,
			"key_files":     keyFiles,
		},
		"structure": res.Structure,
		"warnings":  res.Warnings,
	}
	if res.SummaryScope == "" {
		payload["summary_scope"] = core.ProjectMapSummaryScope
	}
	if includePolicy && reg != nil && reg.engine != nil {
		payload["policy"] = projectMapPolicy(reg, res)
	}
	return payload
}

// projectMapPolicy publishes only facts list_allowed_directories and
// security_policy already expose. It never names policy patterns.
func projectMapPolicy(reg *toolRegistry, res core.ProjectMapResult) map[string]any {
	engine := reg.engine
	profile := string(reg.profile)
	if profile == "" {
		profile = string(profileUltra)
	}
	rootsMode := string(core.RootsReplace)
	if cfg := engine.GetConfig(); cfg != nil && cfg.RootsMode != "" {
		rootsMode = string(cfg.RootsMode)
	}
	notes := []string{}
	if len(res.Roots) == 0 {
		notes = append(notes, insecureOpenWarning)
	} else {
		notes = append(notes, "fail-closed: only the listed roots are reachable")
	}
	if engine.PolicyEnabled() {
		notes = append(notes, "file security policy active")
	}
	policy := core.ProjectMapPolicy{
		Profile:   profile,
		RootsMode: rootsMode,
		AutoOCC:   core.AutoOCCMode(),
		ReadOnly:  engine.IsReadOnly(),
		Notes:     notes,
	}
	return map[string]any{
		"profile":    policy.Profile,
		"roots_mode": policy.RootsMode,
		"auto_occ":   policy.AutoOCC,
		"readonly":   policy.ReadOnly,
		"notes":      policy.Notes,
	}
}

// projectMapLimits renders the limits block.
func projectMapLimits(l core.ProjectMapLimits) map[string]any {
	return map[string]any{
		"max_entries":        l.MaxEntries,
		"max_depth":          l.MaxDepth,
		"max_time_ms":        l.MaxTimeMS,
		"max_bytes_metadata": l.MaxBytesMetadata,
		"elapsed_ms":         l.ElapsedMS,
		"visited_entries":    l.VisitedEntries,
	}
}

// projectMapTopExtensions keeps the most frequent extensions.
func projectMapTopExtensions(byExt map[string]int, limit int) map[string]int {
	if len(byExt) <= limit {
		return byExt
	}
	type pair struct {
		ext   string
		count int
	}
	pairs := make([]pair, 0, len(byExt))
	for ext, count := range byExt {
		pairs = append(pairs, pair{ext: ext, count: count})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].count != pairs[j].count {
			return pairs[i].count > pairs[j].count
		}
		return pairs[i].ext < pairs[j].ext
	})
	out := map[string]int{}
	for i, p := range pairs {
		if i >= limit {
			break
		}
		out[p.ext] = p.count
	}
	return out
}

// projectMapCap trims a string slice to limit entries.
func projectMapCap(items []string, limit int) []string {
	if len(items) <= limit {
		return items
	}
	return items[:limit]
}

// projectMapCapFiles trims the largest_files list.
func projectMapCapFiles(items []core.ProjectMapFile, limit int) []core.ProjectMapFile {
	if len(items) <= limit {
		return items
	}
	return items[:limit]
}

// projectMapText renders the text fallback. Compact mode keeps the counters
// and the directory skeleton but drops the file listing.
func projectMapText(engine *core.UltraFastEngine, path string, res core.ProjectMapResult) string {
	compact := engine != nil && engine.IsCompactMode()
	var b strings.Builder
	fmt.Fprintf(&b, "project_map %s\n", path)
	fmt.Fprintf(&b, "files: %d | dirs: %d | entries: %d | depth: %d | %dms",
		res.Summary.VisitedFiles, res.Summary.VisitedDirs, res.Limits.VisitedEntries,
		res.Limits.MaxDepth, res.Limits.ElapsedMS)
	if res.Truncated {
		b.WriteString(" | truncated")
	}
	b.WriteString("\n")

	if len(res.Summary.ByExtension) > 0 {
		b.WriteString("by extension: ")
		b.WriteString(formatProjectMapExtensions(res.Summary.ByExtension))
		b.WriteString("\n")
	}
	if len(res.Summary.KeyFiles) > 0 {
		fmt.Fprintf(&b, "key files: %s\n", strings.Join(res.Summary.KeyFiles, ", "))
	}
	if !compact && len(res.Summary.LargestFiles) > 0 {
		b.WriteString("largest: ")
		parts := make([]string, 0, len(res.Summary.LargestFiles))
		for _, f := range res.Summary.LargestFiles {
			parts = append(parts, fmt.Sprintf("%s (%d)", f.Path, f.Size))
		}
		b.WriteString(strings.Join(parts, ", "))
		b.WriteString("\n")
	}

	structure := res.Structure
	if len(structure) > 0 {
		b.WriteString("structure:\n")
		shown := 0
		for _, e := range structure {
			if shown >= projectMapMaxTextLines {
				fmt.Fprintf(&b, "… %d more entries (see structuredContent)\n", len(structure)-shown)
				break
			}
			shown++
			indent := strings.Repeat("  ", projectMapTextLevel(e.Path)-1)
			if e.Type == "dir" {
				fmt.Fprintf(&b, "%s%s (%d)", indent, e.Path, e.Children)
				if len(e.Notable) > 0 {
					fmt.Fprintf(&b, " notable: %s", strings.Join(e.Notable, ", "))
				}
				b.WriteString("\n")
				continue
			}
			if compact {
				continue
			}
			fmt.Fprintf(&b, "%s%s\n", indent, e.Path)
		}
	}
	if len(res.Warnings) > 0 {
		fmt.Fprintf(&b, "warnings: %s\n", strings.Join(res.Warnings, "; "))
	}
	return b.String()
}

// formatProjectMapExtensions renders the extension histogram compactly.
func formatProjectMapExtensions(byExt map[string]int) string {
	type pair struct {
		ext   string
		count int
	}
	pairs := make([]pair, 0, len(byExt))
	for ext, count := range byExt {
		pairs = append(pairs, pair{ext: ext, count: count})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].count != pairs[j].count {
			return pairs[i].count > pairs[j].count
		}
		return pairs[i].ext < pairs[j].ext
	})
	parts := make([]string, 0, len(pairs))
	for _, p := range pairs {
		name := p.ext
		if name == "" {
			name = "(none)"
		}
		parts = append(parts, fmt.Sprintf("%s:%d", name, p.count))
	}
	return strings.Join(parts, " ")
}
