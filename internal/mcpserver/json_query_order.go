package mcpserver

import (
	"regexp"
	"strings"
)

var inspectFinalScalarSort = regexp.MustCompile(`\|\s*sort\(\s*@\s*\)\s*$`)

// Standard JMESPath leaves object iteration unordered, and the Go evaluator
// uses map iteration for keys(), values() and object wildcards. A file hash
// alone therefore cannot make those result arrays stable across calls. Refuse
// multi-page output unless a final scalar sort establishes a deterministic order.
// Source JSON arrays, filters and projections retain their original ordering.
func inspectQueryUnordered(query string) bool {
	if inspectFinalScalarSort.MatchString(query) {
		return false
	}
	unordered := false
	previous := byte(0)
	for i := 0; i < len(query); i++ {
		c := query[i]
		if c == '\'' || c == '"' || c == '`' {
			quote := c
			i++
			for i < len(query) {
				if query[i] == '\\' {
					i += 2
					continue
				}
				if query[i] == quote {
					break
				}
				i++
			}
			previous = quote
			continue
		}
		if c == ' ' || c == '\n' || c == '\r' || c == '\t' {
			continue
		}
		if c == '*' && previous != '[' {
			unordered = true
		}
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_' {
			start := i
			for i+1 < len(query) {
				n := query[i+1]
				if (n >= 'a' && n <= 'z') || (n >= 'A' && n <= 'Z') || (n >= '0' && n <= '9') || n == '_' {
					i++
				} else {
					break
				}
			}
			name := query[start : i+1]
			rest := strings.TrimLeft(query[i+1:], " \t\r\n")
			if (name == "keys" || name == "values") && strings.HasPrefix(rest, "(") {
				unordered = true
			}
		}
		previous = c
	}
	return unordered
}
