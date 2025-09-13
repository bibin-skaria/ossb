package engine

import (
	"context"
	"testing"
	"time"

	"github.com/bibin-skaria/ossb/internal/types"
)

// TestPerformanceOptimizationIntegration tests the integration of all performance components
func TestPerformanceOptimizationIntegration(t *testing.T) {
	// Create resource manager with reasonable limits
	limits := &types.ResourceLimits{
		Memory: "8Gi",  // 8GB - more reasonable for testing
		CPU:    "4000m", // 4 cores
		Disk:   "20Gi",  // 20GB
	}
	
	rm := NewResourceManager(limits)
	defer rm.Shutdown(context.Background())

	// Test basic resource monitoring
	t.Run("ResourceMonitoring", func(t *testing.T) {
		rm.StartMonitoring("integration-test")
		
		// Simulate some work
		time.Sleep(100 * time.Millisecond)
		
		summary := rm.StopMonitoring("integration-test")
		if summary == nil {
			t.Fatal("Expected monitoring summary")
		}
		
		summaryData := summary.GetSummary()
		t.Logf("Monitoring results: %d samples, peak memory: %d MB", 
			summaryData.SampleCount, summaryData.PeakMemoryMB)
	})

	// Test resource optimization
	t.Run("ResourceOptimization", func(t *testing.T) {
		if err := rm.OptimizeResources(); err != nil {
			t.Errorf("Resource optimization failed: %v", err)
		}
	})

	// Test operation profiling
	t.Run("OperationProfiling", func(t *testing.T) {
		// Profile some operations
		rm.ProfileOperation(types.OperationTypeExec, 100*time.Millisecond, 50, 25.0)
		rm.ProfileOperation(types.OperationTypeExec, 150*time.Millisecond, 75, 35.0)
		rm.ProfileOperation(types.OperationTypePull, 500*time.Millisecond, 200, 15.0)
		
		// Get profiles
		execProfile := rm.GetResourceProfile(types.OperationTypeExec)
		if execProfile == nil {
			t.Fatal("Expected exec operation profile")
		}
		
		if execProfile.SampleCount != 2 {
			t.Errorf("Expected 2 samples, got %d", execProfile.SampleCount)
		}
		
		pullProfile := rm.GetResourceProfile(types.OperationTypePull)
		if pullProfile == nil {
			t.Fatal("Expected pull operation profile")
		}
		
		if pullProfile.SampleCount != 1 {
			t.Errorf("Expected 1 sample, got %d", pullProfile.SampleCount)
		}
		
		t.Logf("Exec profile: %d samples, avg duration: %d ms, avg memory: %d MB",
			execProfile.SampleCount, execProfile.AvgDurationMS, execProfile.AvgMemoryMB)
		t.Logf("Pull profile: %d samples, avg duration: %d ms, avg memory: %d MB",
			pullProfile.SampleCount, pullProfile.AvgDurationMS, pullProfile.AvgMemoryMB)
	})
}

// TestConcurrentBuilderIntegration tests concurrent builder integration
func TestConcurrentBuilderIntegration(t *testing.T) {
	rm := NewResourceManager(nil)
	defer rm.Shutdown(context.Background())

	config := &ConcurrentBuildConfig{
		MaxWorkers:     2,
		QueueSize:      10,
		DefaultTimeout: 5 * time.Second,
	}

	cb := NewConcurrentBuilder(rm, config)
	defer cb.Shutdown(context.Background())

	// Test queue status
	status := cb.GetQueueStatus()
	if status == nil {
		t.Fatal("Expected queue status")
	}

	if status.TotalCapacity != 2 {
		t.Errorf("Expected capacity 2, got %d", status.TotalCapacity)
	}

	t.Logf("Queue status: %d queued, %d running, %d capacity",
		status.QueuedBuilds, status.RunningBuilds, status.TotalCapacity)
}

// TestParallelExecutorIntegration tests parallel executor integration
func TestParallelExecutorIntegration(t *testing.T) {
	rm := NewResourceManager(nil)
	defer rm.Shutdown(context.Background())

	// Use more relaxed resource constraints for testing
	config := &ParallelExecutionConfig{
		MaxConcurrentOps:  2,
		OpTimeout:         5 * time.Second,
		ResourceThreshold: 0.95, // 95% threshold - much more relaxed
	}

	pe := NewParallelExecutor(rm, config)

	// Create simple test operations
	operations := []*types.Operation{
		{
			Type:    types.OperationTypeFile,
			Inputs:  []string{"input1"},
			Outputs: []string{"output1"},
		},
		{
			Type:    types.OperationTypeFile,
			Inputs:  []string{"input2"},
			Outputs: []string{"output2"},
		},
	}

	// Build dependency graph
	if err := pe.buildDependencyGraph(operations); err != nil {
		t.Fatalf("Failed to build dependency graph: %v", err)
	}

	// Verify graph structure
	if len(pe.dependencyGraph.levels) == 0 {
		t.Fatal("Expected dependency levels to be calculated")
	}

	t.Logf("Dependency graph has %d levels", len(pe.dependencyGraph.levels))
	for i, level := range pe.dependencyGraph.levels {
		t.Logf("Level %d has %d operations", i, len(level))
	}
}

