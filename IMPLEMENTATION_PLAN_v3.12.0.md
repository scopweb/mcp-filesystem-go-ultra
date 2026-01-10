# 📋 Implementation Plan: v3.12.0 "Code Editing Excellence"

**Status**: Ready for Development
**Version**: v3.11.0 → v3.12.0
**Timeline**: 3-4 weeks (20-30 hours active development)
**Risk Level**: LOW-MEDIUM (well-isolated changes)

---

## 🎯 Executive Summary

This document provides detailed specifications for implementing v3.12.0 "Code Editing Excellence" based on comprehensive codebase analysis. The goal is 70-80% reduction in token consumption for code editing workflows through 6 sequential phases.

**Key Insight**: The codebase is **exceptionally well-prepared** for these changes:
- SearchMatch struct already has MatchStart/MatchEnd fields
- MultiEdit operation support already exists
- Risk assessment system is mature and robust
- Error handling patterns are consistent
- Test infrastructure supports rapid expansion

---

## 📐 Architecture Overview

### Current Architecture (v3.11.0)

```
MCP Tool Handler
    ↓
Engine Methods (EditFile, SmartSearch, etc.)
    ↓
Core Operations (6-level optimization pipeline)
    ├─ EditOperations (edit_operations.go)
    ├─ SearchOperations (search_operations.go)
    ├─ EditSafetyValidator (edit_safety_layer.go)
    ├─ ImpactAnalyzer (impact_analyzer.go)
    └─ BackupManager (backup_manager.go)
    ↓
Utilities & Helpers
    ├─ PathConverter (path_converter.go)
    ├─ ErrorTypes (errors.go)
    └─ StreamingOperations (streaming_operations.go)
```

### Modified Architecture (v3.12.0)

```
New MCP Tool: batch_code_edit
    ↓
New: BatchEditOrchestrator (NEW - coordinates multi-file operations)
    ↓
Existing EditFile with transaction support
    ├─ snapshot()          [NEW]
    ├─ executeInOrder()    [NEW]
    └─ rollbackIfNeeded()  [NEW]
```

---

## 🔧 Phase 1: Coordinate Tracking (2-3 Days)

### Objective
Expose character-level positioning in search results to enable precise code location and targeting.

### Current State
- SearchMatch struct HAS fields: MatchStart, MatchEnd (lines 25-33 in edit_operations.go)
- These fields exist but are **not populated or used**
- Character offsets are never calculated in search

### Changes Required

#### 1.1 Enhanced Search Result Population (core/search_operations.go)

**File**: `core/search_operations.go`
**Lines**: ~309-315 (performSmartSearch function)
**Change Type**: Addition

```go
// CURRENT CODE (Line 309-315):
match := SearchMatch{
    File:       currentFile,
    LineNumber: lineNum,
    Line:       strings.TrimSpace(line),
    // MatchStart, MatchEnd NOT SET
}

// NEW CODE:
// Calculate character offset of pattern match in the line
matchIdx := strings.Index(line, pattern)
if matchIdx >= 0 {
    match := SearchMatch{
        File:       currentFile,
        LineNumber: lineNum,
        Line:       strings.TrimSpace(line),
        MatchStart: matchIdx,
        MatchEnd:   matchIdx + len(pattern),
    }
} else {
    // Pattern may have been transformed, estimate position
    match := SearchMatch{
        File:       currentFile,
        LineNumber: lineNum,
        Line:       strings.TrimSpace(line),
        MatchStart: 0,
        MatchEnd:   len(pattern),
    }
}
```

**New File Section**: Add helper function (20 lines)
```go
// calculateCharacterOffset - Determine exact character position of match
func calculateCharacterOffset(line, pattern string) (start, end int) {
    idx := strings.Index(line, pattern)
    if idx < 0 {
        // Fallback: normalized search
        normalized := normalizeLineEndings(line)
        if idx = strings.Index(normalized, pattern); idx < 0 {
            return 0, len(pattern)
        }
    }
    return idx, idx + len(pattern)
}
```

#### 1.2 Context Line Coordinate Calculation (core/search_operations.go)

**Lines**: ~270-285 (performAdvancedTextSearch function)

