package mcpserver

// contextPackHint points read_file at context_pack, but only in the profiles
// where context_pack exists.
//
// The measurement of 2026-11-24 (35.647 calls, 184 sessions) showed that the
// 48,9 % of the sessions that run the ultra profile carry the 76,9 % of the
// tokens, and that read_file is 56,5 % of the whole spend. context_pack
// returns the same material for a task in a fraction of the bytes (3.495 B
// against 31.553 B for a 56 KB Go file), and it had been called twice in a
// month.
//
// The hint is per profile on purpose: in `strict` and in `agent` context_pack
// does not exist, and a description that names a tool the client cannot call
// is a lie that costs a call to discover.
//
// It is a hypothesis with a measurement attached, not a certainty: the metric
// to watch is the number of context_pack calls and the tokens of read_file in
// the ultra sessions.
func contextPackHint(profile toolProfile) string {
	if !profile.allows("context_pack") {
		return ""
	}
	return "context_pack returns the ranked files, the exact line ranges and the bounded snippets of a task in one call (cheaper than reading file by file). "
}
