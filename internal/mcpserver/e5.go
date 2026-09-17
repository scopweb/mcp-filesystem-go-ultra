package mcpserver

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mcp/filesystem-ultra/core"
)

const (
	statusOK        = "ok"
	statusEmpty     = "empty"
	statusPartial   = "partial"
	statusTruncated = "truncated"
	statusFailed    = "failed"
	statusApplied   = "applied"
	statusSimulated = "simulated"
)

const searchContinuationHint = "Use offset with the same path/pattern/max_results, or count_only:true / narrower file_types. Do not retry the same unbounded search."

var (
	searchRipgrepLine = regexp.MustCompile(`^(.+):(\d+):(.*)$`)
	searchCompactLine = regexp.MustCompile(`^(.+):(\d+)\[\d+:\d+\] (.*)$`)
	searchCountLine   = regexp.MustCompile(`(?i)(\d+)\s+matches?`)
)

func markApplied(m map[string]any) map[string]any {
	if m == nil {
		m = map[string]any{}
	}
	m["status"] = statusApplied
	return m
}

func markSimulated(m map[string]any, currentHash, predictedHash string) map[string]any {
	if m == nil {
		m = map[string]any{}
	}
	m["status"] = statusSimulated
	if currentHash != "" {
		m["current_hash"] = currentHash
	}
	if predictedHash != "" {
		m["predicted_hash"] = predictedHash
	}
	return m
}

func structuredOrError(ok bool, payload map[string]any, text string) *mcp.CallToolResult {
	if ok {
		return mcp.NewToolResultStructured(payload, text)
	}
	r := mcp.NewToolResultError(text)
	r.StructuredContent = payload
	return r
}

func contentWasTruncated(s string) bool {
	return strings.Contains(s, "[Truncated:") ||
		strings.Contains(s, "total lines in") ||
		strings.Contains(s, "⚠️ truncated") ||
		strings.Contains(s, "[truncated ") ||
		strings.Contains(s, "[truncated: max_nodes=")
}

func engineTextIsError(text string) bool {
	t := strings.TrimSpace(text)
	return strings.HasPrefix(t, "❌ Error") || strings.HasPrefix(t, "ERROR:")
}

func readStructured(path, content, hash string, truncated bool, startLine, endLine int, files []map[string]any) map[string]any {
	return readStructuredMeta(path, content, hash, readProjection{
		Truncated: truncated, StartLine: startLine, EndLine: endLine,
	}, files)
}

func readStructuredMeta(path, content, hash string, proj readProjection, files []map[string]any) map[string]any {
	truncated := proj.Truncated
	status := statusOK
	if truncated {
		status = statusTruncated
	}
	failed, ok := 0, 0
	for _, f := range files {
		if _, hasErr := f["error"]; hasErr {
			failed++
		} else {
			ok++
		}
	}
	if len(files) > 0 && failed > 0 && ok > 0 {
		status = statusPartial
	} else if len(files) > 0 && failed > 0 && ok == 0 {
		status = statusFailed
	}
	m := map[string]any{
		"content":   content,
		"status":    status,
		"truncated": truncated,
	}
	if path != "" {
		m["path"] = path
	}
	if hash != "" {
		m["content_hash"] = hash
	}
	if proj.StartLine > 0 {
		m["start_line"] = proj.StartLine
	}
	if proj.EndLine > 0 {
		m["end_line"] = proj.EndLine
	}
	if proj.TotalLines > 0 {
		m["total_lines"] = proj.TotalLines
	}
	if len(files) > 0 {
		m["files"] = files
	}
	if truncated && proj.ContinueAt > 0 {
		chunk := proj.EndLine - proj.StartLine + 1
		if chunk < 1 {
			chunk = 1
		}
		m["continuation"] = map[string]any{
			"start_line": proj.ContinueAt,
			"max_lines":  chunk,
			"hint":       "use start_line=" + strconv.Itoa(proj.ContinueAt) + ", max_lines=" + strconv.Itoa(chunk) + " to read the next chunk; do not re-read the whole file",
		}
	}
	return m
}

func listStructured(path, format, text string, entries []map[string]any, truncated bool, hiddenCount int, includeHidden bool) map[string]any {
	status := statusOK
	if truncated {
		status = statusTruncated
	} else if len(entries) == 0 && !strings.Contains(strings.ToLower(format), "tree") {
		status = statusEmpty
	}
	m := map[string]any{
		"path":      path,
		"status":    status,
		"truncated": truncated,
		"message":   text,
	}
	if format != "" {
		m["format"] = format
	}
	if entries != nil {
		m["entries"] = entries
		m["entry_count"] = len(entries)
	}
	if includeHidden {
		m["hidden_count"] = hiddenCount
	}
	if truncated {
		m["continuation"] = "Raise max_nodes/max_depth or narrow the path; the listing was capped."
	}
	return m
}

