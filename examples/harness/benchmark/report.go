package main

import (
	"bufio"
	"encoding/json"
	"os"
	"sort"
	"strings"
	"time"
)

type proxyEntry struct {
	Tool       string `json:"tool"`
	TokensIn   int64  `json:"tokens_in"`
	TokensOut  int64  `json:"tokens_out"`
	DurationMs int64  `json:"duration_ms"`
	DurationNs int64  `json:"duration_ns"`
	RequestID  string `json:"request_id"`
	BytesIn    int64  `json:"bytes_in"`
	BytesOut   int64  `json:"bytes_out"`
	Status     string `json:"status"`
	Error      string `json:"error,omitempty"`
}

type scenarioRow struct {
	Scenario    string    `json:"scenario"`
	Calls       int       `json:"calls"`
	TokensIn    int64     `json:"tokens_in"`
	TokensOut   int64     `json:"tokens_out"`
	DurationMs  int64     `json:"duration_ms"`
	OCCMismatch int       `json:"occ_mismatch"`
	PatchFailed int       `json:"patch_failed"`
	Retries     int       `json:"retries"`
	Errors      int       `json:"errors"`
	Tools       []toolRow `json:"tools,omitempty"`
}

type toolRow struct {
	Tool      string `json:"tool"`
	Calls     int    `json:"calls"`
	TokensOut int64  `json:"tokens_out"`
}

type report struct {
	Generated string        `json:"generated"`
	Note      string        `json:"note"`
	Scenarios []scenarioRow `json:"scenarios"`
	ByTool    []toolRow     `json:"by_tool"`
	Expensive []string      `json:"expensive_tools"`
	Decision  string        `json:"decision"`
}

func loadProxyLog(path string) ([]proxyEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []proxyEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e proxyEntry
		if json.Unmarshal([]byte(line), &e) != nil {
			continue
		}
		out = append(out, e)
	}
	return out, sc.Err()
}

func summarize(entries []proxyEntry, slices map[string][2]int, names []string, decision string) report {
	var rows []scenarioRow
	if len(names) == 0 {
		names = []string{"explore_2k", "occ_clash", "patch_fail", "help_catalog"}
	}
	allTools := map[string]*toolRow{}
	for _, name := range names {
		rng, ok := slices[name]
		if !ok {
			rows = append(rows, scenarioRow{Scenario: name})
			continue
		}
		row := fold(name, entries[rng[0]:rng[1]])
		rows = append(rows, row)
		for i := range row.Tools {
			t := row.Tools[i]
			agg, ok := allTools[t.Tool]
			if !ok {
				agg = &toolRow{Tool: t.Tool}
				allTools[t.Tool] = agg
			}
			agg.Calls += t.Calls
			agg.TokensOut += t.TokensOut
		}
	}
	byTool := make([]toolRow, 0, len(allTools))
	for _, t := range allTools {
		byTool = append(byTool, *t)
	}
	sort.Slice(byTool, func(i, j int) bool {
		if byTool[i].TokensOut == byTool[j].TokensOut {
			return byTool[i].Tool < byTool[j].Tool
		}
		return byTool[i].TokensOut > byTool[j].TokensOut
	})
	detailCandidates := map[string]bool{
		"directory_tree": true,
		"search_files":   true,
		"read_file":      true,
		"help":           true,
	}
	expensive := make([]string, 0, 4)
	for _, tool := range byTool {
		if detailCandidates[tool.Tool] {
			expensive = append(expensive, tool.Tool)
		}
	}
	if decision == "" {
		decision = "PR-4 detail targets (highest tokens_out among eligible tools): " + strings.Join(expensive, ", ")
	}
	return report{
		Generated: time.Now().UTC().Format(time.RFC3339),
		Note:      "paths redacted; tokens = proxy bytes/4 estimates; retries always 0 (harness does not replay); scripted MCP is not a real-model eval",
		Scenarios: rows,
		ByTool:    byTool,
		Expensive: expensive,
		Decision:  decision,
	}
}

func fold(name string, entries []proxyEntry) scenarioRow {
	row := scenarioRow{Scenario: name, Tools: nil}
	by := map[string]*toolRow{}
	for _, e := range entries {
		row.Calls++
		row.TokensIn += e.TokensIn
		row.TokensOut += e.TokensOut
		row.DurationMs += e.DurationMs
		if e.Status != "ok" {
			row.Errors++
		}
		if strings.Contains(e.Error, "OCC_MISMATCH") || strings.Contains(strings.ToLower(e.Error), "stale edit") {
			row.OCCMismatch++
		}
		if strings.Contains(e.Error, "PATCH_APPLY_FAILED") || strings.Contains(e.Error, "PATCH_FAILED") {
			row.PatchFailed++
		}
		t, ok := by[e.Tool]
		if !ok {
			t = &toolRow{Tool: e.Tool}
			by[e.Tool] = t
		}
		t.Calls++
		t.TokensOut += e.TokensOut
	}
	for _, t := range by {
		row.Tools = append(row.Tools, *t)
	}
	sort.Slice(row.Tools, func(i, j int) bool { return row.Tools[i].Tool < row.Tools[j].Tool })
	return row
}
