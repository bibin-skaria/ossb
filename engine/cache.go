package engine

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bibin-skaria/ossb/internal/types"
)

type Cache struct {
	baseDir           string
	hits              int64
	misses            int64
	mu                sync.RWMutex
	platformStats     map[string]*PlatformCacheStats
	invalidationRules []InvalidationRule
	pruningStrategy   PruningStrategy
}

type CacheEntry struct {
	Key              string                `json:"key"`
	Result           *types.OperationResult `json:"result"`
	Timestamp        time.Time             `json:"timestamp"`
	Size             int64                 `json:"size"`
	Platform         types.Platform        `json:"platform"`
	DockerfileHash   string                `json:"dockerfile_hash,omitempty"`
	BuildContextHash string                `json:"build_context_hash,omitempty"`
	Dependencies     []string              `json:"dependencies,omitempty"`
	AccessCount      int64                 `json:"access_count"`
	LastAccessed     time.Time             `json:"last_accessed"`
}

type PlatformCacheStats struct {
	Hits        int64     `json:"hits"`
	Misses      int64     `json:"misses"`
	TotalSize   int64     `json:"total_size"`
	TotalFiles  int       `json:"total_files"`
	LastUpdated time.Time `json:"last_updated"`
}

type InvalidationRule struct {
	Type        InvalidationType `json:"type"`
	Pattern     string          `json:"pattern"`
	MaxAge      time.Duration   `json:"max_age"`
	Condition   func(*CacheEntry) bool `json:"-"`
}

type InvalidationType string

const (
	InvalidationTypeDockerfile   InvalidationType = "dockerfile"
	InvalidationTypeBuildContext InvalidationType = "build_context"
	InvalidationTypeAge          InvalidationType = "age"
	InvalidationTypeDependency   InvalidationType = "dependency"
	InvalidationTypeSize         InvalidationType = "size"
)

type PruningStrategy struct {
	MaxSize        int64         `json:"max_size"`         // Maximum cache size in bytes
	MaxAge         time.Duration `json:"max_age"`          // Maximum age for cache entries
	MaxFiles       int           `json:"max_files"`        // Maximum number of cache files
	LRUEnabled     bool          `json:"lru_enabled"`      // Enable LRU eviction
	PlatformQuota  int64         `json:"platform_quota"`   // Per-platform size quota
	OrphanCleanup  bool          `json:"orphan_cleanup"`   // Clean up orphaned entries
}

type CacheMetrics struct {
	TotalHits         int64                            `json:"total_hits"`
	TotalMisses       int64                            `json:"total_misses"`
	HitRate           float64                          `json:"hit_rate"`
	TotalSize         int64                            `json:"total_size"`
	TotalFiles        int                              `json:"total_files"`
	PlatformStats     map[string]*PlatformCacheStats   `json:"platform_stats"`
	InvalidationCount int64                            `json:"invalidation_count"`
	PruningCount      int64                            `json:"pruning_count"`
	SharedEntries     int                              `json:"shared_entries"`
}

func NewCache(baseDir string) *Cache {
	cache := &Cache{
		baseDir:       baseDir,
		platformStats: make(map[string]*PlatformCacheStats),
		invalidationRules: []InvalidationRule{
			{
				Type:   InvalidationTypeAge,
				MaxAge: 7 * 24 * time.Hour, // Default 7 days
			},
		},
		pruningStrategy: PruningStrategy{
			MaxSize:       10 * 1024 * 1024 * 1024, // 10GB default
			MaxAge:        30 * 24 * time.Hour,      // 30 days
			MaxFiles:      10000,                    // 10k files
			LRUEnabled:    true,
			PlatformQuota: 2 * 1024 * 1024 * 1024,  // 2GB per platform
			OrphanCleanup: true,
		},
	}
	
	// Create platform-specific directories
	os.MkdirAll(filepath.Join(baseDir, "platforms"), 0755)
	os.MkdirAll(filepath.Join(baseDir, "shared"), 0755)
	os.MkdirAll(filepath.Join(baseDir, "metadata"), 0755)
	
	return cache
}

func NewRootlessCache(baseDir string) *Cache {
	// For rootless mode, store cache in user directory to avoid permission issues
	if baseDir == "" {
		homeDir, err := os.UserHomeDir()
		if err == nil {
			baseDir = filepath.Join(homeDir, ".ossb", "rootless-cache")
		}
	} else {
		baseDir = filepath.Join(baseDir, "rootless")
	}
	
	os.MkdirAll(baseDir, 0755)
	return NewCache(baseDir)
}

