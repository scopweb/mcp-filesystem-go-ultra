package core

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// ProtectionLevel is the file-security classification. Higher values are
// more restrictive. A child rule cannot weaken an ancestor.
type ProtectionLevel int

const (
	LevelNormal ProtectionLevel = iota
	LevelReadOnly
	LevelProtected
	LevelHidden
)

func (l ProtectionLevel) String() string {
	switch l {
	case LevelReadOnly:
		return "read_only"
	case LevelProtected:
		return "protected"
	case LevelHidden:
		return "hidden"
	default:
		return "normal"
	}
}

func ParseProtectionLevel(s string) (ProtectionLevel, error) {
	switch strings.TrimSpace(s) {
	case "normal":
		return LevelNormal, nil
	case "read_only":
		return LevelReadOnly, nil
	case "protected":
		return LevelProtected, nil
	case "hidden":
		return LevelHidden, nil
	default:
		return 0, fmt.Errorf("unknown protection level %q", s)
	}
}

// PolicyOp is the operation class checked against a path's effective level.
type PolicyOp int

const (
	OpDiscover PolicyOp = iota
	OpRead
	OpMetadata
	OpCreate
	OpWrite
	OpDelete
	OpCopyRead
	OpCopyWrite
	OpMove
	OpRestore
	OpBackupCreate
)

// FilePolicyError is the public policy failure. Hidden is true when the
// caller must present the path as missing. Force, dry_run and risk flags
// must not override this error.
type FilePolicyError struct {
	Op      string
	Path    string
	Level   ProtectionLevel
	Hidden  bool
	Message string
}

func (e *FilePolicyError) Error() string {
	if e == nil {
		return "file policy denied"
	}
	if e.Hidden {
		if e.Path == "" {
			return "file does not exist"
		}
		return fmt.Sprintf("open %s: file does not exist", e.Path)
	}
	if e.Message != "" {
		return e.Message
	}
	return "operation denied by file policy"
}

func (e *FilePolicyError) Unwrap() error {
	if e != nil && e.Hidden {
		return os.ErrNotExist
	}
	return nil
}

// PolicyDecision is the result of evaluating one path for one operation.
type PolicyDecision struct {
	Level   ProtectionLevel
	Allow   bool
	Redact  bool // protected metadata: name, type, level only
	Hidden  bool
	Message string
}

type compiledRule struct {
	raw      string
	level    ProtectionLevel
	basename bool
	segments []string
}

type policyPin struct {
	key   string
	dir   bool
	level ProtectionLevel
}

// FilePolicy is an immutable rule set loaded at startup. Concurrent reads
// are safe. There is no reload API.
type FilePolicy struct {
	enabled bool
	rules   []compiledRule
	pins    []policyPin
	hash    string
	mu      sync.RWMutex // pins are appended once at startup, before serve
}

type policyDocument struct {
	Version int          `json:"version"`
	Rules   []policyRule `json:"rules"`
}

type policyRule struct {
	Pattern string `json:"pattern"`
	Level   string `json:"level"`
}

// LoadFilePolicy reads and strictly validates a policy file. The returned
// policy is enabled even when rules is empty. It does not pin the config
// path; call PinConfig after a successful load.
func LoadFilePolicy(path string) (*FilePolicy, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("file security config: %w", err)
	}
	return ParseFilePolicy(raw)
}

// ParseFilePolicy validates a policy document. Unknown fields, trailing
// JSON, bad versions, and ambiguous patterns are rejected.
func ParseFilePolicy(raw []byte) (*FilePolicy, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var doc policyDocument
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("file security config: %w", err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("file security config: trailing data after JSON document")
		}
		return nil, fmt.Errorf("file security config: trailing data: %w", err)
	}
	if doc.Version != 1 {
		return nil, fmt.Errorf("file security config: unsupported version %d (want 1)", doc.Version)
	}
	if doc.Rules == nil {
		return nil, fmt.Errorf("file security config: missing rules array")
	}
	rules := make([]compiledRule, 0, len(doc.Rules))
	for i, rule := range doc.Rules {
		compiled, err := compilePattern(rule.Pattern, rule.Level)
		if err != nil {
			return nil, fmt.Errorf("file security config: rules[%d]: %w", i, err)
		}
		rules = append(rules, compiled)
	}
	p := &FilePolicy{enabled: true, rules: rules}
	p.hash = policyHash(rules)
	return p, nil
}

