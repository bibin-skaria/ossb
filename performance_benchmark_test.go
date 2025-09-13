// +build integration

package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bibin-skaria/ossb/engine"
	"github.com/bibin-skaria/ossb/internal/types"
)

// BenchmarkResult represents the results of a benchmark test
type BenchmarkResult struct {
	Tool           string
	TestCase       string
	Duration       time.Duration
	MemoryUsage    int64 // in bytes
	CacheHits      int
	Operations     int
	Success        bool
	Error          string
	ImageSize      int64 // in bytes
	LayerCount     int
}

// TestPerformanceBenchmarkSimpleBuilds benchmarks simple build scenarios
func TestPerformanceBenchmarkSimpleBuilds(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance benchmark in short mode")
	}

	testCases := []struct {
		name       string
		dockerfile string
		files      map[string]string
	}{
		{
			name: "alpine_simple",
			dockerfile: `FROM alpine:latest
RUN echo "Hello World" > /hello.txt
CMD ["cat", "/hello.txt"]`,
			files: map[string]string{},
		},
		{
			name: "ubuntu_with_packages",
			dockerfile: `FROM ubuntu:20.04
RUN apt-get update && apt-get install -y curl wget
RUN echo "Ubuntu with packages" > /info.txt
CMD ["cat", "/info.txt"]`,
			files: map[string]string{},
		},
		{
			name: "node_app",
			dockerfile: `FROM node:18-alpine
WORKDIR /app
COPY package.json .
RUN npm install
COPY . .
CMD ["npm", "start"]`,
			files: map[string]string{
				"package.json": `{
  "name": "test-app",
  "version": "1.0.0",
  "scripts": {
    "start": "node index.js"
  },
  "dependencies": {
    "express": "^4.18.0"
  }
}`,
				"index.js": `const express = require('express');
const app = express();
app.get('/', (req, res) => res.send('Hello World!'));
app.listen(3000);`,
			},
		},
		{
			name: "go_app",
			dockerfile: `FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o main .

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/main .
CMD ["./main"]`,
			files: map[string]string{
				"go.mod": `module test-app
go 1.21`,
				"go.sum": "",
				"main.go": `package main
import (
	"fmt"
	"net/http"
)
func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Hello, World!")
	})
	http.ListenAndServe(":8080", nil)
}`,
			},
		},
	}

	results := make([]BenchmarkResult, 0)

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Test OSSB
			ossbResult := benchmarkOSSB(t, tc.name, tc.dockerfile, tc.files)
			results = append(results, ossbResult)

			// Test Kaniko if available
			if isKanikoAvailable() {
				kanikoResult := benchmarkKaniko(t, tc.name, tc.dockerfile, tc.files)
				results = append(results, kanikoResult)
			}

			// Test BuildKit if available
			if isBuildKitAvailable() {
				buildkitResult := benchmarkBuildKit(t, tc.name, tc.dockerfile, tc.files)
				results = append(results, buildkitResult)
			}
		})
	}

	// Print comparison results
	printBenchmarkComparison(t, results)
}

// TestPerformanceBenchmarkMultiArch benchmarks multi-architecture builds
func TestPerformanceBenchmarkMultiArch(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping multi-arch performance benchmark in short mode")
	}

	dockerfile := `FROM alpine:latest
RUN apk add --no-cache file
RUN echo "Architecture: $(uname -m)" > /arch.txt
CMD ["cat", "/arch.txt"]`

	platforms := []types.Platform{
		{OS: "linux", Architecture: "amd64"},
		{OS: "linux", Architecture: "arm64"},
	}

	t.Run("multiarch_alpine", func(t *testing.T) {
		// Test OSSB multi-arch
		ossbResult := benchmarkOSSBMultiArch(t, "multiarch_alpine", dockerfile, map[string]string{}, platforms)
		
		// Test BuildKit multi-arch if available
		var buildkitResult BenchmarkResult
		if isBuildKitAvailable() {
			buildkitResult = benchmarkBuildKitMultiArch(t, "multiarch_alpine", dockerfile, map[string]string{}, platforms)
		}

		// Compare results
		t.Logf("Multi-arch benchmark results:")
		t.Logf("  OSSB: %v (success: %t)", ossbResult.Duration, ossbResult.Success)
		if buildkitResult.Tool != "" {
			t.Logf("  BuildKit: %v (success: %t)", buildkitResult.Duration, buildkitResult.Success)
			if ossbResult.Success && buildkitResult.Success {
				ratio := float64(ossbResult.Duration) / float64(buildkitResult.Duration)
				t.Logf("  OSSB/BuildKit ratio: %.2fx", ratio)
			}
		}
	})
}

