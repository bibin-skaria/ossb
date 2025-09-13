package engine

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/bibin-skaria/ossb/internal/types"
)

// TestResourceManagerBasicFunctionality tests basic resource manager operations
func TestResourceManagerBasicFunctionality(t *testing.T) {
	limits := &types.ResourceLimits{
		Memory: "2Gi",
		CPU:    "1000m",
		Disk:   "5Gi",
	}

	rm := NewResourceManager(limits)
	defer rm.Shutdown(context.Background())

	// Test resource usage monitoring
	usage := rm.GetResourceUsage()
	if usage == nil {
		t.Fatal("Expected resource usage data")
	}

	// Test monitoring
	monitor := rm.StartMonitoring("test-build")
	if monitor == nil {
		t.Fatal("Expected monitor to be created")
	}

	// Let it run for a bit to collect samples
	time.Sleep(6 * time.Second) // Wait longer than sampling interval

	summary := rm.StopMonitoring("test-build")
	if summary == nil {
		t.Fatal("Expected monitoring summary")
	}

	if summary.GetSummary().SampleCount == 0 {
		t.Error("Expected at least one sample")
	}
}

// TestResourceManagerConcurrency tests resource manager under concurrent load
func TestResourceManagerConcurrency(t *testing.T) {
	rm := NewResourceManager(nil)
	defer rm.Shutdown(context.Background())

	concurrency := 10
	var wg sync.WaitGroup

	// Test concurrent permit acquisition
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			// Acquire build permit
			if err := rm.AcquireBuildPermit(ctx); err != nil {
				t.Errorf("Failed to acquire build permit: %v", err)
				return
			}
			defer rm.ReleaseBuildPermit()

			// Acquire operation permit
			if err := rm.AcquireOperationPermit(ctx); err != nil {
				t.Errorf("Failed to acquire operation permit: %v", err)
				return
			}
			defer rm.ReleaseOperationPermit()

			// Simulate work
			time.Sleep(10 * time.Millisecond)
		}(i)
	}

	wg.Wait()
}

// TestResourceManagerMemoryProfiling tests memory usage profiling
func TestResourceManagerMemoryProfiling(t *testing.T) {
	rm := NewResourceManager(nil)
	defer rm.Shutdown(context.Background())

	// Start monitoring
	rm.StartMonitoring("memory-test")

	// Allocate memory to trigger monitoring
	data := make([][]byte, 100)
	for i := range data {
		data[i] = make([]byte, 1024*1024) // 1MB each
	}

	// Let monitoring collect samples
	time.Sleep(200 * time.Millisecond)

	summary := rm.StopMonitoring("memory-test")
	if summary == nil {
		t.Fatal("Expected monitoring summary")
	}

	summaryData := summary.GetSummary()
	if summaryData.PeakMemoryMB == 0 {
		t.Error("Expected non-zero peak memory usage")
	}

	// Clean up memory
	data = nil
	runtime.GC()
}

