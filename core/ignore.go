package core

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

var ignoreFileNames = []string{".gitignore", ".cursorignore", ".fsultraignore"}

type ignoreRule struct {
	negated bool
	dirOnly bool
	re      *regexp.Regexp
}

// IgnoreMatcher evaluates gitignore-style rules from .gitignore, .cursorignore
// and .fsultraignore. Parent-directory files apply; last matching rule wins.
type IgnoreMatcher struct {
	mu    sync.Mutex
	cache map[string][]ignoreRule // abs dir → rules from ignore files in that dir
}

func NewIgnoreMatcher() *IgnoreMatcher {
	return &IgnoreMatcher{cache: make(map[string][]ignoreRule)}
}

func (m *IgnoreMatcher) Match(absPath string, isDir bool) bool {
	if m == nil {
		return false
	}
	absPath = filepath.Clean(absPath)
	dir := absPath
	if !isDir {
		dir = filepath.Dir(absPath)
	}
	chain := ancestorDirs(dir)
	ignored := false
	for _, d := range chain {
		rel, err := filepath.Rel(d, absPath)
		if err != nil {
			continue
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			continue
		}
		for _, rule := range m.rulesFor(d) {
			if rule.dirOnly && !isDir {
				continue
			}
			if rule.re.MatchString(rel) {
				ignored = !rule.negated
			}
		}
	}
	return ignored
}

func (m *IgnoreMatcher) rulesFor(dir string) []ignoreRule {
	m.mu.Lock()
	defer m.mu.Unlock()
	if rules, ok := m.cache[dir]; ok {
		return rules
	}
	var rules []ignoreRule
	for _, name := range ignoreFileNames {
		p := filepath.Join(dir, name)
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			if r := parseIgnoreLine(sc.Text()); r != nil {
				rules = append(rules, *r)
			}
		}
		_ = f.Close()
	}
	m.cache[dir] = rules
	return rules
}

func parseIgnoreLine(line string) *ignoreRule {
	line = strings.TrimRight(line, "\r")
	if line == "" || strings.HasPrefix(line, "#") {
		return nil
	}
	negated := false
	if strings.HasPrefix(line, "\\!") {
		line = line[1:]
	} else if strings.HasPrefix(line, "!") {
		negated = true
		line = line[1:]
	}
	if line == "" {
		return nil
	}
	dirOnly := strings.HasSuffix(line, "/")
	if dirOnly {
		line = strings.TrimSuffix(line, "/")
	}
	anchored := strings.HasPrefix(line, "/") || strings.Contains(strings.TrimPrefix(line, "/"), "/")
	if strings.HasPrefix(line, "/") {
		line = line[1:]
	}
	re, err := compileGitGlob(line, anchored)
	if err != nil {
		return nil
	}
	return &ignoreRule{negated: negated, dirOnly: dirOnly, re: re}
}

func compileGitGlob(pat string, anchored bool) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteString("^")
	if !anchored {
		b.WriteString("(?:.*/)?")
	}
	for i := 0; i < len(pat); {
		if i+1 < len(pat) && pat[i] == '*' && pat[i+1] == '*' {
			if i+2 < len(pat) && pat[i+2] == '/' {
				b.WriteString("(?:.*/)?")
				i += 3
				continue
			}
			b.WriteString(".*")
			i += 2
			continue
		}
		switch pat[i] {
		case '*':
			b.WriteString("[^/]*")
		case '?':
			b.WriteString("[^/]")
		case '.', '+', '(', ')', '|', '^', '$', '{', '}', '[', ']', '\\':
			b.WriteByte('\\')
			b.WriteByte(pat[i])
		default:
			b.WriteByte(pat[i])
		}
		i++
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}

func ancestorDirs(dir string) []string {
	dir = filepath.Clean(dir)
	var up []string
	for {
		up = append(up, dir)
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	for i, j := 0, len(up)-1; i < j; i, j = i+1, j-1 {
		up[i], up[j] = up[j], up[i]
	}
	return up
}

func extraRipgrepIgnoreFiles(start string) []string {
	d := start
	if fi, err := os.Stat(start); err == nil && !fi.IsDir() {
		d = filepath.Dir(start)
	}
	var files []string
	for i := 0; i < 32; i++ {
		for _, name := range []string{".cursorignore", ".fsultraignore"} {
			p := filepath.Join(d, name)
			if _, err := os.Stat(p); err == nil {
				files = append(files, p)
			}
		}
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			break
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}
	return files
}

func skipWalkDir(name, absPath, walkRoot string, isDir bool, ign *IgnoreMatcher, noIgnore bool) bool {
	if absPath == walkRoot {
		return false
	}
	if IsSecretPath(absPath) {
		return true
	}
	if isDir && searchSkipDirs[name] {
		return true
	}
	if noIgnore || ign == nil {
		return false
	}
	return ign.Match(absPath, isDir)
}