```go
// When building context lines, also track their coordinates
for i, contextLine := range contextLines {
    lineNum := matchLineNum - contextLinesBack + i
    if lineNum >= 0 {
        // Store coordinate info for context
        contextInfo := struct {
            LineNumber int
            Line       string
            IsMatch    bool  // Is this the matching line?
        }{
            LineNumber: lineNum,
            Line:       contextLine,
            IsMatch:    (i == contextLinesBack),
        }
        // Include in search result if needed
    }
}
```

#### 1.3 Tool Handler Enhancement (main.go)

**File**: `main.go`
**Lines**: ~550-600 (search tool registration area)
**Change Type**: Parameter addition

```go
// CURRENT:
smartSearchTool := mcp.NewTool("smart_search",
    mcp.WithDescription("Smart search..."),
    mcp.WithString("path", mcp.Required()),
    mcp.WithString("pattern", mcp.Required()),
    mcp.WithBoolean("include_content"),
)

// NEW - Add optional parameter:
smartSearchTool := mcp.NewTool("smart_search",
    mcp.WithDescription("Smart search..."),
    mcp.WithString("path", mcp.Required()),
    mcp.WithString("pattern", mcp.Required()),
    mcp.WithBoolean("include_content"),
    mcp.WithBoolean("include_coordinates", mcp.Description("Return character offsets")),
)

// In handler, respect the parameter:
if args["include_coordinates"].(bool) {
    // Format output with coordinates
    return formatSearchResultsWithCoordinates(matches)
}
```

#### 1.4 Output Formatting (core/search_operations.go)

**New Functions**: Add formatting helpers (40 lines total)

```go
// FormatSearchResultsWithCoordinates - JSON structure with exact positions
type SearchResultWithCoordinates struct {
    File           string `json:"file"`
    LineNumber     int    `json:"line_number"`
    CharacterStart int    `json:"character_start"`  // NEW
    CharacterEnd   int    `json:"character_end"`    // NEW
    Line           string `json:"line"`
    Context        []struct {
        LineNumber int    `json:"line_number"`
        Line       string `json:"line"`
    } `json:"context,omitempty"`
}
```

### Implementation Checklist (Phase 1)

- [ ] Add calculateCharacterOffset() helper
- [ ] Update performSmartSearch() to populate MatchStart/MatchEnd
- [ ] Update performAdvancedTextSearch() to track coordinates
- [ ] Add include_coordinates parameter to smart_search tool
- [ ] Implement FormatSearchResultsWithCoordinates()
- [ ] Add tests for coordinate accuracy (10 test cases)
- [ ] Verify backward compatibility (old format still works)
- [ ] Update CHANGELOG with new feature

### Testing Strategy (Phase 1)

```go
// Test 1: Basic coordinate calculation
func TestCoordinateCalculation(t *testing.T) {
    match := findPattern("hello world", "world")
    assert.Equal(t, 6, match.MatchStart)
    assert.Equal(t, 11, match.MatchEnd)
}

// Test 2: Multiline context with coordinates
func TestContextCoordinates(t *testing.T) {
    results := advancedSearch(file, pattern, includeContext=true)
    for _, result := range results {
        assert.True(t, result.MatchStart >= 0)
        assert.True(t, result.MatchStart < result.MatchEnd)
        assert.True(t, result.MatchEnd <= len(result.Line))
    }
}

// Test 3: Edge cases (empty file, pattern at start/end)
// Test 4-10: Various file encodings and line endings
```

### Effort Estimate (Phase 1)

| Task | Hours | Notes |
|------|-------|-------|
| Code implementation | 3 | Mostly additions, minimal refactoring |
| Testing | 2 | 10 test cases, edge cases |
| Documentation | 1 | Update HOOKS.md, README.md |
| **Total** | **6 hours** | **Very Low Risk** |

### Risk Assessment (Phase 1)

| Risk | Probability | Mitigation |
|------|-------------|-----------|
| Break existing search results | Low | Use new optional parameter |
| Incorrect coordinate calculation | Low | Thorough testing with various encodings |
| Performance impact | Very Low | No algorithmic changes, only additions |
| Integration issues | Very Low | Tool handler pattern unchanged |

---

## 🔨 Phase 2: Diff-Based Edits (4-5 Days)

### Objective
Enable sending only the differences (diffs) instead of complete files, reducing token consumption by 60%.

### Current State
- Edit operations require sending full old_text and new_text
- No diff-based editing capability
- EditFile and SearchAndReplace always modify complete content

### Changes Required

#### 2.1 Diff Patch Data Structure (core/edit_operations.go)

