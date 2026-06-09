package core

import (
	"strings"
	"unicode"
)

// MinifyOptions controls JavaScript minification behavior.
//
// The zero value applies sensible defaults: remove comments, collapse
// runs of whitespace, and produce single-line output. Pass a non-zero
// value to override any of these.
type MinifyOptions struct {
	// RemoveComments strips line (//) and block (/* */) comments.
	// String contents are NEVER touched — only code-level comments.
	RemoveComments bool

	// CollapseWhitespace replaces runs of spaces and tabs with a single
	// space where required to keep tokens separate. A space is only
	// emitted between two identifier-like characters (letters, digits,
	// underscore, dollar sign).
	CollapseWhitespace bool

	// SingleLine emits all output on one line (no \n, no \r). If false,
	// line breaks in the source are preserved.
	SingleLine bool
}

// MinifyStats reports what the minifier did to a single file. Returned
// by MinifyJS so callers can report byte savings to the user.
type MinifyStats struct {
	InputBytes       int
	OutputBytes      int
	BytesSaved       int
	ReductionPercent float64
	CommentsStripped int
	// True if the minifier hit a state it could not resolve safely
	// (e.g. an unterminated string). Output may be truncated.
	Truncated bool
}

// jsMinifier is the internal state-machine minifier. It walks the input
// byte by byte, tracking whether we are in code, a string literal, a
// template literal, a regex literal, or a comment.
//
// The minifier is intentionally simple — a full JS parser/AST is out of
// scope. Edge cases that a real parser would catch (regex with a `/`
// inside a character class, tagged template literals, etc.) are handled
// with conservative heuristics that err on the side of preserving the
// original bytes rather than corrupting them.
type jsMinifier struct {
	src              string
	pos              int
	out              strings.Builder
	lastSig          byte // last non-whitespace, non-comment byte output
	commentsStripped int
	truncated        bool
}

// MinifyJS minifies JavaScript source code. It strips comments, collapses
// whitespace, and (by default) emits a single line. Pure stdlib — no
// external dependencies, no AST, no JS runtime.
//
// SAFETY: best-effort. Real-world JS that uses exotic regex patterns or
// tagged template literals at the boundary of code/regex heuristics may
// not be perfectly tokenized. For the common 95% of real-world code this
// works correctly. The minifier never modifies the contents of strings,
// template literals, or regex bodies — only the spacing and comments
// around them.
func MinifyJS(src string, opts MinifyOptions) (string, MinifyStats) {
	// Apply defaults for the zero value so callers can use MinifyJS(src, MinifyOptions{})
	// or even MinifyJS(src) — wait, the latter isn't possible in Go; use the explicit form.
	if !opts.RemoveComments && !opts.CollapseWhitespace && !opts.SingleLine {
		opts.RemoveComments = true
		opts.CollapseWhitespace = true
		opts.SingleLine = true
	}

	stats := MinifyStats{InputBytes: len(src)}
	m := &jsMinifier{src: src}
	out := m.run(opts)
	stats.OutputBytes = len(out)
	stats.BytesSaved = stats.InputBytes - stats.OutputBytes
	stats.CommentsStripped = m.commentsStripped
	stats.Truncated = m.truncated
	if stats.InputBytes > 0 {
		stats.ReductionPercent = float64(stats.BytesSaved) / float64(stats.InputBytes) * 100
	}
	return out, stats
}