func searchStructuredFromOutcome(out core.SearchOutcome, scope map[string]any, extraTrunc bool) map[string]any {
	matches := make([]map[string]any, 0, len(out.Matches))
	for _, m := range out.Matches {
		hit := map[string]any{"path": m.File}
		if m.LineNumber > 0 {
			hit["line"] = m.LineNumber
		}
		if m.Line != "" {
			hit["text"] = m.Line
		}
		matches = append(matches, hit)
	}
	count := out.MatchCount
	if count == 0 {
		count = len(out.Matches)
	}
	truncated := out.Truncated || extraTrunc
	if !out.ExactTotal && truncated {
		count = len(out.Matches)
	}
	m := searchStructured(out.Text, scope, matches, count, truncated, out.HiddenCount)
	if truncated {
		page := 0
		if scope != nil {
			if v, ok := scope["max_results"].(int); ok {
				page = v
			}
		}
		reason := out.TruncReason
		if extraTrunc && reason == "" {
			reason = "byte_budget"
		}
		next := out.NextOffset
		if next == 0 {
			next = out.Offset + len(out.Matches)
		}
		m["continuation"] = core.SearchContinuationHint(next, page, reason)
	}
	return m
}

func searchStructured(text string, scope map[string]any, matches []map[string]any, matchCount int, truncated bool, hiddenCount int) map[string]any {
	status := statusOK
	if truncated {
		status = statusTruncated
	} else if matchCount == 0 {
		status = statusEmpty
	}
	if matches == nil {
		matches = []map[string]any{}
	}
	m := map[string]any{
		"status":       status,
		"match_count":  matchCount,
		"truncated":    truncated,
		"hidden_count": hiddenCount,
		"matches":      matches,
		"message":      text,
	}
	if scope != nil {
		m["scope"] = scope
	}
	if truncated {
		m["continuation"] = searchContinuationHint
	}
	return m
}

func parseCountOnly(text string) int {
	if m := searchCountLine.FindStringSubmatch(text); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	return -1
}

func parseSearchPayload(text string) (matches []map[string]any, matchCount int, truncated bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || strings.HasPrefix(trimmed, "No matches") || strings.Contains(trimmed, "No matches found") {
		return []map[string]any{}, 0, false
	}
	if strings.HasPrefix(trimmed, "{") {
		var raw map[string]any
		if json.Unmarshal([]byte(trimmed), &raw) == nil {
			if v, ok := raw["truncated"].(bool); ok {
				truncated = v
			}
			if v, ok := raw["total_matches"].(float64); ok {
				matchCount = int(v)
			} else if v, ok := raw["returned_matches"].(float64); ok {
				matchCount = int(v)
			}
			if arr, ok := raw["matches"].([]any); ok {
				for _, item := range arr {
					obj, ok := item.(map[string]any)
					if !ok {
						continue
					}
					hit := map[string]any{}
					if p, ok := obj["file"].(string); ok {
						hit["path"] = p
					}
					if n, ok := obj["line"].(float64); ok {
						hit["line"] = int(n)
					} else if n, ok := obj["line_number"].(float64); ok {
						hit["line"] = int(n)
					}
					if s, ok := obj["line_content"].(string); ok {
						hit["text"] = s
					}
					if len(hit) > 0 {
						matches = append(matches, hit)
					}
				}
			}
			if matchCount == 0 {
				matchCount = len(matches)
			}
			return matches, matchCount, truncated
		}
	}
	if m := searchCountLine.FindStringSubmatch(trimmed); m != nil && !strings.Contains(trimmed, ":") {
		n, _ := strconv.Atoi(m[1])
		return []map[string]any{}, n, false
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "⚠️") || strings.HasPrefix(line, "💡") {
			continue
		}
		if mm := searchCompactLine.FindStringSubmatch(line); mm != nil {
			n, _ := strconv.Atoi(mm[2])
			matches = append(matches, map[string]any{"path": mm[1], "line": n, "text": mm[3]})
			continue
		}
		if mm := searchRipgrepLine.FindStringSubmatch(line); mm != nil {
			n, _ := strconv.Atoi(mm[2])
			matches = append(matches, map[string]any{"path": mm[1], "line": n, "text": mm[3]})
		}
	}
	return matches, len(matches), false
}

func parseListJSON(listing string) (path string, entries []map[string]any, truncated bool, ok bool) {
	var raw struct {
		Path      string `json:"path"`
		Total     int    `json:"total"`
		Truncated bool   `json:"truncated"`
		Entries   []struct {
			Name     string `json:"name"`
			Type     string `json:"type"`
			Size     int64  `json:"size"`
			Modified string `json:"modified"`
		} `json:"entries"`
	}
	if json.Unmarshal([]byte(listing), &raw) != nil {
		return "", nil, false, false
	}
	entries = make([]map[string]any, 0, len(raw.Entries))
	for _, e := range raw.Entries {
		item := map[string]any{"name": e.Name, "type": e.Type}
		if e.Size > 0 {
			item["size"] = e.Size
		}
		if e.Modified != "" {
			item["modified"] = e.Modified
		}
		entries = append(entries, item)
	}
	return raw.Path, entries, raw.Truncated, true
}

