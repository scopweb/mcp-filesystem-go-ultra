package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// Summaries pool raw samples, never average percentiles across repetitions.
type cacheSummaryRow struct {
	TTL            string  `json:"ttl"`
	Phase          string  `json:"phase"`
	Calls          int     `json:"calls"`
	Errors         int     `json:"errors"`
	Hits           int64   `json:"hits"`
	Misses         int64   `json:"misses"`
	Loads          int64   `json:"disk_loads"`
	ContentBytes   int64   `json:"content_bytes"`
	BytesIn        int64   `json:"bytes_in"`
	BytesOut       int64   `json:"bytes_out"`
	P50Ns          int64   `json:"proxy_p50_ns"`
	P95Ns          int64   `json:"proxy_p95_ns"`
	RunnerP50Ns    int64   `json:"runner_p50_ns"`
	RunnerP95Ns    int64   `json:"runner_p95_ns"`
	P50ByRun       []int64 `json:"proxy_p50_by_run_ns"`
	P95ByRun       []int64 `json:"proxy_p95_by_run_ns"`
	LaunchByRun    []int64 `json:"launch_first_useful_by_run_ns,omitempty"`
	AgeMinNs       int64   `json:"age_min_ns,omitempty"`
	AgeMaxNs       int64   `json:"age_max_ns,omitempty"`
	ResidentBefore []int64 `json:"resident_before_by_run"`
	ResidentAfter  []int64 `json:"resident_after_by_run"`
}

func summarizeCache(r cacheReport) ([]cacheSummaryRow, error) {
	if err := validateCacheEvidence(r); err != nil {
		return nil, err
	}
	var rows []cacheSummaryRow
	for _, ttl := range []string{"3m", "10m"} {
		for _, name := range []string{"initial", "immediate", "aged", "restart"} {
			row := cacheSummaryRow{TTL: ttl, Phase: name}
			var proxy, runner []int64
			for _, run := range r.Runs {
				if run.TTL != ttl {
					continue
				}
				for _, p := range run.Phases {
					if p.Name != name {
						continue
					}
					row.Errors += p.Errors
					row.Hits += p.Hits
					row.Misses += p.Misses
					row.Loads += p.Loads
					row.P50ByRun = append(row.P50ByRun, p.P50Ns)
					row.P95ByRun = append(row.P95ByRun, p.P95Ns)
					row.ResidentBefore = append(row.ResidentBefore, p.Before.Resident)
					row.ResidentAfter = append(row.ResidentAfter, p.After.Resident)
					if p.LaunchFirstUsefulNs > 0 {
						row.LaunchByRun = append(row.LaunchByRun, p.LaunchFirstUsefulNs)
					}
					if p.MinAgeNs > 0 && (row.AgeMinNs == 0 || p.MinAgeNs < row.AgeMinNs) {
						row.AgeMinNs = p.MinAgeNs
					}
					if p.MaxAgeNs > row.AgeMaxNs {
						row.AgeMaxNs = p.MaxAgeNs
					}
					for _, s := range p.Samples {
						row.Calls++
						row.BytesIn += s.BytesIn
						row.BytesOut += s.BytesOut
						row.ContentBytes += int64(s.ContentBytes)
						proxy = append(proxy, s.ProxyNs)
						runner = append(runner, s.RunnerNs)
					}
				}
			}
			if row.Calls == 0 {
				return nil, fmt.Errorf("missing %s/%s samples", ttl, name)
			}
			row.P50Ns, row.P95Ns = percentileNS(proxy, .5), percentileNS(proxy, .95)
			row.RunnerP50Ns, row.RunnerP95Ns = percentileNS(runner, .5), percentileNS(runner, .95)
			rows = append(rows, row)
		}
	}
	return rows, nil
}

func writeCacheSummary(input, output string) error {
	b, err := os.ReadFile(input)
	if err != nil {
		return err
	}
	var r cacheReport
	if err := json.Unmarshal(b, &r); err != nil {
		return err
	}
	rows, err := summarizeCache(r)
	if err != nil {
		return err
	}
	b, err = json.MarshalIndent(rows, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(output, append(b, '\n'), 0600)
}
