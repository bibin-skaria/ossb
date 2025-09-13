package engine

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/bibin-skaria/ossb/internal/types"
)

func BenchmarkCacheGet(b *testing.B) {
	tempDir, err := os.MkdirTemp("", "ossb-cache-bench-*")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	cache := NewCache(tempDir)
	
	// Pre-populate cache with test data
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("test-key-%d", i)
		result := &types.OperationResult{
			Operation: &types.Operation{
				Type:     types.OperationTypeExec,
				Command:  []string{"echo", "test"},
				Platform: types.Platform{OS: "linux", Architecture: "amd64"},
			},
			Success: true,
		}
		cache.Set(key, result)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		keyIndex := 0
		for pb.Next() {
			key := fmt.Sprintf("test-key-%d", keyIndex%1000)
			cache.Get(key)
			keyIndex++
		}
	})
}

func BenchmarkCacheSet(b *testing.B) {
	tempDir, err := os.MkdirTemp("", "ossb-cache-bench-*")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	cache := NewCache(tempDir)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		keyIndex := 0
		for pb.Next() {
			key := fmt.Sprintf("bench-key-%d", keyIndex)
			result := &types.OperationResult{
				Operation: &types.Operation{
					Type:     types.OperationTypeExec,
					Command:  []string{"echo", "test"},
					Platform: types.Platform{OS: "linux", Architecture: "amd64"},
				},
				Success: true,
			}
			cache.Set(key, result)
			keyIndex++
		}
	})
}

func BenchmarkCacheGetWithContext(b *testing.B) {
	tempDir, err := os.MkdirTemp("", "ossb-cache-bench-*")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	cache := NewCache(tempDir)
	platform := types.Platform{OS: "linux", Architecture: "amd64"}
	
	// Pre-populate cache with test data
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("test-key-%d", i)
		result := &types.OperationResult{
			Operation: &types.Operation{
				Type:     types.OperationTypeExec,
				Command:  []string{"echo", "test"},
				Platform: platform,
			},
			Success: true,
		}
		cache.SetWithContext(key, result, platform, "dockerfile-hash", "context-hash", nil)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		keyIndex := 0
		for pb.Next() {
			key := fmt.Sprintf("test-key-%d", keyIndex%1000)
			cache.GetWithContext(key, platform, "dockerfile-hash", "context-hash")
			keyIndex++
		}
	})
}

func BenchmarkCachePrune(b *testing.B) {
	tempDir, err := os.MkdirTemp("", "ossb-cache-bench-*")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	cache := NewCache(tempDir)
	
	// Pre-populate cache with test data
	for i := 0; i < 10000; i++ {
		key := fmt.Sprintf("test-key-%d", i)
		result := &types.OperationResult{
			Operation: &types.Operation{
				Type:     types.OperationTypeExec,
				Command:  []string{"echo", "test"},
				Platform: types.Platform{OS: "linux", Architecture: "amd64"},
			},
			Success: true,
		}
		cache.Set(key, result)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Prune()
	}
}