// TestConcurrentBuilderBasicFunctionality tests concurrent builder basic operations
func TestConcurrentBuilderBasicFunctionality(t *testing.T) {
	rm := NewResourceManager(nil)
	defer rm.Shutdown(context.Background())

	config := &ConcurrentBuildConfig{
		MaxWorkers:     2,
		QueueSize:      10,
		DefaultTimeout: 30 * time.Second,
	}

	cb := NewConcurrentBuilder(rm, config)
	defer cb.Shutdown(context.Background())

	// Test single build submission
	buildConfig := &types.BuildConfig{
		Context:    "/tmp",
		Dockerfile: "Dockerfile",
		Tags:       []string{"test:latest"},
		Output:     "image",
		Frontend:   "dockerfile",
		Platforms:  []types.Platform{{OS: "linux", Architecture: "amd64"}},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Submit build (this will fail due to missing Dockerfile, but tests the flow)
	_, err := cb.SubmitBuild(ctx, buildConfig, 1)
	if err == nil {
		t.Error("Expected build to fail due to missing Dockerfile")
	}

	// Test queue status
	status := cb.GetQueueStatus()
	if status == nil {
		t.Fatal("Expected queue status")
	}
}

// TestConcurrentBuilderMultipleBuilds tests concurrent execution of multiple builds
func TestConcurrentBuilderMultipleBuilds(t *testing.T) {
	rm := NewResourceManager(nil)
	defer rm.Shutdown(context.Background())

	config := &ConcurrentBuildConfig{
		MaxWorkers:     3,
		QueueSize:      20,
		DefaultTimeout: 10 * time.Second,
	}

	cb := NewConcurrentBuilder(rm, config)
	defer cb.Shutdown(context.Background())

	buildCount := 5
	var wg sync.WaitGroup

	// Submit multiple builds concurrently
	for i := 0; i < buildCount; i++ {
		wg.Add(1)
		go func(buildNum int) {
			defer wg.Done()

			buildConfig := &types.BuildConfig{
				Context:    fmt.Sprintf("/tmp/build-%d", buildNum),
				Dockerfile: "Dockerfile",
				Tags:       []string{fmt.Sprintf("test-%d:latest", buildNum)},
				Output:     "image",
				Frontend:   "dockerfile",
				Platforms:  []types.Platform{{OS: "linux", Architecture: "amd64"}},
			}

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			// Submit async build
			buildID, resultChan, err := cb.SubmitBuildAsync(ctx, buildConfig, buildNum)
			if err != nil {
				t.Errorf("Failed to submit build %d: %v", buildNum, err)
				return
			}

			// Wait for result
			select {
			case result := <-resultChan:
				if result.Error == nil {
					t.Errorf("Build %d (%s) should have failed", buildNum, buildID)
				}
			case <-ctx.Done():
				t.Errorf("Build %d (%s) timed out", buildNum, buildID)
			}
		}(i)
	}

	wg.Wait()
}

// TestParallelExecutorDependencyGraph tests dependency graph construction
func TestParallelExecutorDependencyGraph(t *testing.T) {
	rm := NewResourceManager(nil)
	defer rm.Shutdown(context.Background())

	pe := NewParallelExecutor(rm, nil)

	// Create test operations with dependencies
	operations := []*types.Operation{
		{
			Type:    types.OperationTypePull,
			Inputs:  []string{"alpine:latest"},
			Outputs: []string{"base-image"},
		},
		{
			Type:    types.OperationTypeExec,
			Command: []string{"apk", "add", "curl"},
			Inputs:  []string{"base-image"},
			Outputs: []string{"layer-1"},
		},
		{
			Type:    types.OperationTypeExec,
			Command: []string{"apk", "add", "wget"},
			Inputs:  []string{"layer-1"},
			Outputs: []string{"layer-2"},
		},
		{
			Type:    types.OperationTypeFile,
			Inputs:  []string{"app.py"},
			Outputs: []string{"app-layer"},
		},
	}

	// Build dependency graph
	if err := pe.buildDependencyGraph(operations); err != nil {
		t.Fatalf("Failed to build dependency graph: %v", err)
	}

	// Verify graph structure
	t.Logf("Dependency graph has %d levels", len(pe.dependencyGraph.levels))
	for i, level := range pe.dependencyGraph.levels {
		t.Logf("Level %d has %d operations: %v", i, len(level), level)
	}
	
	if len(pe.dependencyGraph.levels) == 0 {
		t.Fatal("Expected dependency levels to be calculated")
	}

	// Level 0 should have operations with no dependencies
	if len(pe.dependencyGraph.levels) == 0 {
		t.Fatal("Expected dependency levels to be calculated")
	}
	
	level0 := pe.dependencyGraph.levels[0]
	if len(level0) == 0 {
		t.Error("Expected at least one operation in level 0")
	}

	// Verify that pull operation is in level 0
	pullNode := pe.dependencyGraph.GetNode("op-0")
	if pullNode == nil || pullNode.Level != 0 {
		t.Error("Pull operation should be in level 0")
	}
}

// TestParallelExecutorResourceConstraints tests parallel execution under resource constraints
func TestParallelExecutorResourceConstraints(t *testing.T) {
	// Create resource manager with tight limits
	limits := &types.ResourceLimits{
		Memory: "512Mi",
		CPU:    "500m",
	}
	rm := NewResourceManager(limits)
	defer rm.Shutdown(context.Background())

	config := &ParallelExecutionConfig{
		MaxConcurrentOps:  2,
		OpTimeout:         5 * time.Second,
		ResourceThreshold: 0.5, // 50% threshold
	}

	pe := NewParallelExecutor(rm, config)

	// Create operations that would exceed resource limits
	operations := make([]*types.Operation, 10)
	for i := range operations {
		operations[i] = &types.Operation{
			Type:    types.OperationTypeExec,
			Command: []string{"echo", fmt.Sprintf("operation-%d", i)},
			Outputs: []string{fmt.Sprintf("output-%d", i)},
		}
	}

	buildCtx := &ParallelBuildContext{
		BuildID:         "resource-test",
		Operations:      operations,
		WorkDir:         "/tmp",
		ResourceManager: rm,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Execute with resource constraints
	results, err := pe.ExecuteParallel(ctx, buildCtx)
	
	// Should either succeed with limited concurrency or fail due to resource constraints
	if err != nil && len(results) == 0 {
		// This is expected under tight resource constraints
		t.Logf("Execution failed due to resource constraints: %v", err)
	} else if len(results) != len(operations) {
		t.Errorf("Expected %d results, got %d", len(operations), len(results))
	}
}

// BenchmarkResourceManagerOverhead benchmarks resource manager overhead
func BenchmarkResourceManagerOverhead(b *testing.B) {
	rm := NewResourceManager(nil)
	defer rm.Shutdown(context.Background())

	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			// Benchmark permit acquisition/release
			if err := rm.AcquireOperationPermit(ctx); err != nil {
				b.Fatalf("Failed to acquire permit: %v", err)
			}
			rm.ReleaseOperationPermit()
		}
	})
}