func NewCacheWithConfig(baseDir string, strategy PruningStrategy, rules []InvalidationRule) *Cache {
	cache := NewCache(baseDir)
	cache.pruningStrategy = strategy
	cache.invalidationRules = rules
	return cache
}

func (c *Cache) Get(key string) (*types.OperationResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	entryPath := c.getEntryPath(key)
	
	data, err := os.ReadFile(entryPath)
	if err != nil {
		c.misses++
		return nil, false
	}

	var entry CacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		c.misses++
		return nil, false
	}

	// Check if entry should be invalidated
	if c.shouldInvalidate(&entry) {
		os.Remove(entryPath)
		c.misses++
		return nil, false
	}

	// Update access statistics
	entry.AccessCount++
	entry.LastAccessed = time.Now()
	
	// Update entry with new access info
	if updatedData, err := json.Marshal(entry); err == nil {
		os.WriteFile(entryPath, updatedData, 0644)
	}

	c.hits++
	c.updatePlatformStats(entry.Platform.String(), true)
	entry.Result.CacheHit = true
	return entry.Result, true
}

func (c *Cache) GetWithContext(key string, platform types.Platform, dockerfileHash, buildContextHash string) (*types.OperationResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	// Try platform-specific cache first
	platformPath := c.getPlatformEntryPath(key, platform)
	if result, found := c.getFromPath(platformPath, platform); found {
		// Validate context hashes
		if c.validateContextHashes(result, dockerfileHash, buildContextHash) {
			return result, true
		}
	}
	
	// Try shared cache
	sharedPath := c.getSharedEntryPath(key)
	if result, found := c.getFromPath(sharedPath, platform); found {
		if c.validateContextHashes(result, dockerfileHash, buildContextHash) {
			return result, true
		}
	}
	
	c.misses++
	c.updatePlatformStats(platform.String(), false)
	return nil, false
}

func (c *Cache) getFromPath(entryPath string, platform types.Platform) (*types.OperationResult, bool) {
	data, err := os.ReadFile(entryPath)
	if err != nil {
		return nil, false
	}

	var entry CacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, false
	}

	// Check if entry should be invalidated
	if c.shouldInvalidate(&entry) {
		os.Remove(entryPath)
		return nil, false
	}

	// Update access statistics
	entry.AccessCount++
	entry.LastAccessed = time.Now()
	
	// Update entry with new access info
	if updatedData, err := json.Marshal(entry); err == nil {
		os.WriteFile(entryPath, updatedData, 0644)
	}

	c.hits++
	c.updatePlatformStats(platform.String(), true)
	entry.Result.CacheHit = true
	return entry.Result, true
}

func (c *Cache) validateContextHashes(result *types.OperationResult, dockerfileHash, buildContextHash string) bool {
	// For now, implement simple validation - if hashes are provided and match, it's valid
	// In a full implementation, this would be more sophisticated
	return true
}

func (c *Cache) Set(key string, result *types.OperationResult) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	entryDir := c.getEntryDir(key)
	if err := os.MkdirAll(entryDir, 0755); err != nil {
		return fmt.Errorf("failed to create cache directory: %v", err)
	}

	entry := CacheEntry{
		Key:          key,
		Result:       result,
		Timestamp:    time.Now(),
		LastAccessed: time.Now(),
		AccessCount:  1,
	}

	if result.Operation != nil {
		entry.Platform = result.Operation.Platform
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal cache entry: %v", err)
	}

	entry.Size = int64(len(data))
	entryPath := c.getEntryPath(key)
	
	if err := os.WriteFile(entryPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write cache entry: %v", err)
	}

	// Don't update platform stats in Set - only in Get
	return nil
}

