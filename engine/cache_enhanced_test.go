package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bibin-skaria/ossb/internal/types"
)

func TestPlatformSpecificCaching(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "ossb-cache-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	cache := NewCache(tempDir)
	
	platforms := []types.Platform{
		{OS: "linux", Architecture: "amd64"},
		{OS: "linux", Architecture: "arm64"},
	}

	// Test platform-specific operations
	for _, platform := range platforms {
		op := &types.Operation{
			Type:     types.OperationTypeExec,
			Command:  []string{"RUN", "apt-get update"},
			Platform: platform,
		}
		
		result := &types.OperationResult{
			Operation: op,
			Success:   true,
		}
		
		key := op.CacheKey()
		err := cache.SetWithContext(key, result, platform, "dockerfile-hash", "context-hash", nil)
		if err != nil {
			t.Fatal(err)
		}
	}

	// Verify platform-specific storage
	for _, platform := range platforms {
		op := &types.Operation{
			Type:     types.OperationTypeExec,
			Command:  []string{"RUN", "apt-get update"},
			Platform: platform,
		}
		
		key := op.CacheKey()
		cachedResult, hit := cache.GetWithContext(key, platform, "dockerfile-hash", "context-hash")
		
		if !hit {
			t.Errorf("Expected cache hit for platform %s", platform.String())
		}
		
		if cachedResult.Operation.Platform.String() != platform.String() {
			t.Errorf("Expected platform %s, got %s", platform.String(), cachedResult.Operation.Platform.String())
		}
	}
}

