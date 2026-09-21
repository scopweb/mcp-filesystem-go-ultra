package main

import (
	"strings"
	"testing"
	"time"
)

func TestValidateCacheEvidenceAcceptsCompleteAndRejectsGaps(t *testing.T) {
	r := validCacheReport()
	if err := validateCacheEvidence(r); err != nil {
		t.Fatal(err)
	}
	r.Complete = false
	if err := validateCacheEvidence(r); err == nil {
		t.Fatal("accepted incomplete")
	}
	r = validCacheReport()
	r.Runs[0].Phases[2].MinAgeNs = 0
	if err := validateCacheEvidence(r); err == nil || !strings.Contains(err.Error(), "age") {
		t.Fatalf("missing age: %v", err)
	}
	r = validCacheReport()
	r.Runs[0].Phases[0].Samples[0].ProxyNs = 0
	if err := validateCacheEvidence(r); err == nil || !strings.Contains(err.Error(), "invalid sample") {
		t.Fatalf("zero duration: %v", err)
	}
	r = validCacheReport()
	r.Runs[0].Phases[1].Hits = 0
	r.Runs[0].Phases[1].Misses = 1
	if err := validateCacheEvidence(r); err == nil {
		t.Fatal("accepted After-Before mismatch")
	}
	r = validCacheReport()
	r.Runs[0].Phases[1].Samples[0].RequestID = r.Runs[0].Phases[0].Samples[0].RequestID
	if err := validateCacheEvidence(r); err == nil || !strings.Contains(err.Error(), "duplicate request") {
		t.Fatalf("duplicate id: %v", err)
	}
	r = validCacheReport()
	r.Runs[0].Phases[2].Started = r.Runs[0].Phases[0].Completed.Add(time.Second)
	r.Runs[0].Phases[2].MinAgeNs = int64(time.Second)
	r.Runs[0].Phases[2].MaxAgeNs = int64(2 * time.Second)
	if err := validateCacheEvidence(r); err == nil || !strings.Contains(err.Error(), "age") {
		t.Fatalf("short age: %v", err)
	}
}
