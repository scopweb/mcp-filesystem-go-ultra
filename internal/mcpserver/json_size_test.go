package mcpserver

import (
	"encoding/json"
	"testing"
)

// Compare response preflight sizing with the independent standard JSON encoder,
// including invalid UTF-8, HTML escaping, controls and repeated graph references.
func FuzzInspectJSONSize(f *testing.F) {
	for _, seed := range []string{"", "plain", "cafè", "<>&", "\"\\\n\r\t\x00", "\u2028\u2029", string([]byte{0xff, 0xfe})} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		shared := map[string]any{"text": text, "number": float64(0.1), "null": nil, "bool": true}
		value := []any{shared, shared, text}
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		budget := len(encoded)
		if !inspectJSONFits(value, &budget) || budget != 0 {
			t.Fatalf("size preflight disagrees at exact limit: remaining=%d encoded=%s", budget, encoded)
		}
		budget = len(encoded) - 1
		if inspectJSONFits(value, &budget) {
			t.Fatal("size preflight admitted one byte beyond the limit")
		}
	})
}