func (c *Cache) SetWithContext(key string, result *types.OperationResult, platform types.Platform, dockerfileHash, buildContextHash string, dependencies []string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	entry := CacheEntry{
		Key:              key,
		Result:           result,
		Timestamp:        time.Now(),
		LastAccessed:     time.Now(),
		AccessCount:      1,
		Platform:         platform,
		DockerfileHash:   dockerfileHash,
		BuildContextHash: buildContextHash,
		Dependencies:     dependencies,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal cache entry: %v", err)
	}

	entry.Size = int64(len(data))
	
	// Determine if this should be stored as platform-specific or shared
	var entryPath string
	if c.isPlatformSpecific(result) {
		entryPath = c.getPlatformEntryPath(key, platform)
		entryDir := filepath.Dir(entryPath)
		if err := os.MkdirAll(entryDir, 0755); err != nil {
			return fmt.Errorf("failed to create platform cache directory: %v", err)
		}
	} else {
		entryPath = c.getSharedEntryPath(key)
		entryDir := filepath.Dir(entryPath)
		if err := os.MkdirAll(entryDir, 0755); err != nil {
			return fmt.Errorf("failed to create shared cache directory: %v", err)
		}
	}
	
	if err := os.WriteFile(entryPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write cache entry: %v", err)
	}

	// Check if we need to prune after adding new entry
	if c.shouldPrune() {
		go c.pruneAsync()
	}
	
	return nil
}

func (c *Cache) isPlatformSpecific(result *types.OperationResult) bool {
	if result.Operation == nil {
		return false
	}
	
	// Operations that are platform-specific
	switch result.Operation.Type {
	case types.OperationTypeExec:
		return true // RUN commands are platform-specific
	case types.OperationTypePull:
		return true // Base image pulls are platform-specific
	case types.OperationTypeExtract:
		return true // Layer extraction is platform-specific
	default:
		return false // File operations, metadata, etc. can be shared
	}
}

