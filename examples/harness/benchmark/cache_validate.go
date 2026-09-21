package main

import (
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// JSON timestamps drop Go's monotonic clock. Windows wall time can then
// disagree with stored MinAgeNs/MaxAgeNs by several milliseconds. 100ms
// covers that round-trip; it does not relax the 3m/10m TTL bounds.
const cacheClockTolerance = int64(100 * time.Millisecond)

func cacheIdentity(path string) string {
	path = filepath.Clean(path)
	if len(path) > 1 && path[1] == ':' || strings.HasPrefix(path, `\\`) {
		return strings.ToLower(strings.ReplaceAll(path, `\`, "/"))
	}
	return path
}

func validateCacheEvidence(r cacheReport) error {
	if !r.Complete || r.Failure != "" || len(r.Corpus) == 0 || len(r.Runs) == 0 || len(r.Runs)%2 != 0 {
		return fmt.Errorf("incomplete cache evidence")
	}
	order, err := cacheOrder(len(r.Runs)/2, time.Duration(r.WaitNs))
	if err != nil {
		return err
	}
	corpus := map[string]cacheFile{}
	var contentBytes int64
	for _, f := range r.Corpus {
		key := cacheIdentity(f.Path)
		hash, err := hex.DecodeString(f.SHA256)
		if f.Path == "" || f.Bytes <= 0 || err != nil || len(hash) != 32 {
			return fmt.Errorf("invalid corpus identity: %q", f.Path)
		}
		if _, ok := corpus[key]; ok {
			return fmt.Errorf("duplicate corpus file: %s", f.Path)
		}
		corpus[key] = f
		contentBytes += int64(f.Bytes)
	}
	logs := map[string]bool{}
	var latestInitial time.Time
	for i, run := range r.Runs {
		if run == nil || run.TTL != order[i] || run.Repetition != i/2+1 || len(run.Phases) != 4 {
			return fmt.Errorf("invalid run identity/order at %d", i)
		}
		for _, dir := range []string{run.LogDir, run.RestartLogDir} {
			key := cacheIdentity(dir)
			if dir == "" || logs[key] {
				return fmt.Errorf("missing/reused process log directory: %s", dir)
			}
			logs[key] = true
		}
		seenIDs := map[string]bool{}
		for j, p := range run.Phases {
			label := fmt.Sprintf("run %d %s", i, p.Name)
			if p.Name != []string{"initial", "immediate", "aged", "restart"}[j] || len(p.Samples) != len(corpus) {
				return fmt.Errorf("%s: phase/sample count", label)
			}
			if p.Started.IsZero() || !p.Completed.After(p.Started) || j > 0 && p.Started.Before(run.Phases[j-1].Completed) {
				return fmt.Errorf("%s: invalid chronology", label)
			}
			if j == 0 || j == 3 {
				seenIDs = map[string]bool{}
				if p.Before.Hits != 0 || p.Before.Misses != 0 || p.Before.Loads != 0 || p.Before.Resident != 0 || p.LaunchFirstUsefulNs <= 0 {
					return fmt.Errorf("%s: missing fresh-process evidence", label)
				}
			} else if p.Before != run.Phases[j-1].After || p.LaunchFirstUsefulNs != 0 {
				return fmt.Errorf("%s: counter continuity/startup mismatch", label)
			}
			for _, s := range []cacheCounters{p.Before, p.After} {
				if s.Hits < 0 || s.Misses < 0 || s.Loads < 0 || s.Resident < 0 || s.Capacity <= 0 || s.Prefetch != 0 || s.PrefetchHits != 0 || s.PrefetchMisses != 0 {
					return fmt.Errorf("%s: invalid snapshot/prefetch", label)
				}
			}
			if p.After.Resident != contentBytes || p.Hits != p.After.Hits-p.Before.Hits || p.Misses != p.After.Misses-p.Before.Misses || p.Loads != p.After.Loads-p.Before.Loads {
				return fmt.Errorf("%s: snapshot delta/resident mismatch", label)
			}
			if err := validateCachePhase(p, run.TTL, len(corpus)); err != nil {
				return err
			}
			if j == 2 {
				low := p.Started.Sub(run.Phases[0].Completed).Nanoseconds()
				high := p.Completed.Sub(run.Phases[0].Started).Nanoseconds()
				if p.MinAgeNs <= int64(3*time.Minute) || p.MaxAgeNs >= int64(10*time.Minute) || p.MinAgeNs > p.MaxAgeNs || low <= int64(3*time.Minute) || high >= int64(10*time.Minute) || absCache(p.MinAgeNs-low) > cacheClockTolerance || absCache(p.MaxAgeNs-high) > cacheClockTolerance {
					return fmt.Errorf("%s: invalid/missing age evidence", label)
				}
			} else if p.MinAgeNs != 0 || p.MaxAgeNs != 0 {
				return fmt.Errorf("%s: unexpected age evidence", label)
			}
			seenFiles := map[string]bool{}
			var proxy, runner []int64
			var bytesIn, bytesOut int64
			for _, s := range p.Samples {
				key := cacheIdentity(s.File)
				f, ok := corpus[key]
				if !ok || seenFiles[key] {
					return fmt.Errorf("%s: unknown/duplicate sample file %s", label, s.File)
				}
				seenFiles[key] = true
				if strings.TrimSpace(s.RequestID) == "" || seenIDs[s.RequestID] {
					return fmt.Errorf("%s: missing/duplicate request ID %s", label, s.RequestID)
				}
				seenIDs[s.RequestID] = true
				if s.Error != "" || s.ProxyNs <= 0 || s.RunnerNs <= 0 || s.BytesIn <= 0 || s.BytesOut <= 0 || s.ContentBytes != f.Bytes {
					return fmt.Errorf("%s: invalid sample %s", label, s.RequestID)
				}
				bytesIn += s.BytesIn
				bytesOut += s.BytesOut
				proxy = append(proxy, s.ProxyNs)
				runner = append(runner, s.RunnerNs)
			}
			if p.BytesIn != bytesIn || p.BytesOut != bytesOut || p.P50Ns != percentileNS(proxy, .5) || p.P95Ns != percentileNS(proxy, .95) || p.RunnerP50Ns != percentileNS(runner, .5) || p.RunnerP95Ns != percentileNS(runner, .95) {
				return fmt.Errorf("%s: sample aggregate mismatch", label)
			}
		}
		if run.Phases[0].Completed.After(latestInitial) {
			latestInitial = run.Phases[0].Completed
		}
	}
	for _, run := range r.Runs {
		if run.Phases[2].Started.Sub(latestInitial).Nanoseconds()+cacheClockTolerance < r.WaitNs {
			return fmt.Errorf("aged phase precedes shared insertion deadline")
		}
	}
	return nil
}

func absCache(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}
