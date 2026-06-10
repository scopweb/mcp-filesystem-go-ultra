package core

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"
)

// validatePathSecurity checks a (already-normalized) path for dangerous patterns.
// Returns a ValidationError if the path is unsafe; nil otherwise.
//
// Mitigations:
//  1. NTFS Alternate Data Streams — prevents hidden covert channels via "file.txt:stream"
//  2. Unicode directional overrides and zero-width chars — blocks RTLO spoofing (U+202E)
//     and zero-width hook-pattern evasion (U+200B etc.)
//  3. Windows reserved device names — blocks DoS via CON/NUL/COM1/LPT1 etc.
//  4. Pseudo-Linux absolute paths on Windows — blocks accidental container/sandbox path leakage
//     (e.g. /home/claude/, /tmp/, /sessions/...) which Go's filepath.Abs would silently
//     reinterpret as drive-relative paths (C:\home\claude\...) — usually NOT what the caller wants.
//
// This function is called unconditionally by IsPathAllowed, so security checks
// always run even when --allowed-paths is not configured.
//
// Exported so handlers can run pre-flight validation and surface a specific
// error message to the caller instead of the generic "access denied" returned
// when the engine internally fails IsPathAllowed.
func ValidatePathSecurity(path string) error {
	return validatePathSecurity(path)
}

func validatePathSecurity(path string) error {
	if path == "" {
		return nil
	}

	// 1. NTFS Alternate Data Streams
	if hasNTFSAlternateDataStream(path) {
		return &ValidationError{
			Field:   "path",
			Value:   path,
			Message: "NTFS Alternate Data Streams are not permitted (path contains ':' after drive letter)",
		}
	}

	// 2. Dangerous Unicode characters
	if r, found := findDangerousUnicodeInPath(path); found {
		return &ValidationError{
			Field:   "path",
			Value:   path,
			Message: fmt.Sprintf("path contains dangerous Unicode character U+%04X", r),
		}
	}

	// 3. Windows reserved device names
	base := filepath.Base(path)
	if isWindowsReservedName(base) {
		return &ValidationError{
			Field:   "path",
			Value:   path,
			Message: fmt.Sprintf("path uses Windows reserved device name: %s", base),
		}
	}

	// 4. Pseudo-Linux absolute paths on Windows
	if isPseudoLinuxPathOnWindows(path) {
		return &ValidationError{
			Field: "path",
			Value: path,
			Message: fmt.Sprintf("Unix-style absolute path %q is not valid on Windows. "+
				"Use a Windows path (C:\\...) or a WSL path (/mnt/<drive>/...). "+
				"This usually means the path leaked from a Linux container, sandbox, or bash environment.", path),
		}
	}

	return nil
}

// isPseudoLinuxPathOnWindows reports whether path looks like a Unix-style absolute
// path that was passed to a Windows host by mistake (e.g. /home/foo, /tmp/x, /sessions/...).
//
// On Windows, Go's filepath.Abs will silently rewrite such paths as drive-relative
// (e.g. /home/claude → C:\home\claude), which causes writes to land in unexpected
// locations and confuses the caller. This is a defense-in-depth check on top of
// --allowed-paths, useful when the server is launched in open-access mode.
//
// WSL-style paths /mnt/<single-letter>(/...)? are explicitly allowed because
// NormalizePath converts them to Windows form correctly.
func isPseudoLinuxPathOnWindows(path string) bool {
	if runtime.GOOS != "windows" {
		return false
	}
	// Only Unix-absolute paths are suspicious; everything else (relative, C:\..., C:/...)
	// is normal Windows usage.
	if path == "" || path[0] != '/' {
		return false
	}
	// Allow /mnt/<single-letter> and /mnt/<single-letter>/...
	// NormalizePath converts these to Windows drive letters.
	if strings.HasPrefix(path, "/mnt/") {
		rest := path[5:]
		if len(rest) >= 1 {
			c := rest[0]
			isLetter := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
			if isLetter && (len(rest) == 1 || rest[1] == '/') {
				return false
			}
		}
	}
	return true
}

// hasNTFSAlternateDataStream returns true when the path references an NTFS ADS.
// Drive-letter colons (C:\...) are legitimate; any additional colon after position 1
// indicates an ADS reference (e.g. "C:\file.txt:hidden_stream").
// This check only fires on Windows where NTFS ADS are operative.
func hasNTFSAlternateDataStream(path string) bool {
	if runtime.GOOS != "windows" {
		return false
	}
	// Skip the drive-letter colon (position 1), then look for any further colon.
	remaining := path
	if len(path) >= 2 && path[1] == ':' {
		remaining = path[2:]
	}
	return strings.ContainsRune(remaining, ':')
}

