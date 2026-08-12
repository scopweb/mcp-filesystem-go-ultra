package main

// Coverage for the multi_edit edits_json trailing-comma recovery: LLM clients
// (Claude Desktop, opencode) frequently emit {"old_text":"a",} or [1,2,] —
// shapes encoding/json rejects. The proxy logs showed ~40% of "Invalid edits
// JSON" errors were exactly this pattern.

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStripTrailingCommas(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"object", `{"a":1,}`, `{"a":1}`},
		{"array", `[1,2,]`, `[1,2]`},
		{"nested", `[{"a":1,},{"b":2,},]`, `[{"a":1},{"b":2}]`},
		{"whitespace_before_close", `{"a":1 , }`, `{"a":1  }`},
		{"comma_inside_string", `{"a":"x,}"}`, `{"a":"x,}"}`},
		{"escaped_quote_before_comma", `{"a":"x\"",}`, `{"a":"x\""}`},
		{"no_trailing", `{"a":1,"b":2}`, `{"a":1,"b":2}`},
		{"empty_object", `{,}`, `{}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := stripTrailingCommas(tc.in)
			if got != tc.want {
				t.Errorf("stripTrailingCommas(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if strings.Contains(got, ",}") || strings.Contains(got, ",]") {
				var v interface{}
				if err := json.Unmarshal([]byte(got), &v); err != nil {
					t.Errorf("stripped output still invalid JSON: %v", err)
				}
			}
		})
	}
}
