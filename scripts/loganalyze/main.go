// loganalyze — one-off analysis of operations.jsonl for the perf plan (#0).
// Usage: go run ./scripts/loganalyze <path-to-operations.jsonl>
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

type entry struct {
	Tool       string                 `json:"tool"`
	Path       string                 `json:"path"`
	DurationMs float64                `json:"duration_ms"`
	Status     string                 `json:"status"`
	Args       map[string]interface{} `json:"args"`
}

type stats struct {
	durs  []float64
	total float64
}

func (s *stats) add(d float64) { s.durs = append(s.durs, d); s.total += d }

func pct(durs []float64, p float64) float64 {
	if len(durs) == 0 {
		return 0
	}
	i := int(p * float64(len(durs)-1))
	return durs[i]
}

func main() {
	logPath := `C:\temp\mcp-ultra-logs\operations.jsonl`
	if len(os.Args) > 1 {
		logPath = os.Args[1]
	}
	f, err := os.Open(logPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()

	byTool := map[string]*stats{}
	// search_files split: with vs without file_types/include
	searchTyped := &stats{}
	searchUntyped := &stats{}
	type slow struct {
		tool string
		path string
		dur  float64
		note string
	}
	var slowest []slow

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	lines := 0
	for sc.Scan() {
		lines++
		var e entry
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			continue
		}
		st := byTool[e.Tool]
		if st == nil {
			st = &stats{}
			byTool[e.Tool] = st
		}
		st.add(e.DurationMs)
		if e.Tool == "search_files" {
			_, hasFT := e.Args["file_types"]
			_, hasInc := e.Args["include"]
			if hasFT || hasInc {
				searchTyped.add(e.DurationMs)
			} else {
				searchUntyped.add(e.DurationMs)
			}
		}
		note := ""
		if p, ok := e.Args["pattern"].(string); ok {
			note = "pattern=" + p
		}
		slowest = append(slowest, slow{e.Tool, e.Path, e.DurationMs, note})
	}
	fmt.Printf("parsed %d lines\n\n", lines)

	// Per-tool table sorted by total time
	type row struct {
		tool                  string
		n                     int
		p50, p95, max, totalS float64
	}
	var rows []row
	for tool, st := range byTool {
		sort.Float64s(st.durs)
		rows = append(rows, row{tool, len(st.durs), pct(st.durs, 0.5), pct(st.durs, 0.95), st.durs[len(st.durs)-1], st.total / 1000})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].totalS > rows[j].totalS })
	fmt.Printf("%-20s %6s %8s %8s %9s %9s\n", "tool", "n", "p50ms", "p95ms", "maxms", "totalS")
	for _, r := range rows {
		fmt.Printf("%-20s %6d %8.0f %8.0f %9.0f %9.1f\n", r.tool, r.n, r.p50, r.p95, r.max, r.totalS)
	}

	sort.Float64s(searchTyped.durs)
	sort.Float64s(searchUntyped.durs)
	fmt.Printf("\nsearch_files WITH file_types/include:    n=%d p50=%.0f p95=%.0f total=%.1fs\n",
		len(searchTyped.durs), pct(searchTyped.durs, 0.5), pct(searchTyped.durs, 0.95), searchTyped.total/1000)
	fmt.Printf("search_files WITHOUT file_types/include: n=%d p50=%.0f p95=%.0f total=%.1fs\n",
		len(searchUntyped.durs), pct(searchUntyped.durs, 0.5), pct(searchUntyped.durs, 0.95), searchUntyped.total/1000)

	sort.Slice(slowest, func(i, j int) bool { return slowest[i].dur > slowest[j].dur })
	fmt.Println("\ntop 15 slowest calls:")
	for i := 0; i < 15 && i < len(slowest); i++ {
		s := slowest[i]
		fmt.Printf("%7.0fms %-14s %s %s\n", s.dur, s.tool, s.path, s.note)
	}
}