// TestPerformanceBenchmarkCaching benchmarks caching performance
func TestPerformanceBenchmarkCaching(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping caching performance benchmark in short mode")
	}

	dockerfile := `FROM alpine:latest
RUN echo "Layer 1" > /layer1.txt
RUN echo "Layer 2" > /layer2.txt
RUN echo "Layer 3" > /layer3.txt
CMD ["cat", "/layer1.txt", "/layer2.txt", "/layer3.txt"]`

	t.Run("caching_performance", func(t *testing.T) {
		// First build (cold cache)
		coldResult := benchmarkOSSB(t, "caching_cold", dockerfile, map[string]string{})
		
		// Second build (warm cache)
		warmResult := benchmarkOSSB(t, "caching_warm", dockerfile, map[string]string{})

		// Compare results
		t.Logf("Caching benchmark results:")
		t.Logf("  Cold cache: %v (cache hits: %d)", coldResult.Duration, coldResult.CacheHits)
		t.Logf("  Warm cache: %v (cache hits: %d)", warmResult.Duration, warmResult.CacheHits)
		
		if warmResult.Success && coldResult.Success {
			speedup := float64(coldResult.Duration) / float64(warmResult.Duration)
			t.Logf("  Cache speedup: %.2fx", speedup)
			
			if warmResult.CacheHits <= coldResult.CacheHits {
				t.Errorf("Warm cache should have more hits: %d vs %d", warmResult.CacheHits, coldResult.CacheHits)
			}
		}
	})
}

// TestPerformanceBenchmarkMemoryUsage benchmarks memory usage
func TestPerformanceBenchmarkMemoryUsage(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping memory usage benchmark in short mode")
	}

	testCases := []struct {
		name       string
		dockerfile string
		files      map[string]string
	}{
		{
			name: "small_image",
			dockerfile: `FROM alpine:latest
RUN echo "small" > /size.txt
CMD ["cat", "/size.txt"]`,
			files: map[string]string{},
		},
		{
			name: "large_image",
			dockerfile: `FROM ubuntu:20.04
RUN apt-get update && apt-get install -y \
    build-essential \
    curl \
    wget \
    git \
    vim \
    python3 \
    python3-pip \
    nodejs \
    npm
RUN echo "large" > /size.txt
CMD ["cat", "/size.txt"]`,
			files: map[string]string{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := benchmarkOSSBWithMemoryTracking(t, tc.name, tc.dockerfile, tc.files)
			
			t.Logf("Memory usage benchmark for %s:", tc.name)
			t.Logf("  Duration: %v", result.Duration)
			t.Logf("  Peak memory: %s", formatBytes(result.MemoryUsage))
			t.Logf("  Success: %t", result.Success)
			
			// Memory usage thresholds
			maxMemoryMB := int64(2048) // 2GB
			if result.MemoryUsage > maxMemoryMB*1024*1024 {
				t.Errorf("Memory usage too high: %s > %dMB", formatBytes(result.MemoryUsage), maxMemoryMB)
			}
		})
	}
}

// TestPerformanceBenchmarkConcurrentBuilds benchmarks concurrent build performance
func TestPerformanceBenchmarkConcurrentBuilds(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping concurrent builds benchmark in short mode")
	}

	dockerfile := `FROM alpine:latest
RUN echo "Concurrent build test" > /test.txt
CMD ["cat", "/test.txt"]`

	concurrencyLevels := []int{1, 2, 4}
	
	for _, concurrency := range concurrencyLevels {
		t.Run(fmt.Sprintf("concurrent_%d", concurrency), func(t *testing.T) {
			startTime := time.Now()
			
			// Run concurrent builds
			results := make(chan BenchmarkResult, concurrency)
			for i := 0; i < concurrency; i++ {
				go func(buildNum int) {
					result := benchmarkOSSB(t, fmt.Sprintf("concurrent_%d_%d", concurrency, buildNum), dockerfile, map[string]string{})
					results <- result
				}(i)
			}
			
			// Collect results
			successCount := 0
			totalDuration := time.Duration(0)
			for i := 0; i < concurrency; i++ {
				result := <-results
				if result.Success {
					successCount++
					totalDuration += result.Duration
				}
			}
			
			overallDuration := time.Since(startTime)
			avgDuration := totalDuration / time.Duration(successCount)
			
			t.Logf("Concurrent builds benchmark (concurrency: %d):", concurrency)
			t.Logf("  Overall duration: %v", overallDuration)
			t.Logf("  Average build duration: %v", avgDuration)
			t.Logf("  Success rate: %d/%d", successCount, concurrency)
			
			if successCount < concurrency {
				t.Errorf("Some concurrent builds failed: %d/%d succeeded", successCount, concurrency)
			}
		})
	}
}

