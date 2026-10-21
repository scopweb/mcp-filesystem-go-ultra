package mcpserver

// context_pack — budget layer.
//
// THE BUDGET CONTRACT
//
//	budget_chars bounds the CONTEXT the pack carries, measured in Unicode
//	code points (never bytes, so a cut can never split a UTF-8 sequence).
//
//	What it limits: the text representation — the header, every per-file
//	block, the coverage notes and the truncation marker. The text never
//	exceeds budget_chars.
//
//	What it does not limit: the fixed envelope of the payload (the action,
//	path and query keys) and the git review, which is charged separately.
//	Both are bounded and reported.
//
//	The structured payload is a PROJECTION of the same selection: it
//	carries exactly the files and fragments the text carries, so it can
//	never keep a body or a fragment the budget excluded. Its own size is
//	reported as structured_chars, and the characters of context it carries
//	as content_chars.
//
//	Progressive reduction. A fragment is admitted at the richest level that
//	fits and degraded otherwise:
//
//	  1. snippet   — the bounded text of the range
//	  2. signature — the signature plus the locator
//	  3. locator   — path, line range and symbol identity only
//	  4. omitted   — dropped and counted in the coverage metadata
//
//	Metadata is reserved before any fragment is admitted, so a pack never
//	spends its whole budget on bodies and then loses the lines that say
//	what was left out.
//
//	The selection does not depend on --response-mode: the canonical
//	rendering is charged either way, so the same query yields the same
//	pack in compat and structured mode. In structured mode the text
//	fallback is a summary, so what the caller receives is smaller, never
//	larger.

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	// packEnvelopeDrift is the slack held back on top of the measured
	// text envelope, so the coverage notes that are written after the
	// measurement cannot push the pack over the budget.
	packEnvelopeDrift = 160
	// packGitBudgetShare is the share of the budget the git review may
	// take. A review that would eat the pack is dropped and reported.
	packGitBudgetShare = 25
	// packMaxNotes bounds the coverage notes so the footer stays bounded.
	packMaxNotes = 10
)

// packCoverage is the provenance of the pack: what was examined, what was
// cut and why. It carries counts only — never the path of a file the caller
// was not allowed to see.
type packCoverage struct {
	Scope                 string
	Query                 string
	QueryMatched          bool
	CandidateFiles        int
	SelectedFiles         int
	SearchTruncated       bool
	SearchMatches         int
	FilesOmittedMaxFiles  int
	FilesOmittedBudget    int
	FilesDenied           int
	FilesUnreadable       int
	CandidatesNotExamined int
	FragmentsReduced      int
	FragmentsOmitted      int
	RangesMerged          int
	RangesStale           int
	SourceCandidates      int
}

// packEntry is one file in the pack, at the detail level the budget allowed.
type packEntry struct {
	Path      string
	Score     int
	Hash      string
	Level     string
	Text      string
	Struct    map[string]any
	Fragments []packFragment
}

