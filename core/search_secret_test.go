package core

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// secretCanary is the synthetic credential planted in every secret fixture.
// A search result must never carry it.
const secretCanary = "API_KEY=supersecretvalue"

// secretFixtureFiles covers every entry of the denylist (including
// upper-case variants, because the denylist is case-insensitive) plus a
// secret-named directory whose content must be pruned with it.
var secretFixtureFiles = []string{
	".env",
	".ENV",
	".env.local",
	"credentials.json",
	"Credentials.JSON",
	".npmrc",
	".pypirc",
	"id_rsa",
	"id_rsa.pub",
	"id_ed25519",
	"id_ecdsa",
	"cert.pem",
	"server.key",
	"bundle.p12",
	"cert.pfx",
	"secrets.pem/inner.txt",
}

// benignFixtureFiles must still be found: the guard must not over-block.
var benignFixtureFiles = []string{"main.go", "env.txt", "docs/notes.md"}

func writeSecretFixtures(t *testing.T, dir string) {
	t.Helper()
	for _, rel := range append(append([]string{}, secretFixtureFiles...), benignFixtureFiles...) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		body := "needle\n"
		if strings.Contains(rel, "inner.txt") {
			body = "needle " + secretCanary + "\n"
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// The secret files carry the canary so a leak is detectable.
	for _, rel := range secretFixtureFiles {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.WriteFile(p, []byte("needle "+secretCanary+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// TestSearchFiles_NeverExposesSecretFiles is the regression test for the
// leak where search_files returned the content of secret files that
// read_file denies (SECRET_DENIED). Both backends must behave identically.
func TestSearchFiles_NeverExposesSecretFiles(t *testing.T) {
	dir := t.TempDir()
	writeSecretFixtures(t, dir)
	e := newTestEngine(dir)
	t.Cleanup(func() { _ = e.Close() })

	available := e.ripgrepAvailable
	for _, backend := range []string{"native", "ripgrep"} {
		for _, allowSecrets := range []bool{false, true} {
			name := backend
			if allowSecrets {
				name += "/allow-secrets"
			}
			t.Run(name, func(t *testing.T) {
				if backend == "ripgrep" && !available {
					if os.Getenv("MCP_REQUIRE_RIPGREP") == "1" {
						t.Fatal("ripgrep required by this CI job")
					}
					t.Skip("ripgrep unavailable")
				}
				e.ripgrepAvailable = backend == "ripgrep"
				e.GetConfig().AllowSecrets = allowSecrets

				out, err := e.SearchFiles(context.Background(), SearchOptions{
					Path:           dir,
					Pattern:        "needle",
					CaseSensitive:  true,
					IncludeContent: true,
					MaxResults:     200,
				})
				if err != nil {
					t.Fatal(err)
				}
				for _, m := range out.Matches {
					if IsSecretPath(m.File) {
						t.Errorf("secret file exposed as match: %s", m.File)
					}
					if strings.Contains(m.Line, secretCanary) {
						t.Errorf("secret content exposed in match line of %s", m.File)
					}
				}
				if strings.Contains(out.Text, secretCanary) {
					t.Errorf("secret content exposed in search output")
				}
				found := map[string]bool{}
				for _, m := range out.Matches {
					found[filepath.Base(m.File)] = true
				}
				for _, want := range benignFixtureFiles {
					if !found[filepath.Base(want)] {
						t.Errorf("expected %s in results, got %+v", want, out.Matches)
					}
				}
			})
		}
	}
}

// TestSearchFiles_ExplicitSecretTargetIsBlocked covers the case where the
// secret file is the search root itself: ripgrep exclusion globs do not
// apply to an explicit target, so the parsed matches must be re-checked.
// The public API already denies the path outright (SECRET_DENIED); the
// internal search functions are what the MCP layer reaches with an already
// validated path, so they are exercised directly too.
func TestSearchFiles_ExplicitSecretTargetIsBlocked(t *testing.T) {
	dir := t.TempDir()
	writeSecretFixtures(t, dir)
	e := newTestEngine(dir)
	t.Cleanup(func() { _ = e.Close() })

	available := e.ripgrepAvailable
	for _, backend := range []string{"native", "ripgrep"} {
		for _, allowSecrets := range []bool{false, true} {
			name := backend
			if allowSecrets {
				name += "/allow-secrets"
			}
			t.Run(name, func(t *testing.T) {
				if backend == "ripgrep" && !available {
					if os.Getenv("MCP_REQUIRE_RIPGREP") == "1" {
						t.Fatal("ripgrep required by this CI job")
					}
					t.Skip("ripgrep unavailable")
				}
				e.ripgrepAvailable = backend == "ripgrep"
				e.GetConfig().AllowSecrets = allowSecrets

				for _, target := range []string{".env", ".env.local", "credentials.json", "cert.pem", "id_rsa"} {
					full := filepath.Join(dir, target)

					// Public API: the path is denied, never read.
					out, err := e.SearchFiles(context.Background(), SearchOptions{
						Path:           full,
						Pattern:        "needle",
						CaseSensitive:  true,
						IncludeContent: true,
						MaxResults:     100,
					})
					if err == nil {
						if out.MatchCount != 0 {
							t.Errorf("explicit secret target %s returned %d matches", target, out.MatchCount)
						}
						if strings.Contains(out.Text, secretCanary) {
							t.Errorf("explicit secret target %s leaked content", target)
						}
					}

					// Internal search: the secret file is the search root.
					matches, _, err := e.performAdvancedTextSearch(context.Background(), full, "needle", true, false, false, 0, "auto", false, nil)
					if err != nil {
						t.Fatal(err)
					}
					if len(matches) != 0 {
						t.Errorf("explicit secret target %s returned %d matches", target, len(matches))
					}
					for _, m := range matches {
						if strings.Contains(m.Line, secretCanary) {
							t.Errorf("explicit secret target %s leaked content", target)
						}
					}
				}
			})
		}
	}
}

// TestSearchFiles_CountExplicitSecretTargetIsBlocked checks the counting
// path: the count must not disclose which lines of a secret hold a match.
// With --allow-secrets the path passes validation, so the guard must hold
// there as well.
func TestSearchFiles_CountExplicitSecretTargetIsBlocked(t *testing.T) {
	dir := t.TempDir()
	writeSecretFixtures(t, dir)
	e := newTestEngine(dir)
	t.Cleanup(func() { _ = e.Close() })

	full := filepath.Join(dir, ".env")

	// Default: the path itself is denied before anything is read.
	if _, _, err := e.countOccurrencesOpts(context.Background(), SearchOptions{
		Path:          full,
		Pattern:       "needle",
		CaseSensitive: true,
	}); err == nil {
		t.Errorf("count on a secret file must be denied without --allow-secrets")
	}

	// With --allow-secrets the file is readable on purpose, but the
	// search must still not disclose its content.
	e.GetConfig().AllowSecrets = true
	text, n, err := e.countOccurrencesOpts(context.Background(), SearchOptions{
		Path:          full,
		Pattern:       "needle",
		CaseSensitive: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("count on secret file returned %d matches", n)
	}
	if strings.Contains(text, secretCanary) {
		t.Errorf("count on secret file leaked content")
	}
}

// TestRunRipgrepSearch_ExcludesSecretFiles exercises the ripgrep backend
// directly (no native fallback possible) for both the directory and the
// explicit-target case.
func TestRunRipgrepSearch_ExcludesSecretFiles(t *testing.T) {
	dir := t.TempDir()
	writeSecretFixtures(t, dir)
	e := newTestEngine(dir)
	t.Cleanup(func() { _ = e.Close() })
	if !e.ripgrepAvailable {
		if os.Getenv("MCP_REQUIRE_RIPGREP") == "1" {
			t.Fatal("ripgrep required by this CI job")
		}
		t.Skip("ripgrep unavailable")
	}

	matches, err := e.RunRipgrepSearch(context.Background(), dir, "needle", true, false, false, 0, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range matches {
		if IsSecretPath(m.File) {
			t.Errorf("ripgrep exposed secret file: %s", m.File)
		}
		if strings.Contains(m.Line, secretCanary) {
			t.Errorf("ripgrep exposed secret content in %s", m.File)
		}
	}

	// Explicit target: exclusion globs do not apply here.
	matches, err = e.RunRipgrepSearch(context.Background(), filepath.Join(dir, ".env"), "needle", true, false, false, 0, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Errorf("ripgrep returned %d matches for an explicit secret target", len(matches))
	}
}

// TestRunRipgrepSearch_SecretGlobsInArgv pins the argv contract: the
// exclusion globs derived from the denylist must be present, and must come
// after the positive globs (in ripgrep the last glob wins).
func TestRunRipgrepSearch_SecretGlobsInArgv(t *testing.T) {
	stubDir, log := buildRgStub(t)
	t.Setenv("STUB_ARGV_LOG", log)
	t.Setenv("PATH", stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	engine, cleanup := setupTestEngine(t)
	defer cleanup()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.RunRipgrepSearch(context.Background(), dir, "needle", true, false, false, 0, false, []string{".txt"}); err != nil {
		t.Fatalf("RunRipgrepSearch: %v", err)
	}

	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("stub log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	var argv []string
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &argv); err != nil {
		t.Fatalf("stub log line not valid JSON: %v", err)
	}

	globs := map[string]int{}
	for i, a := range argv {
		if a == "--glob" && i+1 < len(argv) {
			globs[argv[i+1]] = i
		}
	}
	for _, want := range SecretExcludeGlobs() {
		idx, ok := globs[want]
		if !ok {
			t.Fatalf("missing secret exclusion glob %q; argv: %v", want, argv)
		}
		// Positive globs (file_types/include) must come first: in ripgrep
		// the last matching glob wins, so a later positive glob would
		// re-enable the secret.
		if idx < len(argv) {
			for _, positive := range []string{"*.[tT][xX]"} {
				if pos, ok := globs[positive]; ok && pos > idx {
					t.Errorf("positive glob %q must precede the secret exclusion %q", positive, want)
				}
			}
		}
	}
}

// TestPipelineSearch_ExcludesSecretFiles covers the same leak in the
// pipeline search action of batch_operations, which walks the tree itself
// and can return the full file content.
func TestPipelineSearch_ExcludesSecretFiles(t *testing.T) {
	dir := t.TempDir()
	writeSecretFixtures(t, dir)
	e := newTestEngine(dir)
	t.Cleanup(func() { _ = e.Close() })

	pe := NewPipelineExecutor(e)
	matches, err := pe.performSmartSearchInternal(context.Background(), dir, "needle", true, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range matches {
		if IsSecretPath(m.FilePath) {
			t.Errorf("pipeline search exposed secret file: %s", m.FilePath)
		}
		if strings.Contains(m.Content, secretCanary) {
			t.Errorf("pipeline search exposed secret content in %s", m.FilePath)
		}
	}
	if len(matches) != len(benignFixtureFiles) {
		t.Errorf("pipeline search returned %d files, want %d", len(matches), len(benignFixtureFiles))
	}
}

// TestSecretExcludeGlobs_ExcludeInRipgrep validates layer 1 on its own: the
// exclusion globs must keep ripgrep from even reporting the secret files,
// without relying on the post-parse filter. A broad positive glob comes
// first on purpose — in ripgrep the last glob wins, so the secret globs have
// to defeat it.
func TestSecretExcludeGlobs_ExcludeInRipgrep(t *testing.T) {
	dir := t.TempDir()
	writeSecretFixtures(t, dir)
	rgBin, err := exec.LookPath("rg")
	if err != nil {
		if os.Getenv("MCP_REQUIRE_RIPGREP") == "1" {
			t.Fatal("ripgrep required by this CI job")
		}
		t.Skip("ripgrep unavailable")
	}

	args := []string{"--json", "--hidden", "--encoding=none", "--max-filesize=10M"}
	args = append(args, "--glob", "**/*")
	for _, glob := range SecretExcludeGlobs() {
		args = append(args, "--glob", glob)
	}
	args = append(args, "-e", "needle", "--", ".")

	cmd := exec.Command(rgBin, args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			t.Fatal("ripgrep matched nothing: the positive glob did not reach the fixtures")
		}
		t.Fatalf("ripgrep failed: %v", err)
	}

	reported := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		var record struct {
			Type string `json:"type"`
			Data struct {
				Path struct {
					Text string `json:"text"`
				} `json:"path"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			continue
		}
		if record.Type != "match" && record.Type != "begin" {
			continue
		}
		name := record.Data.Path.Text
		name = name[strings.LastIndexAny(name, "\\/")+1:]
		reported[name] = true
		if IsSecretPath(name) {
			t.Errorf("ripgrep reported the secret file %q: the exclusion glob did not work", name)
		}
	}
	for _, want := range benignFixtureFiles {
		if !reported[filepath.Base(want)] {
			t.Errorf("expected %s in the ripgrep output, got %v", want, reported)
		}
	}
}

// TestSecretExcludeGlobs_CoverDenylist verifies that every denylist entry
// has a matching exclusion glob, so the read guard and the search guard
// cannot drift apart.
func TestSecretExcludeGlobs_CoverDenylist(t *testing.T) {
	globs := SecretExcludeGlobs()
	if len(globs) == 0 {
		t.Fatal("no secret exclusion globs generated")
	}
	samples := []string{
		".env", ".ENV", ".env.local", "credentials.json", "CREDENTIALS.JSON",
		".npmrc", ".pypirc", "id_rsa", "id_rsa.pub", "id_ed25519", "id_ecdsa",
		"cert.pem", "server.key", "bundle.p12", "cert.pfx",
	}
	for _, s := range samples {
		if !IsSecretPath(s) {
			t.Fatalf("%s is not classified as secret", s)
		}
	}
	// The glob set must be case-insensitive for the extension families.
	joined := strings.Join(globs, " ")
	for _, want := range []string{
		"!**/.[eE][nN][vV]",
		"!**/.[eE][nN][vV].*",
		"!**/[cC][rR][eE][dD][eE][nN][tT][iI][aA][lL][sS].[jJ][sS][oO][nN]",
		"!**/.[nN][pP][mM][rR][cC]",
		"!**/.[pP][yY][pP][iI][rR][cC]",
		"!**/[iI][dD]_[rR][sS][aA]*",
		"!**/[iI][dD]_[eE][dD]25519*",
		"!**/[iI][dD]_[eE][cC][dD][sS][aA]*",
		"!**/*.[pP][eE][mM]",
		"!**/*.[kK][eE][yY]",
		"!**/*.[pP]12",
		"!**/*.[pP][fF][xX]",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing exclusion glob %q; got %v", want, globs)
		}
	}
}