**New Type Addition** (lines after EditResult struct, ~line 35)

```go
// DiffPatch - Unified diff format for minimal edits
type DiffPatch struct {
    FilePath      string `json:"file_path"`
    DiffFormat    string `json:"diff_format"`    // "unified", "context", "custom"

    // Unified diff section header
    OldStart      int    `json:"old_start_line"`
    OldCount      int    `json:"old_line_count"`
    NewStart      int    `json:"new_start_line"`
    NewCount      int    `json:"new_line_count"`

    // Actual diff content (unified format)
    DiffContent   string `json:"diff_content"`    // Lines with +/- prefixes

    // Context for validation
    ContextBefore string `json:"context_before"`  // 2-3 lines before change
    ContextAfter  string `json:"context_after"`   // 2-3 lines after change

    // Validation checksums
    OldHash       string `json:"old_hash"`        // SHA256 of old content
    NewHash       string `json:"new_hash"`        // SHA256 of new content
}

// ApplyDiffResult - Result of diff patch application
type ApplyDiffResult struct {
    Success            bool
    FilePath           string
    LinesChanged       int
    CharactersChanged  int
    ValidationHash     string
    RollbackID         string  // Backup reference
    ContextValidation  string  // "valid", "warning", "failed"
    ErrorMessage       string  // If failed
}
```

#### 2.2 Diff Parsing & Application (NEW core/diff_operations.go)

**New File**: `core/diff_operations.go` (200 lines)

```go
package core

import (
    "bufio"
    "fmt"
    "strings"
)

// DiffOperator - Handles diff parsing and application
type DiffOperator struct {
    engine *UltraFastEngine
}

// NewDiffOperator - Constructor
func NewDiffOperator(engine *UltraFastEngine) *DiffOperator {
    return &DiffOperator{engine: engine}
}

// ApplyDiffPatch - Apply unified diff to file
func (do *DiffOperator) ApplyDiffPatch(patch *DiffPatch) (*ApplyDiffResult, error) {
    // 1. Read current file
    currentContent, err := os.ReadFile(patch.FilePath)
    if err != nil {
        return nil, fmt.Errorf("failed to read file: %w", err)
    }

    // 2. Parse diff format
    hunks, err := do.parseDiff(patch.DiffContent)
    if err != nil {
        return nil, fmt.Errorf("failed to parse diff: %w", err)
    }

    // 3. Validate context
    valid, warning := do.validateDiffContext(string(currentContent), hunks)
    if !valid {
        return &ApplyDiffResult{
            Success:           false,
            ContextValidation: "failed",
            ErrorMessage:      warning,
        }, nil
    }

    // 4. Apply hunks in order
    newContent := do.applyHunks(string(currentContent), hunks)

    // 5. Verify by comparing hashes
    if patch.NewHash != "" && !do.verifyHash(newContent, patch.NewHash) {
        return nil, fmt.Errorf("hash mismatch after applying diff")
    }

    // 6. Create backup
    backupID, _ := do.engine.createBackup(patch.FilePath)

    // 7. Write atomically
    if err := do.engine.writeFileAtomic(patch.FilePath, newContent); err != nil {
        return nil, fmt.Errorf("failed to write file: %w", err)
    }

    return &ApplyDiffResult{
        Success:           true,
        FilePath:          patch.FilePath,
        LinesChanged:      hunks.TotalLines(),
        CharactersChanged: len(newContent) - len(currentContent),
        RollbackID:        backupID,
    }, nil
}

// parseDiff - Parse unified diff format
func (do *DiffOperator) parseDiff(diffContent string) (hunks []*DiffHunk, error) {
    scanner := bufio.NewScanner(strings.NewReader(diffContent))
    var currentHunk *DiffHunk

    for scanner.Scan() {
        line := scanner.Text()

        if strings.HasPrefix(line, "@@ ") {
            // New hunk header: @@ -45,3 +45,4 @@
            if currentHunk != nil {
                hunks = append(hunks, currentHunk)
            }
            currentHunk = do.parseHunkHeader(line)
        } else if strings.HasPrefix(line, "+") {
            currentHunk.AddLines = append(currentHunk.AddLines, line[1:])
        } else if strings.HasPrefix(line, "-") {
            currentHunk.RemoveLines = append(currentHunk.RemoveLines, line[1:])
        } else if strings.HasPrefix(line, " ") {
            currentHunk.ContextLines = append(currentHunk.ContextLines, line[1:])
        }
    }

    if currentHunk != nil {
        hunks = append(hunks, currentHunk)
    }

    return hunks, scanner.Err()
}

// applyHunks - Apply parsed hunks to content
func (do *DiffOperator) applyHunks(content string, hunks []*DiffHunk) string {
    lines := strings.Split(content, "\n")

    // Process hunks in reverse order to maintain line numbers
    for i := len(hunks) - 1; i >= 0; i-- {
        hunk := hunks[i]
        // Remove old lines
        lines = append(lines[:hunk.OldStart], lines[hunk.OldStart+hunk.OldCount:]...)
        // Insert new lines
        lines = append(lines[:hunk.OldStart], hunk.AddLines...)
    }

    return strings.Join(lines, "\n")
}

// validateDiffContext - Verify context lines match before applying
func (do *DiffOperator) validateDiffContext(content string, hunks []*DiffHunk) (bool, string) {
    lines := strings.Split(content, "\n")

    for _, hunk := range hunks {
        for i, contextLine := range hunk.ContextLines {
            actualLine := lines[hunk.OldStart+i]
            if strings.TrimSpace(actualLine) != strings.TrimSpace(contextLine) {
                return false, fmt.Sprintf("Context mismatch at line %d", hunk.OldStart+i)
            }
        }
    }

    return true, ""
}

// DiffHunk - Represents a single diff hunk
type DiffHunk struct {
    OldStart      int
    OldCount      int
    NewStart      int
    NewCount      int
    ContextLines  []string
    RemoveLines   []string
    AddLines      []string
}
```

