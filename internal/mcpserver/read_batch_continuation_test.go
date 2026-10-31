package mcpserver

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mcp/filesystem-ultra/core"
)

// Exercise the public handler and replay recovery arguments after a JSON round
// trip, just as a client would, without interpreting any content footer.
func TestReadFile_BatchContinuationRecovery(t *testing.T) {
	for _, responseMode := range []string{core.ResponseModeCompat, core.ResponseModeStructured} {
		for _, detail := range []string{"normal", "summary"} {
			for _, tc := range []struct {
				name              string
				args              map[string]any
				wide              bool
				kind              string
				next, first, last int
				reread, earlier   bool
			}{
				{name: "auto", kind: cutAutoCap, next: 301, first: 1, last: 300},
				{name: "head", args: map[string]any{"mode": "head"}, kind: cutServerMaxLines, next: 101, first: 1, last: 100},
				{name: "tail", args: map[string]any{"mode": "tail"}, kind: cutServerMaxLines, first: 221, last: 320, earlier: true},
				{name: "width", args: map[string]any{"mode": "tail", "max_lines": float64(5)}, wide: true, kind: cutLineWidth, first: 316, last: 320, reread: true},
				{name: "head-width", args: map[string]any{"mode": "head"}, wide: true, kind: cutServerMaxLines + "+" + cutLineWidth, next: 101, first: 1, last: 100, reread: true},
				{name: "tail-width", args: map[string]any{"mode": "tail"}, wide: true, kind: cutServerMaxLines + "+" + cutLineWidth, first: 221, last: 320, reread: true, earlier: true},
				{name: "auto-width", args: map[string]any{"max_line_length": float64(300)}, wide: true, kind: cutAutoCap + "+" + cutLineWidth, next: 301, first: 1, last: 300, reread: true},
				{name: "range", args: map[string]any{"start_line": float64(5), "end_line": float64(8)}},
				{name: "max-lines", args: map[string]any{"max_lines": float64(5)}},
			} {
				t.Run(responseMode+"/"+detail+"/"+tc.name, func(t *testing.T) {
					dir := t.TempDir()
					reg := newHelpTestRegistry(t, dir)
					reg.engine.GetConfig().ResponseMode = responseMode
					path := writeNumberedFile(t, dir, "target.txt", 320, true)
					marker := strings.Repeat("q", 500) + "END"
					if tc.wide {
						if err := os.WriteFile(path, []byte(strings.Repeat(marker+"\n", 320)), 0644); err != nil {
							t.Fatal(err)
						}
					}
					small := filepath.Join(dir, "small.txt")
					if err := os.WriteFile(small, []byte("short\n"), 0644); err != nil {
						t.Fatal(err)
					}
					read := func(args map[string]any) map[string]any {
						t.Helper()
						res := callPatchTool(t, reg, "read_file", args)
						if res.IsError {
							t.Fatal(resultText(t, res))
						}
						data, err := json.Marshal(res.StructuredContent)
						if err != nil {
							t.Fatal(err)
						}
						var m map[string]any
						if err := json.Unmarshal(data, &m); err != nil {
							t.Fatal(err)
						}
						return m
					}
					args := map[string]any{"paths": []any{small, path, filepath.Join(dir, "missing.txt")}, "detail": detail}
					maps.Copy(args, tc.args)
					m := read(args)
					if m["status"] != statusPartial {
						t.Fatalf("status: %v", m["status"])
					}
					files := m["files"].([]any)
					f := files[1].(map[string]any)
					if files[0].(map[string]any)["continuation"] != nil || files[2].(map[string]any)["continuation"] != nil {
						t.Fatal("complete/error entries must not have continuation")
					}
					if detail == "summary" && f["content"] != nil {
						t.Fatal("summary leaked body")
					}
					singleArgs := map[string]any{"path": path}
					maps.Copy(singleArgs, tc.args)
					single := read(singleArgs)
					singleCont, _ := json.Marshal(single["continuation"])
					batchCont, _ := json.Marshal(f["continuation"])
					if string(singleCont) != string(batchCont) {
						t.Fatalf("single/batch continuation differs: %s / %s", singleCont, batchCont)
					}
					if f["content_hash"] == "" || f["content_hash"] != single["content_hash"] {
						t.Fatal("snapshot hash lost")
					}
					if tc.kind == "" {
						if m["truncated"] != false || f["truncated"] != false || m["continuation"] != nil || f["continuation"] != nil {
							t.Fatal("explicit selection has continuation")
						}
						return
					}
					if m["truncated"] != true || f["truncated"] != true {
						t.Fatal("cut not marked")
					}
					index := m["continuation"].(map[string]any)
					indexes := index["file_indexes"].([]any)
					if index["kind"] != "batch" || len(indexes) != 1 || indexes[0] != float64(1) {
						t.Fatalf("index: %#v", index)
					}
					cont := f["continuation"].(map[string]any)
					spilled := cont["kind"] == cutCharSpill+"+"+cutLineWidth
					if cont["kind"] != tc.kind && !spilled {
						t.Fatalf("kind: %v", cont["kind"])
					}
					recoverRange := func(recovery map[string]any, first, last int) {
						t.Helper()
						request := maps.Clone(recovery)
						request["path"] = f["path"]
						recovered := read(request)
						if recovered["content_hash"] != f["content_hash"] {
							t.Fatal("recovery hash changed")
						}
						if recovered["truncated"] == true {
							body, _ := recovered["content"].(string)
							if len(body) > claudeSpillChars {
								t.Fatalf("recovery still spills: %d", len(body))
							}
							return
						}
						if asIntMeta(recovered["start_line"]) != first || asIntMeta(recovered["end_line"]) != last {
							t.Fatalf("recovery range: %#v", recovered)
						}
						body := recovered["content"].(string)
						if tc.wide {
							if strings.Count(body, marker) != last-first+1 {
								t.Fatal("recovery did not restore full-width lines")
							}
						} else {
							assertConsecutive(t, collectNumberedLines(bodyBeforeTruncationFooter(body)), first, last)
						}
					}
					if tc.next > 0 {
						start := asIntMeta(cont["start_line"])
						if spilled {
							if start <= 1 || start > tc.next {
								t.Fatalf("spill cursor %d", start)
							}
						} else if start != tc.next {
							t.Fatal("wrong forward cursor")
						}
						last := min(320, start+asIntMeta(cont["max_lines"])-1)
						recoverRange(map[string]any{"start_line": cont["start_line"], "max_lines": cont["max_lines"]}, start, last)
					} else if cont["start_line"] != nil {
						t.Fatal("invented forward cursor")
					}
					if tc.reread {
						recoverRange(cont["reread"].(map[string]any), tc.first, tc.last)
					} else if cont["reread"] != nil {
						t.Fatal("unexpected reread")
					}
					if tc.earlier {
						recoverRange(cont["earlier"].(map[string]any), 1, tc.first-1)
					} else if cont["earlier"] != nil {
						t.Fatal("unexpected earlier range")
					}
				})
			}
		}
	}
}