func TestCacheHitRateOptimization(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "ossb-cache-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	cache := NewCache(tempDir)
	platform := types.Platform{OS: "linux", Architecture: "amd64"}
	
	// Simulate a typical build scenario
	operations := []types.Operation{
		{Type: types.OperationTypeSource, Command: []string{"COPY", ".", "/app"}, Platform: platform},
		{Type: types.OperationTypeExec, Command: []string{"RUN", "apt-get update"}, Platform: platform},
		{Type: types.OperationTypeExec, Command: []string{"RUN", "apt-get install -y curl"}, Platform: platform},
		{Type: types.OperationTypeExec, Command: []string{"RUN", "npm install"}, Platform: platform},
		{Type: types.OperationTypeExec, Command: []string{"RUN", "npm run build"}, Platform: platform},
	}

	// First build - all cache misses
	firstBuildHits := 0
	for _, op := range operations {
		key := op.CacheKey()
		if _, hit := cache.GetWithContext(key, platform, "dockerfile-v1", "context-v1"); hit {
			firstBuildHits++
		} else {
			result := &types.OperationResult{
				Operation: &op,
				Success:   true,
			}
			cache.SetWithContext(key, result, platform, "dockerfile-v1", "context-v1", nil)
		}
	}

	if firstBuildHits != 0 {
		t.Errorf("Expected 0 cache hits in first build, got %d", firstBuildHits)
	}

	// Second build - should have cache hits
	secondBuildHits := 0
	for _, op := range operations {
		key := op.CacheKey()
		if _, hit := cache.GetWithContext(key, platform, "dockerfile-v1", "context-v1"); hit {
			secondBuildHits++
		}
	}

	expectedHitRate := float64(secondBuildHits) / float64(len(operations))
	if expectedHitRate < 0.8 { // Expect at least 80% hit rate
		t.Errorf("Expected hit rate >= 0.8, got %.2f", expectedHitRate)
	}

	// Third build with Dockerfile change - should invalidate some entries
	thirdBuildHits := 0
	for _, op := range operations {
		key := op.CacheKey()
		if _, hit := cache.GetWithContext(key, platform, "dockerfile-v2", "context-v1"); hit {
			thirdBuildHits++
		}
	}

	// Should have fewer hits due to Dockerfile change
	if thirdBuildHits >= secondBuildHits {
		t.Errorf("Expected fewer cache hits after Dockerfile change, got %d vs %d", thirdBuildHits, secondBuildHits)
	}
}

func TestMultiArchCacheSharing(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "ossb-cache-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	cache := NewCache(tempDir)
	
	platforms := []types.Platform{
		{OS: "linux", Architecture: "amd64"},
		{OS: "linux", Architecture: "arm64"},
		{OS: "linux", Architecture: "arm", Variant: "v7"},
	}

	// Operations that should be shareable across platforms
	shareableOps := []types.Operation{
		{Type: types.OperationTypeSource, Command: []string{"COPY", "package.json", "/app/"}, Metadata: map[string]string{"file": "package.json"}},
		{Type: types.OperationTypeMeta, Metadata: map[string]string{"workdir": "/app"}},
	}

	// Operations that should be platform-specific
	platformSpecificOps := []types.Operation{
		{Type: types.OperationTypeExec, Command: []string{"RUN", "apt-get update"}},
		{Type: types.OperationTypePull, Metadata: map[string]string{"image": "alpine:latest"}},
	}

	// Test shareable operations
	for _, platform := range platforms {
		for _, op := range shareableOps {
			op.Platform = platform
			key := op.CacheKey()
			
			result := &types.OperationResult{
				Operation: &op,
				Success:   true,
			}
			
			cache.SetWithContext(key, result, platform, "dockerfile-hash", "context-hash", nil)
		}
	}

	// Test platform-specific operations
	for _, platform := range platforms {
		for _, op := range platformSpecificOps {
			op.Platform = platform
			key := op.CacheKey()
			
			result := &types.OperationResult{
				Operation: &op,
				Success:   true,
			}
			
			cache.SetWithContext(key, result, platform, "dockerfile-hash", "context-hash", nil)
		}
	}

	// Verify shared entries exist
	sharedEntries, err := cache.GetSharedCacheEntries()
	if err != nil {
		t.Fatal(err)
	}

	if len(sharedEntries) == 0 {
		t.Error("Expected shared cache entries, got none")
	}

	// Verify platform-specific storage
	metrics, err := cache.GetMetrics()
	if err != nil {
		t.Fatal(err)
	}

	if len(metrics.PlatformStats) != len(platforms) {
		t.Errorf("Expected platform stats for %d platforms, got %d", len(platforms), len(metrics.PlatformStats))
	}
}

