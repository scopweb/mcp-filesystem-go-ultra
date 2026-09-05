package core

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type TreeOpts struct {
	MaxDepth      int
	MaxNodes      int
	Exclude       []string
	RespectIgnore bool
	Format        string // "json" (default) or "compact"
}

type treeNode struct {
	Name          string      `json:"name"`
	Type          string      `json:"type"`
	Size          int64       `json:"size,omitempty"`
	Children      []*treeNode `json:"children,omitempty"`
	Truncated     bool        `json:"truncated,omitempty"`
	SkippedIgnore int         `json:"skipped_ignore,omitempty"`
	SkippedExcl   int         `json:"skipped_exclude,omitempty"`
}

func (e *UltraFastEngine) ListDirectoryTreeOpts(ctx context.Context, path string, opts TreeOpts) (string, error) {
	path = NormalizePath(path)
	if opts.MaxDepth <= 0 {
		opts.MaxDepth = 2
	}
	if opts.MaxNodes <= 0 {
		opts.MaxNodes = 500
	}
	if opts.Format == "" {
		opts.Format = "json"
	}

	if err := e.acquireOperation(ctx, "tree"); err != nil {
		return "", err
	}
	start := time.Now()
	defer e.releaseOperation("tree", start)

	if !e.IsPathAllowed(path) {
		return "", fmt.Errorf("access denied: path '%s' is not in allowed paths%s", path, e.AllowedDirsSuffix())
	}

	var ign *IgnoreMatcher
	if opts.RespectIgnore {
		ign = NewIgnoreMatcher()
	}

	nodes := 0
	skippedIgnore := 0
	skippedExcl := 0
	truncated := false

	var build func(dirPath string, depth int) (*treeNode, error)
	build = func(dirPath string, depth int) (*treeNode, error) {
		if depth > opts.MaxDepth {
			return nil, nil
		}
		if nodes >= opts.MaxNodes {
			truncated = true
			return nil, nil
		}

		info, err := os.Lstat(dirPath)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, nil
		}

		node := &treeNode{
			Name: filepath.Base(dirPath),
			Type: "file",
			Size: info.Size(),
		}
		nodes++

		if !info.IsDir() {
			return node, nil
		}
		node.Type = "directory"
		node.Size = 0

		entries, err := os.ReadDir(dirPath)
		if err != nil {
			return node, nil
		}
		node.Children = make([]*treeNode, 0, len(entries))
		for _, entry := range entries {
			if ctx.Err() != nil {
				break
			}
			if nodes >= opts.MaxNodes {
				truncated = true
				break
			}
			childPath := filepath.Join(dirPath, entry.Name())
			if entry.Type()&os.ModeSymlink != 0 {
				continue
			}
			isDir := entry.IsDir()
			if skipWalkDir(entry.Name(), childPath, path, isDir, ign, !opts.RespectIgnore) {
				skippedIgnore++
				continue
			}
			if excludedName(entry.Name(), childPath, path, opts.Exclude) {
				skippedExcl++
				continue
			}
			if isDir {
				child, err := build(childPath, depth+1)
				if err == nil && child != nil {
					node.Children = append(node.Children, child)
				}
			} else {
				childInfo, err := entry.Info()
				size := int64(0)
				if err == nil {
					size = childInfo.Size()
				}
				nodes++
				node.Children = append(node.Children, &treeNode{
					Name: entry.Name(),
					Type: "file",
					Size: size,
				})
			}
		}
		return node, nil
	}

	tree, err := build(path, 0)
	if err != nil {
		return "", fmt.Errorf("failed to build directory tree: %w", err)
	}
	if tree == nil {
		return "", fmt.Errorf("failed to build directory tree: empty")
	}
	tree.Truncated = truncated
	tree.SkippedIgnore = skippedIgnore
	tree.SkippedExcl = skippedExcl

	if opts.Format == "compact" {
		return renderCompactTree(tree, opts), nil
	}
	data, err := json.MarshalIndent(tree, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal tree: %w", err)
	}
	return string(data), nil
}

func excludedName(name, absPath, root string, patterns []string) bool {
	if len(patterns) == 0 {
		return false
	}
	rel, err := filepath.Rel(root, absPath)
	if err != nil {
		rel = name
	}
	rel = filepath.ToSlash(rel)
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		p = filepath.ToSlash(p)
		if ok, _ := filepath.Match(p, name); ok {
			return true
		}
		if ok, _ := filepath.Match(p, rel); ok {
			return true
		}
		if strings.Contains(rel, p) && !strings.ContainsAny(p, "*?") {
			return true
		}
	}
	return false
}

func renderCompactTree(n *treeNode, opts TreeOpts) string {
	var b strings.Builder
	var walk func(node *treeNode, indent string)
	walk = func(node *treeNode, indent string) {
		if node.Type == "directory" {
			b.WriteString(indent)
			b.WriteString(node.Name)
			b.WriteString("/\n")
			for _, ch := range node.Children {
				walk(ch, indent+"  ")
			}
			return
		}
		b.WriteString(indent)
		b.WriteString(node.Name)
		if node.Size > 0 {
			b.WriteString("  ")
			b.WriteString(humanBytes(node.Size))
		}
		b.WriteByte('\n')
	}
	walk(n, "")
	if n.Truncated || n.SkippedIgnore > 0 || n.SkippedExcl > 0 {
		fmt.Fprintf(&b, "[truncated: max_nodes=%d, skipped gitignore=%d, skipped exclude=%d]\n",
			opts.MaxNodes, n.SkippedIgnore, n.SkippedExcl)
	}
	return b.String()
}

func humanBytes(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%dB", n)
	}
	if n < 1024*1024 {
		return fmt.Sprintf("%dk", n/1024)
	}
	return fmt.Sprintf("%.1fM", float64(n)/(1024*1024))
}
