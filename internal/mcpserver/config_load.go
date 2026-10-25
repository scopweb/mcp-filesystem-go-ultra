package mcpserver

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/mcp/filesystem-ultra/core"
)

const (
	mcpPathsFilename   = ".mcp-paths"
	defaultEnvFilename = ".env"
)

// supportedEnvKeys are the only environment variables this process reads.
// Other MCP_* names are ignored. There is no ${VAR} expansion.
var supportedEnvKeys = []string{
	"MCP_ALLOWED_PATHS",
	"MCP_PROFILE",
	"MCP_ROOTS_MODE",
	"MCP_COMPACT_MODE",
	"MCP_READONLY",
	"MCP_AUTO_OCC",
	"MCP_LOG_LEVEL",
	"MCP_BACKUP_DIR",
}

// startupInput is the parsed CLI plus the hooks tests use to avoid the
// process environment and the real disk. Nil hooks fall back to os.*.
//
// Precedence for scalar settings: explicit CLI flag > --preset > process
// environment > .env file > flag default. Process environment wins over the
// file when both set the same key. Allowed paths are not overridden: CLI
// (--allowed-paths and positional args), the paths file, and MCP_ALLOWED_PATHS
// are unioned. CLI entries stay first. Duplicates are dropped by normalized
// identity. An explicit flag is never ignored.
type startupInput struct {
	CWD string

	AllowedPathsFlag string
	Positional       []string
	PathsFile        string
	PathsFileSet     bool
	EnvFile          string
	EnvFileSet       bool
	Preset           string

	Profile     string
	RootsMode   string
	CompactMode bool
	ReadOnly    bool
	AutoOCC     string
	LogLevel    string
	BackupDir   string

	Visited map[string]bool

	Lookup func(string) (string, bool)
	Stat   func(string) (os.FileInfo, error)
	Read   func(string) ([]byte, error)
}

// startupConfig is the resolved startup configuration.
type startupConfig struct {
	AllowedPaths []string
	Profile      string
	RootsMode    string
	CompactMode  bool
	ReadOnly     bool
	AutoOCC      string
	LogLevel     string
	BackupDir    string
}

func loadStartupConfig(in startupInput) (startupConfig, error) {
	if in.Lookup == nil {
		in.Lookup = os.LookupEnv
	}
	if in.Stat == nil {
		in.Stat = os.Stat
	}
	if in.Read == nil {
		in.Read = os.ReadFile
	}
	if in.Visited == nil {
		in.Visited = map[string]bool{}
	}
	if in.CWD == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return startupConfig{}, fmt.Errorf("getwd: %w", err)
		}
		in.CWD = cwd
	}

	cfg := startupConfig{
		Profile:     in.Profile,
		RootsMode:   in.RootsMode,
		CompactMode: in.CompactMode,
		ReadOnly:    in.ReadOnly,
		AutoOCC:     in.AutoOCC,
		LogLevel:    in.LogLevel,
		BackupDir:   in.BackupDir,
	}

	env, err := loadEnvMap(in)
	if err != nil {
		return startupConfig{}, err
	}
	if err := applyEnvScalars(&cfg, env, in.Visited); err != nil {
		return startupConfig{}, err
	}
	if err := applyPreset(in.Preset, &cfg, in.Visited); err != nil {
		return startupConfig{}, err
	}
	paths, err := collectAllowedPaths(in, env)
	if err != nil {
		return startupConfig{}, err
	}
	cfg.AllowedPaths = paths
	return cfg, nil
}

func visitedFlagNames() map[string]bool {
	set := make(map[string]bool)
	flag.Visit(func(f *flag.Flag) {
		set[f.Name] = true
	})
	return set
}

func applyPreset(preset string, cfg *startupConfig, visited map[string]bool) error {
	name := strings.ToLower(strings.TrimSpace(preset))
	var profile string
	switch name {
	case "", "full":
		return nil
	case "minimal":
		profile = "strict"
	case "agent":
		profile = "agent"
	default:
		return fmt.Errorf("invalid --preset %q (want minimal|agent|full)", preset)
	}
	if !visited["profile"] {
		cfg.Profile = profile
	}
	if !visited["compact-mode"] {
		cfg.CompactMode = true
	}
	if !visited["roots-mode"] {
		cfg.RootsMode = "union"
	}
	return nil
}