func policyHash(rules []compiledRule) string {
	var b strings.Builder
	for _, r := range rules {
		b.WriteString(r.level.String())
		b.WriteByte('\t')
		b.WriteString(r.raw)
		b.WriteByte('\n')
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// RejectInsecureOpen reports an error when relative patterns cannot be
// evaluated because the sandbox has no roots.
func (p *FilePolicy) RejectInsecureOpen() error {
	if p == nil || !p.enabled {
		return nil
	}
	for _, r := range p.rules {
		if !r.basename {
			return fmt.Errorf("--file-security-config contains separator patterns, which are evaluated relative to sandbox roots; --insecure-open has no roots, so this combination is rejected")
		}
	}
	return nil
}

// Enabled reports whether a policy file was loaded.
func (p *FilePolicy) Enabled() bool {
	return p != nil && p.enabled
}

// Hash is an opaque fingerprint of the compiled rules. It is not a pattern list.
func (p *FilePolicy) Hash() string {
	if p == nil {
		return ""
	}
	return p.hash
}

// PinConfig protects the configuration file and, when it is a symlink, its
// target. Both are at least protected so tools cannot read or rewrite the
// policy. Call once at startup.
func (p *FilePolicy) PinConfig(path string) error {
	if p == nil {
		return fmt.Errorf("nil file policy")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("file security config path: %w", err)
	}
	abs = filepath.Clean(abs)
	p.pinExact(abs, LevelProtected)
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		p.pinExact(filepath.Clean(resolved), LevelProtected)
	}
	return nil
}

// PinDirectory marks a directory and its descendants as at least level.
// Used for backup, receipt and log directories so tools cannot read stored
// copies. Internal backup code does not go through Authorize.
func (p *FilePolicy) PinDirectory(path string, level ProtectionLevel) {
	if p == nil || path == "" {
		return
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pins = append(p.pins, policyPin{key: foldPath(filepath.Clean(abs)), dir: true, level: level})
}

func (p *FilePolicy) pinExact(path string, level ProtectionLevel) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pins = append(p.pins, policyPin{key: foldPath(filepath.Clean(path)), dir: false, level: level})
}

// HasRelativeRules reports whether any rule contains a path separator.
func (p *FilePolicy) HasRelativeRules() bool {
	if p == nil {
		return false
	}
	for _, r := range p.rules {
		if !r.basename {
			return true
		}
	}
	return false
}

// Level is the most restrictive level of path and its ancestors, including
// pins. roots are the effective sandbox roots used for separator patterns.
// logical and canonical are both evaluated; the higher level wins.
func (p *FilePolicy) Level(logical, canonical string, roots []string) ProtectionLevel {
	if p == nil || !p.enabled {
		return LevelNormal
	}
	best := LevelNormal
	for _, candidate := range []string{logical, canonical} {
		if candidate == "" {
			continue
		}
		best = maxLevel(best, p.levelWalk(candidate, roots))
	}
	return best
}

func (p *FilePolicy) levelWalk(path string, roots []string) ProtectionLevel {
	best := LevelNormal
	cur := filepath.Clean(path)
	for {
		best = maxLevel(best, p.matchPath(cur, roots))
		best = maxLevel(best, p.pinnedLevel(cur))
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	return best
}

func (p *FilePolicy) pinnedLevel(path string) ProtectionLevel {
	if p == nil {
		return LevelNormal
	}
	key := foldPath(filepath.Clean(path))
	p.mu.RLock()
	defer p.mu.RUnlock()
	best := LevelNormal
	for _, pin := range p.pins {
		if pin.dir {
			if key == pin.key || strings.HasPrefix(key, pin.key+string(filepath.Separator)) {
				best = maxLevel(best, pin.level)
			}
			continue
		}
		if key == pin.key {
			best = maxLevel(best, pin.level)
		}
	}
	return best
}

func (p *FilePolicy) matchPath(path string, roots []string) ProtectionLevel {
	best := LevelNormal
	slash := filepath.ToSlash(path)
	segments := splitSegments(slash)
	for _, rule := range p.rules {
		if rule.basename {
			for _, seg := range segments {
				if matchSegment(rule.segments[0], seg) {
					best = maxLevel(best, rule.level)
					break
				}
			}
			continue
		}
		for _, root := range roots {
			rel, ok := relUnderRoot(root, path)
			if !ok {
				continue
			}
			if matchGlob(rule.segments, splitSegments(rel)) {
				best = maxLevel(best, rule.level)
			}
		}
	}
	return best
}

// Decide applies the allow matrix. Hidden denials must be presented as
// not-found. Protected metadata is allowed only in redacted form.
func Decide(level ProtectionLevel, op PolicyOp) PolicyDecision {
	d := PolicyDecision{Level: level, Allow: true}
	switch level {
	case LevelNormal:
		return d
	case LevelReadOnly:
		switch op {
		case OpDiscover, OpRead, OpMetadata, OpCopyRead, OpBackupCreate:
			return d
		default:
			d.Allow = false
			d.Message = "operation denied by file policy: path is read-only"
			return d
		}
	case LevelProtected:
		switch op {
		case OpDiscover:
			return d
		case OpMetadata:
			d.Redact = true
			return d
		default:
			d.Allow = false
			d.Message = "operation denied by file policy: path is protected"
			return d
		}
	default:
		d.Allow = false
		d.Hidden = true
		d.Message = "file does not exist"
		return d
	}
}

func compilePattern(pattern, level string) (compiledRule, error) {
	lvl, err := ParseProtectionLevel(level)
	if err != nil {
		return compiledRule{}, err
	}
	raw := strings.TrimSpace(pattern)
	if raw == "" {
		return compiledRule{}, fmt.Errorf("empty pattern")
	}
	if strings.ContainsRune(raw, 0) {
		return compiledRule{}, fmt.Errorf("pattern contains NUL")
	}
	if strings.Contains(raw, "$") {
		return compiledRule{}, fmt.Errorf("pattern must not contain environment-variable syntax")
	}
	if strings.ContainsAny(raw, "[]{}") {
		return compiledRule{}, fmt.Errorf("character classes and braces are not supported")
	}
	normalized := strings.ReplaceAll(raw, "\\", "/")
	normalized = strings.TrimSuffix(normalized, "/")
	if normalized == "" {
		return compiledRule{}, fmt.Errorf("empty pattern")
	}
	if isAbsolutePattern(normalized) {
		return compiledRule{}, fmt.Errorf("absolute patterns are not allowed")
	}
	if normalized == "~" || strings.HasPrefix(normalized, "~/") {
		return compiledRule{}, fmt.Errorf("tilde expansion is not allowed")
	}
	segments := splitSegments(normalized)
	if len(segments) == 0 {
		return compiledRule{}, fmt.Errorf("empty pattern")
	}
	for _, seg := range segments {
		if seg == "" {
			return compiledRule{}, fmt.Errorf("empty path segment")
		}
		if seg == "." || seg == ".." {
			return compiledRule{}, fmt.Errorf("segment %q is not allowed", seg)
		}
		if seg == "~" {
			return compiledRule{}, fmt.Errorf("tilde expansion is not allowed")
		}
		if strings.Contains(seg, "**") && seg != "**" {
			return compiledRule{}, fmt.Errorf("** must be a whole path segment")
		}
	}
	return compiledRule{
		raw:      normalized,
		level:    lvl,
		basename: !strings.Contains(normalized, "/"),
		segments: segments,
	}, nil
}

func isAbsolutePattern(p string) bool {
	if strings.HasPrefix(p, "/") {
		return true
	}
	if len(p) >= 2 && p[1] == ':' {
		c := p[0]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
			return true
		}
	}
	return false
}

func splitSegments(slashPath string) []string {
	slashPath = strings.Trim(slashPath, "/")
	if slashPath == "" || slashPath == "." {
		return nil
	}
	parts := strings.Split(slashPath, "/")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "" || p == "." {
			continue
		}
		// Drop a Windows volume prefix so basename rules do not match "C:".
		if len(p) == 2 && p[1] == ':' {
			continue
		}
		out = append(out, p)
	}
	return out
}

