package mcpserver

// context_pack — selection layer.
//
// The pack is built in two stages:
//
//  1. Selection (this file): which files, which symbols, which line ranges.
//     Everything is derived from ONE snapshot per file — the same bytes that
//     produce the snippets also produce the content_hash, so a caller can
//     trust that the hash describes the content it is looking at.
//  2. Budgeting (context_pack_budget.go): how much of that selection fits in
//     budget_chars and how it degrades when it does not.
//
// Design rules enforced here:
//   - A range is never invented. Only the analyzers behind analyze_code may
//     claim a range; anything else is a textual window and says so.
//   - A query is only compared against symbol names when it is a plain
//     identifier. A regex is never reinterpreted as an identifier.
//   - Ordering is explicit at every tie-break so the same tree yields the
//     same pack.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/mcp/filesystem-ultra/core"
)

// Detail levels, from richest to poorest. The budgeter degrades a fragment
// through them in this order.
const (
	packDetailSnippet   = "snippet"
	packDetailSignature = "signature"
	packDetailLocator   = "locator"
)

// Range status.
//
//	exact       — an analyzer resolved the range (Go AST, or the brace
//	              matcher behind the C#/JS analyzers).
//	approximate — a range exists but does not correspond to one definition
//	              (a merged span, or a textual window).
//	unavailable — no reliable end is known; only the anchor line is reported.
const (
	packRangeExact       = "exact"
	packRangeApproximate = "approximate"
	packRangeUnavailable = "unavailable"
)

// Range source, so the caller can tell an analyzer range from a window.
const (
	packSourceGoAST      = "go-ast"
	packSourceRegexBrace = "regex-brace"
	packSourceRegex      = "regex"
	packSourceWindow     = "window"
	packSourceMerged     = "merged"
	packSourceNone       = "none"
)

// Fragment priority. Lower is better.
const (
	packPrioDefinitionExact = 0 // symbol whose name IS the query identifier
	packPrioDefinitionLike  = 1 // symbol whose name CONTAINS the identifier
	packPrioSymbol          = 2 // symbol that contains the textual match
	packPrioMention         = 3 // plain textual match, no enclosing symbol
	packPrioAnchor          = 4 // focus-only file: its first symbol
)

const (
	// packMaxSymbols is how many symbols the analyzer may return for one
	// file. It is deliberately larger than the six the pack used to show:
	// the whole point is to reach a relevant symbol that sits past the
	// head of the file.
	packMaxSymbols = 256
	// packMaxMatches bounds the candidate search. The pack reports when it
	// is reached; it never pretends the scope was fully covered.
	packMaxMatches = 200
	// packMaxFragments bounds the fragments kept per file.
	packMaxFragments = 3
	// packCandidateScanLimit bounds how many candidates are opened. Files
	// beyond it are reported as not examined — never as absent.
	packCandidateScanLimit = 60
	// packWindowBefore/packWindowAfter bound the textual fallback window.
	packWindowBefore = 2
	packWindowAfter  = 4
	// packMaxSnippetLines / packMaxSnippetChars bound one snippet. The
	// full range stays reachable through read_file.
	packMaxSnippetLines = 40
	packMaxSnippetChars = 1200
	// packMaxMergedSpan bounds the union of two overlapping ranges.
	packMaxMergedSpan = 400
	// packMinIdentLen is the shortest identifier that may be matched
	// against a symbol name as a substring. Shorter queries match far too
	// many symbols to be a useful ranking signal.
	packMinIdentLen = 3
)

// packIdentRe matches a plain identifier. Anything else (a regex, a glob, a
// path) is never treated as an identifier.
var packIdentRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// packFragment is one bounded piece of context taken from one file.
type packFragment struct {
	Path        string
	StartLine   int
	EndLine     int
	Symbol      string
	Kind        string
	Signature   string
	RangeStatus string
	RangeSource string
	Text        string
	Hash        string
	Priority    int
	Merged      int // how many fragments were merged into this one
}

// packCandidate is one file considered for the pack.
type packCandidate struct {
	Path       string
	FocusIndex int // -1 when the file was not named by focus_paths
	Class      int
	Matches    int
	MatchLines []int
	Fragments  []packFragment
	Hash       string
	LineCount  int
	Unreadable bool
	Stale      bool // symbol ranges came from a different revision
	Merged     int  // overlapping ranges merged into one fragment
}

