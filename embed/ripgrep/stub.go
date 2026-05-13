// Package ripgrep provides embedded ripgrep binaries for high-performance search.
// Use build tag `embed_rg` to include binaries in the build.

//go:build !embed_rg
// +build !embed_rg

package ripgrep

// IsEmbedded returns false when build tag embed_rg is not set.
func IsEmbedded() bool {
	return false
}

// EmbeddedBin returns empty string when embed_rg build tag is not set.
func EmbeddedBin() string {
	return ""
}