package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestCachePlanRealTTLs(t *testing.T) {
	order, err := cacheOrder(3, 190*time.Second)
	if err != nil || !reflect.DeepEqual(order, []string{"3m", "10m", "3m", "10m", "3m", "10m"}) {
		t.Fatalf("%v %v", order, err)
	}
	for _, wait := range []time.Duration{time.Second, 3 * time.Minute, 10 * time.Minute} {
		if _, err := cacheOrder(3, wait); err == nil {
			t.Fatalf("accepted %s", wait)
		}
	}
	if _, err := cacheOrder(0, 190*time.Second); err == nil {
		t.Fatal("accepted no repetitions")
	}
}

func TestCachePercentilesSubMillisecond(t *testing.T) {
	v := []int64{999999, 200, 100, 300, 400}
	if percentileNS(v, .5) != 300 || percentileNS(v, .95) != 999999 || v[0] != 999999 {
		t.Fatal("precision, rank or input mutation")
	}
	if percentileNS(nil, .95) != 0 {
		t.Fatal("empty")
	}
}

func TestCacheScenarioGate(t *testing.T) {
	for _, ttl := range []string{"3m", "10m"} {
		for _, name := range []string{"initial", "immediate", "aged", "restart"} {
			p := cachePhase{Name: name, Misses: 24, Loads: 24}
			if name == "immediate" || name == "aged" && ttl == "10m" {
				p.Hits, p.Misses, p.Loads = 24, 0, 0
			}
			if err := validateCachePhase(p, ttl, 24); err != nil {
				t.Fatal(err)
			}
			bad := p
			bad.Errors++
			if validateCachePhase(bad, ttl, 24) == nil {
				t.Fatal("accepted error")
			}
			bad = p
			bad.After.Prefetch = 1
			if validateCachePhase(bad, ttl, 24) == nil {
				t.Fatal("accepted incidental prefetch")
			}
			bad = p
			bad.Hits++
			if validateCachePhase(bad, ttl, 24) == nil {
				t.Fatal("accepted unmeasured hit")
			}
		}
	}
}

func TestCacheCorpusIsolatedAndStable(t *testing.T) {
	files, err := seedCacheCorpus(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	other, err := seedCacheCorpus(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 24 {
		t.Fatal(len(files))
	}
	for i, f := range files {
		entries, err := os.ReadDir(filepath.Dir(f.Path))
		if err != nil || len(entries) != 1 {
			t.Fatal("prefetch siblings", err)
		}
		b, err := os.ReadFile(f.Path)
		if err != nil {
			t.Fatal(err)
		}
		if len(b) != f.Bytes || fmt.Sprintf("%x", sha256.Sum256(b)) != f.SHA256 || f.SHA256 != other[i].SHA256 {
			t.Fatal("unstable corpus")
		}
	}
}