// packDiscovery is the candidate set plus the facts a caller needs to know
// what was and was not examined.
type packDiscovery struct {
	Candidates       []*packCandidate
	SearchTruncated  bool
	SearchMatches    int
	Denied           int
	QueryHadMatches  bool
	SourceCandidates int
}

// packAnalyzableExts are the extensions the analyzers behind analyze_code
// understand. A source-only scan over them is what keeps a definition
// reachable when the general search is truncated.
var packAnalyzableExts = []string{
	".go", ".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx", ".cs", ".razor", ".sql",
}

// packUniqueInts removes repeated line numbers, keeping the order.
func packUniqueInts(in []int) []int {
	if len(in) < 2 {
		return in
	}
	out := in[:1]
	for _, v := range in[1:] {
		if v != out[len(out)-1] {
			out = append(out, v)
		}
	}
	return out
}

// packQueryIdentifier returns the query as a plain identifier when it can be
// compared against symbol names. A regex or any pattern with metacharacters
// returns "" so the pack never reinterprets it.
func packQueryIdentifier(query string) string {
	q := strings.TrimSpace(query)
	if q == "" || !packIdentRe.MatchString(q) {
		return ""
	}
	return q
}

// packDiscover collects the candidate files: the focus paths plus every file
// the query matched. Files outside allowed-paths, blocked by the file policy
// or protected as secrets are counted, never named.
func packDiscover(ctx context.Context, engine *core.UltraFastEngine, root, query string, focus []string) (*packDiscovery, error) {
	disc := &packDiscovery{}
	byPath := map[string]*packCandidate{}

	candidate := func(p string) *packCandidate {
		p = core.NormalizePath(p)
		if p == "" {
			return nil
		}
		if c, ok := byPath[p]; ok {
			return c
		}
		if !engine.IsPathAllowed(p) {
			disc.Denied++
			return nil
		}
		if core.IsSecretPath(p) && !engine.AllowSecrets() {
			disc.Denied++
			return nil
		}
		if err := engine.Authorize(core.OpRead, p); err != nil {
			disc.Denied++
			return nil
		}
		st, err := os.Stat(p)
		if err != nil || st.IsDir() {
			disc.Denied++
			return nil
		}
		c := &packCandidate{Path: p, FocusIndex: -1}
		byPath[p] = c
		return c
	}

	for i, f := range focus {
		fp := strings.TrimSpace(f)
		if fp == "" {
			continue
		}
		if !filepath.IsAbs(fp) {
			fp = filepath.Join(root, fp)
		}
		c := candidate(fp)
		if c == nil {
			continue
		}
		if c.FocusIndex < 0 || i < c.FocusIndex {
			c.FocusIndex = i
		}
	}

	if query != "" {
		out, err := engine.SearchFiles(ctx, core.SearchOptions{
			Path:           root,
			Pattern:        query,
			IncludeContent: true,
			MaxResults:     packMaxMatches,
		})
		if err != nil {
			return nil, err
		}
		disc.SearchTruncated = out.Truncated
		disc.SearchMatches = out.MatchCount
		disc.QueryHadMatches = len(out.Matches) > 0
		for _, m := range out.Matches {
			c := candidate(m.File)
			if c == nil {
				continue
			}
			c.Matches++
			if m.LineNumber > 0 {
				c.MatchLines = append(c.MatchLines, m.LineNumber)
			}
		}

		// A definition must not be lost to a truncated lexical search. The
		// general search is ordered by path, so on a large tree its cap
		// can be spent entirely on prose and data files while the source
		// file that defines the identifier is never a candidate. A second
		// scan restricted to the extensions the analyzers understand
		// reaches those files: they are far fewer, so the same cap goes
		// much further. It is a second look, not a replacement: a query
		// that is not a plain identifier gets no definition ranking and
		// needs no second scan.
		if packQueryIdentifier(query) != "" {
			src, srcErr := engine.SearchFiles(ctx, core.SearchOptions{
				Path:           root,
				Pattern:        query,
				IncludeContent: true,
				MaxResults:     packMaxMatches,
				FileTypes:      packAnalyzableExts,
			})
			if srcErr != nil {
				return nil, srcErr
			}
			if src.Truncated {
				disc.SearchTruncated = true
			}
			for _, m := range src.Matches {
				c := candidate(m.File)
				if c == nil {
					continue
				}
				if c.Matches == 0 {
					disc.SourceCandidates++
				}
				c.Matches++
				if m.LineNumber > 0 {
					c.MatchLines = append(c.MatchLines, m.LineNumber)
				}
			}
		}
	}

	for _, c := range byPath {
		sort.Ints(c.MatchLines)
		c.MatchLines = packUniqueInts(c.MatchLines)
		// The relevance weight is the number of distinct lines that
		// matched, not the number of scans that saw them.
		c.Matches = len(c.MatchLines)
		disc.Candidates = append(disc.Candidates, c)
	}
	sort.SliceStable(disc.Candidates, func(i, j int) bool {
		return packCandidateLess(disc.Candidates[i], disc.Candidates[j])
	})
	return disc, nil
}

