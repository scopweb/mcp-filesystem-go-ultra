package core

// changedLineCount counts changed line slots: each contiguous edit run counts
// max(deleted, inserted). Multiple patterns changing one line count once.
// -1 means an exact diff exceeds the bounded work budget.
func changedLineCount(before, after string) int {
	if before == after {
		return 0
	}
	a, b := splitLines(before), splitLines(after)
	for len(a) > 0 && len(b) > 0 && a[0] == b[0] {
		a, b = a[1:], b[1:]
	}
	for len(a) > 0 && len(b) > 0 && a[len(a)-1] == b[len(b)-1] {
		a, b = a[:len(a)-1], b[:len(b)-1]
	}
	if len(a) == 0 || len(b) == 0 {
		return max(len(a), len(b))
	}
	if len(a) > maxDiffMatrixCells/len(b) {
		return -1
	}
	total, removed, added := 0, 0, 0
	for _, e := range diff(a, b) {
		switch e.kind {
		case editDelete:
			removed++
		case editInsert:
			added++
		case editEqual:
			total += max(removed, added)
			removed, added = 0, 0
		}
	}
	return total + max(removed, added)
}
