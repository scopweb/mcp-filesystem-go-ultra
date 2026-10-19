package core

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRunRipgrepSearch_ExtensionCaseAndExclusions(t *testing.T) {
	dir := t.TempDir()
	for _, rel := range []string{"plain.js", "UPPER.JS", "bundle.min.js", "node_modules/hidden.js"} {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("needle\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	e := newTestEngine(dir)
	t.Cleanup(func() { _ = e.Close() })
	if !e.ripgrepAvailable {
		if os.Getenv("MCP_REQUIRE_RIPGREP") == "1" {
			t.Fatal("ripgrep required by this CI job")
		}
		t.Skip("ripgrep unavailable")
	}
	matches, err := e.RunRipgrepSearch(context.Background(), dir, "needle", true, false, false, 0, false, []string{".js"})
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 2 {
		t.Fatalf("expected plain.js and UPPER.JS only: %+v", matches)
	}
	for _, match := range matches {
		if base := filepath.Base(match.File); base != "plain.js" && base != "UPPER.JS" {
			t.Fatalf("unexpected file %s", match.File)
		}
	}
}
func TestSearchFilterIntersectionBackends(t *testing.T) {
	dir := t.TempDir()
	for _, rel := range []string{"RootWriter.cs", "Program.cs", "Cobertura.cs", "ProvesBatch.cs", "src/CsvWriter.cs", "src/Reader.cs", "src/nested/JsonWriter.cs", "src/nested/Writer.txt", "Test/Alpha.razor", "ObservacionesPedido/Beta.razor", "Other/Gamma.razor"} {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("needle needle\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	e := newTestEngine(dir)
	t.Cleanup(func() { _ = e.Close() })
	available := e.ripgrepAvailable
	for _, backend := range []string{"native", "ripgrep"} {
		t.Run(backend, func(t *testing.T) {
			if backend == "ripgrep" && !available {
				if os.Getenv("MCP_REQUIRE_RIPGREP") == "1" {
					t.Fatal("ripgrep required by this CI job")
				}
				t.Skip("ripgrep unavailable")
			}
			e.ripgrepAvailable = backend == "ripgrep"
			for _, tc := range []struct {
				name    string
				types   []string
				include []string
				want    int
			}{
				{"writer", []string{".cs"}, []string{"**/*Writer*"}, 3},
				{"writer_glob", nil, []string{"**/*Writer*.cs"}, 3},
				{"disjoint", []string{".go"}, []string{"**/*Writer*"}, 0},
				{"brace_dirs", []string{".razor"}, []string{"Test/*.razor", "ObservacionesPedido/*.razor"}, 2},
			} {
				t.Run(tc.name, func(t *testing.T) {
					for _, mode := range []string{"names", "content", "count"} {
						pattern := "needle"
						if mode == "names" {
							pattern = "."
						}
						opts := SearchOptions{Path: dir, Pattern: pattern, FileTypes: tc.types, Include: tc.include, CaseSensitive: true, IncludeContent: mode == "content", CountOnly: mode == "count", MaxResults: 100}
						out, err := e.SearchFiles(context.Background(), opts)
						if err != nil {
							t.Fatal(err)
						}
						want := tc.want
						if mode == "count" {
							want *= 2
						}
						if out.MatchCount != want {
							t.Fatalf("%s count=%d want=%d: %s", mode, out.MatchCount, want, out.Text)
						}
						if mode != "count" && tc.want > 1 {
							opts.MaxResults = 1
							out, err = e.SearchFiles(context.Background(), opts)
							if err != nil {
								t.Fatal(err)
							}
							if len(out.Matches) != 1 || !out.Truncated || out.NextOffset != 1 {
								t.Fatalf("%s invalid first page: %+v", mode, out)
							}
							opts.Offset = 1
							next, err := e.SearchFiles(context.Background(), opts)
							if err != nil {
								t.Fatal(err)
							}
							if len(next.Matches) != 1 || next.Matches[0].File == out.Matches[0].File {
								t.Fatalf("%s invalid continuation: %+v", mode, next)
							}
						}
					}
					if backend == "ripgrep" {
						// Direct invocation cannot silently fall back to the native walker.
						matches, err := e.RunRipgrepSearch(context.Background(), dir, "needle", true, false, false, 0, false, tc.types, tc.include)
						if err != nil {
							t.Fatal(err)
						}
						if len(matches) != tc.want {
							t.Fatalf("direct ripgrep count=%d want=%d", len(matches), tc.want)
						}
					}
				})
			}
		})
	}
}