#### 2.3 Diff Generation (core/diff_operations.go continued)

```go
// GenerateDiffPatch - Create diff from old and new content
func (do *DiffOperator) GenerateDiffPatch(filePath, oldContent, newContent string) (*DiffPatch, error) {
    oldLines := strings.Split(oldContent, "\n")
    newLines := strings.Split(newContent, "\n")

    // Use Myers diff algorithm or simple line-based diff
    // For simplicity, using line-based approach
    hunks := do.generateHunks(oldLines, newLines)

    // Generate unified diff format
    var diffBuf strings.Builder
    for _, hunk := range hunks {
        diffBuf.WriteString(hunk.String())  // @@...@@ format
        for _, line := range hunk.ContextLines {
            diffBuf.WriteString(" " + line + "\n")
        }
        for _, line := range hunk.RemoveLines {
            diffBuf.WriteString("-" + line + "\n")
        }
        for _, line := range hunk.AddLines {
            diffBuf.WriteString("+" + line + "\n")
        }
    }

    return &DiffPatch{
        FilePath:      filePath,
        DiffFormat:    "unified",
        DiffContent:   diffBuf.String(),
        OldHash:       do.hashContent(oldContent),
        NewHash:       do.hashContent(newContent),
    }, nil
}
```

#### 2.4 MCP Tool Registration (main.go)

**Location**: `main.go`, tool registration section (~line 600+)
**Change Type**: New tool addition

```go
// New tool: apply_diff_patch
applyDiffTool := mcp.NewTool("apply_diff_patch",
    mcp.WithDescription("Apply unified diff to file (minimal token usage)"),
    mcp.WithString("file_path", mcp.Required()),
    mcp.WithString("diff_content", mcp.Required(),
        mcp.Description("Unified diff format (@@...@@ style)")),
    mcp.WithString("old_hash", mcp.Description("SHA256 of original content")),
    mcp.WithString("new_hash", mcp.Description("SHA256 of target content")),
    mcp.WithBoolean("force"),
)

s.AddTool(applyDiffTool, func(ctx context.Context, request mcp.CallToolRequest)
    (*mcp.CallToolResult, error) {
    filePath, _ := request.RequireString("file_path")
    diffContent, _ := request.RequireString("diff_content")
    force, _ := request.GetBool("force")

    patch := &DiffPatch{
        FilePath:    filePath,
        DiffContent: diffContent,
    }

    diffOp := NewDiffOperator(engine)
    result, err := diffOp.ApplyDiffPatch(patch)

    if err != nil {
        return mcp.NewToolResultError(err.Error()), nil
    }

    if !result.Success {
        return mcp.NewToolResultError(result.ErrorMessage), nil
    }

    return mcp.NewToolResultText(fmt.Sprintf(
        "✅ Diff applied: %d lines changed, %d characters modified\nBackup: %s",
        result.LinesChanged, result.CharactersChanged, result.RollbackID,
    )), nil
})

// Companion tool: generate_diff_preview
generateDiffTool := mcp.NewTool("generate_diff_preview",
    mcp.WithDescription("Generate diff without applying (for verification)"),
    mcp.WithString("file_path", mcp.Required()),
    mcp.WithString("old_content", mcp.Required()),
    mcp.WithString("new_content", mcp.Required()),
)

s.AddTool(generateDiffTool, func(ctx context.Context, request mcp.CallToolRequest)
    (*mcp.CallToolResult, error) {
    filePath, _ := request.RequireString("file_path")
    oldContent, _ := request.RequireString("old_content")
    newContent, _ := request.RequireString("new_content")

    diffOp := NewDiffOperator(engine)
    patch, err := diffOp.GenerateDiffPatch(filePath, oldContent, newContent)

    if err != nil {
        return mcp.NewToolResultError(err.Error()), nil
    }

    return mcp.NewToolResultText(patch.DiffContent), nil
})
```

