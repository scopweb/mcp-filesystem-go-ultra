package main

import (
	"testing"
	"time"
)

func validCacheReport() cacheReport {
	t0 := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	file := cacheFile{
		Path:   `C:\temp\isolated-00\content.txt`,
		Bytes:  5,
		SHA256: "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
	}
	sample := func(id string, ns int64) cacheSample {
		return cacheSample{RequestID: id, File: file.Path, RunnerNs: ns + 10, ProxyNs: ns, BytesIn: 2, BytesOut: 10, ContentBytes: file.Bytes}
	}
	phase := func(name string, start, end time.Time, before, after cacheCounters, hits, misses, loads, launch, minAge, maxAge, ns int64, id string) cachePhase {
		s := sample(id, ns)
		return cachePhase{
			Name: name, Started: start, Completed: end, Before: before, After: after,
			Hits: hits, Misses: misses, Loads: loads, MinAgeNs: minAge, MaxAgeNs: maxAge,
			LaunchFirstUsefulNs: launch, P50Ns: ns, P95Ns: ns, RunnerP50Ns: ns + 10, RunnerP95Ns: ns + 10,
			BytesIn: s.BytesIn, BytesOut: s.BytesOut, Samples: []cacheSample{s},
		}
	}
	empty := cacheCounters{Capacity: 100}
	loaded := cacheCounters{Misses: 1, Loads: 1, Resident: 5, Capacity: 100}
	warm := cacheCounters{Hits: 1, Misses: 1, Loads: 1, Resident: 5, Capacity: 100}
	aged3 := cacheCounters{Hits: 1, Misses: 2, Loads: 2, Resident: 5, Capacity: 100}
	aged10 := cacheCounters{Hits: 2, Misses: 1, Loads: 1, Resident: 5, Capacity: 100}
	restarted := cacheCounters{Misses: 1, Loads: 1, Resident: 5, Capacity: 100}
	run := func(ttl string, rep int, ns int64) *cacheRun {
		initialEnd := t0.Add(time.Second)
		agedStart := t0.Add(192 * time.Second)
		agedEnd := t0.Add(193 * time.Second)
		minAge := agedStart.Sub(initialEnd).Nanoseconds()
		maxAge := agedEnd.Sub(t0).Nanoseconds()
		afterImmediate, afterAged, hitsAged, missesAged, loadsAged := warm, aged3, int64(0), int64(1), int64(1)
		if ttl == "10m" {
			afterAged, hitsAged, missesAged, loadsAged = aged10, 1, 0, 0
		}
		prefix := ttl + "-" + time.Duration(ns).String()
		return &cacheRun{
			TTL: ttl, Repetition: rep, LogDir: `C:\temp\logs\` + prefix, RestartLogDir: `C:\temp\restarts\` + prefix,
			Phases: []cachePhase{
				phase("initial", t0, initialEnd, empty, loaded, 0, 1, 1, 1000, 0, 0, ns, prefix+"-i"),
				phase("immediate", initialEnd.Add(time.Millisecond), t0.Add(2*time.Second), loaded, afterImmediate, 1, 0, 0, 0, 0, 0, ns, prefix+"-m"),
				phase("aged", agedStart, agedEnd, afterImmediate, afterAged, hitsAged, missesAged, loadsAged, 0, minAge, maxAge, ns, prefix+"-a"),
				phase("restart", t0.Add(194*time.Second), t0.Add(195*time.Second), empty, restarted, 0, 1, 1, 1000, 0, 0, ns, prefix+"-r"),
			},
		}
	}
	return cacheReport{
		Complete: true, WaitNs: int64(190 * time.Second), Corpus: []cacheFile{file},
		Runs: []*cacheRun{run("3m", 1, 100), run("10m", 1, 200)},
	}
}

func TestCacheSummaryPoolsRawSamplesAndRejectsPartial(t *testing.T) {
	base := validCacheReport()
	var runs []*cacheRun
	for i, ns := range []int64{100, 200, 900} {
		a := validCacheReport()
		a.Runs[0].Repetition = i + 1
		a.Runs[1].Repetition = i + 1
		a.Runs[0].LogDir += string(rune('a' + i))
		a.Runs[0].RestartLogDir += string(rune('a' + i))
		a.Runs[1].LogDir += string(rune('a' + i))
		a.Runs[1].RestartLogDir += string(rune('a' + i))
		for _, run := range a.Runs {
			for pi := range run.Phases {
				p := &run.Phases[pi]
				p.P50Ns, p.P95Ns, p.RunnerP50Ns, p.RunnerP95Ns = ns, ns, ns+10, ns+10
				p.Samples[0].ProxyNs, p.Samples[0].RunnerNs = ns, ns+10
				p.Samples[0].RequestID += string(rune('a' + i))
			}
		}
		runs = append(runs, a.Runs...)
	}
	base.Runs = runs
	rows, err := summarizeCache(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 8 {
		t.Fatal(len(rows))
	}
	for _, row := range rows {
		if row.Calls != 3 || row.P50Ns != 200 || row.P95Ns != 900 || row.ContentBytes != 15 || row.BytesIn != 6 || row.BytesOut != 30 {
			t.Fatalf("%+v", row)
		}
	}
	partial := validCacheReport()
	partial.Complete = false
	if _, err := summarizeCache(partial); err == nil {
		t.Fatal("accepted incomplete report")
	}
	partial = validCacheReport()
	partial.Runs[0].Phases = partial.Runs[0].Phases[:3]
	if _, err := summarizeCache(partial); err == nil {
		t.Fatal("accepted missing phase")
	}
	partial = validCacheReport()
	partial.Runs[0].Phases[2].MinAgeNs = 0
	if _, err := summarizeCache(partial); err == nil {
		t.Fatal("accepted missing ages")
	}
}