// packRender turns a selection into the text fallback and the structured
// payload, both under the same budget.
//
// It returns the text, the payload and the final coverage.
func packRender(cands []*packCandidate, cov packCoverage, gitText string, gitPayload map[string]any, gitNote string, budget int) (string, map[string]any, packCoverage) {
	gitCost := packRuneLen(gitText) + packJSONLen(gitPayload)
	if gitCost > budget*packGitBudgetShare/100 {
		// A review this large would crowd out the context it describes.
		gitText = ""
		gitPayload = nil
		gitNote = "budget"
		gitCost = 0
	}

	// The text envelope — header, coverage notes, truncation marker — is
	// provenance, not context, but it is part of what the caller reads, so
	// it is measured first and taken out of the budget. What is left is
	// what the fragments may use.
	envelopeText, _ := packAssemble(nil, cov, gitText, gitPayload, gitNote, budget, true)
	overhead := packRuneLen(envelopeText)
	drift := packEnvelopeDrift
	if d := budget / 8; d < drift {
		drift = d
	}
	if drift < 32 {
		drift = 32
	}
	contentBudget := budget - overhead - drift
	if contentBudget < 0 {
		contentBudget = 0
	}

	used := 0
	entries := make([]*packEntry, 0, len(cands))
	omittedByBudget := 0
	fragmentsOmitted := 0
	reduced := 0

	for _, c := range cands {
		if len(c.Fragments) == 0 {
			// Nothing to anchor the file on beyond its path.
			continue
		}
		e := packEntryAt(c, packDetailLocator)
		cost := packEntryTextCost(e)
		if used+cost > contentBudget {
			omittedByBudget++
			fragmentsOmitted += len(c.Fragments)
			continue
		}
		used += cost
		entries = append(entries, e)
	}

	// Upgrade in relevance order: the most relevant file gets the richest
	// level first. A fragment without a signature goes straight from the
	// locator to the snippet — there is no signature level to gain.
	for _, e := range entries {
		if e.Level == packDetailLocator {
			if packHasSignature(e) {
				if up, ok := packUpgrade(e, packDetailSignature); ok {
					delta := packEntryTextCost(up) - packEntryTextCost(e)
					if used+delta <= contentBudget {
						used += delta
						*e = *up
					}
				}
			}
		}
		if e.Level == packDetailLocator || e.Level == packDetailSignature {
			if up, ok := packUpgrade(e, packDetailSnippet); ok {
				delta := packEntryTextCost(up) - packEntryTextCost(e)
				if used+delta <= contentBudget {
					used += delta
					*e = *up
				}
			}
		}
	}

	// A fragment that ended below the snippet level was reduced by the
	// budget. That is a reduction, not a loss: the body is gone, the
	// locator and the signature are still there.
	for _, e := range entries {
		if e.Level != packDetailSnippet {
			reduced++
		}
	}

	cov.FilesOmittedBudget = omittedByBudget
	cov.FragmentsReduced = reduced
	cov.FragmentsOmitted = fragmentsOmitted
	cov.SelectedFiles = len(entries)

	text, payload := packAssemble(entries, cov, gitText, gitPayload, gitNote, budget, false)

	// Safety net: the exact artifact size decides. If the reserve was too
	// small, degrade the least relevant entries until the text fits.
	for packRuneLen(text) > budget && len(entries) > 0 {
		last := entries[len(entries)-1]
		down, ok := packDowngrade(last)
		if !ok {
			entries = entries[:len(entries)-1]
			cov.SelectedFiles = len(entries)
			cov.FilesOmittedBudget++
			cov.FragmentsOmitted += len(last.Fragments)
			text, payload = packAssemble(entries, cov, gitText, gitPayload, gitNote, budget, false)
			continue
		}
		*last = *down
		cov.FragmentsReduced++
		text, payload = packAssemble(entries, cov, gitText, gitPayload, gitNote, budget, false)
	}

	// Last resort: the envelope alone is larger than the budget (a very
	// long path, for instance). The text is cut on a rune boundary so it
	// still fits and never splits a UTF-8 sequence; the provenance stays
	// in the structured payload, which is not bounded by the budget.
	if packRuneLen(text) > budget {
		text = packCutRunes(text, budget)
		payload["truncated"] = true
	}

	return text, payload, cov
}