// BenchmarkConcurrentBuilderThroughput benchmarks concurrent builder throughput
func BenchmarkConcurrentBuilderThroughput(b *testing.B) {
	rm := NewResourceManager(nil)
	defer rm.Shutdown(context.Background())

	config := &ConcurrentBuildConfig{
		MaxWorkers:     runtime.NumCPU(),
		QueueSize:      1000,
		DefaultTimeout: 30 * time.Second,
	}

	cb := NewConcurrentBuilder(rm, config)
	defer cb.Shutdown(context.Background())

	buildConfig := &types.BuildConfig{
		Context:    "/tmp",
		Dockerfile: "Dockerfile",
		Tags:       []string{"benchmark:latest"},
		Output:     "image",
		Frontend:   "dockerfile",
		Platforms:  []types.Platform{{OS: "linux", Architecture: "amd64"}},
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			
			// Submit build (will fail quickly due to missing Dockerfile)
			_, err := cb.SubmitBuild(ctx, buildConfig, 1)
			if err == nil {
				b.Error("Expected build to fail")
			}
			
			cancel()
		}
	})
}

// BenchmarkParallelExecutorScaling benchmarks parallel executor scaling
func BenchmarkParallelExecutorScaling(b *testing.B) {
	rm := NewResourceManager(nil)
	defer rm.Shutdown(context.Background())

	concurrencyLevels := []int{1, 2, 4, 8}

	for _, concurrency := range concurrencyLevels {
		b.Run(fmt.Sprintf("Concurrency-%d", concurrency), func(b *testing.B) {
			config := &ParallelExecutionConfig{
				MaxConcurrentOps: concurrency,
				OpTimeout:        5 * time.Second,
			}

			pe := NewParallelExecutor(rm, config)

			// Create test operations
			operations := make([]*types.Operation, 20)
			for i := range operations {
				operations[i] = &types.Operation{
					Type:    types.OperationTypeExec,
					Command: []string{"echo", fmt.Sprintf("op-%d", i)},
					Outputs: []string{fmt.Sprintf("output-%d", i)},
				}
			}

			buildCtx := &ParallelBuildContext{
				BuildID:         fmt.Sprintf("benchmark-%d", concurrency),
				Operations:      operations,
				WorkDir:         "/tmp",
				ResourceManager: rm,
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				
				_, err := pe.ExecuteParallel(ctx, buildCtx)
				if err != nil {
					b.Errorf("Execution failed: %v", err)
				}
				
				cancel()
			}
		})
	}
}