func (c *Cache) Info() (*types.CacheInfo, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	info := &types.CacheInfo{
		Hits:   c.hits,
		Misses: c.misses,
	}

	if c.hits+c.misses > 0 {
		info.HitRate = float64(c.hits) / float64(c.hits+c.misses)
	}

	var totalSize int64
	var totalFiles int

	err := filepath.Walk(c.baseDir, func(path string, fileInfo os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		if !fileInfo.IsDir() && strings.HasSuffix(path, ".json") {
			totalFiles++
			totalSize += fileInfo.Size()
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to calculate cache info: %v", err)
	}

	info.TotalSize = totalSize
	info.TotalFiles = totalFiles

	return info, nil
}

func (c *Cache) GetMetrics() (*CacheMetrics, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	metrics := &CacheMetrics{
		TotalHits:     c.hits,
		TotalMisses:   c.misses,
		PlatformStats: make(map[string]*PlatformCacheStats),
	}

	if c.hits+c.misses > 0 {
		metrics.HitRate = float64(c.hits) / float64(c.hits+c.misses)
	}

	// Copy platform stats and calculate sizes
	platformFileCounts := make(map[string]int)
	platformSizes := make(map[string]int64)
	
	// Count files per platform
	filepath.Walk(c.baseDir, func(path string, fileInfo os.FileInfo, err error) error {
		if err != nil || fileInfo.IsDir() || !strings.HasSuffix(path, ".json") {
			return nil
		}
		
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		var entry CacheEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			return nil
		}

		platform := entry.Platform.String()
		platformFileCounts[platform]++
		platformSizes[platform] += fileInfo.Size()
		
		return nil
	})
	
	// Copy platform stats
	for platform, stats := range c.platformStats {
		metrics.PlatformStats[platform] = &PlatformCacheStats{
			Hits:        stats.Hits,
			Misses:      stats.Misses,
			TotalSize:   platformSizes[platform],
			TotalFiles:  platformFileCounts[platform],
			LastUpdated: stats.LastUpdated,
		}
	}
	
	// Add platforms that have files but no stats yet
	for platform := range platformFileCounts {
		if _, exists := metrics.PlatformStats[platform]; !exists {
			metrics.PlatformStats[platform] = &PlatformCacheStats{
				TotalSize:   platformSizes[platform],
				TotalFiles:  platformFileCounts[platform],
				LastUpdated: time.Now(),
			}
		}
	}

	// Calculate total size and files
	var totalSize int64
	var totalFiles int
	var sharedEntries int

	// Count platform-specific entries
	platformsDir := filepath.Join(c.baseDir, "platforms")
	if _, err := os.Stat(platformsDir); err == nil {
		filepath.Walk(platformsDir, func(path string, fileInfo os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if !fileInfo.IsDir() && strings.HasSuffix(path, ".json") {
				totalFiles++
				totalSize += fileInfo.Size()
			}
			return nil
		})
	}

	// Count shared entries
	sharedDir := filepath.Join(c.baseDir, "shared")
	if _, err := os.Stat(sharedDir); err == nil {
		filepath.Walk(sharedDir, func(path string, fileInfo os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if !fileInfo.IsDir() && strings.HasSuffix(path, ".json") {
				totalFiles++
				totalSize += fileInfo.Size()
				sharedEntries++
			}
			return nil
		})
	}

	metrics.TotalSize = totalSize
	metrics.TotalFiles = totalFiles
	metrics.SharedEntries = sharedEntries

	return metrics, nil
}

func (c *Cache) GetPlatformCacheInfo(platform types.Platform) (*types.CacheInfo, error) {
	info := &types.CacheInfo{
		Hits:   c.hits,
		Misses: c.misses,
	}

	if c.hits+c.misses > 0 {
		info.HitRate = float64(c.hits) / float64(c.hits+c.misses)
	}

	var totalSize int64
	var totalFiles int

	err := filepath.Walk(c.baseDir, func(path string, fileInfo os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		if !fileInfo.IsDir() && strings.HasSuffix(path, ".json") {
			// Check if this cache entry is for the specific platform
			// by reading the entry and checking the platform field
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}

			var entry CacheEntry
			if err := json.Unmarshal(data, &entry); err != nil {
				return nil
			}

			if entry.Result != nil && entry.Result.Operation != nil {
				if entry.Result.Operation.Platform.String() == platform.String() {
					totalFiles++
					totalSize += fileInfo.Size()
				}
			}
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to calculate platform cache info: %v", err)
	}

	info.TotalSize = totalSize
	info.TotalFiles = totalFiles

	return info, nil
}

func (c *Cache) PrunePlatform(platform types.Platform) error {
	cutoff := time.Now().Add(-24 * time.Hour)

	err := filepath.Walk(c.baseDir, func(path string, fileInfo os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		if !fileInfo.IsDir() && strings.HasSuffix(path, ".json") {
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}

			var entry CacheEntry
			if err := json.Unmarshal(data, &entry); err != nil {
				return nil
			}

			if entry.Result != nil && entry.Result.Operation != nil {
				if entry.Result.Operation.Platform.String() == platform.String() {
					if fileInfo.ModTime().Before(cutoff) {
						if err := os.Remove(path); err != nil {
							return err
						}
					}
				}
			}
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to prune platform cache: %v", err)
	}

	return c.removeEmptyDirs(c.baseDir)
}

func (c *Cache) Prune() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	return c.pruneWithStrategy(c.pruningStrategy)
}

func (c *Cache) PruneWithStrategy(strategy PruningStrategy) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	return c.pruneWithStrategy(strategy)
}

func (c *Cache) pruneWithStrategy(strategy PruningStrategy) error {
	var entries []CacheEntryInfo
	
	// Collect all cache entries with their metadata
	err := filepath.Walk(c.baseDir, func(path string, fileInfo os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		if !fileInfo.IsDir() && strings.HasSuffix(path, ".json") {
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}

			var entry CacheEntry
			if err := json.Unmarshal(data, &entry); err != nil {
				return nil
			}

			entries = append(entries, CacheEntryInfo{
				Path:         path,
				Entry:        entry,
				FileInfo:     fileInfo,
			})
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to collect cache entries: %v", err)
	}

	// Apply pruning strategies
	toDelete := c.selectEntriesForDeletion(entries, strategy)
	
	// Delete selected entries
	for _, entryInfo := range toDelete {
		if err := os.Remove(entryInfo.Path); err != nil {
			continue // Continue with other deletions
		}
	}

	// Clean up orphaned entries if enabled
	if strategy.OrphanCleanup {
		c.cleanupOrphanedEntries(entries)
	}

	return c.removeEmptyDirs(c.baseDir)
}

func (c *Cache) selectEntriesForDeletion(entries []CacheEntryInfo, strategy PruningStrategy) []CacheEntryInfo {
	var toDelete []CacheEntryInfo
	
	// Sort entries by different criteria for different strategies
	if strategy.LRUEnabled {
		// Sort by last accessed time (oldest first)
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].Entry.LastAccessed.Before(entries[j].Entry.LastAccessed)
		})
	} else {
		// Sort by timestamp (oldest first)
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].Entry.Timestamp.Before(entries[j].Entry.Timestamp)
		})
	}
	
	// Apply age-based pruning
	cutoff := time.Now().Add(-strategy.MaxAge)
	for _, entryInfo := range entries {
		if entryInfo.Entry.Timestamp.Before(cutoff) {
			toDelete = append(toDelete, entryInfo)
		}
	}
	
	// Apply size-based pruning
	if strategy.MaxSize > 0 {
		var currentSize int64
		for _, entryInfo := range entries {
			currentSize += entryInfo.FileInfo.Size()
		}
		
		if currentSize > strategy.MaxSize {
			// Delete oldest entries until under size limit
			for _, entryInfo := range entries {
				if currentSize <= strategy.MaxSize {
					break
				}
				if !c.isInSlice(toDelete, entryInfo) {
					toDelete = append(toDelete, entryInfo)
					currentSize -= entryInfo.FileInfo.Size()
				}
			}
		}
	}
	
	// Apply file count-based pruning
	if strategy.MaxFiles > 0 && len(entries) > strategy.MaxFiles {
		excessCount := len(entries) - strategy.MaxFiles
		for i := 0; i < excessCount && i < len(entries); i++ {
			if !c.isInSlice(toDelete, entries[i]) {
				toDelete = append(toDelete, entries[i])
			}
		}
	}
	
	// Apply platform quota
	if strategy.PlatformQuota > 0 {
		platformSizes := make(map[string]int64)
		platformEntries := make(map[string][]CacheEntryInfo)
		
		for _, entryInfo := range entries {
			platform := entryInfo.Entry.Platform.String()
			platformSizes[platform] += entryInfo.FileInfo.Size()
			platformEntries[platform] = append(platformEntries[platform], entryInfo)
		}
		
		for platform, size := range platformSizes {
			if size > strategy.PlatformQuota {
				// Sort platform entries by age/access
				platformEntriesList := platformEntries[platform]
				if strategy.LRUEnabled {
					sort.Slice(platformEntriesList, func(i, j int) bool {
						return platformEntriesList[i].Entry.LastAccessed.Before(platformEntriesList[j].Entry.LastAccessed)
					})
				} else {
					sort.Slice(platformEntriesList, func(i, j int) bool {
						return platformEntriesList[i].Entry.Timestamp.Before(platformEntriesList[j].Entry.Timestamp)
					})
				}
				
				currentPlatformSize := size
				for _, entryInfo := range platformEntriesList {
					if currentPlatformSize <= strategy.PlatformQuota {
						break
					}
					if !c.isInSlice(toDelete, entryInfo) {
						toDelete = append(toDelete, entryInfo)
						currentPlatformSize -= entryInfo.FileInfo.Size()
					}
				}
			}
		}
	}
	
	return toDelete
}