// packEntryAt builds the entry for a candidate at a given detail level.
func packEntryAt(c *packCandidate, level string) *packEntry {
	e := &packEntry{
		Path:      c.Path,
		Score:     packScore(c),
		Hash:      c.Hash,
		Level:     level,
		Fragments: c.Fragments,
	}
	f := c.Fragments[0]

	st := map[string]any{
		"path":         c.Path,
		"score":        e.Score,
		"content_hash": c.Hash,
		"detail":       level,
	}
	if f.StartLine > 0 {
		st["line"] = f.StartLine
		st["start_line"] = f.StartLine
	}
	if f.EndLine > 0 {
		st["end_line"] = f.EndLine
	}
	st["range_status"] = f.RangeStatus
	st["range_source"] = f.RangeSource
	if f.Symbol != "" {
		st["symbol"] = f.Symbol
	}
	if f.Kind != "" {
		st["kind"] = f.Kind
	}
	if level == packDetailSignature || level == packDetailSnippet {
		if f.Signature != "" {
			st["signature"] = f.Signature
		}
		// Secondary fragments keep their locator so the caller can keep
		// going with read_file, but they never carry a body.
		if len(c.Fragments) > 1 {
			st["fragments"] = packFragmentRefs(c.Fragments[1:])
		}
	}
	if level == packDetailSnippet && f.Text != "" {
		st["snippet"] = f.Text
	}
	e.Struct = st
	e.Text = packEntryText(e, f, level)
	return e
}

// packFragmentRefs renders the secondary fragments as locators only.
func packFragmentRefs(frags []packFragment) []map[string]any {
	out := make([]map[string]any, 0, len(frags))
	for _, f := range frags {
		item := map[string]any{
			"range_status": f.RangeStatus,
			"range_source": f.RangeSource,
		}
		if f.StartLine > 0 {
			item["start_line"] = f.StartLine
			item["line"] = f.StartLine
		}
		if f.EndLine > 0 {
			item["end_line"] = f.EndLine
		}
		if f.Symbol != "" {
			item["symbol"] = f.Symbol
		}
		if f.Kind != "" {
			item["kind"] = f.Kind
		}
		out = append(out, item)
	}
	return out
}

