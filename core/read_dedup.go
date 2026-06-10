package core

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"golang.org/x/sync/singleflight"
)

// readFlight deduplicates concurrent disk reads for the same normalized path.
var readFlight singleflight.Group

// diskReadCount increments on each actual os.ReadFile in the dedup path (observable in tests).
var diskReadCount atomic.Int64

// readFileBytesDeduped returns file content from cache or disk, deduplicating
// concurrent loads for the same path via singleflight.
func (e *UltraFastEngine) readFileBytesDeduped(ctx context.Context, path string) ([]byte, error) {
	if cached, hit := e.cache.GetFile(path); hit {
		return cached, nil
	}

	type readResult struct {
		data []byte
		err  error
	}

	v, err, _ := readFlight.Do(path, func() (interface{}, error) {
		if cached, hit := e.cache.GetFile(path); hit {
			return readResult{data: cached}, nil
		}

		if err := ctx.Err(); err != nil {
			return nil, &ContextError{Op: "read_file", Details: "operation cancelled before disk read"}
		}

		diskReadCount.Add(1)
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil, &PathError{Op: "read", Path: path, Err: readErr}
		}

		e.cache.SetFile(path, data)
		e.cache.TrackAccess(path)
		return readResult{data: data}, nil
	})
	if err != nil {
		return nil, err
	}

	result := v.(readResult)
	return result.data, result.err
}

// forgetReadFlight drops any in-flight dedup state for path so invalidation
// does not let waiters attach to a stale load.
func forgetReadFlight(path string) {
	if path == "" {
		return
	}
	readFlight.Forget(path)
}

// invalidateFileReadCache removes cached bytes and forgets read dedup for path.
func (e *UltraFastEngine) invalidateFileReadCache(path string) {
	if e == nil || e.cache == nil || path == "" {
		forgetReadFlight(path)
		return
	}
	e.cache.InvalidateFile(path)
	forgetReadFlight(path)
}

// extractLineRangeFromBytes builds the same response shape as ReadFileRange's
// scanner path, including the footer with real total line count.
func extractLineRangeFromBytes(content []byte, path string, startLine, endLine int) (string, error) {
	if startLine < 1 {
		return "", fmt.Errorf("start_line must be >= 1, got %d", startLine)
	}
	if endLine < startLine {
		return "", fmt.Errorf("end_line (%d) must be >= start_line (%d)", endLine, startLine)
	}

	var result strings.Builder
	lineNum := 0

	scanner := bufioNewScannerBytes(content)
	for scanner.Scan() {
		lineNum++
		if lineNum < startLine {
			continue
		}
		if lineNum <= endLine {
			if result.Len() > 0 {
				result.WriteString("\n")
			}
			result.WriteString(scanner.Text())
		}
	}
	totalLines := lineNum

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("error reading file: %w", err)
	}

	actualEndLine := endLine
	if actualEndLine > totalLines {
		actualEndLine = totalLines
	}

	footer := fmt.Sprintf("\n\n[Lines %d-%d of %d total lines in %s",
		startLine, actualEndLine, totalLines, filepath.Base(path))
	if actualEndLine < totalLines {
		rangeSize := endLine - startLine
		if rangeSize < 0 {
			rangeSize = 0
		}
		nextStart := actualEndLine + 1
		nextEnd := nextStart + rangeSize
		if nextEnd > totalLines {
			nextEnd = totalLines
		}
		footer += fmt.Sprintf(" \u2014 use start_line/end_line to read more, e.g. start_line=%d end_line=%d",
			nextStart, nextEnd)
	}
	footer += "]"
	result.WriteString(footer)

	return result.String(), nil
}

// bufioNewScannerBytes is a thin wrapper so tests can scan in-memory content
// the same way as file_operations.ReadFileRange.
func bufioNewScannerBytes(content []byte) *bytesLineScanner {
	return newBytesLineScanner(content)
}

// bytesLineScanner provides bufio.Scanner-like Scan/Text over a byte slice.
type bytesLineScanner struct {
	lines []string
	idx   int
	err   error
}

func newBytesLineScanner(content []byte) *bytesLineScanner {
	// Match bufio.Scanner line semantics: a trailing newline does not add
	// an extra empty line (unlike naive strings.Split).
	if len(content) == 0 {
		return &bytesLineScanner{lines: nil}
	}

	raw := strings.Split(string(content), "\n")
	if len(raw) > 0 && raw[len(raw)-1] == "" && content[len(content)-1] == '\n' {
		raw = raw[:len(raw)-1]
	}

	lines := make([]string, len(raw))
	for i, line := range raw {
		lines[i] = strings.TrimRight(line, "\r")
	}
	return &bytesLineScanner{lines: lines}
}

func (s *bytesLineScanner) Scan() bool {
	if s.idx < len(s.lines) {
		s.idx++
		return true
	}
	return false
}

func (s *bytesLineScanner) Text() string {
	if s.idx == 0 || s.idx > len(s.lines) {
		return ""
	}
	return s.lines[s.idx-1]
}

func (s *bytesLineScanner) Err() error {
	return s.err
}