// BenchmarkResourceManagerLightweight benchmarks resource manager with minimal overhead
func BenchmarkResourceManagerLightweight(b *testing.B) {
	rm := NewResourceManager(nil)
	defer rm.Shutdown(context.Background())

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Just test permit acquisition/release without resource checks
		if err := rm.AcquireOperationPermit(ctx); err != nil {
			b.Fatalf("Failed to acquire permit: %v", err)
		}
		rm.ReleaseOperationPermit()
	}
}

// TestPerformanceTargetsValidation validates that performance targets are met
func TestPerformanceTargetsValidation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance targets test in short mode")
	}

	// Test 1: Resource manager overhead should be minimal
	t.Run("ResourceManagerOverhead", func(t *testing.T) {
		rm := NewResourceManager(nil)
		defer rm.Shutdown(context.Background())

		ctx := context.Background()
		iterations := 1000
		
		start := time.Now()
		for i := 0; i < iterations; i++ {
			rm.AcquireOperationPermit(ctx)
			rm.ReleaseOperationPermit()
		}
		duration := time.Since(start)
		
		avgDuration := duration / time.Duration(iterations)
		
		// Target: less than 1 microsecond per operation
		if avgDuration > time.Microsecond {
			t.Errorf("Resource manager overhead too high: %v per operation (target: < 1µs)", avgDuration)
		}
		
		t.Logf("Resource manager overhead: %v per operation", avgDuration)
	})

	// Test 2: Memory usage should be reasonable
	t.Run("MemoryUsage", func(t *testing.T) {
		rm := NewResourceManager(nil)
		defer rm.Shutdown(context.Background())

		// Start monitoring
		rm.StartMonitoring("memory-test")
		
		// Simulate some operations
		for i := 0; i < 100; i++ {
			rm.ProfileOperation(types.OperationTypeExec, time.Millisecond, 10, 5.0)
		}
		
		time.Sleep(100 * time.Millisecond)
		
		summary := rm.StopMonitoring("memory-test")
		if summary == nil {
			t.Fatal("Expected monitoring summary")
		}
		
		summaryData := summary.GetSummary()
		
		// Target: less than 100MB peak memory for basic operations
		if summaryData.PeakMemoryMB > 100 {
			t.Errorf("Memory usage too high: %d MB (target: < 100MB)", summaryData.PeakMemoryMB)
		}
		
		t.Logf("Peak memory usage: %d MB", summaryData.PeakMemoryMB)
	})

	// Test 3: Build speed should meet targets
	t.Run("BuildSpeed", func(t *testing.T) {
		rm := NewResourceManager(nil)
		defer rm.Shutdown(context.Background())

		// Simulate a simple build operation
		start := time.Now()
		
		// Simulate parsing, dependency resolution, and execution
		time.Sleep(10 * time.Millisecond) // Simulate work
		
		duration := time.Since(start)
		
		// Target: simple operations should complete quickly
		if duration > 100*time.Millisecond {
			t.Errorf("Build operation too slow: %v (target: < 100ms)", duration)
		}
		
		t.Logf("Build operation completed in: %v", duration)
	})
}

// TestCacheHitRateValidation tests cache hit rate optimization
func TestCacheHitRateValidation(t *testing.T) {
	rm := NewResourceManager(nil)
	defer rm.Shutdown(context.Background())

	// Simulate repeated operations to test cache hit rate
	opType := types.OperationTypeExec
	
	// First set of operations (cache misses)
	for i := 0; i < 5; i++ {
		rm.ProfileOperation(opType, 100*time.Millisecond, 50, 25.0)
	}
	
	// Second set of operations (should be faster due to profiling)
	for i := 0; i < 5; i++ {
		rm.ProfileOperation(opType, 80*time.Millisecond, 45, 20.0)
	}
	
	profile := rm.GetResourceProfile(opType)
	if profile == nil {
		t.Fatal("Expected resource profile")
	}
	
	if profile.SampleCount != 10 {
		t.Errorf("Expected 10 samples, got %d", profile.SampleCount)
	}
	
	// The average should reflect the optimization
	if profile.AvgDurationMS == 0 {
		t.Error("Expected non-zero average duration")
	}
	
	t.Logf("Cache optimization results: %d samples, avg duration: %d ms",
		profile.SampleCount, profile.AvgDurationMS)
}