// packCandidateLess is the one ordering used everywhere. Every tie-break is
// explicit: focus order, then relevance class, then match count, then path.
func packCandidateLess(a, b *packCandidate) bool {
	af, bf := a.FocusIndex >= 0, b.FocusIndex >= 0
	if af != bf {
		return af
	}
	if af && bf && a.FocusIndex != b.FocusIndex {
		return a.FocusIndex < b.FocusIndex
	}
	if a.Class != b.Class {
		return a.Class > b.Class
	}
	if a.Matches != b.Matches {
		return a.Matches > b.Matches
	}
	return a.Path < b.Path
}

// packScore is the relevance number published per file. Focus paths always
// outrank query matches, as documented.
func packScore(c *packCandidate) int {
	if c.FocusIndex >= 0 {
		return 10000 + (100 - c.FocusIndex)
	}
	return c.Class*10 + c.Matches
}

// packBuildCandidate fills the candidate with its snapshot, hash, symbols and
// fragments. The hash comes from the very same bytes the snippets are cut
// from, so hash and content always describe the same revision.
func packBuildCandidate(ctx context.Context, engine *core.UltraFastEngine, c *packCandidate, ident string) {
	snap, err := engine.ReadFileSnapshot(ctx, c.Path)
	if err != nil {
		c.Unreadable = true
		return
	}
	c.Hash = snap.Hash
	lines := packSplitLines(string(snap.Bytes))
	c.LineCount = len(lines)

	symbols, stale := packSymbols(engine, c.Path, snap.Hash)
	c.Stale = stale

	var frags []packFragment

	// Definitions first — only when the query is a plain identifier.
	if ident != "" {
		for _, s := range symbols {
			prio := packNamePriority(s.Symbol, ident)
			if prio < 0 {
				continue
			}
			frags = append(frags, packFragmentFromSymbol(c, s, prio, lines))
		}
	}

	// Then the symbol that encloses each textual match.
	for _, ln := range c.MatchLines {
		if s, ok := packSymbolAt(symbols, ln); ok {
			frags = append(frags, packFragmentFromSymbol(c, s, packPrioSymbol, lines))
			continue
		}
		frags = append(frags, packWindowFragment(c, ln, lines))
	}

	// A focus-only file with no match still deserves an anchor.
	if len(frags) == 0 {
		if s, ok := packFirstSymbol(symbols); ok {
			frags = append(frags, packFragmentFromSymbol(c, s, packPrioAnchor, lines))
		}
	}

	c.Fragments, c.Merged = packDedupeFragments(frags, lines)
	c.Class = packClass(c)
}

// packSymbols returns the analyzer findings for one file. When the analyzer
// read a different revision than the snapshot the pack holds, the ranges are
// reported as stale and the caller says so.
func packSymbols(engine *core.UltraFastEngine, path, hash string) ([]core.AnalyzeFinding, bool) {
	res := core.AnalyzeSymbols(engine, path, "", packMaxSymbols)
	if res.Status == "error" {
		return nil, false
	}
	if hash == "" || res.ContentHash == "" || res.ContentHash == hash {
		return res.Findings, false
	}
	// The file changed between the snapshot and the analysis. Retry once;
	// if it still disagrees, the ranges are approximate.
	retry := core.AnalyzeSymbols(engine, path, "", packMaxSymbols)
	if retry.ContentHash == hash {
		return retry.Findings, false
	}
	for i := range retry.Findings {
		if retry.Findings[i].RangeStatus == packRangeExact {
			retry.Findings[i].RangeStatus = packRangeApproximate
		}
	}
	return retry.Findings, true
}