func relUnderRoot(root, path string) (string, bool) {
	if rel, ok := relUnderClean(root, path); ok {
		return rel, true
	}
	if runtime.GOOS != "windows" {
		return "", false
	}
	return relUnderClean(evalOrSame(root), evalOrSame(path))
}

func evalOrSame(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		abs = p
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	return filepath.Clean(abs)
}

func relUnderClean(root, path string) (string, bool) {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return "", false
	}
	if rel == "." {
		return "", false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

func matchGlob(pattern, path []string) bool {
	return matchFrom(pattern, path, 0, 0)
}

func matchFrom(pattern, path []string, pi, si int) bool {
	for pi < len(pattern) {
		if pattern[pi] == "**" {
			if pi == len(pattern)-1 {
				return true
			}
			for skip := 0; si+skip <= len(path); skip++ {
				if matchFrom(pattern, path, pi+1, si+skip) {
					return true
				}
			}
			return false
		}
		if si >= len(path) {
			return false
		}
		if !matchSegment(pattern[pi], path[si]) {
			return false
		}
		pi++
		si++
	}
	return si == len(path)
}

func matchSegment(pattern, name string) bool {
	return matchWild(foldName(pattern), foldName(name), 0, 0)
}

func matchWild(pattern, name string, pi, ni int) bool {
	for pi < len(pattern) {
		switch pattern[pi] {
		case '*':
			if pi == len(pattern)-1 {
				return true
			}
			for n := ni; n <= len(name); n++ {
				if matchWild(pattern, name, pi+1, n) {
					return true
				}
			}
			return false
		case '?':
			if ni >= len(name) {
				return false
			}
			pi++
			ni++
		default:
			if ni >= len(name) || pattern[pi] != name[ni] {
				return false
			}
			pi++
			ni++
		}
	}
	return ni == len(name)
}

func maxLevel(a, b ProtectionLevel) ProtectionLevel {
	if b > a {
		return b
	}
	return a
}

func policyCaseFold() bool {
	return runtime.GOOS == "windows" || os.PathSeparator == '\\'
}

func foldName(s string) string {
	if policyCaseFold() {
		return strings.ToLower(s)
	}
	return s
}

func foldPath(p string) string {
	if policyCaseFold() {
		return strings.ToLower(p)
	}
	return p
}

// ResolvePolicyPaths returns the cleaned absolute logical path and a
// canonical path. Missing destinations use the deepest existing ancestor
// plus the pending segments, so a symlink parent cannot hide the target.
func ResolvePolicyPaths(path string) (logical, canonical string) {
	path = NormalizePath(path)
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	logical = filepath.Clean(abs)
	if resolved, err := filepath.EvalSymlinks(logical); err == nil {
		return logical, filepath.Clean(resolved)
	}
	current := logical
	var suffix []string
	for {
		parent := filepath.Dir(current)
		suffix = append([]string{filepath.Base(current)}, suffix...)
		if parent == current {
			break
		}
		if parentResolved, parentErr := filepath.EvalSymlinks(parent); parentErr == nil {
			canon := parentResolved
			for _, s := range suffix {
				canon = filepath.Join(canon, s)
			}
			return logical, filepath.Clean(canon)
		}
		current = parent
	}
	return logical, logical
}
