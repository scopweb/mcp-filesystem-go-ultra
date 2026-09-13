package core

import (
	"context"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
)

const analyzeLintTimeout = 20 * time.Second

var (
	lookPath       = exec.LookPath
	commandContext = exec.CommandContext
	allowedAnalyze = map[string]struct{}{"go": {}, "staticcheck": {}, "govulncheck": {}}
	credAssignRe   = regexp.MustCompile(`(?i)\b(password|passwd|api[_-]?key|secret|token|credential)\s*[:=]\s*["'][^"'\n]{8,}["']`)
	pathJoinDotsRe = regexp.MustCompile(`\b(?:filepath|path)\.Join\([^)]*\.\.`)
	sqlConcatRe    = regexp.MustCompile(`(?i)["'](SELECT|INSERT|UPDATE|DELETE|EXEC)\b[^"']*["']\s*\+`)
	vetLineRe      = regexp.MustCompile(`(?m)^(.+?):(\d+):(?:(\d+):)?\s*(.+)$`)
)

// AnalyzeOptions is the typed input for analyze_code.
type AnalyzeOptions struct {
	Action      string
	Path        string
	Query       string
	MaxFindings int
}

// AnalyzeFinding is one structured hit.
type AnalyzeFinding struct {
	Path       string `json:"path"`
	Line       int    `json:"line,omitempty"`
	Col        int    `json:"col,omitempty"`
	Symbol     string `json:"symbol,omitempty"`
	Kind       string `json:"kind,omitempty"`
	Message    string `json:"message"`
	Severity   string `json:"severity,omitempty"`
	Suggestion string `json:"suggestion,omitempty"`
}

// AnalyzeResult is the structured analyze_code payload.
type AnalyzeResult struct {
	Status    string
	Action    string
	Findings  []AnalyzeFinding
	Truncated bool
	Tool      string
	Retryable bool
	Message   string
}

// ErrToolUnavailable means an allowlisted binary was not on PATH.
type ErrToolUnavailable struct {
	Bin        string
	Suggestion string
}

func (e *ErrToolUnavailable) Error() string {
	return fmt.Sprintf("tool unavailable: %s", e.Bin)
}

func capFindings(in []AnalyzeFinding, max int) ([]AnalyzeFinding, bool) {
	if max <= 0 {
		max = 50
	}
	if len(in) <= max {
		return in, false
	}
	return in[:max], true
}

// StubLookPath replaces binary lookup in tests. Restore with the returned func.
func StubLookPath(fn func(string) (string, error)) func() {
	prev := lookPath
	lookPath = fn
	return func() { lookPath = prev }
}

func resolveAnalyzeBin(name string) (string, error) {
	if _, ok := allowedAnalyze[name]; !ok {
		return "", fmt.Errorf("binary %q is not allowlisted", name)
	}
	p, err := lookPath(name)
	if err != nil {
		return "", &ErrToolUnavailable{Bin: name, Suggestion: "install " + name + " on PATH; analyze_code will not download it"}
	}
	base := strings.TrimSuffix(filepath.Base(p), ".exe")
	if !strings.EqualFold(base, name) {
		return "", fmt.Errorf("refusing unexpected binary %s", p)
	}
	return p, nil
}

func goFilesIn(path string) ([]string, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		if strings.HasSuffix(strings.ToLower(path), ".go") {
			return []string{path}, nil
		}
		return nil, nil
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if strings.HasSuffix(n, ".go") && !strings.HasSuffix(n, "_test.go") {
			out = append(out, filepath.Join(path, n))
		}
	}
	return out, nil
}

func packageDir(path string) string {
	st, err := os.Stat(path)
	if err != nil {
		return filepath.Dir(path)
	}
	if st.IsDir() {
		return path
	}
	return filepath.Dir(path)
}

