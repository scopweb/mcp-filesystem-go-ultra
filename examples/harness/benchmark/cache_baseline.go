package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/mcp/filesystem-ultra/internal/benchclock"
)

// The production TTLs are intentional; unit tests test planning, not a shortened TTL.
func cacheOrder(repetitions int, wait time.Duration) ([]string, error) {
	if repetitions < 1 || repetitions > 10 || wait <= 3*time.Minute || wait >= 10*time.Minute {
		return nil, fmt.Errorf("cache requires 1..10 repetitions and 3m < wait < 10m")
	}
	var order []string
	for i := 0; i < repetitions; i++ {
		order = append(order, "3m", "10m")
	}
	return order, nil
}

type cacheCounters struct {
	Hits           int64 `json:"file_hits"`
	Misses         int64 `json:"file_misses"`
	Loads          int64 `json:"disk_loads"`
	Prefetch       int64 `json:"prefetch_fills"`
	PrefetchHits   int64 `json:"prefetch_hits"`
	PrefetchMisses int64 `json:"prefetch_misses"`
	Resident       int64 `json:"resident_bytes_tracked"`
	Capacity       int64 `json:"capacity_bytes"`
}

type cacheFile struct {
	Path   string `json:"path"`
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type cacheSample struct {
	RequestID    string `json:"request_id"`
	File         string `json:"file"`
	RunnerNs     int64  `json:"runner_ns"`
	ProxyNs      int64  `json:"proxy_ns"`
	BytesIn      int64  `json:"bytes_in"`
	BytesOut     int64  `json:"bytes_out"`
	ContentBytes int    `json:"content_bytes"`
	Error        string `json:"error,omitempty"`
}

type cachePhase struct {
	Name                string        `json:"name"`
	Started             time.Time     `json:"started"`
	Completed           time.Time     `json:"completed"`
	Before              cacheCounters `json:"before"`
	After               cacheCounters `json:"after"`
	Hits                int64         `json:"demand_hits"`
	Misses              int64         `json:"demand_misses"`
	Loads               int64         `json:"disk_loads"`
	MinAgeNs            int64         `json:"min_age_ns,omitempty"`
	MaxAgeNs            int64         `json:"max_age_ns,omitempty"`
	LaunchFirstUsefulNs int64         `json:"launch_first_useful_ns,omitempty"`
	P50Ns               int64         `json:"proxy_p50_ns"`
	P95Ns               int64         `json:"proxy_p95_ns"`
	RunnerP50Ns         int64         `json:"runner_p50_ns"`
	RunnerP95Ns         int64         `json:"runner_p95_ns"`
	BytesIn             int64         `json:"bytes_in"`
	BytesOut            int64         `json:"bytes_out"`
	Errors              int           `json:"errors"`
	Samples             []cacheSample `json:"samples"`
}

type cacheRun struct {
	TTL                 string       `json:"ttl"`
	Repetition          int          `json:"repetition"`
	LogDir              string       `json:"log_dir"`
	RestartLogDir       string       `json:"restart_log_dir"`
	Phases              []cachePhase `json:"phases"`
	client              *rpcClient
	launch              int64
	firstInsertionBound time.Time
	lastInsertionBound  time.Time
}

type cacheReport struct {
	Generated   time.Time         `json:"generated"`
	Complete    bool              `json:"complete"`
	Failure     string            `json:"failure,omitempty"`
	Environment map[string]string `json:"environment"`
	Method      string            `json:"method"`
	Workspace   string            `json:"workspace"`
	WaitNs      int64             `json:"wait_ns"`
	Corpus      []cacheFile       `json:"corpus"`
	Runs        []*cacheRun       `json:"runs"`
}

func percentileNS(values []int64, p float64) int64 {
	if len(values) == 0 {
		return 0
	}
	v := append([]int64(nil), values...)
	sort.Slice(v, func(i, j int) bool { return v[i] < v[j] })
	i := int(math.Ceil(float64(len(v))*p)) - 1 // nearest rank
	if i < 0 {
		i = 0
	}
	if i >= len(v) {
		i = len(v) - 1
	}
	return v[i]
}

func seedCacheCorpus(root string) ([]cacheFile, error) {
	var files []cacheFile
	for i := 0; i < 24; i++ {
		// Exactly one file per directory: TrackAccess's third-read sibling
		// prefetch cannot reinsert any corpus file, including during aged reads.
		dir := filepath.Join(root, fmt.Sprintf("isolated-%02d", i))
		if err := os.MkdirAll(dir, 0700); err != nil {
			return nil, err
		}
		n := []int{1024, 8 * 1024, 32 * 1024, 128 * 1024}[i%4]
		body := []byte(strings.Repeat(fmt.Sprintf("fixture-%02d abcdefghijklmnopqrstuvwxyz 0123456789\n", i), n/40+1))[:n]
		path := filepath.Join(dir, "content.txt")
		if err := os.WriteFile(path, body, 0600); err != nil {
			return nil, err
		}
		files = append(files, cacheFile{path, n, fmt.Sprintf("%x", sha256.Sum256(body))})
	}
	return files, nil
}

func readCacheCounters(c *rpcClient) (cacheCounters, error) {
	var counters cacheCounters
	res, err := c.tool("server_info", map[string]any{"action": "stats"})
	if err != nil {
		return counters, err
	}
	if isError(res) {
		return counters, fmt.Errorf("stats: %s", resultText(res))
	}
	m, ok := structured(res)["cache"].(map[string]any)
	if !ok {
		return counters, fmt.Errorf("server lacks structured cache counters")
	}
	for _, key := range []string{"file_hits", "file_misses", "disk_loads", "prefetch_fills", "prefetch_hits", "prefetch_misses", "resident_bytes_tracked", "capacity_bytes"} {
		if _, ok := m[key].(float64); !ok {
			return counters, fmt.Errorf("missing numeric cache counter %s", key)
		}
	}
	b, err := json.Marshal(m)
	if err != nil {
		return counters, err
	}
	err = json.Unmarshal(b, &counters)
	return counters, err
}

func measureCachePhase(c *rpcClient, files []cacheFile, name, logDir string, launch int64, insertionStart, insertionEnd time.Time) (p cachePhase, err error) {
	p.Name = name
	p.Before, err = readCacheCounters(c)
	if err != nil {
		return p, err
	}
	p.Started = time.Now()
	if !insertionEnd.IsZero() {
		p.MinAgeNs = p.Started.Sub(insertionEnd).Nanoseconds()
		if p.MinAgeNs <= int64(3*time.Minute) {
			return p, fmt.Errorf("aged read started too early")
		}
	}
	for _, file := range files {
		start := benchclock.Now()
		// Base64 returns complete bytes without text projection/truncation. It
		// calls the same ReadFileSnapshot demand cache path as text reads.
		res, callErr := c.tool("read_file", map[string]any{"path": file.Path, "encoding": "base64"})
		s := cacheSample{RequestID: fmt.Sprint(c.id), File: file.Path, RunnerNs: benchclock.Since(start).Nanoseconds()}
		if callErr == nil && isError(res) {
			callErr = fmt.Errorf("%s", resultText(res))
		}
		if callErr == nil {
			body, _ := structured(res)["content"].(string)
			decoded, decodeErr := base64.StdEncoding.DecodeString(body)
			if decodeErr != nil || len(decoded) != file.Bytes || fmt.Sprintf("%x", sha256.Sum256(decoded)) != file.SHA256 {
				callErr = fmt.Errorf("content verification failed for %s", file.Path)
			} else {
				s.ContentBytes = len(decoded)
			}
		}
		if callErr != nil {
			s.Error = callErr.Error()
			p.Errors++
		}
		if len(p.Samples) == 0 && callErr == nil && launch != 0 {
			p.LaunchFirstUsefulNs = benchclock.Since(launch).Nanoseconds()
		}
		p.Samples = append(p.Samples, s)
	}
	p.Completed = time.Now()
	if !insertionStart.IsZero() {
		p.MaxAgeNs = p.Completed.Sub(insertionStart).Nanoseconds()
		if p.MaxAgeNs >= int64(10*time.Minute) {
			return p, fmt.Errorf("aged read exceeded 10m window")
		}
	}
	p.After, err = readCacheCounters(c)
	if err != nil {
		return p, err
	}
	p.Hits = p.After.Hits - p.Before.Hits
	p.Misses = p.After.Misses - p.Before.Misses
	p.Loads = p.After.Loads - p.Before.Loads
	entries, err := loadProxyLog(filepath.Join(logDir, "proxy.jsonl"))
	if err != nil {
		return p, err
	}
	byID := map[string]proxyEntry{}
	for _, entry := range entries {
		if entry.Tool == "read_file" {
			if _, exists := byID[entry.RequestID]; exists {
				return p, fmt.Errorf("duplicate proxy request ID %s", entry.RequestID)
			}
			byID[entry.RequestID] = entry
		}
	}
	var proxyNS, runnerNS []int64
	for i := range p.Samples {
		s := &p.Samples[i]
		e, ok := byID[s.RequestID]
		if !ok || e.DurationNs <= 0 {
			return p, fmt.Errorf("missing high resolution proxy sample %s", s.RequestID)
		}
		s.ProxyNs, s.BytesIn, s.BytesOut = e.DurationNs, e.BytesIn, e.BytesOut
		if e.Status != "ok" && s.Error == "" {
			s.Error = e.Status + ": " + e.Error
			p.Errors++
		}
		proxyNS = append(proxyNS, s.ProxyNs)
		runnerNS = append(runnerNS, s.RunnerNs)
		p.BytesIn += s.BytesIn
		p.BytesOut += s.BytesOut
	}
	p.P50Ns, p.P95Ns = percentileNS(proxyNS, .5), percentileNS(proxyNS, .95)
	p.RunnerP50Ns, p.RunnerP95Ns = percentileNS(runnerNS, .5), percentileNS(runnerNS, .95)
	return p, nil
}

func validateCachePhase(p cachePhase, ttl string, count int) error {
	wantHits := int64(0)
	if p.Name == "immediate" || (p.Name == "aged" && ttl == "10m") {
		wantHits = int64(count)
	}
	if p.Errors != 0 || p.Hits != wantHits || p.Misses != int64(count)-wantHits || p.Loads != p.Misses {
		return fmt.Errorf("%s/%s: errors=%d hits=%d misses=%d loads=%d; want hits=%d count=%d", ttl, p.Name, p.Errors, p.Hits, p.Misses, p.Loads, wantHits, count)
	}
	if p.After.Prefetch != 0 || p.After.PrefetchHits != 0 || p.After.PrefetchMisses != 0 {
		return fmt.Errorf("unexpected prefetch activity")
	}
	return nil
}

func runCacheBaseline(proxy, server, dir, out string, wait time.Duration, repetitions int) (retErr error) {
	order, err := cacheOrder(repetitions, wait)
	if err != nil {
		return err
	}
	if dir == "" {
		return fmt.Errorf("-cache-dir is required; artifacts are retained")
	}
	root, err := os.MkdirTemp(dir, "cache-run-")
	if err != nil {
		return err
	}
	report := cacheReport{Generated: time.Now().UTC(), Workspace: root, WaitNs: int64(wait), Environment: map[string]string{},
		Method: "Synthetic local corpus; 24 isolated files, 1/8/32/128 KiB x6; base64 full read_file with SHA256 verification. Alternating 3m/10m, independent live MCPs, sequential measurement, shared idle wait from latest initial completion (upper bound on last insertion). Immediate reads must be measured hits with no loads; no TTL renewal. No OS cache purge: seeding already warms OS cache; restart means cold MCP RAM, OS cache left warm but not directly measured. Proxy ns excludes response log write/forward; runner ns includes wire decoding; launch-first-useful includes initialize, discovery and stats before first validated read. Resident tracked bytes can retain expired entries until demand lookup; capacity is not RSS. Nearest-rank percentiles; raw samples retained."}
	defer func() {
		for _, run := range report.Runs {
			if run.client != nil {
				run.client.close()
			}
		}
		report.Complete = retErr == nil
		if retErr != nil {
			report.Failure = retErr.Error()
		}
		b, err := json.MarshalIndent(report, "", "  ")
		if err == nil {
			err = os.WriteFile(out, append(b, '\n'), 0600)
		}
		if err != nil {
			retErr = fmt.Errorf("report write: %w (experiment error: %v)", err, retErr)
		}
	}()
	report.Environment["platform"] = runtime.GOOS + "/" + runtime.GOARCH
	report.Environment["go"] = runtime.Version()
	report.Environment["cpus"] = fmt.Sprint(runtime.NumCPU())
	report.Environment["command"] = strings.Join(os.Args, " ")
	report.Environment["flags"] = "--profile ultra --compact-mode --roots-mode union --cache-ttl {3m|10m} --backup-dir <isolated>; cache-size default; proxy --reap-stale=false"
	for key, args := range map[string][]string{"commit": {"rev-parse", "HEAD"}, "dirty": {"status", "--short"}} {
		b, err := exec.Command("git", args...).Output()
		if err != nil {
			return err
		}
		report.Environment[key] = strings.TrimSpace(string(b))
	}
	for key, path := range map[string]string{"proxy": proxy, "server": server} {
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		report.Environment[key+"_sha256"] = fmt.Sprintf("%x", sha256.Sum256(b))
		report.Environment[key+"_path"] = path
	}
	report.Corpus, err = seedCacheCorpus(filepath.Join(root, "corpus"))
	if err != nil {
		return err
	}
	start := func(run *cacheRun, restart bool) error {
		logDir := run.LogDir
		if restart {
			logDir = run.RestartLogDir
		}
		if err := os.MkdirAll(logDir, 0700); err != nil {
			return err
		}
		run.launch = benchclock.Now()
		c, err := startRPC(proxy, logDir, server, filepath.Join(root, "corpus"), "ultra", "--cache-ttl", run.TTL, "--backup-dir", filepath.Join(logDir, "backups"))
		if err != nil {
			return err
		}
		run.client = c
		res, err := c.tool("list_allowed_directories", map[string]any{})
		if err != nil {
			return err
		}
		if isError(res) {
			return fmt.Errorf("discovery failed: %s", resultText(res))
		}
		return nil
	}
	measure := func(run *cacheRun, name string) error {
		logDir, launch := run.LogDir, int64(0)
		if name == "initial" || name == "restart" {
			launch = run.launch
		}
		if name == "restart" {
			logDir = run.RestartLogDir
		}
		a, b := time.Time{}, time.Time{}
		if name == "aged" {
			a, b = run.firstInsertionBound, run.lastInsertionBound
		}
		p, err := measureCachePhase(run.client, report.Corpus, name, logDir, launch, a, b)
		run.Phases = append(run.Phases, p)
		if err != nil {
			return err
		}
		if name == "initial" {
			run.firstInsertionBound, run.lastInsertionBound = p.Started, p.Completed
		}
		fmt.Printf("TTL=%s repetition=%d %s: p50=%.3fms p95=%.3fms hits=%d misses=%d errors=%d\n", run.TTL, run.Repetition, name, float64(p.P50Ns)/1e6, float64(p.P95Ns)/1e6, p.Hits, p.Misses, p.Errors)
		return validateCachePhase(p, run.TTL, len(report.Corpus))
	}
	for i, ttl := range order {
		run := &cacheRun{TTL: ttl, Repetition: i/2 + 1, LogDir: filepath.Join(root, fmt.Sprintf("session-%02d", i)), RestartLogDir: filepath.Join(root, fmt.Sprintf("restart-%02d", i))}
		report.Runs = append(report.Runs, run)
		if err := start(run, false); err != nil {
			return err
		}
		if err := measure(run, "initial"); err != nil {
			return err
		}
		if err := measure(run, "immediate"); err != nil {
			return err
		}
	}
	deadline := report.Runs[len(report.Runs)-1].lastInsertionBound.Add(wait)
	fmt.Printf("Idle until %s; no corpus reads during wait. Workspace: %s\n", deadline.Format(time.RFC3339), root)
	if delay := time.Until(deadline); delay > 0 {
		time.Sleep(delay)
	}
	for _, run := range report.Runs {
		if err := measure(run, "aged"); err != nil {
			return err
		}
	}
	for _, run := range report.Runs {
		run.client.close()
		run.client = nil
		if err := start(run, true); err != nil {
			return err
		}
		if err := measure(run, "restart"); err != nil {
			return err
		}
		run.client.close()
		run.client = nil
	}
	fmt.Printf("Cache baseline completed: %s\n", out)
	return nil
}