### Implementation Checklist (Phase 2)

- [ ] Create core/diff_operations.go with DiffOperator
- [ ] Implement parseDiff() for unified format
- [ ] Implement applyHunks() with atomic writes
- [ ] Implement validateDiffContext() with 2-3 line context
- [ ] Implement GenerateDiffPatch() for preview
- [ ] Register apply_diff_patch tool in main.go
- [ ] Register generate_diff_preview tool in main.go
- [ ] Add error handling with custom DiffError type
- [ ] Integrate with BackupManager for rollback support
- [ ] Add comprehensive tests (15 test cases)
- [ ] Document in HOOKS.md with examples

### Testing Strategy (Phase 2)

```go
// Test 1: Simple single-line diff
func TestSimpleDiffApplication(t *testing.T) {
    // Diff: change line 5
    // Expected: only line 5 modified, others unchanged
}

// Test 2: Multi-hunk diff
func TestMultiHunkDiff(t *testing.T) {
    // Diff with 3 separate hunks
    // Expected: all hunks applied in correct order
}

// Test 3: Context validation
func TestContextValidationFails(t *testing.T) {
    // File modified since diff generated
    // Expected: Error "Context mismatch"
}

// Test 4: Diff generation accuracy
func TestDiffGeneration(t *testing.T) {
    // Generate diff from old→new
    // Apply diff to old content
    // Expected: result == new content
}

// Test 5-15: Edge cases (empty files, large diffs, special chars, etc.)
```

### Performance Expectations (Phase 2)

| Scenario | Tokens Saved | Notes |
|----------|--------------|-------|
| 500KB file, 2KB change | ~99.6% (500KB → 2KB) | Main use case |
| 100KB file, 10KB change | ~90% (100KB → 10KB) | Typical refactoring |
| 10KB file, 1KB change | ~90% (10KB → 1KB) | Small edit |

### Effort Estimate (Phase 2)

| Task | Hours | Notes |
|------|-------|-------|
| DiffOperator implementation | 6 | ~200 LOC, core logic |
| Diff parsing/application | 4 | Myers or line-based diff |
| Tool registration | 2 | Two tools (apply + preview) |
| Testing | 4 | 15 test cases, edge cases |
| Documentation | 2 | Examples, best practices |
| Integration & debugging | 2 | Integration testing |
| **Total** | **20 hours** | **Low Risk** |

### Risk Assessment (Phase 2)

| Risk | Probability | Mitigation |
|------|-------------|-----------|
| Diff parsing incorrect | Medium | Comprehensive test suite, line-by-line validation |
| Atomic write failure | Low | Use existing writeFileAtomic() function |
| Context validation too strict | Medium | Configurable context line count |
| Performance degradation | Low | Diff parsing is O(n), not worse than full edit |
| Backward compatibility | Low | New tools don't affect existing functions |

---

## 📋 Phase 3: Preview Mode (1 Day)

### Objective
Allow simulating changes without applying them, providing safety and token efficiency.

### Changes Required

#### 3.1 Preview Parameter Addition (main.go)

Add optional `preview_only` parameter to existing tools:
- `intelligent_write`
- `smart_edit_file`
- `edit_file`

```go
// Add to tool definitions:
mcp.WithBoolean("preview_only", mcp.Description("Return changes without applying"))
```