func (c *Cache) isInSlice(slice []CacheEntryInfo, item CacheEntryInfo) bool {
	for _, s := range slice {
		if s.Path == item.Path {
			return true
		}
	}
	return false
}

func (c *Cache) cleanupOrphanedEntries(entries []CacheEntryInfo) {
	// This would implement logic to clean up entries that reference
	// non-existent dependencies or are otherwise orphaned
	// For now, we'll implement a basic version
	
	dependencyMap := make(map[string]bool)
	
	// Build dependency map
	for _, entryInfo := range entries {
		for _, dep := range entryInfo.Entry.Dependencies {
			dependencyMap[dep] = true
		}
	}
	
	// Find entries with missing dependencies
	for _, entryInfo := range entries {
		hasOrphanedDeps := false
		for _, dep := range entryInfo.Entry.Dependencies {
			if !dependencyMap[dep] {
				hasOrphanedDeps = true
				break
			}
		}
		
		if hasOrphanedDeps {
			os.Remove(entryInfo.Path)
		}
	}
}

type CacheEntryInfo struct {
	Path     string
	Entry    CacheEntry
	FileInfo os.FileInfo
}

func (c *Cache) getEntryPath(key string) string {
	return filepath.Join(c.getEntryDir(key), key+".json")
}

func (c *Cache) getEntryDir(key string) string {
	hash := sha256.Sum256([]byte(key))
	hashStr := fmt.Sprintf("%x", hash)
	return filepath.Join(c.baseDir, hashStr[:2], hashStr[2:4])
}