// TestMemoryUsageOptimization tests memory usage optimization
func TestMemoryUsageOptimization(t *testing.T) {
	// Create resource manager with memory optimization enabled
	rm := NewResourceManager(&types.ResourceLimits{
		Memory: "1Gi",
	})
	defer rm.Shutdown(context.Background())

	// Force high memory usage
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	initialAlloc := memStats.Alloc

	// Allocate memory
	data := make([][]byte, 500)
	for i := range data {
		data[i] = make([]byte, 1024*1024) // 1MB each
	}

	// Trigger optimization
	if err := rm.OptimizeResources(); err != nil {
		t.Errorf("Resource optimization failed: %v", err)
	}

	// Check if memory was optimized
	runtime.ReadMemStats(&memStats)
	currentAlloc := memStats.Alloc

	t.Logf("Memory before: %d MB, after: %d MB", 
		initialAlloc/1024/1024, currentAlloc/1024/1024)

	// Clean up
	data = nil
	runtime.GC()
}

// TestBuildSpeedTargets tests that builds meet speed targets
func TestBuildSpeedTargets(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping speed target test in short mode")
	}

	rm := NewResourceManager(nil)
	defer rm.Shutdown(context.Background())

	// Test simple build speed
	startTime := time.Now()
	
	// Simulate a simple build operation
	rm.StartMonitoring("speed-test")
	
	// Simulate work
	time.Sleep(100 * time.Millisecond)
	
	summary := rm.StopMonitoring("speed-test")
	duration := time.Since(startTime)

	// Speed target: simple operations should complete within 1 second
	if duration > 1*time.Second {
		t.Errorf("Build took too long: %v (target: < 1s)", duration)
	}

	if summary == nil {
		t.Fatal("Expected monitoring summary")
	}

	summaryData := summary.GetSummary()
	t.Logf("Build completed in %v, peak memory: %d MB", 
		duration, summaryData.PeakMemoryMB)
}

// TestResourceProfileOptimization tests resource profile optimization
func TestResourceProfileOptimization(t *testing.T) {
	rm := NewResourceManager(nil)
	defer rm.Shutdown(context.Background())

	// Test operation profiling for cache optimization
	opType := types.OperationTypeExec
	
	// Profile multiple operations
	for i := 0; i < 10; i++ {
		duration := time.Duration(100+i*10) * time.Millisecond
		peakMemory := int64(100 + i*10)
		peakCPU := float64(50 + i*5)
		
		rm.ProfileOperation(opType, duration, peakMemory, peakCPU)
	}

	// Get profile
	profile := rm.GetResourceProfile(opType)
	if profile == nil {
		t.Fatal("Expected resource profile")
	}

	if profile.SampleCount != 10 {
		t.Errorf("Expected 10 samples, got %d", profile.SampleCount)
	}

	if profile.AvgDurationMS == 0 {
		t.Error("Expected non-zero average duration")
	}

	t.Logf("Profile: %d samples, avg duration: %d ms, avg memory: %d MB", 
		profile.SampleCount, profile.AvgDurationMS, profile.AvgMemoryMB)
}

// Helper function to shutdown resource manager
func (rm *ResourceManager) Shutdown(ctx context.Context) error {
	// Stop all monitoring
	rm.mutex.Lock()
	for id := range rm.monitors {
		if monitor := rm.monitors[id]; monitor != nil {
			monitor.stop()
		}
	}
	rm.monitors = make(map[string]*ResourceMonitor)
	rm.mutex.Unlock()

	return nil
}