func TestCachePruningStrategies(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "ossb-cache-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	// Test size-based pruning
	t.Run("SizeBasedPruning", func(t *testing.T) {
		cache := NewCache(tempDir)
		
		// Create entries that exceed size limit
		for i := 0; i < 100; i++ {
			key := fmt.Sprintf("size-test-%d", i)
			result := &types.OperationResult{
				Operation: &types.Operation{
					Type:     types.OperationTypeExec,
					Command:  []string{"echo", "test"},
					Platform: types.Platform{OS: "linux", Architecture: "amd64"},
				},
				Success: true,
			}
			cache.Set(key, result)
		}

		initialMetrics, _ := cache.GetMetrics()
		
		// Prune with small size limit
		strategy := PruningStrategy{
			MaxSize:    100, // Very small limit to force pruning
			MaxAge:     24 * time.Hour,
			MaxFiles:   1000,
			LRUEnabled: true,
		}
		
		cache.PruneWithStrategy(strategy)
		
		finalMetrics, _ := cache.GetMetrics()
		
		if finalMetrics.TotalSize >= initialMetrics.TotalSize {
			t.Error("Expected cache size to decrease after pruning")
		}
	})

	// Test age-based pruning
	t.Run("AgeBasedPruning", func(t *testing.T) {
		cache := NewCache(tempDir)
		
		// Create old entries
		for i := 0; i < 10; i++ {
			key := fmt.Sprintf("age-test-%d", i)
			result := &types.OperationResult{
				Operation: &types.Operation{
					Type:     types.OperationTypeExec,
					Command:  []string{"echo", "test"},
					Platform: types.Platform{OS: "linux", Architecture: "amd64"},
				},
				Success: true,
			}
			cache.Set(key, result)
		}

		initialMetrics, _ := cache.GetMetrics()
		
		// Prune with very short age limit
		strategy := PruningStrategy{
			MaxSize:    10 * 1024 * 1024,
			MaxAge:     1 * time.Millisecond, // Very short age
			MaxFiles:   1000,
			LRUEnabled: false,
		}
		
		// Wait a bit to ensure entries are old enough
		time.Sleep(2 * time.Millisecond)
		
		cache.PruneWithStrategy(strategy)
		
		finalMetrics, _ := cache.GetMetrics()
		
		if finalMetrics.TotalFiles >= initialMetrics.TotalFiles {
			t.Error("Expected file count to decrease after age-based pruning")
		}
	})

	// Test LRU pruning
	t.Run("LRUPruning", func(t *testing.T) {
		cache := NewCache(tempDir)
		
		// Create entries and access some of them
		for i := 0; i < 20; i++ {
			key := fmt.Sprintf("lru-test-%d", i)
			result := &types.OperationResult{
				Operation: &types.Operation{
					Type:     types.OperationTypeExec,
					Command:  []string{"echo", "test"},
					Platform: types.Platform{OS: "linux", Architecture: "amd64"},
				},
				Success: true,
			}
			cache.Set(key, result)
		}

		// Access some entries to make them "recently used"
		for i := 0; i < 10; i++ {
			key := fmt.Sprintf("lru-test-%d", i)
			cache.Get(key)
		}

		// Prune with file count limit
		strategy := PruningStrategy{
			MaxSize:    10 * 1024 * 1024,
			MaxAge:     24 * time.Hour,
			MaxFiles:   15, // Force pruning
			LRUEnabled: true,
		}
		
		cache.PruneWithStrategy(strategy)
		
		// Verify recently accessed entries are more likely to survive
		survivedRecent := 0
		survivedOld := 0
		
		for i := 0; i < 10; i++ {
			key := fmt.Sprintf("lru-test-%d", i)
			if _, hit := cache.Get(key); hit {
				survivedRecent++
			}
		}
		
		for i := 10; i < 20; i++ {
			key := fmt.Sprintf("lru-test-%d", i)
			if _, hit := cache.Get(key); hit {
				survivedOld++
			}
		}
		
		if survivedRecent <= survivedOld {
			t.Errorf("Expected more recently accessed entries to survive LRU pruning: recent=%d, old=%d", survivedRecent, survivedOld)
		}
	})
}

