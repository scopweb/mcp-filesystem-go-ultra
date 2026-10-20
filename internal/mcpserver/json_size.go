package mcpserver

import (
	"encoding/json"
	"unicode/utf8"
)

// inspectJSONFits measures a selected JSON value before rendering/marshaling.
// In particular, a single large object must not allocate an enormous indented
// string merely to discover that it cannot fit in a 64 KiB response. Budget is
// consumed on traversal, including repeated references produced by JMESPath.
func inspectJSONFits(value any, remaining *int) bool {
	if *remaining < 0 {
		return false
	}
	switch v := value.(type) {
	case nil:
		*remaining -= 4
	case bool:
		if v {
			*remaining -= 4
		} else {
			*remaining -= 5
		}
	case float64:
		raw, err := json.Marshal(v)
		if err != nil {
			return false
		}
		*remaining -= len(raw)
	case string:
		return inspectJSONStringFits(v, remaining)
	case []any:
		*remaining -= 2
		for i, item := range v {
			if i > 0 {
				*remaining--
			}
			if !inspectJSONFits(item, remaining) {
				return false
			}
		}
	case map[string]any:
		*remaining -= 2
		first := true
		for key, item := range v {
			if !first {
				*remaining--
			}
			first = false
			if !inspectJSONStringFits(key, remaining) {
				return false
			}
			*remaining--
			if !inspectJSONFits(item, remaining) {
				return false
			}
		}
	default:
		return false
	}
	return *remaining >= 0
}

func inspectNumberPreview(value string) string {
	if len(value) > 64 {
		return value[:64] + "..."
	}
	return value
}
func inspectJSONStringFits(value string, remaining *int) bool {
	*remaining -= 2
	for i := 0; i < len(value); {
		if *remaining < 0 {
			return false
		}
		c := value[i]
		if c < utf8.RuneSelf {
			switch c {
			case '\\', '"', '\b', '\f', '\n', '\r', '\t':
				*remaining -= 2
			case '<', '>', '&':
				*remaining -= 6
			default:
				if c < 32 {
					*remaining -= 6
				} else {
					*remaining--
				}
			}
			i++
			continue
		}
		r, n := utf8.DecodeRuneInString(value[i:])
		if r == utf8.RuneError && n == 1 {
			// Go versions differ in whether replacement is literal UTF-8 or
			// escaped. Measure one invalid byte, never the whole large string.
			raw, _ := json.Marshal(value[i : i+1])
			*remaining -= len(raw) - 2
		} else if r == 0x2028 || r == 0x2029 {
			*remaining -= 6
		} else {
			*remaining -= n
		}
		i += n
	}
	return *remaining >= 0
}
