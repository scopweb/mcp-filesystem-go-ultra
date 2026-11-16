package mcpserver

import "encoding/json"

// structuredJSONSize is the encoded size of structuredContent. The text block
// is capped separately; a client that also receives this object still spills
// once the JSON itself passes claudeSpillChars.
func structuredJSONSize(payload map[string]any) int {
	if payload == nil {
		return 0
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return claudeSpillChars + 1
	}
	return len(raw)
}

// fitSearchStructured drops matches from the end until the JSON of
// structuredContent is within limit. The text block is left alone. A cut
// keeps the continuation hint so the omitted hits can be fetched with offset.
func fitSearchStructured(sc map[string]any, limit int) map[string]any {
	if sc == nil || limit <= 0 || structuredJSONSize(sc) <= limit {
		return sc
	}
	matches, _ := sc["matches"].([]map[string]any)
	if len(matches) == 0 {
		return sc
	}
	scope, _ := sc["scope"].(map[string]any)
	base, page := 0, 0
	if scope != nil {
		base, _ = scope["offset"].(int)
		page, _ = scope["max_results"].(int)
	}
	best := 0
	for lo, hi := 0, len(matches); lo <= hi; {
		mid := (lo + hi) / 2
		if searchPayloadSize(sc, matches[:mid], base+mid, page) <= limit {
			best = mid
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}
	if best >= len(matches) {
		return sc
	}
	if best == 0 {
		// One hit is larger than the budget. Keep the path and line, drop
		// the line text, and tell the caller to read_file that path.
		hit := map[string]any{}
		for k, v := range matches[0] {
			if k == "text" || k == "context" || k == "context_line_numbers" {
				continue
			}
			hit[k] = v
		}
		hit["text_omitted"] = true
		matches = []map[string]any{hit}
		best = 0
	} else {
		matches = matches[:best]
	}
	sc["matches"] = matches
	sc["truncated"] = true
	sc["status"] = statusTruncated
	next := base + len(matches)
	if best == 0 {
		next = base + 1
	}
	sc["continuation"] = searchContinuationObject(next, page, "byte_budget")
	sc["message"] = searchSummaryMessage(scope, matchCountOf(sc), len(matches), true)
	if structuredJSONSize(sc) > limit && len(matches) > 0 {
		sc["matches"] = []map[string]any{}
		sc["continuation"] = searchContinuationObject(base, page, "byte_budget")
		sc["message"] = searchSummaryMessage(scope, matchCountOf(sc), 0, true)
	}
	return sc
}

func searchPayloadSize(sc map[string]any, matches []map[string]any, next, page int) int {
	trial := make(map[string]any, len(sc)+1)
	for k, v := range sc {
		trial[k] = v
	}
	trial["matches"] = matches
	trial["truncated"] = true
	trial["status"] = statusTruncated
	trial["continuation"] = searchContinuationObject(next, page, "byte_budget")
	return structuredJSONSize(trial)
}

func matchCountOf(sc map[string]any) int {
	n, _ := sc["match_count"].(int)
	return n
}

// fitReadStructured keeps a batch read's structuredContent within limit.
// compat was sending the combined text in content and again in files[].content.
// The text block already has that body. content becomes the one-line batch
// summary, never absent: the output schema requires it. Bodies stay in
// files[].content, cut from the end with a continuation hint when the JSON
// still does not fit.
// structured mode is unchanged in shape: the body remains in files[].content.
func fitReadStructured(sc map[string]any, limit int) map[string]any {
	if sc == nil || limit <= 0 {
		return sc
	}
	files, ok := sc["files"].([]map[string]any)
	if !ok || len(files) == 0 {
		return sc
	}
	if body, isStr := sc["content"].(string); isStr && len(body) > 1000 && filesHaveContent(files) {
		truncated, _ := sc["truncated"].(bool)
		sc["content"] = batchReadSummary(files, truncated)
	}
	if structuredJSONSize(sc) <= limit {
		return sc
	}
	// Shorten bodies before dropping them. A file that already has a read
	// continuation keeps it; replacing that hint breaks recovery.
	trimLargestFileContent(files, limit, sc)
	for i := len(files) - 1; i >= 0 && structuredJSONSize(sc) > limit; i-- {
		if _, has := files[i]["content"]; !has {
			continue
		}
		if files[i]["continuation"] != nil {
			continue
		}
		delete(files[i], "content")
		files[i]["truncated"] = true
		files[i]["continuation"] = batchResumeContinuation()
		markBatchCut(sc)
	}
	if structuredJSONSize(sc) > limit {
		for i := len(files) - 1; i >= 0 && structuredJSONSize(sc) > limit; i-- {
			if _, has := files[i]["content"]; !has {
				continue
			}
			delete(files[i], "content")
			files[i]["truncated"] = true
			if files[i]["continuation"] == nil {
				files[i]["continuation"] = batchResumeContinuation()
			}
			markBatchCut(sc)
		}
	}
	if sc["truncated"] == true {
		if cont, ok := sc["continuation"].(map[string]any); !ok || cont["file_indexes"] == nil {
			sc["continuation"] = map[string]any{
				"kind":         "batch",
				"file_indexes": continuationIndexes(files),
				"hint":         "structured payload cut to stay within 50000 bytes. Re-read the listed files; those bodies are not in this page.",
			}
		}
	}
	return sc
}

func markBatchCut(sc map[string]any) {
	sc["truncated"] = true
	if sc["status"] != statusPartial && sc["status"] != statusFailed {
		sc["status"] = statusTruncated
	}
}

// fitDiffStructured keeps a diff payload's structuredContent within limit.
// The diff body lives in "message", which is byte-identical to the text
// content block, so the cut is applied to the TEXT as well: the payload
// never disagrees with the text the client reads. The continuation keeps the
// line cursor, so the rest of the diff is still reachable with diff_files
// offset.
func fitDiffStructured(payload map[string]any, msg string, offset, byteOffset, limit int) (map[string]any, string) {
	if payload == nil || limit <= 0 {
		return payload, ""
	}
	text, _ := payload["message"].(string)
	if structuredJSONSize(payload) <= limit {
		return payload, text
	}
	budget := len(text)
	for i := 0; i < 12 && budget > 0; i++ {
		bounded, cont, err := boundDiffTextLimit(msg, offset, byteOffset, budget)
		if err != nil {
			return payload, text
		}
		candidate := make(map[string]any, len(payload))
		for k, v := range payload {
			candidate[k] = v
		}
		candidate["message"] = bounded
		if cont != nil {
			candidate["truncated"] = true
			candidate["continuation"] = cont
			candidate["status"] = statusTruncated
		} else {
			delete(candidate, "truncated")
			delete(candidate, "continuation")
			if candidate["status"] == statusTruncated {
				candidate["status"] = statusOK
			}
		}
		size := structuredJSONSize(candidate)
		if size <= limit {
			return candidate, bounded
		}
		if len(bounded) == 0 {
			break
		}
		// Every escape costs more than one byte in the JSON, so the text has
		// to shrink by at least the overflow, measured in JSON bytes.
		factor := float64(size) / float64(len(bounded))
		remove := float64(size-limit)/factor + 64
		next := int(float64(budget) - remove)
		if next < 1 {
			next = 1
		}
		if next >= budget {
			next = budget / 2
		}
		if next < 1 {
			break
		}
		budget = next
	}
	return payload, text
}

// cutListingToBudget is the last resort for a listing whose structured
// payload still does not fit: the entries are dropped (they are the part
// that repeats the text) and the text is cut until the JSON fits. The cursor
// points back at the start of this page, because the entries of this page
// are not in the response: the client re-reads the page with a smaller
// max_nodes instead of skipping over unseen entries.
func cutListingToBudget(sc map[string]any, text string, offset, limit int) (map[string]any, string) {
	if sc == nil || limit <= 0 {
		return sc, text
	}
	if structuredJSONSize(sc) <= limit {
		return sc, text
	}
	delete(sc, "entries")
	delete(sc, "entry_count")
	cut := text
	for i := 0; i < 24 && cut != "" && structuredJSONSize(sc) > limit; i++ {
		cut = cut[:utf8SafeEnd(cut, len(cut)*3/4)]
		sc["message"] = cut
	}
	if structuredJSONSize(sc) > limit {
		cut = "listing cut to stay within the response budget"
		sc["message"] = cut
	}
	sc["truncated"] = true
	sc["status"] = statusTruncated
	sc["continuation"] = map[string]any{
		"offset": offset,
		"hint":   "The listing page was cut to fit the response budget and its entries are not in this page. Re-read the same path with a smaller max_nodes.",
	}
	return sc, cut
}

func filesHaveContent(files []map[string]any) bool {
	for _, f := range files {
		if _, ok := f["content"]; ok {
			return true
		}
	}
	return false
}

func continuationIndexes(files []map[string]any) []int {
	var indexes []int
	for i, f := range files {
		if f["continuation"] != nil {
			indexes = append(indexes, i)
		}
	}
	return indexes
}

func trimLargestFileContent(files []map[string]any, limit int, sc map[string]any) {
	for structuredJSONSize(sc) > limit {
		idx, bestLen := -1, 0
		for i, f := range files {
			body, ok := f["content"].(string)
			if ok && len(body) > bestLen {
				bestLen = len(body)
				idx = i
			}
		}
		if idx < 0 || bestLen == 0 {
			return
		}
		f := files[idx]
		body := f["content"].(string)
		lo, hi, best := 0, len(body), 0
		for lo <= hi {
			mid := (lo + hi) / 2
			f["content"] = body[:utf8SafeEnd(body, mid)]
			if structuredJSONSize(sc) <= limit {
				best = mid
				lo = mid + 1
			} else {
				hi = mid - 1
			}
		}
		if best == 0 || best == len(body) {
			// Nothing smaller fits, or the body was not the overflow.
			// Drop it only when shrinking it cannot help.
			if best == len(body) {
				return
			}
			delete(f, "content")
		} else {
			f["content"] = body[:utf8SafeEnd(body, best)]
		}
		f["truncated"] = true
		if f["continuation"] == nil {
			f["continuation"] = batchResumeContinuation()
		}
		markBatchCut(sc)
		if best > 0 && best < len(body) {
			return
		}
	}
}
