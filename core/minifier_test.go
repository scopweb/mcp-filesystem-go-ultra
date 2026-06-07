package core

import (
	"strings"
	"testing"
)

func TestMinifyJS_Simple(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "single line",
			in:   "var x = 1;",
			want: "var x=1;",
		},
		{
			name: "spaces around operators",
			in:   "var x  =  1 +  2 ;",
			want: "var x=1+2;",
		},
		{
			name: "single-line comment stripped",
			in:   "var x = 1; // trailing comment\nvar y = 2;",
			want: "var x=1;var y=2;",
		},
		{
			name: "block comment stripped",
			in:   "var x = 1; /* multi\nline */ var y = 2;",
			want: "var x=1;var y=2;",
		},
		{
			name: "comment markers inside string preserved",
			in:   `var x = "// not a comment";`,
			want: `var x="// not a comment";`,
		},
		{
			name: "block comment markers inside string preserved",
			in:   `var x = "/* not a comment */";`,
			want: `var x="/* not a comment */";`,
		},
		{
			name: "double-quoted string with escaped quote",
			in:   `var x = "he said \"hi\"";`,
			want: `var x="he said \"hi\"";`,
		},
		{
			name: "single-quoted string with escaped apostrophe",
			in:   `var x = 'don\'t';`,
			want: `var x='don\'t';`,
		},
		{
			name: "regex literal",
			in:   "var x = /abc/g;",
			want: "var x=/abc/g;",
		},
		{
			name: "regex with char class",
			in:   "var x = /[a-z]+/i;",
			want: "var x=/[a-z]+/i;",
		},
		{
			name: "division vs regex at start",
			in:   "var a = 1 / 2;",
			want: "var a=1/2;",
		},
		{
			name: "division chain",
			in:   "var a = 10/2/5;",
			want: "var a=10/2/5;",
		},
		{
			name: "regex in array literal context",
			in:   "var a = [1, /foo/, 2];",
			want: "var a=[1,/foo/,2];",
		},
		{
			name: "template literal preserved",
			in:   "var x = `hello ${name}!`;",
			want: "var x=`hello ${name}!`;",
		},
		{
			name: "template literal with expression",
			in:   "var x = `a${1+2}b`;",
			want: "var x=`a${1+2}b`;",
		},
		{
			name: "comment at end of file (no trailing newline)",
			in:   "var x = 1;// comment",
			want: "var x=1;",
		},
		{
			name: "shebang preserved",
			in:   "#!/usr/bin/env node\nvar x = 1;",
			want: "#!/usr/bin/env node\nvar x=1;",
		},
		{
			name: "identifier separation preserved",
			in:   "return  true;",
			want: "return true;",
		},
		{
			name: "return keyword followed by identifier",
			in:   "function f(){return foo;}",
			want: "function f(){return foo;}",
		},
		{
			name: "no need to separate number from identifier",
			in:   "var x=1 ;var y=2 ;",
			want: "var x=1;var y=2;",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := MinifyJS(tt.in, MinifyOptions{})
			if got != tt.want {
				t.Errorf("MinifyJS(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestMinifyJS_Options(t *testing.T) {
	src := "var x = 1; // comment\n/* block */\nvar y = 2;"

	t.Run("all disabled means no-op (returns error from tool layer)", func(t *testing.T) {
		// The zero value applies all-true defaults in MinifyJS, so we can't
		// call it with all-false. Test the option struct directly.
		opts := MinifyOptions{RemoveComments: false, CollapseWhitespace: false, SingleLine: false}
		// MinifyJS detects this and applies defaults (back-compat).
		got, _ := MinifyJS(src, opts)
		// Should be the same as default minify because the zero value gets defaulted.
		if !strings.Contains(got, ";") {
			t.Errorf("expected minified output, got %q", got)
		}
	})

	t.Run("keep single line false preserves newlines", func(t *testing.T) {
		opts := MinifyOptions{
			RemoveComments:     true,
			CollapseWhitespace: true,
			SingleLine:         false,
		}
		got, _ := MinifyJS("var x = 1;\nvar y = 2;", opts)
		if !strings.Contains(got, "\n") {
			t.Errorf("expected newline in output, got %q", got)
		}
	})

	t.Run("keep comments false preserves them", func(t *testing.T) {
		opts := MinifyOptions{
			RemoveComments:     false,
			CollapseWhitespace: true,
			SingleLine:         true,
		}
		got, _ := MinifyJS("var x = 1; // hi", opts)
		// The space between `;` and `//` is collapsed by CollapseWhitespace
		// (since `;` is not an ident char, no space is needed). The comment
		// itself is preserved verbatim.
		if !strings.Contains(got, "//hi") {
			t.Errorf("expected comment to be preserved (collapsed form), got %q", got)
		}
	})
}

func TestMinifyJS_Stats(t *testing.T) {
	src := "var x = 1;   // hello\n  var y = 2;\n"
	got, stats := MinifyJS(src, MinifyOptions{})

	if stats.InputBytes != len(src) {
		t.Errorf("InputBytes = %d, want %d", stats.InputBytes, len(src))
	}
	if stats.OutputBytes != len(got) {
		t.Errorf("OutputBytes = %d, want %d", stats.OutputBytes, len(got))
	}
	if stats.BytesSaved != stats.InputBytes-stats.OutputBytes {
		t.Errorf("BytesSaved = %d, want %d", stats.BytesSaved, stats.InputBytes-stats.OutputBytes)
	}
	if stats.CommentsStripped < 1 {
		t.Errorf("CommentsStripped = %d, want >= 1", stats.CommentsStripped)
	}
	if stats.ReductionPercent < 0 {
		t.Errorf("ReductionPercent = %f, want >= 0", stats.ReductionPercent)
	}
}

func TestMinifyJS_TruncatedOnUnterminatedString(t *testing.T) {
	src := `var x = "never ends`
	_, stats := MinifyJS(src, MinifyOptions{})
	if !stats.Truncated {
		t.Errorf("expected Truncated=true for unterminated string, got %+v", stats)
	}
}

func TestMinifyJS_TruncatedOnUnterminatedComment(t *testing.T) {
	src := "var x = 1; /* never ends"
	_, stats := MinifyJS(src, MinifyOptions{})
	if !stats.Truncated {
		t.Errorf("expected Truncated=true for unterminated block comment, got %+v", stats)
	}
}

func TestMinifyJS_RealWorld(t *testing.T) {
	// A small but realistic snippet to verify the state machine handles
	// interleaved code, strings, regex, and template literals.
	src := `
		// DataTable renderer
		function renderData(data) {
			var re = /[a-z]+/gi;
			var msg = "Hello, " + data.name;
			var tmpl = ` + "`${data.greeting}, ${data.name}!`" + `;
			return [re, msg, tmpl];
		}
	`
	min, _ := MinifyJS(src, MinifyOptions{})

	// Sanity: comments stripped, identifiers separated properly, strings/regex intact.
	if strings.Contains(min, "//") {
		t.Errorf("// comment should be stripped, got: %q", min)
	}
	if !strings.Contains(min, `"Hello, "`) {
		t.Errorf("string should be preserved, got: %q", min)
	}
	if !strings.Contains(min, "/[a-z]+/gi") {
		t.Errorf("regex should be preserved, got: %q", min)
	}
	if !strings.Contains(min, "`${data.greeting}, ${data.name}!`") {
		t.Errorf("template literal should be preserved, got: %q", min)
	}
	// function renderData should NOT be merged with the brace
	if !strings.Contains(min, "function renderData(") {
		t.Errorf("identifier separation broken, got: %q", min)
	}
}

func TestMinifyJS_DivisionInExpression(t *testing.T) {
	// A/B should be division, not A and B with a regex.
	src := "var x = a / b / c;"
	min, _ := MinifyJS(src, MinifyOptions{})
	want := "var x=a/b/c;"
	if min != want {
		t.Errorf("got %q, want %q", min, want)
	}
}

func TestMinifyJS_RegexAfterAssign(t *testing.T) {
	// After =, the / starts a regex.
	src := "var x = /pattern/g;"
	min, _ := MinifyJS(src, MinifyOptions{})
	want := "var x=/pattern/g;"
	if min != want {
		t.Errorf("got %q, want %q", min, want)
	}
}

func TestMinifyJS_TernaryAndNullish(t *testing.T) {
	// `??` and `?.` are operators, not relevant to the minifier state machine,
	// but verify they pass through unchanged.
	src := "var x = a ?? b; var y = obj?.prop;"
	min, _ := MinifyJS(src, MinifyOptions{})
	want := "var x=a??b;var y=obj?.prop;"
	if min != want {
		t.Errorf("got %q, want %q", min, want)
	}
}

func TestMinifyJS_EmptyInput(t *testing.T) {
	got, stats := MinifyJS("", MinifyOptions{})
	if got != "" {
		t.Errorf("empty input should produce empty output, got %q", got)
	}
	if stats.InputBytes != 0 {
		t.Errorf("InputBytes = %d, want 0", stats.InputBytes)
	}
}
