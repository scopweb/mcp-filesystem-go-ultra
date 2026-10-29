package core

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const lineCensusMaxFile = 5 << 20

// LineCountOpts controls list_directory output_format=lines.
// MaxDepth 0 means no depth cap. MaxNodes caps files counted.
type LineCountOpts struct {
	MaxDepth      int
	MaxNodes      int
	Include       []string
	Exclude       []string
	RespectIgnore bool
}

// LineCountEntry preserves a census row for structured clients.
type LineCountEntry struct {
	Path  string
	Lines int
}

// LineCensus is a wc -l substitute: newline counts, sorted by path.
type LineCensus struct {
	Text      string
	Truncated bool
	Counted   int
	Entries   []LineCountEntry
}

// ListLineCounts walks path and counts newlines per text file.
// It skips build dirs (bin/obj) and, by default, gitignore. It does not
// follow symlinks. This replaces find | wc -l.
func (e *UltraFastEngine) ListLineCounts(ctx context.Context, path string, opts LineCountOpts) (LineCensus, error) {
	path = NormalizePath(path)
	if opts.MaxNodes <= 0 {
		opts.MaxNodes = 500
	}
	if err := e.acquireOperation(ctx, "lines"); err != nil {
		return LineCensus{}, err
	}
	start := time.Now()
	defer e.releaseOperation("lines", start)

	if !e.IsPathAllowed(path) {
		return LineCensus{}, &PathError{Op: "list", Path: path, Err: fmt.Errorf("access denied: path %q is not in allowed paths", path)}
	}
	if err := e.Authorize(OpRead, path); err != nil {
		return LineCensus{}, err
	}

	var ign *IgnoreMatcher
	if opts.RespectIgnore {
		ign = NewIgnoreMatcher()
	}

	type hit struct {
		path  string
		lines int
	}
	var hits []hit
	truncated := false

	var walk func(dir string, depth int) error
	walk = func(dir string, depth int) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if opts.MaxDepth > 0 && depth > opts.MaxDepth {
			return nil
		}
		entries, err := e.readDirWithinRoot(dir)
		if err != nil {
			return nil
		}
		for _, entry := range entries {
			name := entry.Name()
			if name == "." || name == ".." {
				continue
			}
			child := filepath.Join(dir, name)
			if entry.Type()&os.ModeSymlink != 0 {
				continue
			}
			isDir := entry.IsDir()
			if e.policySkipWalk(child, isDir) {
				continue
			}
			if skipWalkDir(name, child, path, isDir, ign, !opts.RespectIgnore) {
				continue
			}
			if matchExclude(name, child, opts.Exclude) {
				continue
			}
			if isDir {
				if searchSkipDirs[name] {
					continue
				}
				if err := walk(child, depth+1); err != nil {
					return err
				}
				continue
			}
			if len(opts.Include) > 0 {
				if !FileMatchesTypeFilterIn(child, path, opts.Include) {
					continue
				}
			} else if !isTextCandidate(child) {
				continue
			}
			if len(hits) >= opts.MaxNodes {
				truncated = true
				return nil
			}
			info, ierr := entry.Info()
			if ierr != nil || info.Size() > lineCensusMaxFile {
				continue
			}
			n, nerr := countNewlines(child)
			if nerr != nil {
				continue
			}
			hits = append(hits, hit{path: child, lines: n})
		}
		return nil
	}

	info, err := e.lstatWithinRoot(path)
	if err != nil {
		return LineCensus{}, err
	}
	if !info.IsDir() {
		n, nerr := countNewlines(path)
		if nerr != nil {
			return LineCensus{}, nerr
		}
		return LineCensus{Text: fmt.Sprintf("%d %s\n", n, path), Counted: 1, Entries: []LineCountEntry{{Path: path, Lines: n}}}, nil
	}
	if err := walk(path, 1); err != nil {
		return LineCensus{}, err
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].path < hits[j].path })
	var b strings.Builder
	entries := make([]LineCountEntry, 0, len(hits))
	for _, h := range hits {
		fmt.Fprintf(&b, "%d %s\n", h.lines, h.path)
		entries = append(entries, LineCountEntry{Path: h.path, Lines: h.lines})
	}
	if truncated {
		b.WriteString("truncated. Narrow include or raise max_nodes. Do not find/wc.\n")
	}
	return LineCensus{Text: b.String(), Truncated: truncated, Counted: len(hits), Entries: entries}, nil
}

func matchExclude(name, full string, exclude []string) bool {
	for _, g := range exclude {
		g = strings.TrimSpace(g)
		if g == "" {
			continue
		}
		if ok, _ := filepath.Match(g, name); ok {
			return true
		}
		if ok, _ := filepath.Match(g, filepath.Base(full)); ok {
			return true
		}
	}
	return false
}

func countNewlines(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	r := bufio.NewReader(io.LimitReader(f, lineCensusMaxFile))
	n := 0
	for {
		b, err := r.ReadByte()
		if err == io.EOF {
			return n, nil
		}
		if err != nil {
			return n, err
		}
		if b == 0 {
			return 0, nil
		}
		if b == '\n' {
			n++
		}
	}
}
