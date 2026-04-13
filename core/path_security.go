package core

import (
	"fmt"
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
//
// This function is called unconditionally by IsPathAllowed, so security checks
// always run even when --allowed-paths is not configured.
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

	return nil
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
