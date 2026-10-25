package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func quietStartup(t *testing.T) startupInput {
	t.Helper()
	return startupInput{
		CWD:       t.TempDir(),
		Profile:   "ultra",
		RootsMode: "replace",
		AutoOCC:   "warn",
		LogLevel:  "info",
		Lookup:    func(string) (string, bool) { return "", false },
		Stat:      os.Stat,
		Read:      os.ReadFile,
	}
}

func TestParsePathsContent_CommentsBlanksSpaces(t *testing.T) {
	in := "# comment\r\n\r\n  \r\nC:\\Program Files\\proj\r\n# trailing\r\nD:\\plain\r\nC:\\foo,bar\r\n"
	got := parsePathsContent(in)
	want := []string{`C:\Program Files\proj`, `D:\plain`, `C:\foo,bar`}
	if len(got) != len(want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("entry %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestLoadStartup_RelativePathsFile(t *testing.T) {
	in := quietStartup(t)
	path := filepath.Join(in.CWD, "roots.txt")
	body := "# skip\n\nrel dir\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	in.PathsFile = "roots.txt"
	in.PathsFileSet = true
	got, err := loadStartupConfig(in)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Clean(filepath.Join(in.CWD, "rel dir"))
	if len(got.AllowedPaths) != 1 || got.AllowedPaths[0] != want {
		t.Fatalf("got %#v, want %q", got.AllowedPaths, want)
	}
}

func TestLoadStartup_AutoDetectMCPPaths(t *testing.T) {
	in := quietStartup(t)
	body := "# roots\n\n" + filepath.Join(in.CWD, "proj") + "\n"
	if err := os.WriteFile(filepath.Join(in.CWD, mcpPathsFilename), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := loadStartupConfig(in)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(in.CWD, "proj")
	if len(got.AllowedPaths) != 1 || got.AllowedPaths[0] != want {
		t.Fatalf("got %#v, want %q", got.AllowedPaths, want)
	}
}

func TestLoadStartup_ExplicitPathsFileSkipsAutoDetect(t *testing.T) {
	in := quietStartup(t)
	auto := filepath.Join(in.CWD, "from-auto")
	explicit := filepath.Join(in.CWD, "from-flag")
	if err := os.WriteFile(filepath.Join(in.CWD, mcpPathsFilename), []byte(auto+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	flagFile := filepath.Join(in.CWD, "other.txt")
	if err := os.WriteFile(flagFile, []byte(explicit+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	in.PathsFile = flagFile
	in.PathsFileSet = true
	got, err := loadStartupConfig(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.AllowedPaths) != 1 || got.AllowedPaths[0] != explicit {
		t.Fatalf("got %#v, want only %q", got.AllowedPaths, explicit)
	}
}

func TestLoadStartup_EnvAllowedPathsAndProfile(t *testing.T) {
	in := quietStartup(t)
	env := "" +
		"# ignored\n" +
		"OTHER=nope\n" +
		"export MCP_PROFILE=\"strict\"\n" +
		"MCP_ALLOWED_PATHS='C:\\proyecto;C:\\otros' # comment\n" +
		"MCP_COMPACT_MODE=yes\n" +
		"UNPREFIXED_MCP=1\n"
	if err := os.WriteFile(filepath.Join(in.CWD, defaultEnvFilename), []byte(env), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := loadStartupConfig(in)
	if err != nil {
		t.Fatal(err)
	}
	if got.Profile != "strict" {
		t.Fatalf("profile = %q, want strict", got.Profile)
	}
	if !got.CompactMode {
		t.Fatal("compact mode was not set from .env")
	}
	want := []string{`C:\proyecto`, `C:\otros`}
	if len(got.AllowedPaths) != len(want) || got.AllowedPaths[0] != want[0] || got.AllowedPaths[1] != want[1] {
		t.Fatalf("paths = %#v, want %#v", got.AllowedPaths, want)
	}
}

func TestLoadStartup_MissingDefaultEnvIsOK(t *testing.T) {
	in := quietStartup(t)
	in.AllowedPathsFlag = `C:\proj`
	got, err := loadStartupConfig(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.AllowedPaths) != 1 || got.AllowedPaths[0] != `C:\proj` {
		t.Fatalf("got %#v", got.AllowedPaths)
	}
}

func TestLoadStartup_ExplicitEnvFileMissing(t *testing.T) {
	in := quietStartup(t)
	in.EnvFile = filepath.Join(in.CWD, "missing.env")
	in.EnvFileSet = true
	_, err := loadStartupConfig(in)
	if err == nil || !strings.Contains(err.Error(), "env file") {
		t.Fatalf("got %v, want env file error", err)
	}
}

func TestLoadStartup_CLIFlagBeatsEnv(t *testing.T) {
	in := quietStartup(t)
	env := "MCP_PROFILE=strict\nMCP_COMPACT_MODE=yes\nMCP_ROOTS_MODE=union\nMCP_ALLOWED_PATHS=D:\\from-env\n"
	if err := os.WriteFile(filepath.Join(in.CWD, defaultEnvFilename), []byte(env), 0o644); err != nil {
		t.Fatal(err)
	}
	in.Profile = "ultra"
	in.CompactMode = false
	in.RootsMode = "replace"
	in.Visited = map[string]bool{
		"profile":      true,
		"compact-mode": true,
		"roots-mode":   true,
	}
	in.AllowedPathsFlag = `C:\from-cli`
	got, err := loadStartupConfig(in)
	if err != nil {
		t.Fatal(err)
	}
	if got.Profile != "ultra" || got.CompactMode || got.RootsMode != "replace" {
		t.Fatalf("CLI lost to env: %+v", got)
	}
	if len(got.AllowedPaths) != 2 || got.AllowedPaths[0] != `C:\from-cli` || got.AllowedPaths[1] != `D:\from-env` {
		t.Fatalf("paths = %#v, want CLI path kept and env path unioned", got.AllowedPaths)
	}
}

func TestLoadStartup_ProcessEnvBeatsFile(t *testing.T) {
	in := quietStartup(t)
	env := "MCP_PROFILE=strict\n"
	if err := os.WriteFile(filepath.Join(in.CWD, defaultEnvFilename), []byte(env), 0o644); err != nil {
		t.Fatal(err)
	}
	in.Lookup = func(key string) (string, bool) {
		if key == "MCP_PROFILE" {
			return "agent", true
		}
		return "", false
	}
	got, err := loadStartupConfig(in)
	if err != nil {
		t.Fatal(err)
	}
	if got.Profile != "agent" {
		t.Fatalf("profile = %q, want agent from process env", got.Profile)
	}
}

func TestMergeAllowedPaths_UnionDedup(t *testing.T) {
	got := mergeAllowedPaths(
		[]string{`C:\a`, `C:\a`},
		[]string{`C:\b`},
		[]string{`C:\a`, `C:\c`},
		[]string{`C:\b`, `C:\d`},
	)
	want := []string{`C:\a`, `C:\b`, `C:\c`, `C:\d`}
	if len(got) != len(want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %#v, want %#v", got, want)
		}
	}
}

func TestLoadStartup_UnionAcrossSources(t *testing.T) {
	in := quietStartup(t)
	filePath := filepath.Join(in.CWD, "roots.txt")
	if err := os.WriteFile(filePath, []byte("C:\\a\nC:\\file\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(in.CWD, defaultEnvFilename), []byte("MCP_ALLOWED_PATHS=C:\\file;C:\\env\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	in.AllowedPathsFlag = `C:\a,C:\cli`
	in.Positional = []string{`C:\pos`}
	in.PathsFile = filePath
	in.PathsFileSet = true
	got, err := loadStartupConfig(in)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{`C:\a`, `C:\cli`, `C:\pos`, `C:\file`, `C:\env`}
	if len(got.AllowedPaths) != len(want) {
		t.Fatalf("got %#v, want %#v", got.AllowedPaths, want)
	}
	for i := range want {
		if got.AllowedPaths[i] != want[i] {
			t.Fatalf("got %#v, want %#v", got.AllowedPaths, want)
		}
	}
}

func TestLoadStartup_MissingPathsFile(t *testing.T) {
	in := quietStartup(t)
	in.PathsFile = filepath.Join(in.CWD, "missing.paths")
	in.PathsFileSet = true
	_, err := loadStartupConfig(in)
	if err == nil || !strings.Contains(err.Error(), "allowed paths file") {
		t.Fatalf("got %v, want a clear allowed paths file error", err)
	}
}

func TestLoadStartup_UnreadablePathsFile(t *testing.T) {
	in := quietStartup(t)
	in.PathsFile = filepath.Join(in.CWD, "roots.txt")
	in.PathsFileSet = true
	if err := os.WriteFile(in.PathsFile, []byte("C:\\a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	in.Read = func(string) ([]byte, error) {
		return nil, os.ErrPermission
	}
	_, err := loadStartupConfig(in)
	if err == nil || !strings.Contains(err.Error(), "allowed paths file") {
		t.Fatalf("got %v, want an unreadable-file error", err)
	}
}

func TestLoadStartup_NoPathsIsEmpty(t *testing.T) {
	in := quietStartup(t)
	got, err := loadStartupConfig(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.AllowedPaths) != 0 {
		t.Fatalf("got %#v, want no paths", got.AllowedPaths)
	}
	if err := requireAllowedPaths(got.AllowedPaths, false); err != errFailClosed {
		t.Fatalf("fail-closed: %v", err)
	}
	if err := requireAllowedPaths(got.AllowedPaths, true); err != nil {
		t.Fatalf("insecure-open: %v", err)
	}
}

func TestLoadStartup_PresetMinimalDoesNotOverrideExplicit(t *testing.T) {
	in := quietStartup(t)
	in.Preset = "minimal"
	in.Profile = "ultra"
	in.CompactMode = false
	in.RootsMode = "replace"
	in.Visited = map[string]bool{
		"profile":      true,
		"compact-mode": true,
		"roots-mode":   true,
		"preset":       true,
	}
	in.AllowedPathsFlag = `C:\proj`
	got, err := loadStartupConfig(in)
	if err != nil {
		t.Fatal(err)
	}
	if got.Profile != "ultra" || got.CompactMode || got.RootsMode != "replace" {
		t.Fatalf("preset overrode explicit flags: %+v", got)
	}
}

func TestLoadStartup_PresetBeatsEnv(t *testing.T) {
	in := quietStartup(t)
	in.Preset = "minimal"
	in.Lookup = func(key string) (string, bool) {
		if key == "MCP_PROFILE" {
			return "ultra", true
		}
		if key == "MCP_COMPACT_MODE" {
			return "no", true
		}
		return "", false
	}
	got, err := loadStartupConfig(in)
	if err != nil {
		t.Fatal(err)
	}
	if got.Profile != "strict" || !got.CompactMode || got.RootsMode != "union" {
		t.Fatalf("env beat preset: %+v", got)
	}
}

func TestLoadStartup_PresetMinimalFillsUnset(t *testing.T) {
	in := quietStartup(t)
	in.Preset = "minimal"
	in.AllowedPathsFlag = `C:\proj`
	got, err := loadStartupConfig(in)
	if err != nil {
		t.Fatal(err)
	}
	if got.Profile != "strict" || !got.CompactMode || got.RootsMode != "union" {
		t.Fatalf("preset did not fill defaults: %+v", got)
	}
}

func TestLoadStartup_PresetDoesNotSupplyPaths(t *testing.T) {
	in := quietStartup(t)
	in.Preset = "minimal"
	got, err := loadStartupConfig(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.AllowedPaths) != 0 {
		t.Fatalf("preset invented paths: %#v", got.AllowedPaths)
	}
	if err := requireAllowedPaths(got.AllowedPaths, false); err != errFailClosed {
		t.Fatal("preset must not bypass fail-closed")
	}
}

func TestLoadStartup_PresetFullIsNoop(t *testing.T) {
	in := quietStartup(t)
	in.Preset = "full"
	in.Lookup = func(key string) (string, bool) {
		if key == "MCP_PROFILE" {
			return "agent", true
		}
		return "", false
	}
	got, err := loadStartupConfig(in)
	if err != nil {
		t.Fatal(err)
	}
	if got.Profile != "agent" || got.CompactMode || got.RootsMode != "replace" {
		t.Fatalf("full preset changed config: %+v", got)
	}
}

func TestLoadStartup_InvalidPreset(t *testing.T) {
	in := quietStartup(t)
	in.Preset = "nope"
	_, err := loadStartupConfig(in)
	if err == nil || !strings.Contains(err.Error(), "--preset") {
		t.Fatalf("got %v", err)
	}
}

func TestParseEnvContent_NoExpansion(t *testing.T) {
	got, err := parseEnvContent("MCP_BACKUP_DIR=$HOME/backups\nNOT_MCP=1\n")
	if err != nil {
		t.Fatal(err)
	}
	if got["MCP_BACKUP_DIR"] != "$HOME/backups" {
		t.Fatalf("expanded or dropped value: %#v", got)
	}
	if _, ok := got["NOT_MCP"]; ok {
		t.Fatalf("kept a non-MCP key: %#v", got)
	}
}

func TestSplitListPaths_CommaAndSemicolon(t *testing.T) {
	got := splitListPaths(` C:\a, C:\b ;D:\c `)
	want := []string{`C:\a`, `C:\b`, `D:\c`}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("got %#v", got)
	}
}