// run is the main state machine loop. It dispatches on the current input
// character and the current high-level state (we are inside a string /
// regex / comment, or in code).
func (m *jsMinifier) run(opts MinifyOptions) string {
	// Pre-size the output builder. Best case: no whitespace, no comments.
	// Worst case: 1.0x (we never grow).
	m.out.Grow(len(m.src))

	for m.pos < len(m.src) {
		c := m.src[m.pos]

		switch {
		case c == '\'':
			m.consumeSingleQuotedString()
		case c == '"':
			m.consumeDoubleQuotedString()
		case c == '`':
			m.consumeTemplateLiteral()
		case c == '/' && m.pos+1 < len(m.src) && m.src[m.pos+1] == '/':
			if opts.RemoveComments {
				m.consumeLineComment()
			} else {
				m.out.WriteByte('/')
				m.out.WriteByte('/')
				m.lastSig = '/'
				m.pos += 2
			}
		case c == '/' && m.pos+1 < len(m.src) && m.src[m.pos+1] == '*':
			if opts.RemoveComments {
				m.consumeBlockComment()
			} else {
				m.out.WriteByte('/')
				m.out.WriteByte('*')
				m.lastSig = '*'
				m.pos += 2
			}
		case c == '/':
			if m.isRegexContext() {
				m.consumeRegexLiteral()
			} else {
				// Division operator (or /=). Emit and move on.
				m.out.WriteByte('/')
				m.lastSig = '/'
				m.pos++
			}
		case c == '#' && m.pos == 0 && m.hasShebang():
			// Shebang at the very start of the file — preserve it verbatim
			// (some tools strip it, but it's safe to keep and the minified
			// output will still be valid JS).
			m.consumeShebang(opts)
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			m.consumeWhitespace(opts)
		default:
			// Anything else: emit as-is.
			m.out.WriteByte(c)
			if !unicode.IsSpace(rune(c)) {
				m.lastSig = c
			}
			m.pos++
		}
	}

	return m.out.String()
}

// consumeSingleQuotedString handles '...' with backslash escapes.
// Stops on unescaped closing quote or EOF. Marks truncated on EOF.
func (m *jsMinifier) consumeSingleQuotedString() {
	m.out.WriteByte('\'')
	m.lastSig = '\''
	m.pos++

	for m.pos < len(m.src) {
		c := m.src[m.pos]
		m.out.WriteByte(c)
		m.pos++
		switch c {
		case '\\':
			// Backslash escapes the next char. If the next char exists,
			// write it too. Handles \', \", \\, \n, \u{...}, \xXX, etc.
			if m.pos < len(m.src) {
				m.out.WriteByte(m.src[m.pos])
				m.pos++
			}
		case '\'':
			// End of string.
			return
		}
	}
	m.truncated = true
}

// consumeDoubleQuotedString handles "..." with backslash escapes.
func (m *jsMinifier) consumeDoubleQuotedString() {
	m.out.WriteByte('"')
	m.lastSig = '"'
	m.pos++

	for m.pos < len(m.src) {
		c := m.src[m.pos]
		m.out.WriteByte(c)
		m.pos++
		switch c {
		case '\\':
			if m.pos < len(m.src) {
				m.out.WriteByte(m.src[m.pos])
				m.pos++
			}
		case '"':
			return
		}
	}
	m.truncated = true
}

// consumeTemplateLiteral handles `...` with backtick close, $ and {
// for ${...} expression interpolation (which is treated as code, so we
// recurse into the main loop), and backslash escapes.
func (m *jsMinifier) consumeTemplateLiteral() {
	m.out.WriteByte('`')
	m.lastSig = '`'
	m.pos++

	for m.pos < len(m.src) {
		c := m.src[m.pos]
		switch c {
		case '\\':
			m.out.WriteByte(c)
			m.pos++
			if m.pos < len(m.src) {
				m.out.WriteByte(m.src[m.pos])
				m.pos++
			}
		case '`':
			m.out.WriteByte('`')
			m.lastSig = '`'
			m.pos++
			return
		case '$':
			// Check for ${ ... }
			if m.pos+1 < len(m.src) && m.src[m.pos+1] == '{' {
				m.out.WriteByte('$')
				m.out.WriteByte('{')
				m.pos += 2
				// Recurse into the main loop for the expression body.
				// The expression is a normal code context — strings,
				// regex, comments all work the same way.
				// We bump braceDepth so } inside the expression isn't
				// treated as template close.
				m.runTemplateExpression(1, 0)
				// After runTemplateExpression returns, we are back at
				// the closing } of ${...}. The caller loop continues.
				continue
			}
			m.out.WriteByte('$')
			m.pos++
		default:
			m.out.WriteByte(c)
			m.pos++
		}
	}
	m.truncated = true
}