func applyEnvScalars(cfg *startupConfig, env map[string]string, visited map[string]bool) error {
	if !visited["profile"] {
		if v, ok := nonempty(env, "MCP_PROFILE"); ok {
			if _, err := parseToolProfile(v); err != nil {
				return fmt.Errorf("MCP_PROFILE: %w", err)
			}
			cfg.Profile = strings.ToLower(strings.TrimSpace(v))
		}
	}
	if !visited["roots-mode"] {
		if v, ok := nonempty(env, "MCP_ROOTS_MODE"); ok {
			mode := strings.ToLower(strings.TrimSpace(v))
			switch mode {
			case "replace", "union", "ignore":
				cfg.RootsMode = mode
			default:
				return fmt.Errorf("invalid MCP_ROOTS_MODE %q (want replace|union|ignore)", v)
			}
		}
	}
	if !visited["compact-mode"] {
		if v, ok := nonempty(env, "MCP_COMPACT_MODE"); ok {
			b, err := parseEnvBool(v)
			if err != nil {
				return fmt.Errorf("MCP_COMPACT_MODE: %w", err)
			}
			cfg.CompactMode = b
		}
	}
	if !visited["readonly"] {
		if v, ok := nonempty(env, "MCP_READONLY"); ok {
			b, err := parseEnvBool(v)
			if err != nil {
				return fmt.Errorf("MCP_READONLY: %w", err)
			}
			cfg.ReadOnly = b
		}
	}
	if !visited["auto-occ"] {
		if v, ok := nonempty(env, "MCP_AUTO_OCC"); ok {
			mode := strings.ToLower(strings.TrimSpace(v))
			switch mode {
			case "off", "warn", "block":
				cfg.AutoOCC = mode
			default:
				return fmt.Errorf("invalid MCP_AUTO_OCC %q (want off|warn|block)", v)
			}
		}
	}
	if !visited["log-level"] {
		if v, ok := nonempty(env, "MCP_LOG_LEVEL"); ok {
			level := strings.ToLower(strings.TrimSpace(v))
			if _, err := parseLogLevel(level); err != nil {
				return fmt.Errorf("MCP_LOG_LEVEL: %w", err)
			}
			cfg.LogLevel = level
		}
	}
	if !visited["backup-dir"] {
		if v, ok := nonempty(env, "MCP_BACKUP_DIR"); ok {
			cfg.BackupDir = strings.TrimSpace(v)
		}
	}
	return nil
}

func nonempty(env map[string]string, key string) (string, bool) {
	v, ok := env[key]
	if !ok || strings.TrimSpace(v) == "" {
		return "", false
	}
	return v, true
}

func parseEnvBool(s string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes":
		return true, nil
	case "0", "false", "no":
		return false, nil
	default:
		return false, fmt.Errorf("invalid boolean %q (want 1, true, yes, 0, false, no)", s)
	}
}

func collectAllowedPaths(in startupInput, env map[string]string) ([]string, error) {
	var fromFlag []string
	if strings.TrimSpace(in.AllowedPathsFlag) != "" {
		fromFlag = sanitizeAllowedPaths(strings.Split(in.AllowedPathsFlag, ","))
	}
	fromPos := sanitizeAllowedPaths(in.Positional)

	var fromFile []string
	filePath, ok, err := resolvePathsFile(in)
	if err != nil {
		return nil, err
	}
	if ok {
		b, err := in.Read(filePath)
		if err != nil {
			return nil, fmt.Errorf("allowed paths file %q: %w", filePath, err)
		}
		fromFile = resolvePathsContent(string(b), in.CWD)
	}

	var fromEnv []string
	if v, ok := nonempty(env, "MCP_ALLOWED_PATHS"); ok {
		fromEnv = splitListPaths(v)
	}
	return mergeAllowedPaths(fromFlag, fromPos, fromFile, fromEnv), nil
}

func resolvePathsFile(in startupInput) (string, bool, error) {
	if in.PathsFileSet {
		p := strings.TrimSpace(in.PathsFile)
		if p == "" {
			return "", false, errors.New("--allowed-paths-file requires a path")
		}
		p = joinIfRelative(p, in.CWD)
		return requireRegularFile(p, "allowed paths file", in.Stat)
	}
	candidate := filepath.Join(in.CWD, mcpPathsFilename)
	st, err := in.Stat(candidate)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("allowed paths file %q: %w", candidate, err)
	}
	if st.IsDir() {
		return "", false, fmt.Errorf("allowed paths file %q: is a directory", candidate)
	}
	return candidate, true, nil
}

func loadEnvMap(in startupInput) (map[string]string, error) {
	fileVals := map[string]string{}
	path, ok, err := resolveEnvFile(in)
	if err != nil {
		return nil, err
	}
	if ok {
		b, err := in.Read(path)
		if err != nil {
			return nil, fmt.Errorf("env file %q: %w", path, err)
		}
		fileVals, err = parseEnvContent(string(b))
		if err != nil {
			return nil, fmt.Errorf("env file %q: %w", path, err)
		}
	}
	return mergeProcessEnv(fileVals, in.Lookup), nil
}

func resolveEnvFile(in startupInput) (string, bool, error) {
	if in.EnvFileSet {
		p := strings.TrimSpace(in.EnvFile)
		if p == "" {
			return "", false, errors.New("--env-file requires a path")
		}
		p = joinIfRelative(p, in.CWD)
		return requireRegularFile(p, "env file", in.Stat)
	}
	candidate := filepath.Join(in.CWD, defaultEnvFilename)
	st, err := in.Stat(candidate)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("env file %q: %w", candidate, err)
	}
	if st.IsDir() {
		return "", false, nil
	}
	return candidate, true, nil
}