#### 3.2 Preview Implementation (core/claude_optimizer.go)

```go
func (o *ClaudeDesktopOptimizer) PreviewEdit(ctx context.Context,
    path, oldText, newText string) (string, error) {

    // Read file
    content, _ := os.ReadFile(path)

    // Generate diff instead of applying
    diffOp := NewDiffOperator(o.engine)
    patch, _ := diffOp.GenerateDiffPatch(path, string(content), newText)

    // Return formatted diff without writing
    return patch.DiffContent, nil
}
```

### Effort Estimate: 3 hours

---

## 🎯 Phase 3-6 Summary

### Phase 3: Preview Mode (1 Day)
- Add `preview_only` parameter
- Return formatted diff without applying
- 3 hours effort

### Phase 4: High-Level Tools (3-4 Days)
- `find_and_replace()` - Search + replace in one call
- `replace_function_body()` - Targeted refactoring
- `rename_symbol()` - Variable/function renaming
- 16 hours effort

### Phase 5: Telemetry (1 Day)
- Log edit operations (full rewrite vs targeted)
- Capture efficiency metrics
- 4 hours effort

### Phase 6: Documentation (1 Day)
- Update HOOKS.md with workflows
- Create guides/EFFICIENT_EDIT_WORKFLOWS.md
- Update README.md with best practices
- 4 hours effort

---

## 📊 Overall Timeline & Resource Allocation

### Week 1: Phase 1 (Coordinate Tracking)
- **Mon-Tue**: Implementation (6h)
- **Wed**: Testing (4h)
- **Thu**: Documentation (2h)
- **Fri**: Review & integration (2h)
- **Total**: 14 hours, **LOW RISK**

### Week 2: Phase 2 (Diff-Based Edits)
- **Mon-Tue**: Core DiffOperator (6h)
- **Wed**: Tool integration (4h)
- **Thu**: Testing (6h)
- **Fri**: Debugging & refinement (4h)
- **Total**: 20 hours, **LOW-MEDIUM RISK**

### Week 3: Phases 3-6 (Polish & Documentation)
- **Mon**: Phase 3 - Preview mode (3h)
- **Tue-Wed**: Phase 4 - High-level tools (8h)
- **Thu**: Phase 5 - Telemetry (4h)
- **Fri**: Phase 6 - Documentation (4h)
- **Total**: 19 hours, **LOW RISK**

### **Grand Total**: 53 hours (7-8 developer days)
**Timeline**: 3-4 weeks (with concurrent work on other features)

---

## ✅ Success Criteria

### Phase 1 Success
- [ ] Character offsets returned for all search results
- [ ] include_coordinates parameter works correctly
- [ ] Backward compatibility maintained
- [ ] 10/10 coordinate tests passing
- [ ] Zero regression in existing search tests

### Phase 2 Success
- [ ] Diff applied without errors
- [ ] Context validation working
- [ ] apply_diff_patch tool registered
- [ ] generate_diff_preview tool registered
- [ ] 15/15 diff tests passing
- [ ] 99%+ token reduction on large file edits

### Phase 3+ Success
- [ ] Preview mode shows changes without writing
- [ ] All new tools registered and functional
- [ ] Telemetry data being collected
- [ ] Documentation complete with examples
- [ ] All 50+ tests passing
- [ ] Zero regressions

### Overall v3.12.0 Success
- [ ] 70-80% token reduction for typical workflows
- [ ] 0 breaking changes
- [ ] All 3 new tools working
- [ ] Complete documentation
- [ ] Release note ready
- [ ] No performance degradation

---

## 📚 Key Files for Development

### Phase 1 Files
- `core/search_operations.go` - calculateCharacterOffset() addition
- `main.go` - Tool parameter enhancement
- `tests/coordinate_tracking_test.go` - New test file

### Phase 2 Files
- `core/diff_operations.go` - NEW FILE (200 LOC)
- `core/edit_operations.go` - Integration hooks
- `main.go` - Tool registration
- `tests/diff_operations_test.go` - New test file

### Phase 3-6 Files
- `core/claude_optimizer.go` - Preview implementation
- `core/batch_operations.go` - High-level tools
- `core/engine.go` - Telemetry integration
- `guides/EFFICIENT_EDIT_WORKFLOWS.md` - NEW FILE

---

## 🔗 Dependencies & Integration Points