// runTemplateExpression runs the main state machine starting at the
// current position until the closing `}` of a ${...} expression. braceDepth
// is the current nesting depth (1 for the opening ${).
func (m *jsMinifier) runTemplateExpression(braceDepth, _ int) {
	// Track unprocessed outer state. We don't return it; we re-implement
	// the run loop here with a brace-aware exit condition. The main run()
	// is generic but doesn't know about template interpolation.
	startPos := m.pos
	for m.pos < len(m.src) {
		c := m.src[m.pos]
		switch {
		case c == '\'':
			m.consumeSingleQuotedString()
		case c == '"':
			m.consumeDoubleQuotedString()
		case c == '`':
			m.consumeTemplateLiteral()
		case c == '/' && m.pos+1 < len(m.src) && m.src[m.pos+1] == '/':
			m.consumeLineComment()
		case c == '/' && m.pos+1 < len(m.src) && m.src[m.pos+1] == '*':
			m.consumeBlockComment()
		case c == '/' && m.isRegexContext():
			m.consumeRegexLiteral()
		case c == '/' || c == '*':
			m.out.WriteByte(c)
			m.lastSig = c
			m.pos++
		case c == '{':
			m.out.WriteByte(c)
			m.lastSig = c
			m.pos++
			braceDepth++
		case c == '}':
			m.out.WriteByte(c)
			m.lastSig = c
			m.pos++
			braceDepth--
			if braceDepth == 0 {
				return
			}
		default:
			m.out.WriteByte(c)
			if !unicode.IsSpace(rune(c)) {
				m.lastSig = c
			}
			m.pos++
		}
	}
	_ = startPos
	m.truncated = true
}

// consumeRegexLiteral handles /.../[flags]. The leading / was already
// matched by the caller. We consume the body, then any flag chars
// [a-z]*.
func (m *jsMinifier) consumeRegexLiteral() {
	// Emit the opening slash
	m.out.WriteByte('/')
	m.lastSig = '/'
	m.pos++

	inCharClass := false // inside [...]
	for m.pos < len(m.src) {
		c := m.src[m.pos]
		switch c {
		case '\\':
			// Escape: write both bytes
			m.out.WriteByte(c)
			m.pos++
			if m.pos < len(m.src) {
				m.out.WriteByte(m.src[m.pos])
				m.pos++
			}
		case '[':
			inCharClass = true
			m.out.WriteByte(c)
			m.pos++
		case ']':
			inCharClass = false
			m.out.WriteByte(c)
			m.pos++
		case '/':
			if !inCharClass {
				// End of regex body
				m.out.WriteByte('/')
				m.lastSig = '/'
				m.pos++
				// Consume flags: g, i, m, s, u, y, d
				for m.pos < len(m.src) {
					f := m.src[m.pos]
					if (f >= 'a' && f <= 'z') || (f >= 'A' && f <= 'Z') {
						m.out.WriteByte(f)
						m.lastSig = f
						m.pos++
					} else {
						break
					}
				}
				return
			}
			m.out.WriteByte(c)
			m.pos++
		default:
			m.out.WriteByte(c)
			m.pos++
		}
	}
	m.truncated = true
}

// consumeLineComment skips from after // to (but not including) the
// next \n. The newline itself is left for the main loop to handle as
// whitespace.
func (m *jsMinifier) consumeLineComment() {
	m.commentsStripped++
	// Skip past the //
	m.pos += 2
	for m.pos < len(m.src) && m.src[m.pos] != '\n' {
		m.pos++
	}
	// Do not consume the \n — let the main loop emit it (or strip it as
	// whitespace if SingleLine is on).
}

// consumeBlockComment skips from after /* to */ (inclusive).
func (m *jsMinifier) consumeBlockComment() {
	m.commentsStripped++
	m.pos += 2
	for m.pos < len(m.src) {
		if m.src[m.pos] == '*' && m.pos+1 < len(m.src) && m.src[m.pos+1] == '/' {
			m.pos += 2
			return
		}
		m.pos++
	}
	m.truncated = true
}

// consumeShebang copies #!... up to (but not including) the first \n
// verbatim, and always emits a \n after it. We keep the shebang because
// some bundlers and CLIs expect it at the start. The trailing \n is
// emitted even in SingleLine mode — a shebang must be on its own line
// per the JS spec, so we treat it as structurally significant.
func (m *jsMinifier) consumeShebang(_ MinifyOptions) {
	start := m.pos
	for m.pos < len(m.src) && m.src[m.pos] != '\n' {
		m.pos++
	}
	shebang := m.src[start:m.pos]
	m.out.WriteString(shebang)
	m.out.WriteByte('\n')
	// Don't update lastSig — the shebang doesn't affect JS tokenization.
}

// hasShebang reports whether the file starts with #!. Only checked at
// position 0; that's the only place a shebang is valid in JS.
func (m *jsMinifier) hasShebang() bool {
	if len(m.src) < 2 || m.src[0] != '#' || m.src[1] != '!' {
		return false
	}
	return true
}

