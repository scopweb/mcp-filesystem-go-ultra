package main

import (
	"reflect"
	"testing"
)

func TestArgumentKeys(t *testing.T) {
	if got := argumentKeys(nil); got != nil {
		t.Fatalf("no arguments must give no keys: %#v", got)
	}
	got := argumentKeys(map[string]any{
		"path":       "C:/tmp/a.txt",
		"end_line":   float64(20),
		"start_line": float64(1),
	})
	want := []string{"end_line", "path", "start_line"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argumentKeys = %v, want %v", got, want)
	}
}

func TestTargetProfile(t *testing.T) {
	cases := []struct {
		name   string
		target []string
		want   string
	}{
		{"separate value", []string{"C:/bin/filesystem-ultra.exe", "--profile", "strict", "C:/tmp"}, "strict"},
		{"inline value", []string{"C:/bin/filesystem-ultra.exe", "--profile=agent", "C:/tmp"}, "agent"},
		{"default of the server", []string{"C:/bin/filesystem-ultra-v4-embed_rg.exe", "C:/tmp"}, "ultra"},
		{"another server", []string{"C:/bin/mssql.exe"}, ""},
		{"another server with a profile flag", []string{"C:/bin/other.exe", "--profile", "x"}, "x"},
		{"no target", nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := targetProfile(tc.target); got != tc.want {
				t.Fatalf("targetProfile(%v) = %q, want %q", tc.target, got, tc.want)
			}
		})
	}
}