func AnalyzeSymbols(path, query string, maxFindings int) AnalyzeResult {
	files, err := goFilesIn(path)
	res := AnalyzeResult{Action: "symbols", Tool: "go-ast", Findings: []AnalyzeFinding{}, Retryable: false}
	if err != nil {
		res.Status = "error"
		res.Message = err.Error()
		return res
	}
	if len(files) == 0 {
		res.Status = "empty"
		res.Message = "no Go files"
		return res
	}
	fset := token.NewFileSet()
	var parsed []*ast.File
	pkgName := ""
	for _, fpath := range files {
		f, err := parser.ParseFile(fset, fpath, nil, parser.ParseComments)
		if err != nil {
			continue
		}
		if pkgName == "" {
			pkgName = f.Name.Name
		}
		parsed = append(parsed, f)
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.FuncDecl:
				if x.Name == nil {
					return true
				}
				kind := "func"
				if x.Recv != nil {
					kind = "method"
				}
				addSymbol(&res, fset, fpath, x.Name, kind, query)
			case *ast.TypeSpec:
				if x.Name != nil {
					addSymbol(&res, fset, fpath, x.Name, "type", query)
				}
			}
			return true
		})
		ast.Inspect(f, func(n ast.Node) bool {
			gd, ok := n.(*ast.GenDecl)
			if !ok {
				return true
			}
			kind := "var"
			if gd.Tok.String() == "const" {
				kind = "const"
			}
			if gd.Tok.String() != "const" && gd.Tok.String() != "var" {
				return true
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, ident := range vs.Names {
					addSymbol(&res, fset, fpath, ident, kind, query)
				}
			}
			return true
		})
	}
	if len(parsed) > 0 {
		conf := types.Config{Importer: importer.Default(), Error: func(error) {}}
		if _, err := conf.Check(pkgName, fset, parsed, nil); err != nil {
			res.Status = "partial"
			res.Tool = "go-ast"
		}
	}
	res.Findings, res.Truncated = capFindings(res.Findings, maxFindings)
	if res.Status == "" {
		if len(res.Findings) == 0 {
			res.Status = "empty"
		} else {
			res.Status = "ok"
		}
	} else if res.Status == "partial" && len(res.Findings) == 0 {
		res.Status = "partial"
	}
	res.Message = formatAnalyzeText(res)
	return res
}

func addSymbol(res *AnalyzeResult, fset *token.FileSet, fpath string, ident *ast.Ident, kind, query string) {
	if ident == nil {
		return
	}
	name := ident.Name
	if query != "" && !strings.Contains(strings.ToLower(name), strings.ToLower(query)) {
		return
	}
	pos := fset.Position(ident.Pos())
	res.Findings = append(res.Findings, AnalyzeFinding{
		Path:     fpath,
		Line:     pos.Line,
		Col:      pos.Column,
		Symbol:   name,
		Kind:     kind,
		Message:  kind + " " + name,
		Severity: "info",
	})
}

func AnalyzeSec(path string, maxFindings int) AnalyzeResult {
	res := AnalyzeResult{Action: "sec", Tool: "go-ast", Findings: []AnalyzeFinding{}, Retryable: false}
	files, err := goFilesIn(path)
	if err != nil {
		res.Status = "error"
		res.Message = err.Error()
		return res
	}
	if len(files) == 0 {
		st, _ := os.Stat(path)
		if st != nil && !st.IsDir() {
			files = []string{path}
		}
	}
	for _, fpath := range files {
		raw, err := os.ReadFile(fpath)
		if err != nil {
			continue
		}
		lines := strings.Split(string(raw), "\n")
		for i, line := range lines {
			if credAssignRe.MatchString(line) {
				res.Findings = append(res.Findings, AnalyzeFinding{
					Path: fpath, Line: i + 1, Kind: "secret", Severity: "warning",
					Message: "possible hardcoded credential", Suggestion: "use env or a secret store",
				})
			}
			if pathJoinDotsRe.MatchString(line) {
				res.Findings = append(res.Findings, AnalyzeFinding{
					Path: fpath, Line: i + 1, Kind: "path-traversal", Severity: "warning",
					Message: "Join with .. looks like path traversal", Suggestion: "clean and reject .. before Join",
				})
			}
			if sqlConcatRe.MatchString(line) {
				res.Findings = append(res.Findings, AnalyzeFinding{
					Path: fpath, Line: i + 1, Kind: "sql-concat", Severity: "warning",
					Message: "SQL string concatenation", Suggestion: "use parameterized queries",
				})
			}
		}
	}
	res.Findings, res.Truncated = capFindings(res.Findings, maxFindings)
	if len(res.Findings) == 0 {
		res.Status = "empty"
	} else {
		res.Status = "ok"
	}
	res.Message = formatAnalyzeText(res)
	return res
}