func TestCacheInvalidation(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "ossb-cache-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	cache := NewCache(tempDir)
	platform := types.Platform{OS: "linux", Architecture: "amd64"}

	// Create test entries with different context hashes
	entries := []struct {
		key              string
		dockerfileHash   string
		buildContextHash string
	}{
		{"test-1", "dockerfile-v1", "context-v1"},
		{"test-2", "dockerfile-v1", "context-v2"},
		{"test-3", "dockerfile-v2", "context-v1"},
		{"test-4", "dockerfile-v2", "context-v2"},
	}

	for _, entry := range entries {
		result := &types.OperationResult{
			Operation: &types.Operation{
				Type:     types.OperationTypeExec,
				Command:  []string{"echo", "test"},
				Platform: platform,
			},
			Success: true,
		}
		cache.SetWithContext(entry.key, result, platform, entry.dockerfileHash, entry.buildContextHash, nil)
	}

	initialMetrics, _ := cache.GetMetrics()

	// Test Dockerfile-based invalidation
	cache.InvalidateByDockerfile("dockerfile-v1")

	afterDockerfileInvalidation, _ := cache.GetMetrics()
	
	if afterDockerfileInvalidation.TotalFiles >= initialMetrics.TotalFiles {
		t.Error("Expected file count to decrease after Dockerfile invalidation")
	}

	// Test build context-based invalidation
	cache.InvalidateByBuildContext("context-v1")

	afterContextInvalidation, _ := cache.GetMetrics()
	
	if afterContextInvalidation.TotalFiles >= afterDockerfileInvalidation.TotalFiles {
		t.Error("Expected file count to decrease after build context invalidation")
	}
}

func TestCacheBuildSpeedImprovement(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "ossb-cache-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	cache := NewCache(tempDir)
	platform := types.Platform{OS: "linux", Architecture: "amd64"}
	
	// Simulate a typical build with multiple operations
	operations := []types.Operation{
		{Type: types.OperationTypeSource, Command: []string{"COPY", ".", "/app"}, Platform: platform},
		{Type: types.OperationTypeExec, Command: []string{"RUN", "apt-get update"}, Platform: platform},
		{Type: types.OperationTypeExec, Command: []string{"RUN", "apt-get install -y curl"}, Platform: platform},
		{Type: types.OperationTypeExec, Command: []string{"RUN", "npm install"}, Platform: platform},
		{Type: types.OperationTypeExec, Command: []string{"RUN", "npm run build"}, Platform: platform},
	}

	// First "build" - simulate cache misses and population
	start1 := time.Now()
	firstBuildHits := 0
	for _, op := range operations {
		key := op.CacheKey()
		if _, hit := cache.GetWithContext(key, platform, "dockerfile-v1", "context-v1"); hit {
			firstBuildHits++
		} else {
			// Simulate operation execution time
			time.Sleep(10 * time.Millisecond)
			result := &types.OperationResult{
				Operation: &op,
				Success:   true,
			}
			cache.SetWithContext(key, result, platform, "dockerfile-v1", "context-v1", nil)
		}
	}
	duration1 := time.Since(start1)

	// Second "build" - should have cache hits
	start2 := time.Now()
	secondBuildHits := 0
	for _, op := range operations {
		key := op.CacheKey()
		if _, hit := cache.GetWithContext(key, platform, "dockerfile-v1", "context-v1"); hit {
			secondBuildHits++
		} else {
			// Simulate operation execution time (should not happen)
			time.Sleep(10 * time.Millisecond)
		}
	}
	duration2 := time.Since(start2)

	// Verify cache performance
	if secondBuildHits <= firstBuildHits {
		t.Errorf("Expected more cache hits in second build: first=%d, second=%d", firstBuildHits, secondBuildHits)
	}

	expectedHitRate := float64(secondBuildHits) / float64(len(operations))
	if expectedHitRate < 0.8 { // Expect at least 80% hit rate
		t.Errorf("Expected hit rate >= 0.8, got %.2f", expectedHitRate)
	}

	// Verify second build was faster
	if duration2 >= duration1 {
		t.Errorf("Expected second build to be faster: first=%v, second=%v", duration1, duration2)
	}

	speedupRatio := float64(duration1) / float64(duration2)
	
	t.Logf("Cache performance test completed:")
	t.Logf("  First build:  %v (cache hits: %d/%d)", duration1, firstBuildHits, len(operations))
	t.Logf("  Second build: %v (cache hits: %d/%d)", duration2, secondBuildHits, len(operations))
	t.Logf("  Hit rate:     %.2f", expectedHitRate)
	t.Logf("  Speedup:      %.2fx", speedupRatio)
}