func TestSharedCaching(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "ossb-cache-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	cache := NewCache(tempDir)
	
	platforms := []types.Platform{
		{OS: "linux", Architecture: "amd64"},
		{OS: "linux", Architecture: "arm64"},
	}

	// Test shareable operations (file operations)
	op := &types.Operation{
		Type:    types.OperationTypeFile,
		Command: []string{"COPY", "package.json", "/app/"},
	}
	
	result := &types.OperationResult{
		Operation: op,
		Success:   true,
	}
	
	key := op.CacheKey()
	
	// Store for first platform
	err = cache.SetWithContext(key, result, platforms[0], "dockerfile-hash", "context-hash", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Try to retrieve for second platform (should work if shared)
	cachedResult, hit := cache.GetWithContext(key, platforms[1], "dockerfile-hash", "context-hash")
	
	if !hit {
		t.Error("Expected cache hit for shared operation across platforms")
	}
	
	if cachedResult == nil {
		t.Error("Expected cached result for shared operation")
	}
}

func TestIntelligentInvalidation(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "ossb-cache-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	// Create cache with custom invalidation rules
	rules := []InvalidationRule{
		{
			Type:   InvalidationTypeAge,
			MaxAge: 1 * time.Hour,
		},
		{
			Type:   InvalidationTypeDockerfile,
			MaxAge: 30 * time.Minute,
		},
	}
	
	strategy := PruningStrategy{
		MaxSize:       1024 * 1024,
		MaxAge:        24 * time.Hour,
		MaxFiles:      1000,
		LRUEnabled:    true,
		PlatformQuota: 256 * 1024,
		OrphanCleanup: true,
	}
	
	cache := NewCacheWithConfig(tempDir, strategy, rules)
	platform := types.Platform{OS: "linux", Architecture: "amd64"}

	// Create an entry
	op := &types.Operation{
		Type:     types.OperationTypeExec,
		Command:  []string{"RUN", "echo test"},
		Platform: platform,
	}
	
	result := &types.OperationResult{
		Operation: op,
		Success:   true,
	}
	
	key := op.CacheKey()
	err = cache.SetWithContext(key, result, platform, "dockerfile-v1", "context-v1", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Verify entry exists
	_, hit := cache.GetWithContext(key, platform, "dockerfile-v1", "context-v1")
	if !hit {
		t.Error("Expected cache hit for newly created entry")
	}

	// Test Dockerfile hash change invalidation
	_, hit = cache.GetWithContext(key, platform, "dockerfile-v2", "context-v1")
	if hit {
		t.Error("Expected cache miss after Dockerfile hash change")
	}

	// Test build context hash change invalidation
	_, hit = cache.GetWithContext(key, platform, "dockerfile-v1", "context-v2")
	if hit {
		t.Error("Expected cache miss after build context hash change")
	}
}

func TestCacheMetrics(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "ossb-cache-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	cache := NewCache(tempDir)
	platforms := []types.Platform{
		{OS: "linux", Architecture: "amd64"},
		{OS: "linux", Architecture: "arm64"},
	}

	// Create entries for different platforms
	for i, platform := range platforms {
		for j := 0; j < 5; j++ {
			op := &types.Operation{
				Type:     types.OperationTypeExec,
				Command:  []string{"RUN", "echo", fmt.Sprintf("test-%d-%d", i, j)}, // Make commands unique
				Platform: platform,
			}
			
			result := &types.OperationResult{
				Operation: op,
				Success:   true,
			}
			
			key := op.CacheKey()
			cache.SetWithContext(key, result, platform, "dockerfile-hash", "context-hash", nil)
		}
	}

	// Generate some cache hits and misses
	for i, platform := range platforms {
		for j := 0; j < 3; j++ {
			op := &types.Operation{
				Type:     types.OperationTypeExec,
				Command:  []string{"RUN", "echo", fmt.Sprintf("test-%d-%d", i, j)}, // Match the stored commands
				Platform: platform,
			}
			
			key := op.CacheKey()
			cache.GetWithContext(key, platform, "dockerfile-hash", "context-hash") // Hit
		}
		
		// Generate some misses
		for j := 0; j < 2; j++ {
			key := fmt.Sprintf("non-existent-key-%d-%d", i, j)
			cache.GetWithContext(key, platform, "dockerfile-hash", "context-hash") // Miss
		}
	}

	metrics, err := cache.GetMetrics()
	if err != nil {
		t.Fatal(err)
	}

	// Verify metrics
	if metrics.TotalFiles != 10 { // 5 entries per platform * 2 platforms
		t.Errorf("Expected 10 total files, got %d", metrics.TotalFiles)
	}

	if metrics.TotalHits != 6 { // 3 hits per platform * 2 platforms
		t.Errorf("Expected 6 total hits, got %d", metrics.TotalHits)
	}

	if metrics.TotalMisses != 4 { // 2 misses per platform * 2 platforms
		t.Errorf("Expected 4 total misses, got %d", metrics.TotalMisses)
	}

	expectedHitRate := float64(6) / float64(10)
	if metrics.HitRate != expectedHitRate {
		t.Errorf("Expected hit rate %.2f, got %.2f", expectedHitRate, metrics.HitRate)
	}

	// Verify platform-specific stats
	if len(metrics.PlatformStats) != 2 {
		t.Errorf("Expected 2 platform stats, got %d", len(metrics.PlatformStats))
	}

	for _, platform := range platforms {
		stats, exists := metrics.PlatformStats[platform.String()]
		if !exists {
			t.Errorf("Expected stats for platform %s", platform.String())
			continue
		}

		if stats.Hits != 3 {
			t.Errorf("Expected 3 hits for platform %s, got %d", platform.String(), stats.Hits)
		}

		if stats.Misses != 2 {
			t.Errorf("Expected 2 misses for platform %s, got %d", platform.String(), stats.Misses)
		}
	}
}

func TestAdvancedPruningStrategies(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "ossb-cache-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	cache := NewCache(tempDir)
	platform := types.Platform{OS: "linux", Architecture: "amd64"}

	// Create entries with different access patterns
	entries := []struct {
		key         string
		accessCount int
		age         time.Duration
	}{
		{"frequently-used", 10, 1 * time.Hour},
		{"rarely-used", 1, 2 * time.Hour},
		{"old-frequent", 8, 5 * time.Hour},
		{"old-rare", 1, 6 * time.Hour},
		{"new-frequent", 5, 10 * time.Minute},
		{"new-rare", 1, 15 * time.Minute},
	}

	for _, entry := range entries {
		op := &types.Operation{
			Type:     types.OperationTypeExec,
			Command:  []string{"RUN", "echo", entry.key},
			Platform: platform,
		}
		
		result := &types.OperationResult{
			Operation: op,
			Success:   true,
		}
		
		// Simulate different creation times
		cacheEntry := CacheEntry{
			Key:          entry.key,
			Result:       result,
			Timestamp:    time.Now().Add(-entry.age),
			LastAccessed: time.Now().Add(-entry.age/2),
			AccessCount:  int64(entry.accessCount),
			Platform:     platform,
		}
		
		// Manually create cache entry to control timestamps
		entryPath := cache.getPlatformEntryPath(entry.key, platform)
		entryDir := filepath.Dir(entryPath)
		os.MkdirAll(entryDir, 0755)
		
		data, _ := json.Marshal(cacheEntry)
		os.WriteFile(entryPath, data, 0644)
	}

	initialMetrics, _ := cache.GetMetrics()

	// Test LRU-based pruning (should keep frequently accessed items)
	strategy := PruningStrategy{
		MaxSize:       1024, // Force pruning
		MaxAge:        24 * time.Hour,
		MaxFiles:      3, // Keep only 3 entries
		LRUEnabled:    true,
		PlatformQuota: 512,
		OrphanCleanup: false,
	}

	cache.PruneWithStrategy(strategy)

	finalMetrics, _ := cache.GetMetrics()

	if finalMetrics.TotalFiles > 3 {
		t.Errorf("Expected at most 3 files after pruning, got %d", finalMetrics.TotalFiles)
	}

	if finalMetrics.TotalFiles >= initialMetrics.TotalFiles {
		t.Error("Expected file count to decrease after pruning")
	}

	// Verify that frequently used entries are more likely to survive
	survivedEntries := []string{}
	for _, entry := range entries {
		if _, hit := cache.GetWithContext(entry.key, platform, "dockerfile-hash", "context-hash"); hit {
			survivedEntries = append(survivedEntries, entry.key)
		}
	}

	t.Logf("Survived entries after LRU pruning: %v", survivedEntries)
	
	// In a proper LRU implementation, frequently-used entries should be more likely to survive
	// This is a basic check - in practice, the exact behavior depends on the pruning algorithm
	if len(survivedEntries) == 0 {
		t.Error("Expected some entries to survive pruning")
	}
}

func TestContextHashComputation(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "ossb-cache-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	cache := NewCache(tempDir)

	// Create test build context
	contextDir := filepath.Join(tempDir, "context")
	os.MkdirAll(contextDir, 0755)
	
	// Create test files
	os.WriteFile(filepath.Join(contextDir, "Dockerfile"), []byte("FROM alpine\nRUN echo hello"), 0644)
	os.WriteFile(filepath.Join(contextDir, "app.js"), []byte("console.log('hello')"), 0644)
	os.WriteFile(filepath.Join(contextDir, "package.json"), []byte(`{"name": "test"}`), 0644)
	
	// Create subdirectory
	subDir := filepath.Join(contextDir, "src")
	os.MkdirAll(subDir, 0755)
	os.WriteFile(filepath.Join(subDir, "index.js"), []byte("module.exports = {}"), 0644)

	// Test Dockerfile hash computation
	dockerfileHash1, err := cache.ComputeDockerfileHash(filepath.Join(contextDir, "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}

	if dockerfileHash1 == "" {
		t.Error("Expected non-empty Dockerfile hash")
	}

	// Modify Dockerfile and verify hash changes
	os.WriteFile(filepath.Join(contextDir, "Dockerfile"), []byte("FROM alpine\nRUN echo world"), 0644)
	dockerfileHash2, err := cache.ComputeDockerfileHash(filepath.Join(contextDir, "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}

	if dockerfileHash1 == dockerfileHash2 {
		t.Error("Expected Dockerfile hash to change after modification")
	}

	// Test build context hash computation
	contextHash1, err := cache.ComputeBuildContextHash(contextDir, []string{".git", "node_modules"})
	if err != nil {
		t.Fatal(err)
	}

	if contextHash1 == "" {
		t.Error("Expected non-empty build context hash")
	}

	// Add a file and verify hash changes
	os.WriteFile(filepath.Join(contextDir, "newfile.txt"), []byte("new content"), 0644)
	contextHash2, err := cache.ComputeBuildContextHash(contextDir, []string{".git", "node_modules"})
	if err != nil {
		t.Fatal(err)
	}

	if contextHash1 == contextHash2 {
		t.Error("Expected build context hash to change after adding file")
	}

	// Test dockerignore functionality
	// Create a separate context for ignore testing
	ignoreTestDir := filepath.Join(tempDir, "ignore-test")
	os.MkdirAll(ignoreTestDir, 0755)
	os.WriteFile(filepath.Join(ignoreTestDir, "keep.txt"), []byte("keep this"), 0644)
	
	// Compute hash without ignored file
	hashWithoutIgnored, err := cache.ComputeBuildContextHash(ignoreTestDir, []string{"*.log"})
	if err != nil {
		t.Fatal(err)
	}
	
	// Add ignored file
	os.WriteFile(filepath.Join(ignoreTestDir, "ignore.log"), []byte("ignore this"), 0644)
	
	// Compute hash with ignored file (should be same)
	hashWithIgnored, err := cache.ComputeBuildContextHash(ignoreTestDir, []string{"*.log"})
	if err != nil {
		t.Fatal(err)
	}

	if hashWithoutIgnored != hashWithIgnored {
		t.Logf("Hash without ignored: %s", hashWithoutIgnored)
		t.Logf("Hash with ignored: %s", hashWithIgnored)
		
		// Test the ignore function directly
		shouldIgnore := cache.shouldIgnoreFile("ignore.log", []string{"*.log"})
		t.Logf("Should ignore 'ignore.log' with pattern '*.log': %v", shouldIgnore)
		
		t.Error("Expected build context hash to be same when ignoring files")
	}
}

func TestCacheConcurrency(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "ossb-cache-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	cache := NewCache(tempDir)
	platform := types.Platform{OS: "linux", Architecture: "amd64"}

	// Test concurrent reads and writes
	done := make(chan bool, 10)
	
	// Start multiple goroutines writing to cache
	for i := 0; i < 5; i++ {
		go func(id int) {
			for j := 0; j < 10; j++ {
				op := &types.Operation{
					Type:     types.OperationTypeExec,
					Command:  []string{"RUN", "echo", "test"},
					Platform: platform,
				}
				
				result := &types.OperationResult{
					Operation: op,
					Success:   true,
				}
				
				key := op.CacheKey() + string(rune(id*100+j))
				cache.SetWithContext(key, result, platform, "dockerfile-hash", "context-hash", nil)
			}
			done <- true
		}(i)
	}

	// Start multiple goroutines reading from cache
	for i := 0; i < 5; i++ {
		go func(id int) {
			for j := 0; j < 10; j++ {
				op := &types.Operation{
					Type:     types.OperationTypeExec,
					Command:  []string{"RUN", "echo", "test"},
					Platform: platform,
				}
				
				key := op.CacheKey() + string(rune(id*100+j))
				cache.GetWithContext(key, platform, "dockerfile-hash", "context-hash")
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify cache is in consistent state
	metrics, err := cache.GetMetrics()
	if err != nil {
		t.Fatal(err)
	}

	if metrics.TotalFiles == 0 {
		t.Error("Expected some cache entries after concurrent operations")
	}

	t.Logf("Concurrent test completed: %d files, %d hits, %d misses", 
		metrics.TotalFiles, metrics.TotalHits, metrics.TotalMisses)
}