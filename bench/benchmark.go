package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/mcp/filesystem-ultra/mcp"

	"github.com/mcp/filesystem-ultra/cache"
	"github.com/mcp/filesystem-ultra/core"
	"github.com/mcp/filesystem-ultra/protocol"
)

// BenchmarkResults holds benchmark test results
type BenchmarkResults struct {
	TestName        string
	Operations      int
	TotalTime       time.Duration
	AverageTime     time.Duration
	OperationsPerSec float64
	CacheHitRate    float64
	MemoryUsage     int64
}

// BenchmarkSuite runs comprehensive performance tests
type BenchmarkSuite struct {
	engine    *core.UltraFastEngine
	testDir   string
	testFiles []string
}

func main() {
	fmt.Printf("🧪 MCP Filesystem Server Ultra-Fast Benchmark\n")
	fmt.Printf("🖥️ System: %s/%s, CPUs: %d\n", runtime.GOOS, runtime.GOARCH, runtime.NumCPU())
	fmt.Printf("📊 Go: %s\n\n", runtime.Version())

	// Initialize benchmark suite
	suite, err := NewBenchmarkSuite()
	if err != nil {
		log.Fatalf("Failed to initialize benchmark: %v", err)
	}
	defer suite.Cleanup()

	// Run benchmark tests
	results := suite.RunAllBenchmarks()

	// Print results
	suite.PrintResults(results)
}

// NewBenchmarkSuite creates a new benchmark suite
func NewBenchmarkSuite() (*BenchmarkSuite, error) {
	// Create temporary test directory
	testDir, err := ioutil.TempDir("", "mcp-fs-benchmark-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %v", err)
	}

	// Initialize ultra-fast engine
	cacheSystem, err := cache.NewIntelligentCache(50 * 1024 * 1024) // 50MB cache
	if err != nil {
		return nil, fmt.Errorf("failed to create cache: %v", err)
	}

	protocolHandler := protocol.NewOptimizedHandler(1024 * 1024) // 1MB threshold

	engine, err := core.NewUltraFastEngine(&core.Config{
		Cache:            cacheSystem,
		ProtocolHandler:  protocolHandler,
		ParallelOps:      runtime.NumCPU() * 2,
		VSCodeAPIEnabled: false,
		DebugMode:        false,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create engine: %v", err)
	}

	suite := &BenchmarkSuite{
		engine:  engine,
		testDir: testDir,
	}

	// Generate test files
	if err := suite.generateTestFiles(); err != nil {
		return nil, fmt.Errorf("failed to generate test files: %v", err)
	}

	return suite, nil
}

// generateTestFiles creates various test files for benchmarking
func (s *BenchmarkSuite) generateTestFiles() error {
	testCases := []struct {
		name    string
		size    int
		content string
	}{
		{"small.txt", 1024, "Small test file content"},
		{"medium.txt", 100 * 1024, "Medium sized file with repeated content. "},
		{"large.txt", 5 * 1024 * 1024, "Large file content for testing memory mapping. "},
		{"code.go", 10 * 1024, `package main

import "fmt"

func main() {
    fmt.Println("Hello, World!")
}
`},
	}

	for _, tc := range testCases {
		path := filepath.Join(s.testDir, tc.name)
		
		// Generate content of specified size
		content := tc.content
		for len(content) < tc.size {
			content += tc.content
		}
		content = content[:tc.size]

		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to create test file %s: %v", tc.name, err)
		}

		s.testFiles = append(s.testFiles, path)
	}

	// Create subdirectories with files
	subDir := filepath.Join(s.testDir, "subdir")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		return err
	}

	for i := 0; i < 100; i++ {
		path := filepath.Join(subDir, fmt.Sprintf("file%03d.txt", i))
		content := fmt.Sprintf("File number %d content", i)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return err
		}
	}

	return nil
}

// RunAllBenchmarks executes all benchmark tests
func (s *BenchmarkSuite) RunAllBenchmarks() []BenchmarkResults {
	var results []BenchmarkResults

	fmt.Printf("🚀 Starting benchmarks...\n\n")

	// File reading benchmarks
	results = append(results, s.benchmarkFileReading())
	results = append(results, s.benchmarkCachedReading())
	results = append(results, s.benchmarkDirectoryListing())
	results = append(results, s.benchmarkFileWriting())
	results = append(results, s.benchmarkMixedOperations())

	return results
}

// benchmarkFileReading tests file reading performance
func (s *BenchmarkSuite) benchmarkFileReading() BenchmarkResults {
	fmt.Printf("📖 Benchmarking file reading...\n")
	
	ctx := context.Background()
	operations := 0
	start := time.Now()

	// Read each test file multiple times
	for i := 0; i < 10; i++ {
		for _, filePath := range s.testFiles {
			request := mcp.CallToolRequest{
				Arguments: map[string]interface{}{
					"path": filePath,
				},
			}
			
			_, err := s.engine.ReadFile(ctx, request)
			if err != nil {
				log.Printf("Read error: %v", err)
				continue
			}
			operations++
		}
	}

	duration := time.Since(start)
	
	return BenchmarkResults{
		TestName:        "File Reading",
		Operations:      operations,
		TotalTime:       duration,
		AverageTime:     duration / time.Duration(operations),
		OperationsPerSec: float64(operations) / duration.Seconds(),
		CacheHitRate:    s.engine.Cache.GetHitRate(),
		MemoryUsage:     s.engine.Cache.GetMemoryUsage(),
	}
}