// packNamePriority ranks a symbol name against the query identifier.
// It returns -1 when the name does not match at all.
func packNamePriority(name, ident string) int {
	if name == "" || ident == "" {
		return -1
	}
	if name == ident {
		return packPrioDefinitionExact
	}
	if strings.EqualFold(name, ident) {
		return packPrioDefinitionExact
	}
	if len(ident) >= packMinIdentLen && strings.Contains(strings.ToLower(name), strings.ToLower(ident)) {
		return packPrioDefinitionLike
	}
	return -1
}

// packSymbolAt finds the most specific symbol whose range covers line.
// A symbol with no reliable end still anchors the line.
func packSymbolAt(symbols []core.AnalyzeFinding, line int) (core.AnalyzeFinding, bool) {
	var best core.AnalyzeFinding
	found := false
	bestSpan := 0
	for _, s := range symbols {
		if s.StartLine <= 0 {
			continue
		}
		end := s.EndLine
		if end <= 0 {
			end = s.StartLine
		}
		if line < s.StartLine || line > end {
			continue
		}
		span := end - s.StartLine
		if !found || span < bestSpan {
			best, found = s, true
			bestSpan = span
		}
	}
	return best, found
}

// packFirstSymbol returns the earliest symbol of a file.
func packFirstSymbol(symbols []core.AnalyzeFinding) (core.AnalyzeFinding, bool) {
	var best core.AnalyzeFinding
	found := false
	for _, s := range symbols {
		if s.StartLine <= 0 {
			continue
		}
		if !found || s.StartLine < best.StartLine {
			best, found = s, true
		}
	}
	return best, found
}

// packFragmentFromSymbol turns an analyzer finding into a fragment. The range
// is reported only when the analyzer actually resolved it.
func packFragmentFromSymbol(c *packCandidate, s core.AnalyzeFinding, prio int, lines []string) packFragment {
	f := packFragment{
		Path:      c.Path,
		StartLine: s.StartLine,
		Symbol:    s.Symbol,
		Kind:      s.Kind,
		Signature: strings.TrimSpace(s.Signature),
		Hash:      c.Hash,
		Priority:  prio,
	}
	switch s.Parser {
	case "go-ast":
		f.RangeSource = packSourceGoAST
	case "regex-brace":
		f.RangeSource = packSourceRegexBrace
	case "regex":
		f.RangeSource = packSourceRegex
	default:
		f.RangeSource = packSourceNone
	}
	if s.RangeStatus == packRangeExact && s.StartLine > 0 && s.EndLine >= s.StartLine {
		f.EndLine = s.EndLine
		f.RangeStatus = packRangeExact
		f.Text = packSnippet(lines, s.StartLine, s.EndLine)
	} else {
		// No reliable extent: report the anchor line only.
		f.EndLine = 0
		f.RangeStatus = packRangeUnavailable
		f.Text = packSnippet(lines, s.StartLine, s.StartLine)
	}
	if f.Signature == "" {
		f.Signature = packLineText(lines, s.StartLine)
	}
	return f
}

// packWindowFragment is the textual fallback used when no symbol encloses the
// match. The range is a window, not a definition, and says so.
func packWindowFragment(c *packCandidate, line int, lines []string) packFragment {
	start := line - packWindowBefore
	if start < 1 {
		start = 1
	}
	end := line + packWindowAfter
	if end > len(lines) {
		end = len(lines)
	}
	if end < start {
		end = start
	}
	return packFragment{
		Path:        c.Path,
		StartLine:   start,
		EndLine:     end,
		RangeStatus: packRangeApproximate,
		RangeSource: packSourceWindow,
		Hash:        c.Hash,
		Priority:    packPrioMention,
		Text:        packSnippet(lines, start, end),
	}
}

