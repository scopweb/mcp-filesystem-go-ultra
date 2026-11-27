package mcpserver

import (
	"bytes"
	"testing"
)

// read_perf_test.go — cost of the read projection (the part of read_file that
// turns file bytes into the response body).
//
// The projection receives the whole file as a string and splits it into
// lines before clamping the body to the spill budget. For a 5 MB file that
// copies 5 MB and materializes every line, only to return ~50 KB.

func benchContent(b *testing.B, size int) []byte {
	b.Helper()
	line := []byte("BENCH_READ_LINE_0123456789 padding for the read projection\n")
	return bytes.Repeat(line, size/len(line)+1)[:size]
}

func BenchmarkReadProjection_Small(b *testing.B) {
	raw := benchContent(b, 2*1024)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := projectReadCursor(string(raw), "x.txt", 0, 0, 0, "all", 0, 0, 0)
		if p.TotalLines == 0 {
			b.Fatal("no lines")
		}
	}
}

func BenchmarkReadProjection_5MB(b *testing.B) {
	raw := benchContent(b, 5*1024*1024)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := projectReadCursor(string(raw), "x.txt", 0, 0, 0, "all", 0, 0, 0)
		if p.TotalLines == 0 {
			b.Fatal("no lines")
		}
	}
}

// The string conversion alone, which the handler pays before projecting.
func BenchmarkReadProjection_StringCopy5MB(b *testing.B) {
	raw := benchContent(b, 5*1024*1024)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s := string(raw)
		if len(s) != len(raw) {
			b.Fatal("bad copy")
		}
	}
}

// Splitting alone: how many lines the projection materializes.
func BenchmarkReadProjection_Split5MB(b *testing.B) {
	raw := benchContent(b, 5*1024*1024)
	s := string(raw)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lines := splitContentLines(s)
		if len(lines) == 0 {
			b.Fatal("no lines")
		}
	}
}