// benchmarkCachedReading tests cache performance
func (s *BenchmarkSuite) benchmarkCachedReading() BenchmarkResults {
	fmt.Printf("📦 Benchmarking cached reading...\n")
	
	ctx := context.Background()
	
	// Prime the cache
	for _, filePath := range s.testFiles {
		request := mcp.CallToolRequest{
			Arguments: map[string]interface{}{
				"path": filePath,
			},
		}
		s.engine.ReadFile(ctx, request)
	}

	// Now benchmark cached reads
	operations := 0
	start := time.Now()

	for i := 0; i < 100; i++ {
		for _, filePath := range s.testFiles {
			request := mcp.CallToolRequest{
				Arguments: map[string]interface{}{
					"path": filePath,
				},
			}
			
			_, err := s.engine.ReadFile(ctx, request)
			if err != nil {
				log.Printf("Cached read error: %v", err)
				continue
			}
			operations++
		}
	}

	duration := time.Since(start)
	
	return BenchmarkResults{
		TestName:        "Cached Reading",
		Operations:      operations,
		TotalTime:       duration,
		AverageTime:     duration / time.Duration(operations),
		OperationsPerSec: float64(operations) / duration.Seconds(),
		CacheHitRate:    s.engine.Cache.GetHitRate(),
		MemoryUsage:     s.engine.Cache.GetMemoryUsage(),
	}
}

// benchmarkDirectoryListing tests directory listing performance
func (s *BenchmarkSuite) benchmarkDirectoryListing() BenchmarkResults {
	fmt.Printf("📁 Benchmarking directory listing...\n")
	
	ctx := context.Background()
	operations := 0
	start := time.Now()

	// List directories multiple times
	for i := 0; i < 50; i++ {
		request := mcp.CallToolRequest{
			Arguments: map[string]interface{}{
				"path": s.testDir,
			},
		}
		
		_, err := s.engine.ListDirectory(ctx, request)
		if err != nil {
			log.Printf("Directory listing error: %v", err)
			continue
		}
		operations++

		// Also list subdirectory
		subDirRequest := mcp.CallToolRequest{
			Arguments: map[string]interface{}{
				"path": filepath.Join(s.testDir, "subdir"),
			},
		}
		
		_, err = s.engine.ListDirectory(ctx, subDirRequest)
		if err == nil {
			operations++
		}
	}

	duration := time.Since(start)
	
	return BenchmarkResults{
		TestName:        "Directory Listing",
		Operations:      operations,
		TotalTime:       duration,
		AverageTime:     duration / time.Duration(operations),
		OperationsPerSec: float64(operations) / duration.Seconds(),
		CacheHitRate:    s.engine.Cache.GetHitRate(),
		MemoryUsage:     s.engine.Cache.GetMemoryUsage(),
	}
}

// benchmarkFileWriting tests file writing performance
func (s *BenchmarkSuite) benchmarkFileWriting() BenchmarkResults {
	fmt.Printf("✍️ Benchmarking file writing...\n")
	
	ctx := context.Background()
	operations := 0
	start := time.Now()

	// Write multiple files
	for i := 0; i < 100; i++ {
		filePath := filepath.Join(s.testDir, fmt.Sprintf("write_test_%d.txt", i))
		content := fmt.Sprintf("Test file %d content with some data to write", i)
		
		request := mcp.CallToolRequest{
			Arguments: map[string]interface{}{
				"path":    filePath,
				"content": content,
			},
		}
		
		_, err := s.engine.WriteFile(ctx, request)
		if err != nil {
			log.Printf("Write error: %v", err)
			continue
		}
		operations++
	}

	duration := time.Since(start)
	
	return BenchmarkResults{
		TestName:        "File Writing",
		Operations:      operations,
		TotalTime:       duration,
		AverageTime:     duration / time.Duration(operations),
		OperationsPerSec: float64(operations) / duration.Seconds(),
		CacheHitRate:    s.engine.Cache.GetHitRate(),
		MemoryUsage:     s.engine.Cache.GetMemoryUsage(),
	}
}