// packEntryText renders the text block of one file.
func packEntryText(e *packEntry, f packFragment, level string) string {
	var b strings.Builder
	b.WriteString(e.Path)
	if f.StartLine > 0 {
		fmt.Fprintf(&b, ":%d", f.StartLine)
		if f.EndLine > f.StartLine {
			fmt.Fprintf(&b, "-%d", f.EndLine)
		}
	}
	fmt.Fprintf(&b, " [%s %s]", f.RangeStatus, f.RangeSource)
	if f.Kind != "" || f.Symbol != "" {
		fmt.Fprintf(&b, " %s %s", f.Kind, f.Symbol)
	}
	if level == packDetailSignature || level == packDetailSnippet {
		if f.Signature != "" {
			fmt.Fprintf(&b, " — %s", f.Signature)
		}
	}
	b.WriteByte('\n')
	if level == packDetailSnippet && f.Text != "" {
		for _, line := range strings.Split(f.Text, "\n") {
			b.WriteString("  > ")
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// packUpgrade returns the entry at a richer level. A fragment without a
// signature has nothing to gain from the signature level, so the upgrade is
// refused and the entry keeps its honest level.
func packUpgrade(e *packEntry, level string) (*packEntry, bool) {
	if e.Level == level {
		return nil, false
	}
	if level == packDetailSignature && !packHasSignature(e) {
		return nil, false
	}
	return packRebuild(e, level), true
}

// packHasSignature reports whether the primary fragment carries a signature.
func packHasSignature(e *packEntry) bool {
	if len(e.Fragments) == 0 {
		return false
	}
	return e.Fragments[0].Signature != ""
}

// packDowngrade returns the entry at a poorer level. Unlike an upgrade, a
// downgrade is always possible: it only removes content.
func packDowngrade(e *packEntry) (*packEntry, bool) {
	switch e.Level {
	case packDetailSnippet:
		if packHasSignature(e) {
			return packRebuild(e, packDetailSignature), true
		}
		return packRebuild(e, packDetailLocator), true
	case packDetailSignature:
		return packRebuild(e, packDetailLocator), true
	default:
		return nil, false
	}
}

// packRebuild renders the entry at a given level.
func packRebuild(e *packEntry, level string) *packEntry {
	c := &packCandidate{
		Path:       e.Path,
		Hash:       e.Hash,
		Fragments:  e.Fragments,
		FocusIndex: -1,
	}
	up := packEntryAt(c, level)
	up.Score = e.Score
	up.Struct["score"] = e.Score
	return up
}

// packEntryTextCost is the characters the entry adds to the text fallback.
func packEntryTextCost(e *packEntry) int {
	return packRuneLen(e.Text)
}

// packJSONLen measures the JSON size of a payload fragment.
func packJSONLen(v any) int {
	if v == nil {
		return 0
	}
	b, err := json.Marshal(v)
	if err != nil {
		return 0
	}
	return len(b)
}

// packSelfCounters are the payload keys that cannot contain their own size.
var packSelfCounters = []string{"text_chars", "structured_chars"}

// packStructuredLen measures the structured payload without the
// self-referential counters.
func packStructuredLen(payload map[string]any) int {
	if payload == nil {
		return 0
	}
	clone := make(map[string]any, len(payload))
	for k, v := range payload {
		clone[k] = v
	}
	for _, k := range packSelfCounters {
		delete(clone, k)
	}
	return packJSONLen(clone)
}

// packContentChars is the volume of context the pack carries: the snippet
// bodies it shows. It is what the structured payload must not exceed.
func packContentChars(entries []*packEntry) int {
	total := 0
	for _, e := range entries {
		if snippet, ok := e.Struct["snippet"].(string); ok {
			total += packRuneLen(snippet)
		}
	}
	return total
}

// packAssemble renders the final text and payload. The probe flag suppresses
// the "nothing selected" line, so the envelope can be measured without
// paying for a line that only appears when there is nothing to show.
func packAssemble(entries []*packEntry, cov packCoverage, gitText string, gitPayload map[string]any, gitNote string, budget int, probe bool) (string, map[string]any) {
	notes := packNotes(cov)
	truncated := cov.FilesOmittedBudget > 0 || cov.FragmentsReduced > 0 || cov.FragmentsOmitted > 0

	files := make([]map[string]any, 0, len(entries))
	var b strings.Builder
	fmt.Fprintf(&b, "context_pack %s", cov.Scope)
	if cov.Query != "" {
		fmt.Fprintf(&b, " query=%q", cov.Query)
	}
	fmt.Fprintf(&b, " files:%d/%d budget_chars:%d\n", len(entries), cov.CandidateFiles, budget)
	if gitNote != "" {
		fmt.Fprintf(&b, "git: %s\n", gitNote)
	}
	if gitText != "" {
		b.WriteString(gitText)
		if !strings.HasSuffix(gitText, "\n") {
			b.WriteByte('\n')
		}
	}
	for _, e := range entries {
		b.WriteString(e.Text)
		files = append(files, e.Struct)
	}
	if len(entries) == 0 && !probe {
		b.WriteString("no context selected; pass query or focus_paths\n")
	}
	if truncated {
		b.WriteString("[TRUNCADO: budget_chars]")
	}
	if len(notes) > 0 {
		if truncated {
			b.WriteString(" ")
		}
		b.WriteString(strings.Join(notes, "; "))
	}
	b.WriteByte('\n')

	payload := map[string]any{
		"action":        "context_pack",
		"path":          cov.Scope,
		"files":         files,
		"coverage":      packCoverageMap(cov, notes),
		"omitted":       packOmittedTotal(cov),
		"truncated":     truncated,
		"budget_chars":  budget,
		"content_chars": packContentChars(entries),
	}
	if cov.Query != "" {
		payload["query"] = cov.Query
	}
	if gitPayload != nil {
		payload["git"] = gitPayload
	}
	if gitNote != "" {
		payload["git_omitted"] = gitNote
	}
	textLen := packRuneLen(b.String())
	structLen := packStructuredLen(payload)
	payload["text_chars"] = textLen
	payload["structured_chars"] = structLen
	return b.String(), payload
}

// packCoverageMap renders the coverage metadata. It carries counts only —
// never the path of a file the caller was not allowed to see. Counters that
// are zero are left out, so the envelope stays small enough to leave room
// for the context it describes.
func packCoverageMap(cov packCoverage, notes []string) map[string]any {
	m := map[string]any{
		"scope":         cov.Scope,
		"query_matched": cov.QueryMatched,
	}
	if cov.Query != "" {
		m["query"] = cov.Query
	}
	put := func(key string, value int) {
		if value != 0 {
			m[key] = value
		}
	}
	put("candidate_files", cov.CandidateFiles)
	put("selected_files", cov.SelectedFiles)
	put("search_match_count", cov.SearchMatches)
	put("files_omitted_max_files", cov.FilesOmittedMaxFiles)
	put("files_omitted_budget", cov.FilesOmittedBudget)
	put("files_denied", cov.FilesDenied)
	put("files_unreadable", cov.FilesUnreadable)
	put("candidates_not_examined", cov.CandidatesNotExamined)
	put("source_candidates", cov.SourceCandidates)
	put("fragments_reduced", cov.FragmentsReduced)
	put("fragments_omitted", cov.FragmentsOmitted)
	put("ranges_merged", cov.RangesMerged)
	put("ranges_stale", cov.RangesStale)
	if cov.SearchTruncated {
		m["search_truncated"] = true
	}
	if len(notes) > 0 {
		m["notes"] = notes
	}
	return m
}

// packOmittedTotal is the number of files the pack did not carry.
func packOmittedTotal(cov packCoverage) int {
	return cov.FilesDenied + cov.FilesUnreadable + cov.FilesOmittedMaxFiles + cov.FilesOmittedBudget
}

// packNotes renders the human-readable coverage notes. Every reason the
// caller must know about is stated here, so a partial pack never reads as a
// complete one.
func packNotes(cov packCoverage) []string {
	var notes []string
	add := func(s string) {
		if len(notes) >= packMaxNotes {
			return
		}
		notes = append(notes, s)
	}
	if cov.Query != "" && !cov.QueryMatched {
		add("no matches for query in the examined scope")
	}
	if cov.SearchTruncated {
		add(fmt.Sprintf("candidate search truncated (%d matches found)", cov.SearchMatches))
	}
	if cov.CandidatesNotExamined > 0 {
		add(fmt.Sprintf("%d candidate files not examined", cov.CandidatesNotExamined))
	}
	if cov.SourceCandidates > 0 {
		add(fmt.Sprintf("%d definition candidates found by the source-file scan", cov.SourceCandidates))
	}
	if cov.FilesOmittedMaxFiles > 0 {
		add(fmt.Sprintf("%d files omitted by max_files", cov.FilesOmittedMaxFiles))
	}
	if cov.FilesOmittedBudget > 0 {
		add(fmt.Sprintf("%d files omitted by budget_chars", cov.FilesOmittedBudget))
	}
	if cov.FragmentsReduced > 0 {
		add(fmt.Sprintf("%d fragments reduced to signature or locator", cov.FragmentsReduced))
	}
	if cov.FragmentsOmitted > 0 {
		add(fmt.Sprintf("%d fragments omitted by budget_chars", cov.FragmentsOmitted))
	}
	if cov.RangesMerged > 0 {
		add(fmt.Sprintf("%d duplicate or overlapping ranges merged", cov.RangesMerged))
	}
	if cov.RangesStale > 0 {
		add(fmt.Sprintf("%d symbol ranges read from another revision", cov.RangesStale))
	}
	if cov.FilesDenied > 0 {
		add(fmt.Sprintf("%d files excluded by access policy or secret protection", cov.FilesDenied))
	}
	if cov.FilesUnreadable > 0 {
		add(fmt.Sprintf("%d files could not be read", cov.FilesUnreadable))
	}
	return notes
}