func requireRegularFile(path, what string, stat func(string) (os.FileInfo, error)) (string, bool, error) {
	st, err := stat(path)
	if err != nil {
		return "", false, fmt.Errorf("%s %q: %w", what, path, err)
	}
	if st.IsDir() {
		return "", false, fmt.Errorf("%s %q: is a directory", what, path)
	}
	return path, true, nil
}

func mergeProcessEnv(file map[string]string, lookup func(string) (string, bool)) map[string]string {
	if lookup == nil {
		lookup = os.LookupEnv
	}
	out := make(map[string]string, len(supportedEnvKeys))
	for _, key := range supportedEnvKeys {
		if v, ok := lookup(key); ok && strings.TrimSpace(v) != "" {
			out[key] = v
			continue
		}
		if v, ok := file[key]; ok && strings.TrimSpace(v) != "" {
			out[key] = v
		}
	}
	return out
}

// parseEnvContent reads KEY=VALUE lines. Only MCP_* keys are kept. Blank
// lines and lines whose first non-space character is # are ignored. An
// optional export prefix is accepted. Quoted values keep their contents
// literally (no escape processing, no variable expansion). A # starts a
// comment only outside quotes.
func parseEnvContent(content string) (map[string]string, error) {
	content = strings.TrimPrefix(content, "\uFEFF")
	out := make(map[string]string)
	for i, line := range strings.Split(content, "\n") {
		line = strings.TrimRight(line, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if rest, ok := strings.CutPrefix(trimmed, "export "); ok {
			trimmed = strings.TrimSpace(rest)
		}
		key, val, err := splitEnvLine(trimmed)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		if !strings.HasPrefix(key, "MCP_") {
			continue
		}
		out[key] = val
	}
	return out, nil
}

func splitEnvLine(line string) (string, string, error) {
	eq := strings.IndexByte(line, '=')
	if eq <= 0 {
		return "", "", errors.New("expected KEY=VALUE")
	}
	key := strings.TrimSpace(line[:eq])
	if key == "" || strings.ContainsAny(key, " \t") {
		return "", "", fmt.Errorf("invalid key %q", key)
	}
	val, err := unquoteEnvValue(strings.TrimSpace(line[eq+1:]))
	if err != nil {
		return "", "", err
	}
	return key, val, nil
}

func unquoteEnvValue(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	if raw[0] == '"' || raw[0] == '\'' {
		q := raw[0]
		end := strings.IndexByte(raw[1:], q)
		if end < 0 {
			return "", errors.New("unterminated quote")
		}
		end++
		rest := strings.TrimSpace(raw[end+1:])
		if rest != "" && !strings.HasPrefix(rest, "#") {
			return "", errors.New("trailing data after quoted value")
		}
		return raw[1:end], nil
	}
	if i := strings.IndexByte(raw, '#'); i >= 0 {
		raw = strings.TrimSpace(raw[:i])
	}
	return raw, nil
}

// parsePathsContent returns one path per non-empty, non-comment line.
// Commas are part of the path. Leading and trailing space is trimmed;
// internal spaces are kept.
func parsePathsContent(content string) []string {
	content = strings.TrimPrefix(content, "\uFEFF")
	var out []string
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimRight(line, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

func resolvePathsContent(content, cwd string) []string {
	raw := parsePathsContent(content)
	out := make([]string, 0, len(raw))
	for _, p := range raw {
		if pathIsRelative(p) {
			p = filepath.Clean(filepath.Join(cwd, p))
		}
		out = append(out, p)
	}
	return out
}

func pathIsRelative(p string) bool {
	if p == "" || filepath.IsAbs(p) {
		return false
	}
	if p[0] == '/' || p[0] == '\\' {
		return false
	}
	if strings.HasPrefix(p, `\\`) || strings.HasPrefix(p, `//`) {
		return false
	}
	if len(p) >= 2 && p[1] == ':' {
		return false
	}
	return true
}

func joinIfRelative(p, cwd string) string {
	if pathIsRelative(p) {
		return filepath.Join(cwd, p)
	}
	return p
}

// splitListPaths splits MCP_ALLOWED_PATHS on comma or semicolon.
// --allowed-paths stays comma-only; this splitter is for the env value.
func splitListPaths(s string) []string {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ';'
	})
	return sanitizeAllowedPaths(fields)
}

// mergeAllowedPaths unions groups in order. The first spelling of a path wins.
func mergeAllowedPaths(groups ...[]string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0)
	for _, group := range groups {
		for _, p := range sanitizeAllowedPaths(group) {
			key := pathIdentity(p)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, p)
		}
	}
	return out
}

func pathIdentity(p string) string {
	n := core.NormalizePath(strings.TrimSpace(p))
	n = filepath.Clean(n)
	if runtime.GOOS == "windows" {
		n = strings.ToLower(n)
	}
	return n
}
