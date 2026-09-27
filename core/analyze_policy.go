package core

import "sync"

// analyzeContentBlocked is set only for the duration of an analyze call when
// a file security policy is active. It keeps directory walks from reading
// protected or hidden files. Nil means no extra filter.
var (
	analyzeBlockMu       sync.Mutex
	analyzeContentBlocked func(string) bool
)

// WithAnalyzeBlock runs fn while content reads inside analyze helpers skip
// paths for which block returns true. Calls are serialized.
func WithAnalyzeBlock(block func(string) bool, fn func()) {
	analyzeBlockMu.Lock()
	defer analyzeBlockMu.Unlock()
	prev := analyzeContentBlocked
	analyzeContentBlocked = block
	defer func() { analyzeContentBlocked = prev }()
	fn()
}

func analyzeBlocked(path string) bool {
	if analyzeContentBlocked == nil || path == "" {
		return false
	}
	return analyzeContentBlocked(path)
}
