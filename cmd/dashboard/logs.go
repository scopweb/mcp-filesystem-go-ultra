package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
)

// auditLines correlates lifecycle breadcrumbs before applying limits. Aggregates
// count only completed top-level calls; the operation view retains unfinished
// calls and pipeline steps. Invalid or partially written JSON is never served.
func auditLines(data []byte, includePending bool) []string {
	type record struct {
		entry AuditEntry
		line  string
	}
	var records []record
	finished := make(map[string]bool)
	for _, line := range strings.Split(string(data), "\n") {
		var e AuditEntry
		if json.Unmarshal([]byte(line), &e) != nil || e.Tool == "" {
			continue
		}
		if e.Status != "in_progress" && e.RequestID != "" && !strings.HasPrefix(e.SubOp, "step:") {
			finished[e.RequestID] = true
		}
		records = append(records, record{e, line})
	}
	lines := make([]string, 0, len(records))
	for _, r := range records {
		if r.entry.Status == "in_progress" {
			if !includePending || finished[r.entry.RequestID] {
				continue
			}
		} else if !includePending && strings.HasPrefix(r.entry.SubOp, "step:") {
			continue
		}
		lines = append(lines, r.line)
	}
	return lines
}

// streamAuditLine drops start breadcrumbs. The poll shows an unfinished call;
// the live stream must not also keep that start after the completion arrives.
func streamAuditLine(line string) bool {
	var e struct {
		Status string `json:"status"`
	}
	if json.Unmarshal([]byte(line), &e) != nil {
		return false
	}
	return e.Status != "" && e.Status != "in_progress"
}

// logTail follows replacement and truncation, retaining partial lines until the
// writer terminates them. The caller owns it (one per SSE connection).
type logTail struct {
	info   os.FileInfo
	offset int64
}

func (t *logTail) read(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil
	}
	if t.info != nil && (!os.SameFile(t.info, info) || info.Size() < t.offset) {
		t.offset = 0
	}
	t.info = info
	if _, err := f.Seek(t.offset, io.SeekStart); err != nil {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(f, 16<<20))
	if err != nil {
		return nil
	}
	end := bytes.LastIndexByte(data, '\n')
	if end < 0 {
		return nil
	}
	t.offset += int64(end + 1)
	var lines []string
	for _, line := range bytes.Split(data[:end], []byte{'\n'}) {
		if json.Valid(line) {
			lines = append(lines, string(line))
		}
	}
	return lines
}
