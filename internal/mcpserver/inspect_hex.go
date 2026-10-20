package mcpserver

import (
	"fmt"
	"strings"
)

// inspectHexDump prints absolute byte offsets, including for nonzero ranges.
func inspectHexDump(raw []byte, offset int64) string {
	var b strings.Builder
	for i := 0; i < len(raw); i += 16 {
		end := min(i+16, len(raw))
		fmt.Fprintf(&b, "%08X  ", offset+int64(i))
		for j := i; j < i+16; j++ {
			if j < end {
				fmt.Fprintf(&b, "%02X ", raw[j])
			} else {
				b.WriteString("   ")
			}
		}
		b.WriteString(" |")
		for _, c := range raw[i:end] {
			if c >= 32 && c <= 126 {
				b.WriteByte(c)
			} else {
				b.WriteByte('.')
			}
		}
		b.WriteString("|\n")
	}
	return b.String()
}