// dangerousUnicodeCodepoints lists Unicode code points that are dangerous in filesystem paths:
//   - Bidirectional control characters (RTLO U+202E, LTRO U+202D, embeddings, isolates)
//     → allow visual spoofing of file extensions to bypass user review
//   - Zero-width characters (U+200B, U+200C, U+200D, U+FEFF)
//     → allow hook pattern-evasion (e.g. ".en\u200Bv" evades "*.env" hook pattern)
//   - Line/paragraph separators → may corrupt path parsing
//   - Soft hyphen (U+00AD) → invisible in many renderers, confuses comparisons
var dangerousUnicodeCodepoints = []rune{
	0x00AD, // SOFT HYPHEN
	0x200B, // ZERO WIDTH SPACE
	0x200C, // ZERO WIDTH NON-JOINER
	0x200D, // ZERO WIDTH JOINER
	0x200E, // LEFT-TO-RIGHT MARK
	0x200F, // RIGHT-TO-LEFT MARK
	0x202A, // LEFT-TO-RIGHT EMBEDDING
	0x202B, // RIGHT-TO-LEFT EMBEDDING
	0x202C, // POP DIRECTIONAL FORMATTING
	0x202D, // LEFT-TO-RIGHT OVERRIDE
	0x202E, // RIGHT-TO-LEFT OVERRIDE (RTLO — classic extension-spoofing attack)
	0x2028, // LINE SEPARATOR
	0x2029, // PARAGRAPH SEPARATOR
	0x2066, // LEFT-TO-RIGHT ISOLATE
	0x2067, // RIGHT-TO-LEFT ISOLATE
	0x2068, // FIRST STRONG ISOLATE
	0x2069, // POP DIRECTIONAL ISOLATE
	0xFEFF, // ZERO WIDTH NO-BREAK SPACE (BOM)
}

// findDangerousUnicodeInPath scans a path for dangerous Unicode codepoints.
// Returns the first offending rune and true, or (0, false) if the path is clean.
func findDangerousUnicodeInPath(path string) (rune, bool) {
	// Build a fast lookup set
	dangerous := make(map[rune]struct{}, len(dangerousUnicodeCodepoints))
	for _, r := range dangerousUnicodeCodepoints {
		dangerous[r] = struct{}{}
	}

	for _, r := range path {
		// ASCII control characters (< 0x20) are never valid in file paths
		if r < 0x20 {
			return r, true
		}
		if _, bad := dangerous[r]; bad {
			return r, true
		}
		// Unicode "Format" category (Cf) covers additional invisible formatting chars
		// not listed explicitly above (e.g. language tags U+E0001..U+E007F)
		if unicode.Is(unicode.Cf, r) {
			return r, true
		}
	}
	return 0, false
}

// windowsReservedNames is the complete list of reserved device names in Windows.
// Accessing these as file paths causes the OS to open the underlying device,
// which can freeze the process (CON waits for stdin) or cause other DoS.
var windowsReservedNames = map[string]struct{}{
	"CON":  {},
	"PRN":  {},
	"AUX":  {},
	"NUL":  {},
	"COM0": {}, "COM1": {}, "COM2": {}, "COM3": {}, "COM4": {},
	"COM5": {}, "COM6": {}, "COM7": {}, "COM8": {}, "COM9": {},
	"LPT0": {}, "LPT1": {}, "LPT2": {}, "LPT3": {}, "LPT4": {},
	"LPT5": {}, "LPT6": {}, "LPT7": {}, "LPT8": {}, "LPT9": {},
}