### External Dependencies
- No new external dependencies required
- Uses existing: os, fmt, strings, bufio, crypto

### Internal Dependencies
```
Phase 1 → Phase 2 (coordinates inform diff application)
Phase 2 → Phase 3 (preview uses diff generation)
Phases 1-3 → Phase 4 (high-level tools use all above)
All phases → Phase 5 (telemetry observes all operations)
```

---

## 🚀 Deployment Strategy

### Pre-release
1. **Merge to develop branch** (all phases complete)
2. **Run full test suite** (50+ tests)
3. **Performance benchmark** (vs v3.11.0)
4. **Security review** (path validation, edge cases)
5. **Documentation review** (HOOKS.md, guides)

### Release
1. **Tag as v3.12.0** with git
2. **Create GitHub release** with changelog
3. **Announce in CHANGELOG.md**
4. **Update README.md** version reference

### Post-release Monitoring
1. **Collect telemetry** for 1 week
2. **Monitor error rates** (diff application failures)
3. **Track adoption** (tool usage stats)
4. **Gather user feedback** (GitHub issues)
5. **Plan v3.12.1 hotfixes** if needed

---

## 📋 Git Commit Strategy

Suggested commit sequence:

```bash
# Phase 1
git commit -m "feat(search): Add coordinate tracking to search results

- Calculate and expose character offsets (MatchStart/MatchEnd)
- Add include_coordinates parameter to smart_search
- Support formatted output with character positions
- 10 new tests for coordinate accuracy"

# Phase 2
git commit -m "feat(edit): Implement diff-based edits (60% token reduction)

- New diff_operations.go module for diff parsing/application
- apply_diff_patch tool for minimal edits
- generate_diff_preview tool for verification
- Context validation before applying diffs
- Integration with BackupManager for rollback"

# Phase 3
git commit -m "feat(edit): Add preview mode to edit tools

- preview_only parameter for safe verification
- No-write simulation of changes
- Works with intelligent_write and smart_edit_file"

# Phase 4
git commit -m "feat(optimize): Add high-level refactoring tools

- find_and_replace: Search + replace in one call
- replace_function_body: Targeted function editing
- rename_symbol: Variable/function renaming with scope"

# Phase 5
git commit -m "feat(telemetry): Add edit operation metrics

- Track full rewrite vs targeted edit ratio
- Measure efficiency gains
- Enable continuous improvement"

# Phase 6
git commit -m "docs: Update guides for Code Editing Excellence

- EFFICIENT_EDIT_WORKFLOWS.md with examples
- HOOKS.md with new tool documentation
- README.md with best practices"

# Final
git commit -m "release: Version 3.12.0 - Code Editing Excellence

- 70-80% token reduction for code edits
- Phase 1: Coordinate tracking (precise locations)
- Phase 2: Diff-based edits (minimal updates)
- Phase 3: Preview mode (safe verification)
- Phase 4: High-level tools (simplified workflows)
- Phase 5: Telemetry (continuous improvement)
- Phase 6: Documentation (best practices)

Backward compatible, zero breaking changes."
```

---

## 🎓 Learning & References

### Unified Diff Format
- Standard format used by `git diff`
- Header: `@@ -old_start,old_count +new_start,new_count @@`
- Lines: ` ` (context), `-` (remove), `+` (add)

### Myers Diff Algorithm
- Advanced alternative to line-based diff
- Lower token overhead for large changes
- Considered for Phase 2 optimization

### Character Encoding Considerations
- UTF-8 vs other encodings
- Character vs byte offset (important!)
- Multiline diff handling

---

## ✅ Conclusion

This implementation plan is **comprehensive, low-risk, and achievable** within the 3-4 week timeline. The codebase foundation is excellent, with many required pieces already in place (SearchMatch struct fields, MultiEdit support, risk validation).

**Key Success Factors**:
1. Phased approach allows testing and refinement
2. Low coupling between phases enables parallel work
3. Existing test infrastructure supports rapid validation
4. Clear success criteria for each phase

**Next Steps**:
1. Review this plan with stakeholders
2. Approve timeline and resource allocation
3. Create feature branch: `feature/v3.12.0-code-editing-excellence`
4. Begin Phase 1 implementation

---

**Document Version**: 1.0
**Created**: 2025-01-10
**Last Updated**: 2025-01-10
**Status**: Ready for Development
**Estimated Completion**: 2025-02-21