// Benchmark helper functions

func benchmarkOSSB(t *testing.T, testName, dockerfile string, files map[string]string) BenchmarkResult {
	tempDir, err := ioutil.TempDir("", fmt.Sprintf("ossb-bench-%s-", testName))
	if err != nil {
		return BenchmarkResult{Tool: "OSSB", TestCase: testName, Success: false, Error: err.Error()}
	}
	defer os.RemoveAll(tempDir)

	// Create files
	if err := createTestFiles(tempDir, dockerfile, files); err != nil {
		return BenchmarkResult{Tool: "OSSB", TestCase: testName, Success: false, Error: err.Error()}
	}

	config := &types.BuildConfig{
		Context:    tempDir,
		Dockerfile: "Dockerfile",
		Tags:       []string{fmt.Sprintf("ossb-bench-%s:latest", testName)},
		Output:     "image",
		Frontend:   "dockerfile",
		NoCache:    false,
		Progress:   false, // Disable progress for cleaner benchmarks
		BuildArgs:  map[string]string{},
		Platforms:  []types.Platform{{OS: "linux", Architecture: "amd64"}},
		Push:       false,
	}

	builder, err := engine.NewBuilder(config)
	if err != nil {
		return BenchmarkResult{Tool: "OSSB", TestCase: testName, Success: false, Error: err.Error()}
	}
	defer builder.Cleanup()

	startTime := time.Now()
	result, err := builder.Build()
	duration := time.Since(startTime)

	benchResult := BenchmarkResult{
		Tool:     "OSSB",
		TestCase: testName,
		Duration: duration,
		Success:  err == nil && result.Success,
	}

	if err != nil {
		benchResult.Error = err.Error()
	} else if !result.Success {
		benchResult.Error = result.Error
	} else {
		benchResult.CacheHits = result.CacheHits
		benchResult.Operations = result.Operations
	}

	return benchResult
}

func benchmarkOSSBMultiArch(t *testing.T, testName, dockerfile string, files map[string]string, platforms []types.Platform) BenchmarkResult {
	tempDir, err := ioutil.TempDir("", fmt.Sprintf("ossb-multiarch-bench-%s-", testName))
	if err != nil {
		return BenchmarkResult{Tool: "OSSB", TestCase: testName, Success: false, Error: err.Error()}
	}
	defer os.RemoveAll(tempDir)

	if err := createTestFiles(tempDir, dockerfile, files); err != nil {
		return BenchmarkResult{Tool: "OSSB", TestCase: testName, Success: false, Error: err.Error()}
	}

	config := &types.BuildConfig{
		Context:    tempDir,
		Dockerfile: "Dockerfile",
		Tags:       []string{fmt.Sprintf("ossb-multiarch-bench-%s:latest", testName)},
		Output:     "multiarch",
		Frontend:   "dockerfile",
		NoCache:    false,
		Progress:   false,
		BuildArgs:  map[string]string{},
		Platforms:  platforms,
		Push:       false,
	}

	builder, err := engine.NewBuilder(config)
	if err != nil {
		return BenchmarkResult{Tool: "OSSB", TestCase: testName, Success: false, Error: err.Error()}
	}
	defer builder.Cleanup()

	startTime := time.Now()
	result, err := builder.Build()
	duration := time.Since(startTime)

	benchResult := BenchmarkResult{
		Tool:     "OSSB",
		TestCase: testName,
		Duration: duration,
		Success:  err == nil && result.Success,
	}

	if err != nil {
		benchResult.Error = err.Error()
	} else if !result.Success {
		benchResult.Error = result.Error
	} else {
		benchResult.CacheHits = result.CacheHits
		benchResult.Operations = result.Operations
	}

	return benchResult
}