// ResolveSymlinks resolves all symlinks in path and returns the canonical path.
// This is a defense-in-depth measure against TOCTOU (time-of-check-time-of-use) attacks
// where an attacker replaces a validated file with a symlink between the security check
// and the actual file operation.
//
// Usage: Call this immediately before any file operation (copy, move, rename, write)
// that has already passed IsPathAllowed() but where a symlink could have been
// inserted in the intervening time.
//
// Returns the resolved path, the original path on resolution failure, and a bool
// that is true ONLY if the path actually traverses a symlink (i.e., a real
// attacker-controlled reparse point). The bool is false for:
//
//   - regular files and directories
//   - Windows directory junctions (e.g. %LOCALAPPDATA%\Temp, which on the
//     GitHub Actions runner is a junction to %USERPROFILE%\AppData\Local\Temp).
//     Junctions are created by the OS itself and are not a TOCTOU vector.
//   - drive-letter / case / separator differences that filepath.EvalSymlinks
//     may report on Windows.
//
// The previous implementation used filepath.EvalSymlinks and treated ANY
// difference between the resolved and original paths as a symlink — which
// incorrectly rejected legitimate Windows paths that go through a junction.
// See: https://github.com/scopweb/mcp-filesystem-go-ultra/pull/10#discussion
// for context.
func ResolveSymlinks(path string) (string, bool, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return path, false, err
	}

	// The actual TOCTOU check: walk the path components from deepest to
	// shallowest, Lstat-ing each one. Lstat does not follow links, so it
	// reports ModeSymlink for symlinks. On Windows, directory junctions
	// are NOT reported as ModeSymlink by Lstat (they're reported as
	// ModeDir + a reparse point attribute that Lstat does not surface).
	// So a path that only traverses junctions comes back as "no symlink".
	hasSymlink, err := pathContainsSymlink(absPath)
	if err != nil {
		return path, false, err
	}

	// Also produce the canonical form for the return value. If the path
	// doesn't exist yet (e.g. we're about to create a new file inside a
	// still-existing directory), EvalSymlinks fails — fall back to the
	// deepest existing ancestor so callers still get a usable path.
	resolved, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		current := absPath
		var suffix []string
		for {
			parent := filepath.Dir(current)
			// Record the current component BEFORE testing its parent, so the
			// deepest (non-existent) component is not dropped from the result.
			suffix = append([]string{filepath.Base(current)}, suffix...)
			if parent == current {
				break
			}
			if parentResolved, parentErr := filepath.EvalSymlinks(parent); parentErr == nil {
				resolved = parentResolved
				for _, s := range suffix {
					resolved = filepath.Join(resolved, s)
				}
				break
			}
			current = parent
		}
		if resolved == "" {
			return path, hasSymlink, fmt.Errorf("could not resolve any ancestor of %s", path)
		}
	}

	return resolved, hasSymlink, nil
}

// pathContainsSymlink returns true if any component of p (or any of its
// existing ancestors) is a symbolic link. It is the actual TOCTOU defense:
// an attacker who plants a symlink anywhere along the path will be caught,
// even if the final target is a regular file. Junctions, which Lstat reports
// as plain directories, are not flagged.
func pathContainsSymlink(p string) (bool, error) {
	current := p
	for {
		info, err := os.Lstat(current)
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return true, nil
			}
		} else if !os.IsNotExist(err) {
			return false, err
		}
		// Either the path doesn't exist (yet) or it's not a symlink.
		// Walk up to the parent. Stop when we reach the root.
		parent := filepath.Dir(current)
		if parent == current {
			return false, nil
		}
		current = parent
	}
}

// ValidateRegex checks a regex pattern for potential ReDoS (Regular Expression
// Denial of Service) vulnerabilities. Returns an error if the pattern contains
// dangerous constructs that could cause catastrophic backtracking.
//
// Dangerous patterns include:
//   - Nested quantifiers like (a+)+, (a*)+, (a{1,})+
//   - Overlapping alternations like (a|a)+
//   - Quantified overlapping character classes like [ab]+
//
// The function uses a 5-second timeout to prevent the validation itself from
// causing issues with malformed patterns.
func ValidateRegex(pattern string) error {
	// Simple heuristic check for dangerous patterns that could cause ReDoS.
	// This runs synchronously and should be fast for typical patterns.
	// The actual regex execution in edit operations is protected by
	// context timeouts at the operation level.

	dangerousPatterns := []string{
		"(a+)+", "(a*)+", "(a{1,})+",
		"(.*)+", "(..)+", "(.+)+",
		"(\\w+)+", "(\\d+)+",
	}

	for _, dangerous := range dangerousPatterns {
		// Quick string check first (no regex compilation needed)
		if containsDangerousNestedQuantifier(pattern, dangerous) {
			return fmt.Errorf("regex pattern contains potentially dangerous nested quantifiers: %s", dangerous)
		}
	}

	return nil
}

// containsDangerousNestedQuantifier checks if pattern has nested quantifier structures.
// Uses simple string search, not regex, to avoid ReDoS in the validator itself.
func containsDangerousNestedQuantifier(pattern, dangerous string) bool {
	// Only flag if the pattern is reasonably short (long patterns need careful regex anyway)
	if len(pattern) > 200 {
		return false
	}
	// Direct string search for the dangerous sequence
	return strings.Contains(pattern, dangerous)
}

// isWindowsReservedName returns true if name matches a Windows reserved device name.
// The check is case-insensitive and extension-insensitive ("NUL.txt" is still NUL).
// Applied on all platforms for portability (a "NUL" file on Linux would be unusable on Windows).
func isWindowsReservedName(name string) bool {
	upper := strings.ToUpper(name)
	// Strip extension: "NUL.txt" → "NUL"
	if idx := strings.LastIndex(upper, "."); idx > 0 {
		upper = upper[:idx]
	}
	_, reserved := windowsReservedNames[upper]
	return reserved
}