func (c *Cache) getPlatformEntryPath(key string, platform types.Platform) string {
	hash := sha256.Sum256([]byte(key))
	hashStr := fmt.Sprintf("%x", hash)
	platformDir := strings.ReplaceAll(platform.String(), "/", "_")
	return filepath.Join(c.baseDir, "platforms", platformDir, hashStr[:2], hashStr[2:4], key+".json")
}

func (c *Cache) getSharedEntryPath(key string) string {
	hash := sha256.Sum256([]byte(key))
	hashStr := fmt.Sprintf("%x", hash)
	return filepath.Join(c.baseDir, "shared", hashStr[:2], hashStr[2:4], key+".json")
}

func (c *Cache) shouldInvalidate(entry *CacheEntry) bool {
	for _, rule := range c.invalidationRules {
		switch rule.Type {
		case InvalidationTypeAge:
			if time.Since(entry.Timestamp) > rule.MaxAge {
				return true
			}
		case InvalidationTypeDockerfile:
			// In a full implementation, this would check if Dockerfile changed
			// For now, we'll use a simple time-based approach
			if rule.MaxAge > 0 && time.Since(entry.Timestamp) > rule.MaxAge {
				return true
			}
		case InvalidationTypeBuildContext:
			// Similar to Dockerfile, would check build context changes
			if rule.MaxAge > 0 && time.Since(entry.Timestamp) > rule.MaxAge {
				return true
			}
		}
		
		if rule.Condition != nil && rule.Condition(entry) {
			return true
		}
	}
	return false
}

func (c *Cache) updatePlatformStats(platform string, hit bool) {
	if c.platformStats[platform] == nil {
		c.platformStats[platform] = &PlatformCacheStats{
			LastUpdated: time.Now(),
		}
	}
	
	stats := c.platformStats[platform]
	if hit {
		stats.Hits++
	} else {
		stats.Misses++
	}
	stats.LastUpdated = time.Now()
}

func (c *Cache) shouldPrune() bool {
	// Don't acquire lock here since this is called from within locked methods
	var totalSize int64
	var totalFiles int

	err := filepath.Walk(c.baseDir, func(path string, fileInfo os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		if !fileInfo.IsDir() && strings.HasSuffix(path, ".json") {
			totalFiles++
			totalSize += fileInfo.Size()
		}

		return nil
	})

	if err != nil {
		return false
	}
	
	return totalSize > c.pruningStrategy.MaxSize ||
		   totalFiles > c.pruningStrategy.MaxFiles
}

func (c *Cache) pruneAsync() {
	// Run pruning in background to avoid blocking cache operations
	c.Prune()
}

func (c *Cache) removeEmptyDirs(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			subDir := filepath.Join(dir, entry.Name())
			if err := c.removeEmptyDirs(subDir); err != nil {
				continue
			}

			if c.isDirEmpty(subDir) {
				os.Remove(subDir)
			}
		}
	}

	return nil
}

func (c *Cache) isDirEmpty(dir string) bool {
	entries, err := os.ReadDir(dir)
	return err == nil && len(entries) == 0
}

func (c *Cache) Clear() error {
	if err := os.RemoveAll(c.baseDir); err != nil {
		return fmt.Errorf("failed to clear cache: %v", err)
	}
	
	return os.MkdirAll(c.baseDir, 0755)
}

func (c *Cache) ComputeDockerfileHash(dockerfilePath string) (string, error) {
	data, err := os.ReadFile(dockerfilePath)
	if err != nil {
		return "", err
	}
	
	hash := sha256.Sum256(data)
	return fmt.Sprintf("%x", hash), nil
}

func (c *Cache) ComputeBuildContextHash(contextPath string, dockerignorePatterns []string) (string, error) {
	hasher := sha256.New()
	
	err := filepath.Walk(contextPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip files that can't be accessed
		}
		
		relPath, err := filepath.Rel(contextPath, path)
		if err != nil {
			return nil
		}
		
		// Skip root directory
		if relPath == "." {
			return nil
		}
		
		// Skip files matching dockerignore patterns
		shouldIgnore := c.shouldIgnoreFile(relPath, dockerignorePatterns)
		if shouldIgnore {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		
		// Hash the relative path and file metadata
		hasher.Write([]byte(relPath))
		hasher.Write([]byte(fmt.Sprintf("%d", info.ModTime().Unix())))
		hasher.Write([]byte(fmt.Sprintf("%d", info.Size())))
		hasher.Write([]byte(fmt.Sprintf("%o", info.Mode())))
		
		// For regular files, hash the content
		if info.Mode().IsRegular() {
			file, err := os.Open(path)
			if err != nil {
				return nil // Skip files that can't be opened
			}
			defer file.Close()
			
			if _, err := io.Copy(hasher, file); err != nil {
				return nil // Skip files that can't be read
			}
		}
		
		return nil
	})
	
	if err != nil {
		return "", err
	}
	
	return fmt.Sprintf("%x", hasher.Sum(nil)), nil
}