// benchmarkMixedOperations tests mixed read/write/list operations
func (s *BenchmarkSuite) benchmarkMixedOperations() BenchmarkResults {
	fmt.Printf("🔄 Benchmarking mixed operations...\n")
	
	ctx := context.Background()
	operations := 0
	start := time.Now()

	// Mixed operations simulation
	for i := 0; i < 50; i++ {
		// Read a file
		if len(s.testFiles) > 0 {
			filePath := s.testFiles[i%len(s.testFiles)]
			request := mcp.CallToolRequest{
				Arguments: map[string]interface{}{
					"path": filePath,
				},
			}
			
			if _, err := s.engine.ReadFile(ctx, request); err == nil {
				operations++
			}
		}

		// List directory
		dirRequest := mcp.CallToolRequest{
			Arguments: map[string]interface{}{
				"path": s.testDir,
			},
		}
		
		if _, err := s.engine.ListDirectory(ctx, dirRequest); err == nil {
			operations++
		}

		// Write a small file
		writeFilePath := filepath.Join(s.testDir, fmt.Sprintf("mixed_test_%d.txt", i))
		writeContent := fmt.Sprintf("Mixed operation test %d", i)
		
		writeRequest := mcp.CallToolRequest{
			Arguments: map[string]interface{}{
				"path":    writeFilePath,
				"content": writeContent,
			},
		}
		
		if _, err := s.engine.WriteFile(ctx, writeRequest); err == nil {
			operations++
		}
	}

	duration := time.Since(start)
	
	return BenchmarkResults{
		TestName:        "Mixed Operations",
		Operations:      operations,
		TotalTime:       duration,
		AverageTime:     duration / time.Duration(operations),
		OperationsPerSec: float64(operations) / duration.Seconds(),
		CacheHitRate:    s.engine.Cache.GetHitRate(),
		MemoryUsage:     s.engine.Cache.GetMemoryUsage(),
	}
}

// PrintResults displays benchmark results in a formatted table
func (s *BenchmarkSuite) PrintResults(results []BenchmarkResults) {
	fmt.Printf("\n📊 Benchmark Results\n")
	fmt.Printf("═══════════════════════════════════════════════════════════════════════════════\n")
	fmt.Printf("%-20s %8s %12s %12s %12s %8s %10s\n", 
		"Test", "Ops", "Total Time", "Avg Time", "Ops/Sec", "Cache%", "Memory")
	fmt.Printf("───────────────────────────────────────────────────────────────────────────────\n")

	for _, result := range results {
		fmt.Printf("%-20s %8d %12s %12s %12.1f %7.1f%% %10s\n",
			result.TestName,
			result.Operations,
			formatDuration(result.TotalTime),
			formatDuration(result.AverageTime),
			result.OperationsPerSec,
			result.CacheHitRate*100,
			formatSize(result.MemoryUsage))
	}

	fmt.Printf("═══════════════════════════════════════════════════════════════════════════════\n")

	// Overall performance summary
	totalOps := 0
	totalTime := time.Duration(0)
	for _, result := range results {
		totalOps += result.Operations
		totalTime += result.TotalTime
	}

	avgOpsPerSec := float64(totalOps) / totalTime.Seconds()
	
	fmt.Printf("\n🎯 Overall Performance Summary:\n")
	fmt.Printf("   Total Operations: %d\n", totalOps)
	fmt.Printf("   Total Time: %s\n", formatDuration(totalTime))
	fmt.Printf("   Average Ops/Sec: %.1f\n", avgOpsPerSec)
	fmt.Printf("   Final Cache Hit Rate: %.1f%%\n", results[len(results)-1].CacheHitRate*100)
	fmt.Printf("   Peak Memory Usage: %s\n", formatSize(results[len(results)-1].MemoryUsage))

	// Performance tier classification
	fmt.Printf("\n⚡ Performance Classification:\n")
	if avgOpsPerSec > 1000 {
		fmt.Printf("   🔥 ULTRA-FAST: Exceeds target performance!\n")
	} else if avgOpsPerSec > 500 {
		fmt.Printf("   🚀 HIGH: Very good performance\n")
	} else if avgOpsPerSec > 200 {
		fmt.Printf("   ⚡ GOOD: Acceptable performance\n")
	} else {
		fmt.Printf("   🐌 SLOW: Needs optimization\n")
	}

	fmt.Printf("\n💡 Recommendations:\n")
	lastResult := results[len(results)-1]
	if lastResult.CacheHitRate < 0.8 {
		fmt.Printf("   • Consider increasing cache size for better hit rates\n")
	}
	if lastResult.MemoryUsage > 80*1024*1024 {
		fmt.Printf("   • Memory usage is high, consider cache tuning\n")
	}
	if avgOpsPerSec < 500 {
		fmt.Printf("   • Performance is below target, review optimization strategies\n")
	}
}

// Cleanup removes temporary test files
func (s *BenchmarkSuite) Cleanup() {
	if s.engine != nil {
		s.engine.Close()
	}
	if s.testDir != "" {
		os.RemoveAll(s.testDir)
	}
}

// Helper functions

func formatDuration(d time.Duration) string {
	if d < time.Microsecond {
		return fmt.Sprintf("%dns", d.Nanoseconds())
	} else if d < time.Millisecond {
		return fmt.Sprintf("%.1fμs", float64(d.Nanoseconds())/1000.0)
	} else if d < time.Second {
		return fmt.Sprintf("%.1fms", float64(d.Nanoseconds())/1000000.0)
	} else {
		return fmt.Sprintf("%.2fs", d.Seconds())
	}
}

func formatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%dB", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
