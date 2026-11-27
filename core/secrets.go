package core

import (
	"path/filepath"
	"strings"
	"unicode"
)

// The secret denylist is the single source of truth for every path that is
// allowed to expose credential material. Two views are derived from the same
// table so they can never drift apart:
//
//   - IsSecretPath  — the path predicate used by the read/write guard
//     (engine.IsPathAllowed) and by the native walk (ignore.skipWalkDir).
//   - SecretExcludeGlobs — the ripgrep exclusion globs used by the search
//     backend so ripgrep never even opens a secret file.
//
// A file that read_file refuses (SECRET_DENIED) must never be exposed
// through a search result either.
var (
	// secretExactNames matches the basename exactly (case-insensitive).
	secretExactNames = []string{
		".env",
		"credentials.json",
		".npmrc",
		".pypirc",
	}
	// secretNamePrefixes matches basenames starting with the prefix
	// (case-insensitive), e.g. ".env.local" or "id_rsa.pub".
	secretNamePrefixes = []string{
		".env.",
		"id_rsa",
		"id_ed25519",
		"id_ecdsa",
	}
	// secretExtensions matches by file extension (case-insensitive).
	secretExtensions = []string{
		".pem",
		".key",
		".p12",
		".pfx",
	}
)

// IsSecretPath reports whether path points at a file that holds credentials.
// It is a pure path predicate: runtime configuration (e.g. --allow-secrets)
// is applied by the caller, not here.
func IsSecretPath(path string) bool {
	base := strings.ToLower(anyBase(path))
	for _, name := range secretExactNames {
		if base == name {
			return true
		}
	}
	for _, prefix := range secretNamePrefixes {
		if strings.HasPrefix(base, prefix) {
			return true
		}
	}
	ext := filepath.Ext(base)
	for _, candidate := range secretExtensions {
		if ext == candidate {
			return true
		}
	}
	return false
}

// SecretExcludeGlobs returns ripgrep-style exclusion globs that mirror
// IsSecretPath. They are case-insensitive (character classes) because the
// denylist is case-insensitive too, and they match at any depth.
//
// Caveat: ripgrep does not apply exclusion globs to a file that is given as
// the explicit search target, so the search backend must ALSO re-check every
// parsed match against IsSecretPath.
func SecretExcludeGlobs() []string {
	globs := make([]string, 0, len(secretExactNames)+len(secretNamePrefixes)+len(secretExtensions))
	for _, name := range secretExactNames {
		globs = append(globs, "!**/"+globCaseInsensitive(name))
	}
	for _, prefix := range secretNamePrefixes {
		globs = append(globs, "!**/"+globCaseInsensitive(prefix)+"*")
	}
	for _, ext := range secretExtensions {
		globs = append(globs, "!**/*"+globCaseInsensitive(ext))
	}
	return globs
}

// globCaseInsensitive rewrites a glob so it also matches the upper/lower case
// variants of every ASCII letter it contains. Non-letters (dots, underscores,
// wildcards) are preserved verbatim.
func globCaseInsensitive(glob string) string {
	var b strings.Builder
	for _, r := range glob {
		lower, upper := unicode.ToLower(r), unicode.ToUpper(r)
		if lower != upper {
			b.WriteByte('[')
			b.WriteRune(lower)
			b.WriteRune(upper)
			b.WriteByte(']')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