// packDedupeFragments removes repeated ranges and merges overlapping ones.
// Merging preserves the union of the content, so a fragment is never dropped
// just because it shares lines with another one.
func packDedupeFragments(frags []packFragment, lines []string) ([]packFragment, int) {
	if len(frags) == 0 {
		return nil, 0
	}
	sort.SliceStable(frags, func(i, j int) bool {
		if frags[i].Priority != frags[j].Priority {
			return frags[i].Priority < frags[j].Priority
		}
		if frags[i].StartLine != frags[j].StartLine {
			return frags[i].StartLine < frags[j].StartLine
		}
		return frags[i].EndLine < frags[j].EndLine
	})

	var accepted []packFragment
	merged := 0
	for _, f := range frags {
		if len(accepted) >= packMaxFragments {
			break
		}
		hit := -1
		contained := false
		for i := range accepted {
			a := accepted[i]
			if f.StartLine > a.EndLine || f.EndLine < a.StartLine {
				continue
			}
			hit = i
			// The new range sits inside the accepted one: it carries no
			// line the accepted fragment does not already have, so it is
			// a duplicate and not an overlap to merge.
			if f.StartLine >= a.StartLine && f.EndLine <= a.EndLine {
				contained = true
			}
			break
		}
		if hit < 0 {
			accepted = append(accepted, f)
			continue
		}
		if contained {
			// Duplicate of a range already kept: the higher-priority
			// fragment wins and its exact range status survives. It is
			// still a reduction, so it is counted.
			merged++
			continue
		}
		a := accepted[hit]
		if a.RangeStatus == packRangeExact && f.RangeStatus != packRangeExact {
			// The kept fragment has an analyzer-resolved range. A looser
			// fragment that merely overlaps it is folded in, but the exact
			// range survives: widening it would throw away the
			// definition's own bounds, and those bounds are what
			// replace_range and analyze_code agree on.
			a.Merged++
			accepted[hit] = a
			merged++
			continue
		}
		if f.RangeStatus == packRangeExact && a.RangeStatus != packRangeExact {
			// The incoming fragment is the precise one. It replaces the
			// looser fragment; the looser one is folded into it.
			f.Merged += a.Merged
			accepted[hit] = f
			merged++
			continue
		}
		start, end := a.StartLine, a.EndLine
		if f.StartLine < start {
			start = f.StartLine
		}
		if f.EndLine > end {
			end = f.EndLine
		}
		if end-start > packMaxMergedSpan {
			// Too wide to merge without losing the point of a bounded
			// fragment. Keep the higher-priority one.
			continue
		}
		a.StartLine = start
		a.EndLine = end
		a.Merged++
		a.RangeStatus = packRangeApproximate
		a.RangeSource = packSourceMerged
		a.Text = packSnippet(lines, start, end)
		accepted[hit] = a
		merged++
	}
	return accepted, merged
}

// packClass maps the best fragment priority to a relevance class.
func packClass(c *packCandidate) int {
	best := packPrioAnchor
	for _, f := range c.Fragments {
		if f.Priority < best {
			best = f.Priority
		}
	}
	switch best {
	case packPrioDefinitionExact:
		return 500
	case packPrioDefinitionLike:
		return 450
	case packPrioSymbol:
		return 300
	case packPrioMention:
		return 100
	default:
		return 50
	}
}

// packSplitLines splits file content into lines, tolerating CRLF.
func packSplitLines(content string) []string {
	return strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
}

// packLineText returns one trimmed line, 1-based.
func packLineText(lines []string, line int) string {
	if line < 1 || line > len(lines) {
		return ""
	}
	return strings.TrimSpace(lines[line-1])
}

// packSnippet cuts a bounded snippet out of the snapshot. The cut is made on
// rune boundaries and says what it left out.
func packSnippet(lines []string, start, end int) string {
	if start < 1 {
		start = 1
	}
	if end > len(lines) {
		end = len(lines)
	}
	if end < start {
		return ""
	}
	chunk := lines[start-1 : end]
	droppedLines := 0
	if len(chunk) > packMaxSnippetLines {
		droppedLines = len(chunk) - packMaxSnippetLines
		chunk = chunk[:packMaxSnippetLines]
	}
	text := strings.Join(chunk, "\n")
	droppedChars := false
	if packRuneLen(text) > packMaxSnippetChars {
		text = packCutRunes(text, packMaxSnippetChars)
		droppedChars = true
	}
	if droppedLines > 0 || droppedChars {
		marks := make([]string, 0, 2)
		if droppedLines > 0 {
			marks = append(marks, fmt.Sprintf("+%d lines", droppedLines))
		}
		if droppedChars {
			marks = append(marks, "cut")
		}
		text += " [… " + strings.Join(marks, ", ") + " — read_file path:" +
			fmt.Sprintf("%d-%d", start, end) + "]"
	}
	return text
}

// packRuneLen counts Unicode code points, not bytes.
func packRuneLen(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}

// packCutRunes cuts a string on a rune boundary so a UTF-8 sequence is never
// split.
func packCutRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	n := 0
	for i := range s {
		if n == max {
			return s[:i]
		}
		n++
	}
	return s
}