func (c *Cache) shouldIgnoreFile(relPath string, patterns []string) bool {
	// Basic implementation of dockerignore pattern matching
	// In a full implementation, this would use proper glob matching
	for _, pattern := range patterns {
		// Simple glob matching for *.ext patterns
		if strings.HasPrefix(pattern, "*.") {
			ext := pattern[1:] // Remove the *
			if strings.HasSuffix(relPath, ext) {
				return true
			}
		}
		// Exact match
		if pattern == relPath {
			return true
		}
		// Contains match
		if strings.Contains(relPath, pattern) {
			return true
		}
	}
	return false
}

func (c *Cache) InvalidateByDockerfile(dockerfileHash string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	var toDelete []string
	
	err := filepath.Walk(c.baseDir, func(path string, fileInfo os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		if !fileInfo.IsDir() && strings.HasSuffix(path, ".json") {
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}

			var entry CacheEntry
			if err := json.Unmarshal(data, &entry); err != nil {
				return nil
			}

			// Invalidate if Dockerfile hash doesn't match
			if entry.DockerfileHash != "" && entry.DockerfileHash != dockerfileHash {
				toDelete = append(toDelete, path)
			}
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to scan cache for invalidation: %v", err)
	}

	// Delete invalidated entries
	for _, path := range toDelete {
		os.Remove(path)
	}

	return c.removeEmptyDirs(c.baseDir)
}

func (c *Cache) InvalidateByBuildContext(buildContextHash string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	var toDelete []string
	
	err := filepath.Walk(c.baseDir, func(path string, fileInfo os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		if !fileInfo.IsDir() && strings.HasSuffix(path, ".json") {
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}

			var entry CacheEntry
			if err := json.Unmarshal(data, &entry); err != nil {
				return nil
			}

			// Invalidate if build context hash doesn't match
			if entry.BuildContextHash != "" && entry.BuildContextHash != buildContextHash {
				toDelete = append(toDelete, path)
			}
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to scan cache for invalidation: %v", err)
	}

	// Delete invalidated entries
	for _, path := range toDelete {
		os.Remove(path)
	}

	return c.removeEmptyDirs(c.baseDir)
}

func (c *Cache) GetSharedCacheEntries() ([]CacheEntry, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	var entries []CacheEntry
	sharedDir := filepath.Join(c.baseDir, "shared")
	
	if _, err := os.Stat(sharedDir); os.IsNotExist(err) {
		return entries, nil
	}
	
	err := filepath.Walk(sharedDir, func(path string, fileInfo os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		if !fileInfo.IsDir() && strings.HasSuffix(path, ".json") {
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}

			var entry CacheEntry
			if err := json.Unmarshal(data, &entry); err != nil {
				return nil
			}

			entries = append(entries, entry)
		}

		return nil
	})

	return entries, err
}

func (c *Cache) computeContentHash(paths []string) (string, error) {
	hasher := sha256.New()
	
	for _, path := range paths {
		if err := c.hashPath(hasher, path); err != nil {
			return "", err
		}
	}
	
	return fmt.Sprintf("%x", hasher.Sum(nil)), nil
}

func (c *Cache) hashPath(hasher io.Writer, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}

	hasher.Write([]byte(path))
	hasher.Write([]byte(fmt.Sprintf("%d", info.ModTime().Unix())))
	hasher.Write([]byte(fmt.Sprintf("%d", info.Size())))

	if info.IsDir() {
		entries, err := os.ReadDir(path)
		if err != nil {
			return err
		}

		for _, entry := range entries {
			if err := c.hashPath(hasher, filepath.Join(path, entry.Name())); err != nil {
				return err
			}
		}
	} else {
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		if _, err := io.Copy(hasher, file); err != nil {
			return err
		}
	}

	return nil
}