func benchmarkOSSBWithMemoryTracking(t *testing.T, testName, dockerfile string, files map[string]string) BenchmarkResult {
	tempDir, err := ioutil.TempDir("", fmt.Sprintf("ossb-memory-bench-%s-", testName))
	if err != nil {
		return BenchmarkResult{Tool: "OSSB", TestCase: testName, Success: false, Error: err.Error()}
	}
	defer os.RemoveAll(tempDir)

	if err := createTestFiles(tempDir, dockerfile, files); err != nil {
		return BenchmarkResult{Tool: "OSSB", TestCase: testName, Success: false, Error: err.Error()}
	}

	config := &types.BuildConfig{
		Context:    tempDir,
		Dockerfile: "Dockerfile",
		Tags:       []string{fmt.Sprintf("ossb-memory-bench-%s:latest", testName)},
		Output:     "image",
		Frontend:   "dockerfile",
		NoCache:    false,
		Progress:   false,
		BuildArgs:  map[string]string{},
		Platforms:  []types.Platform{{OS: "linux", Architecture: "amd64"}},
		Push:       false,
	}

	builder, err := engine.NewBuilder(config)
	if err != nil {
		return BenchmarkResult{Tool: "OSSB", TestCase: testName, Success: false, Error: err.Error()}
	}
	defer builder.Cleanup()

	// Start memory tracking
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	initialMemory := memStats.Alloc

	startTime := time.Now()
	result, err := builder.Build()
	duration := time.Since(startTime)

	// Get peak memory usage
	runtime.ReadMemStats(&memStats)
	peakMemory := memStats.Alloc - initialMemory

	benchResult := BenchmarkResult{
		Tool:        "OSSB",
		TestCase:    testName,
		Duration:    duration,
		MemoryUsage: int64(peakMemory),
		Success:     err == nil && result.Success,
	}

	if err != nil {
		benchResult.Error = err.Error()
	} else if !result.Success {
		benchResult.Error = result.Error
	} else {
		benchResult.CacheHits = result.CacheHits
		benchResult.Operations = result.Operations
	}

	return benchResult
}

func benchmarkKaniko(t *testing.T, testName, dockerfile string, files map[string]string) BenchmarkResult {
	tempDir, err := ioutil.TempDir("", fmt.Sprintf("kaniko-bench-%s-", testName))
	if err != nil {
		return BenchmarkResult{Tool: "Kaniko", TestCase: testName, Success: false, Error: err.Error()}
	}
	defer os.RemoveAll(tempDir)

	if err := createTestFiles(tempDir, dockerfile, files); err != nil {
		return BenchmarkResult{Tool: "Kaniko", TestCase: testName, Success: false, Error: err.Error()}
	}

	// Run Kaniko
	cmd := exec.Command("kaniko",
		"--context", tempDir,
		"--dockerfile", filepath.Join(tempDir, "Dockerfile"),
		"--destination", fmt.Sprintf("kaniko-bench-%s:latest", testName),
		"--no-push",
	)

	startTime := time.Now()
	err = cmd.Run()
	duration := time.Since(startTime)

	return BenchmarkResult{
		Tool:     "Kaniko",
		TestCase: testName,
		Duration: duration,
		Success:  err == nil,
		Error:    func() string { if err != nil { return err.Error() }; return "" }(),
	}
}

func benchmarkBuildKit(t *testing.T, testName, dockerfile string, files map[string]string) BenchmarkResult {
	tempDir, err := ioutil.TempDir("", fmt.Sprintf("buildkit-bench-%s-", testName))
	if err != nil {
		return BenchmarkResult{Tool: "BuildKit", TestCase: testName, Success: false, Error: err.Error()}
	}
	defer os.RemoveAll(tempDir)

	if err := createTestFiles(tempDir, dockerfile, files); err != nil {
		return BenchmarkResult{Tool: "BuildKit", TestCase: testName, Success: false, Error: err.Error()}
	}

	// Run BuildKit via docker buildx
	cmd := exec.Command("docker", "buildx", "build",
		"--tag", fmt.Sprintf("buildkit-bench-%s:latest", testName),
		tempDir,
	)

	startTime := time.Now()
	err = cmd.Run()
	duration := time.Since(startTime)

	return BenchmarkResult{
		Tool:     "BuildKit",
		TestCase: testName,
		Duration: duration,
		Success:  err == nil,
		Error:    func() string { if err != nil { return err.Error() }; return "" }(),
	}
}

