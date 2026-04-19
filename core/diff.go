package core

import (
	"fmt"
	"strings"
)

// UnifiedDiff generates a unified diff string from old and new content.
// Format follows the standard unified diff (-3 context lines by default).
func UnifiedDiff(oldContent, newContent, filePath string) string {
	return UnifiedDiffContext(oldContent, newContent, filePath, 3)
}

// UnifiedDiffContext generates a unified diff with configurable context lines.
func UnifiedDiffContext(oldContent, newContent, filePath string, contextLines int) string {
	oldLines := splitLines(oldContent)
	newLines := splitLines(newContent)

	hunks := computeHunks(oldLines, newLines, contextLines)
	if len(hunks) == 0 {
		return "" // no changes
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("--- a/%s\n", filePath))
	sb.WriteString(fmt.Sprintf("+++ b/%s\n", filePath))

	for _, h := range hunks {
		sb.WriteString(h.header())
		for _, line := range h.lines {
			sb.WriteString(line)
			if !strings.HasSuffix(line, "\n") {
				sb.WriteString("\n")
			}
		}
	}

	return sb.String()
}

// DiffStats returns a compact summary of lines added/removed (e.g. "+5 -3").
func DiffStats(oldContent, newContent string) string {
	oldLines := splitLines(oldContent)
	newLines := splitLines(newContent)
	added, removed := countChanges(oldLines, newLines)
	return fmt.Sprintf("+%d -%d", added, removed)
}

// --- internal types ---

type hunk struct {
	oldStart int
	oldCount int
	newStart int
	newCount int
	lines    []string
}

func (h *hunk) header() string {
	return fmt.Sprintf("@@ -%d,%d +%d,%d @@\n", h.oldStart, h.oldCount, h.newStart, h.newCount)
}

// --- LCS-based diff engine ---

// editType marks a diff operation
type editType int

const (
	editEqual  editType = iota
	editInsert          // present in new, absent in old
	editDelete          // present in old, absent in new
)

type edit struct {
	kind editType
	text string
}

// diff computes the edit script between oldLines and newLines using Myers algorithm (simplified).
// For simplicity we use the standard DP LCS approach — adequate for file sizes we handle.
func diff(oldLines, newLines []string) []edit {
	m := len(oldLines)
	n := len(newLines)

	// dp[i][j] = length of LCS for oldLines[:i], newLines[:j]
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}
	for i := m - 1; i >= 0; i-- {
		for j := n - 1; j >= 0; j-- {
			if oldLines[i] == newLines[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] > dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}

	// backtrack
	var edits []edit
	i, j := 0, 0
	for i < m && j < n {
		if oldLines[i] == newLines[j] {
			edits = append(edits, edit{editEqual, oldLines[i]})
			i++
			j++
		} else if dp[i+1][j] >= dp[i][j+1] {
			edits = append(edits, edit{editDelete, oldLines[i]})
			i++
		} else {
			edits = append(edits, edit{editInsert, newLines[j]})
			j++
		}
	}
	for ; i < m; i++ {
		edits = append(edits, edit{editDelete, oldLines[i]})
	}
	for ; j < n; j++ {
		edits = append(edits, edit{editInsert, newLines[j]})
	}
	return edits
}

func computeHunks(oldLines, newLines []string, ctx int) []hunk {
	edits := diff(oldLines, newLines)

	var hunks []hunk
	var current *hunk

	oldLine := 1
	newLine := 1

	for idx, e := range edits {
		switch e.kind {
		case editEqual:
			if current != nil {
				// Check if we are still within context range of a change
				remainingChanges := hasChangeAhead(edits[idx:], ctx)
				if remainingChanges {
					current.lines = append(current.lines, " "+e.text)
					current.oldCount++
					current.newCount++
				} else {
					// Add trailing context lines
					current.lines = append(current.lines, " "+e.text)
					current.oldCount++
					current.newCount++
					if trailingContext(edits[idx:]) >= ctx {
						hunks = append(hunks, *current)
						current = nil
					}
				}
			}
			oldLine++
			newLine++

		case editDelete:
			if current == nil {
				// Start a new hunk — backtrack ctx lines for leading context
				start := idx - ctx
				if start < 0 {
					start = 0
				}
				current = &hunk{
					oldStart: oldLine - (idx - start),
					newStart: newLine - (idx - start),
				}
				// Add leading context
				for _, prev := range edits[start:idx] {
					if prev.kind == editEqual {
						current.lines = append(current.lines, " "+prev.text)
						current.oldCount++
						current.newCount++
					}
				}
			}
			current.lines = append(current.lines, "-"+e.text)
			current.oldCount++
			oldLine++

		case editInsert:
			if current == nil {
				start := idx - ctx
				if start < 0 {
					start = 0
				}
				current = &hunk{
					oldStart: oldLine - (idx - start),
					newStart: newLine - (idx - start),
				}
				for _, prev := range edits[start:idx] {
					if prev.kind == editEqual {
						current.lines = append(current.lines, " "+prev.text)
						current.oldCount++
						current.newCount++
					}
				}
			}
			current.lines = append(current.lines, "+"+e.text)
			current.newCount++
			newLine++
		}
	}

	if current != nil {
		hunks = append(hunks, *current)
	}

	return hunks
}

func hasChangeAhead(edits []edit, within int) bool {
	for i, e := range edits {
		if i >= within {
			break
		}
		if e.kind != editEqual {
			return true
		}
	}
	return false
}

func trailingContext(edits []edit) int {
	count := 0
	for _, e := range edits {
		if e.kind == editEqual {
			count++
		} else {
			return count
		}
	}
	return count
}

func countChanges(oldLines, newLines []string) (added, removed int) {
	edits := diff(oldLines, newLines)
	for _, e := range edits {
		switch e.kind {
		case editInsert:
			added++
		case editDelete:
			removed++
		}
	}
	return
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	// preserve trailing newline behaviour: don't add a phantom empty line
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	result := make([]string, len(lines))
	for i, l := range lines {
		result[i] = l
	}
	return result
}
