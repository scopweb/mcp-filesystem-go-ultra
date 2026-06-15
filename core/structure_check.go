package core

import (
	"fmt"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
)

// Point 2: lightweight post-edit structural verification.
//
// The build is the only real safety net for syntax, but a cheap delimiter
// balance check catches the most common class of edit accident — leaving an
// unbalanced brace/paren/bracket after extracting or deleting a code block
// (e.g. removing a Blazor @code section or a Go func body). It is a smoke alarm,
// never a block: edit_file/multi_edit still apply the change and create a
// backup; the warning is attached to the response so the model can re-check
// before running the build.
//
// DELTA approach (key to avoiding false positives): we only warn when the OLD
// content was balanced and the NEW content is not. Files that legitimately do
// not balance on their own (fragments, templates, partials) or that were
// already unbalanced before the edit never trigger a warning — only an
// imbalance *introduced by this edit* does.

// isBalanceCheckedExt reports whether the file extension is a C-like / brace
// language where delimiter balance is a meaningful signal.
func isBalanceCheckedExt(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go", ".cs", ".razor", ".cshtml", ".js", ".jsx", ".ts", ".tsx",
		".c", ".cc", ".cpp", ".h", ".hpp", ".java", ".rs", ".json",
		".css", ".scss", ".less":
		return true
	}
	return false
}

// delimiterBalance counts the net balance of {}, () and [] in s, ignoring
// delimiters that appear inside strings ("...", '...', `...`) and comments
// (// line and /* block */). A balanced section returns (0, 0, 0). The scanner
// is generic C-like; it is intentionally simple (not a full parser) because the
// DELTA comparison tolerates minor scanning imprecision: as long as old and new
// are scanned the same way, a pre-existing quirk cancels out.
func delimiterBalance(s string) (curly, round, square int) {
	var stringQuote byte // 0 = not in a string; otherwise the opening quote
	inLineComment := false
	inBlockComment := false
	escaped := false

	for i := 0; i < len(s); i++ {
		c := s[i]

		switch {
		case inLineComment:
			if c == '\n' {
				inLineComment = false
			}
			continue
		case inBlockComment:
			if c == '*' && i+1 < len(s) && s[i+1] == '/' {
				inBlockComment = false
				i++
			}
			continue
		case stringQuote != 0:
			if escaped {
				escaped = false
				continue
			}
			// Backtick (raw) strings do not process escapes.
			if c == '\\' && stringQuote != '`' {
				escaped = true
				continue
			}
			if c == stringQuote {
				stringQuote = 0
			}
			continue
		}

		// Not inside a string or comment.
		switch c {
		case '/':
			if i+1 < len(s) {
				if s[i+1] == '/' {
					inLineComment = true
					i++
					continue
				}
				if s[i+1] == '*' {
					inBlockComment = true
					i++
					continue
				}
			}
		case '"', '\'', '`':
			stringQuote = c
		case '{':
			curly++
		case '}':
			curly--
		case '(':
			round++
		case ')':
			round--
		case '[':
			square++
		case ']':
			square--
		}
	}
	return curly, round, square
}

// CheckBalanceDelta returns a non-empty warning when an edit introduces a
// delimiter imbalance that was not present before. Returns "" when the file
// extension is not brace-based, when nothing changed, or when the imbalance
// (if any) already existed before the edit. Never blocks (point 2).
func CheckBalanceDelta(oldContent, newContent, path string) string {
	if !isBalanceCheckedExt(path) {
		return ""
	}

	oCurly, oRound, oSquare := delimiterBalance(oldContent)
	nCurly, nRound, nSquare := delimiterBalance(newContent)

	var issues []string
	if oCurly == 0 && nCurly != 0 {
		issues = append(issues, fmt.Sprintf("curly braces {} now off by %+d", nCurly))
	}
	if oRound == 0 && nRound != 0 {
		issues = append(issues, fmt.Sprintf("parentheses () now off by %+d", nRound))
	}
	if oSquare == 0 && nSquare != 0 {
		issues = append(issues, fmt.Sprintf("square brackets [] now off by %+d", nSquare))
	}
	if len(issues) == 0 {
		return ""
	}

	return "⚠ possible structural imbalance after edit: " + strings.Join(issues, ", ") +
		". The file was balanced before this edit but is not now — an anchor may have been off. Verify the file before building."
}

// CheckStructureDelta runs the best available post-edit structural check for the
// file type and returns a non-blocking warning (or "" when clean):
//   - .go  → a real Go parse (go/parser), which catches far more than brace
//     balance (missing keywords, bad tokens, dangling exprs) for free in-process.
//   - other brace languages → the lexical brace-balance delta.
//
// Both follow the DELTA principle: only warn when the OLD content was valid and
// the NEW content is not, so pre-existing breakage never triggers a false alarm.
// This is the single entry point used by EditFile / MultiEdit / DeleteLineRange.
func CheckStructureDelta(oldContent, newContent, path string) string {
	if strings.EqualFold(filepath.Ext(path), ".go") {
		return CheckGoSyntax(oldContent, newContent, path)
	}
	return CheckBalanceDelta(oldContent, newContent, path)
}

// CheckGoSyntax parses newContent as Go and returns a warning if it fails to
// parse while oldContent parsed (delta). Pure stdlib (go/parser + go/token), in
// process, no external toolchain — complements `go vet`/`go build` by catching
// the error at edit time instead of in the build cycle. Returns "" for non-.go
// paths. Warning only; never blocks.
func CheckGoSyntax(oldContent, newContent, path string) string {
	if !strings.EqualFold(filepath.Ext(path), ".go") {
		return ""
	}
	// Delta: if the file didn't parse before the edit, the edit didn't introduce
	// the breakage — stay silent (e.g. a partial/generated file under repair).
	if parseGoError(oldContent, path) != "" {
		return ""
	}
	if msg := parseGoError(newContent, path); msg != "" {
		return "⚠ Go syntax error introduced by this edit: " + msg +
			". The file parsed before this edit but no longer does — verify before building."
	}
	return ""
}

// parseGoError returns the first parse error message for Go source, or "" if it
// parses. SkipObjectResolution keeps it fast (we only care about syntax).
func parseGoError(content, path string) string {
	name := filepath.Base(path)
	if name == "" || name == "." {
		name = "src.go"
	}
	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, name, content, parser.SkipObjectResolution); err != nil {
		return err.Error()
	}
	return ""
}