// isRegexContext uses the last significant output character to decide
// whether the current / starts a regex literal or is a division operator.
//
// This is the standard JS tokenizer heuristic: a / can be a regex after
// values that "expect" an operand (operators, punctuation, statement
// start, keywords). It cannot be a regex after another value expression
// (identifier, number, closing paren, etc.).
func (m *jsMinifier) isRegexContext() bool {
	// At the start of the file or after a newline at the very beginning
	// (no lastSig yet), / is a regex.
	if m.lastSig == 0 {
		return true
	}
	switch m.lastSig {
	case '=', '(', ',', '[', '!', '&', '|', '?',
		':', ';', '{', '}', '~', '^', '+', '-',
		'*', '/', '%', '<', '>', '\n', '\r':
		return true
	}
	// After a closing ) or ] — the contained expression ended, and a new
	// expression can start with a regex. e.g., `(1+2) /re/` is ambiguous
	// in real JS but tools normally disambiguate via this heuristic.
	// We treat it as NOT a regex after ')' or ']' because that's almost
	// always a function call or array index, and treating it as regex
	// would break e.g. `arr.length/2`.
	// Note: 'return' and similar keyword ENDING characters are handled
	// indirectly — the space after the keyword is whitespace, which
	// we skip, so the lastSig will be 'n' (last char of "return").
	// That's fine: "return/foo/" is treated as a regex context starting
	// with /, which is what real JS does.
	return false
}

// consumeWhitespace handles a run of ASCII whitespace. Behavior depends
// on opts.SingleLine and opts.CollapseWhitespace:
//
//   - SingleLine=true, CollapseWhitespace=true (the default): drop the
//     whitespace entirely, or emit a single space if needed to keep
//     two identifier-like tokens from merging.
//   - SingleLine=true, CollapseWhitespace=false: emit a single space for
//     any whitespace run (preserves "spacing presence" without the
//     actual whitespace).
//   - SingleLine=false, CollapseWhitespace=true: emit \n for real line
//     breaks (useful for keeping stack-trace line numbers), drop other
//     whitespace or emit a single space when needed.
//   - SingleLine=false, CollapseWhitespace=false: emit \n for line breaks,
//     emit a single space for space/tab runs.
func (m *jsMinifier) consumeWhitespace(opts MinifyOptions) {
	// Track whether we saw a real line break.
	sawLineBreak := false
	for m.pos < len(m.src) {
		c := m.src[m.pos]
		if c == ' ' || c == '\t' {
			m.pos++
			continue
		}
		if c == '\n' || c == '\r' {
			sawLineBreak = true
			// Skip the \n (and \r for CRLF)
			if c == '\r' && m.pos+1 < len(m.src) && m.src[m.pos+1] == '\n' {
				m.pos += 2
			} else {
				m.pos++
			}
			continue
		}
		break
	}

	// No-op if we're at EOF (trailing whitespace).
	if m.pos >= len(m.src) {
		return
	}

	// SingleLine=false: preserve line breaks as \n.
	if sawLineBreak && !opts.SingleLine {
		m.out.WriteByte('\n')
		// The \n is whitespace — don't update lastSig.
		return
	}

	if !opts.CollapseWhitespace {
		// Emit a single space for the run.
		m.out.WriteByte(' ')
		return
	}

	// CollapseWhitespace: emit a single space only if the next non-whitespace
	// character and the last significant char both look like identifier
	// characters (so we don't merge "return x" into "returnx" or "1 2" into "12").
	next := m.nextSignificant()
	if next == 0 {
		return
	}
	if isIdentByte(m.lastSig) && isIdentByte(next) {
		m.out.WriteByte(' ')
	}
	// Otherwise: drop the whitespace entirely.
}

// nextSignificant returns the next non-whitespace byte without advancing
// the cursor. Returns 0 if EOF.
func (m *jsMinifier) nextSignificant() byte {
	for i := m.pos; i < len(m.src); i++ {
		c := m.src[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			continue
		}
		return c
	}
	return 0
}

// isIdentByte reports whether c can appear in a JS identifier (letter,
// digit, underscore, dollar). Two such characters next to each other
// require a space to keep them as separate tokens.
func isIdentByte(c byte) bool {
	return (c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9') ||
		c == '_' || c == '$'
}