func batchStructured(kind string, success bool, total, completed, failed int, items []map[string]any, backupID, rollbackStatus, message string, simulated bool) map[string]any {
	status := statusOK
	if simulated {
		status = statusSimulated
	} else if !success && completed > 0 && failed > 0 {
		status = statusPartial
	} else if !success {
		status = statusFailed
	}
	if items == nil {
		items = []map[string]any{}
	}
	retryable := false
	if !success && completed > 0 {
		retryable = false
	}
	m := map[string]any{
		"status":    status,
		"kind":      kind,
		"success":   success,
		"total":     total,
		"completed": completed,
		"failed":    failed,
		"items":     items,
		"retryable": retryable,
		"message":   message,
	}
	if backupID != "" {
		m["backup_id"] = backupID
	}
	if rollbackStatus != "" {
		m["rollback_status"] = rollbackStatus
	}
	return m
}

func batchFromResult(result core.BatchResult, message string) map[string]any {
	items := make([]map[string]any, 0, len(result.Results))
	for _, op := range result.Results {
		item := map[string]any{
			"index":   op.Index,
			"type":    op.Type,
			"path":    op.Path,
			"success": op.Success,
		}
		if op.Error != "" {
			item["error"] = op.Error
		}
		if op.Skipped {
			item["skipped"] = true
		}
		items = append(items, item)
	}
	return batchStructured("request", result.Success, result.TotalOps, result.CompletedOps, result.FailedOps, items, result.BackupID, result.RollbackStatus, message, result.ValidationOnly)
}

func batchFromPipeline(result *core.PipelineResult, message string) map[string]any {
	if result == nil {
		return batchStructured("pipeline", false, 0, 0, 0, nil, "", "", message, false)
	}
	items := make([]map[string]any, 0, len(result.Results))
	failed := 0
	for i, step := range result.Results {
		item := map[string]any{
			"index":   i,
			"type":    step.Action,
			"path":    step.StepID,
			"success": step.Success,
		}
		if step.Error != "" {
			item["error"] = step.Error
			failed++
		}
		if step.Skipped {
			item["skipped"] = true
		}
		items = append(items, item)
	}
	return batchStructured("pipeline", result.Success, result.TotalSteps, result.CompletedSteps, failed, items, result.BackupID, result.RollbackStatus, message, result.DryRun)
}

func batchFromRename(result *core.BatchRenameResult, message string) map[string]any {
	if result == nil {
		return batchStructured("rename", false, 0, 0, 0, nil, "", "", message, false)
	}
	items := make([]map[string]any, 0, len(result.Operations))
	for _, op := range result.Operations {
		item := map[string]any{
			"index":   op.Index,
			"type":    "rename",
			"path":    op.OldPath,
			"success": op.Success,
		}
		if op.Error != "" {
			item["error"] = op.Error
		}
		if op.Skipped {
			item["skipped"] = true
		}
		if op.NewPath != "" {
			item["dest"] = op.NewPath
		}
		items = append(items, item)
	}
	return batchStructured("rename", result.Success, result.TotalFiles, result.RenamedCount, result.ErrorCount, items, "", "", message, result.Preview)
}

func backupResult(action, text string, extra map[string]any) *mcp.CallToolResult {
	return mcp.NewToolResultStructured(backupStructured(action, text, extra), text)
}

func backupStructured(action, text string, extra map[string]any) map[string]any {
	status := statusOK
	if items, ok := extra["items"].([]map[string]any); ok && len(items) == 0 {
		status = statusEmpty
	}
	m := map[string]any{
		"status":  status,
		"action":  action,
		"message": text,
	}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func backupItems(backups []core.BackupInfo) []map[string]any {
	items := make([]map[string]any, 0, len(backups))
	for _, b := range backups {
		items = append(items, map[string]any{
			"backup_id":  b.BackupID,
			"operation":  b.Operation,
			"file_count": len(b.Files),
			"timestamp":  b.Timestamp.UTC().Format("2006-01-02T15:04:05Z"),
		})
	}
	return items
}

// retryableForCode is the stable Failure Intelligence policy. True means
// callers must first use the envelope data; it never authorizes replaying an
// identical mutation blindly.
func retryableForCode(code string) bool {
	switch code {
	case errCodeOCCMismatch, errCodeHashRequired:
		return true
	default:
		return false
	}
}