func benchmarkBuildKitMultiArch(t *testing.T, testName, dockerfile string, files map[string]string, platforms []types.Platform) BenchmarkResult {
	tempDir, err := ioutil.TempDir("", fmt.Sprintf("buildkit-multiarch-bench-%s-", testName))
	if err != nil {
		return BenchmarkResult{Tool: "BuildKit", TestCase: testName, Success: false, Error: err.Error()}
	}
	defer os.RemoveAll(tempDir)

	if err := createTestFiles(tempDir, dockerfile, files); err != nil {
		return BenchmarkResult{Tool: "BuildKit", TestCase: testName, Success: false, Error: err.Error()}
	}

	// Convert platforms to string
	platformStrs := make([]string, len(platforms))
	for i, p := range platforms {
		platformStrs[i] = p.String()
	}
	platformArg := strings.Join(platformStrs, ",")

	cmd := exec.Command("docker", "buildx", "build",
		"--platform", platformArg,
		"--tag", fmt.Sprintf("buildkit-multiarch-bench-%s:latest", testName),
		tempDir,
	)

	startTime := time.Now()
	err = cmd.Run()
	duration := time.Since(startTime)

	return BenchmarkResult{
		Tool:     "BuildKit",
		TestCase: testName,
		Duration: duration,
		Success:  err == nil,
		Error:    func() string { if err != nil { return err.Error() }; return "" }(),
	}
}

func createTestFiles(dir, dockerfile string, files map[string]string) error {
	// Create Dockerfile
	dockerfilePath := filepath.Join(dir, "Dockerfile")
	if err := ioutil.WriteFile(dockerfilePath, []byte(dockerfile), 0644); err != nil {
		return err
	}

	// Create additional files
	for filename, content := range files {
		filePath := filepath.Join(dir, filename)
		
		// Create directory if needed
		if fileDir := filepath.Dir(filePath); fileDir != dir {
			if err := os.MkdirAll(fileDir, 0755); err != nil {
				return err
			}
		}

		if err := ioutil.WriteFile(filePath, []byte(content), 0644); err != nil {
			return err
		}
	}

	return nil
}

func isKanikoAvailable() bool {
	_, err := exec.LookPath("kaniko")
	return err == nil
}

func isBuildKitAvailable() bool {
	cmd := exec.Command("docker", "buildx", "version")
	return cmd.Run() == nil
}

func printBenchmarkComparison(t *testing.T, results []BenchmarkResult) {
	t.Log("\n=== Performance Benchmark Comparison ===")
	
	// Group results by test case
	testCases := make(map[string][]BenchmarkResult)
	for _, result := range results {
		testCases[result.TestCase] = append(testCases[result.TestCase], result)
	}

	for testCase, caseResults := range testCases {
		t.Logf("\nTest Case: %s", testCase)
		t.Log("Tool\t\tDuration\t\tSuccess\tCache Hits\tOperations")
		t.Log("----\t\t--------\t\t-------\t----------\t----------")
		
		for _, result := range caseResults {
			status := "✓"
			if !result.Success {
				status = "✗"
			}
			
			t.Logf("%s\t\t%v\t\t%s\t%d\t\t%d",
				result.Tool,
				result.Duration,
				status,
				result.CacheHits,
				result.Operations,
			)
		}

		// Calculate performance ratios
		if len(caseResults) > 1 {
			ossbResult := findResultByTool(caseResults, "OSSB")
			if ossbResult != nil && ossbResult.Success {
				for _, other := range caseResults {
					if other.Tool != "OSSB" && other.Success {
						ratio := float64(ossbResult.Duration) / float64(other.Duration)
						t.Logf("OSSB vs %s: %.2fx", other.Tool, ratio)
					}
				}
			}
		}
	}
}

func findResultByTool(results []BenchmarkResult, tool string) *BenchmarkResult {
	for _, result := range results {
		if result.Tool == tool {
			return &result
		}
	}
	return nil
}

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}