func AnalyzeLint(ctx context.Context, path string, maxFindings int) AnalyzeResult {
	res := AnalyzeResult{Action: "lint", Tool: "go-vet", Findings: []AnalyzeFinding{}, Retryable: false}
	goBin, err := resolveAnalyzeBin("go")
	if err != nil {
		if u, ok := err.(*ErrToolUnavailable); ok {
			res.Status = "unavailable"
			res.Message = u.Error() + "; " + u.Suggestion
			return res
		}
		res.Status = "error"
		res.Message = err.Error()
		return res
	}
	ctx, cancel := context.WithTimeout(ctx, analyzeLintTimeout)
	defer cancel()
	dir := packageDir(path)
	args := []string{"vet"}
	st, statErr := os.Stat(path)
	if statErr == nil && !st.IsDir() {
		args = append(args, filepath.Base(path))
	} else {
		args = append(args, ".")
	}
	cmd := commandContext(ctx, goBin, args...)
	cmd.Dir = dir
	out, runErr := cmd.CombinedOutput()
	parseVetOutput(&res, string(out), dir)
	if sc, scErr := resolveAnalyzeBin("staticcheck"); scErr == nil {
		scCmd := commandContext(ctx, sc, ".")
		scCmd.Dir = dir
		scOut, _ := scCmd.CombinedOutput()
		parseVetOutput(&res, string(scOut), dir)
		if len(res.Findings) > 0 {
			res.Tool = "staticcheck"
		}
	}
	_ = runErr
	res.Findings, res.Truncated = capFindings(res.Findings, maxFindings)
	if len(res.Findings) == 0 {
		res.Status = "empty"
	} else {
		res.Status = "ok"
	}
	res.Message = formatAnalyzeText(res)
	return res
}

func parseVetOutput(res *AnalyzeResult, text, dir string) {
	for _, m := range vetLineRe.FindAllStringSubmatch(text, -1) {
		line := 0
		col := 0
		fmt.Sscanf(m[2], "%d", &line)
		if m[3] != "" {
			fmt.Sscanf(m[3], "%d", &col)
		}
		p := m[1]
		if !filepath.IsAbs(p) {
			p = filepath.Join(dir, p)
		}
		res.Findings = append(res.Findings, AnalyzeFinding{
			Path: p, Line: line, Col: col, Kind: "lint", Severity: "warning", Message: m[4],
		})
	}
}

func AnalyzeImpact(ctx context.Context, engine *UltraFastEngine, path, query string, maxFindings int) AnalyzeResult {
	res := AnalyzeResult{Action: "impact", Tool: "search", Findings: []AnalyzeFinding{}, Retryable: false}
	root := path
	if st, err := os.Stat(path); err == nil && !st.IsDir() {
		root = filepath.Dir(path)
		if query == "" {
			base := filepath.Base(path)
			query = strings.TrimSuffix(base, filepath.Ext(base))
		}
	}
	if query == "" {
		res.Status = "empty"
		res.Message = "impact needs query or a file basename"
		return res
	}
	out, err := engine.SearchFiles(ctx, SearchOptions{
		Path: root, Pattern: regexp.QuoteMeta(query), IncludeContent: true,
		CaseSensitive: true, MaxResults: maxFindings,
	})
	if err != nil {
		res.Status = "error"
		res.Message = err.Error()
		return res
	}
	for _, m := range out.Matches {
		res.Findings = append(res.Findings, AnalyzeFinding{
			Path: m.File, Line: m.LineNumber, Kind: "ref", Severity: "info",
			Symbol: query, Message: m.Line,
		})
	}
	res.Findings, res.Truncated = capFindings(res.Findings, maxFindings)
	if out.Truncated {
		res.Truncated = true
	}
	if len(res.Findings) == 0 {
		res.Status = "empty"
	} else {
		res.Status = "ok"
	}
	res.Message = formatAnalyzeText(res)
	return res
}

func formatAnalyzeText(r AnalyzeResult) string {
	if r.Status == "unavailable" || r.Status == "error" {
		return r.Message
	}
	if len(r.Findings) == 0 {
		return fmt.Sprintf("%s %s | 0 findings | %s", r.Action, r.Status, r.Tool)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s | %d findings | %s", r.Action, r.Status, len(r.Findings), r.Tool)
	if r.Truncated {
		b.WriteString(" | truncated")
	}
	b.WriteByte('\n')
	for _, f := range r.Findings {
		fmt.Fprintf(&b, "%s:%d: %s\n", f.Path, f.Line, f.Message)
	}
	return strings.TrimRightFunc(b.String(), unicode.IsSpace)
}
