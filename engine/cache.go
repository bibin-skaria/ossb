package engine

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bibin-skaria/ossb/internal/types"
)

type Cache struct {
	baseDir string
	hits    int64
	misses  int64
}

type CacheEntry struct {
	Key       string                `json:"key"`
	Result    *types.OperationResult `json:"result"`
	Timestamp time.Time             `json:"timestamp"`
	Size      int64                 `json:"size"`
}

func NewCache(baseDir string) *Cache {
	return &Cache{
		baseDir: baseDir,
	}
}

func (c *Cache) Get(key string) (*types.OperationResult, bool) {
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

	c.hits++
	entry.Result.CacheHit = true
	return entry.Result, true
}

func (c *Cache) Set(key string, result *types.OperationResult) error {
	entryDir := c.getEntryDir(key)
	if err := os.MkdirAll(entryDir, 0755); err != nil {
		return fmt.Errorf("failed to create cache directory: %v", err)
	}

	entry := CacheEntry{
		Key:       key,
		Result:    result,
		Timestamp: time.Now(),
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

	return nil
}

func (c *Cache) Info() (*types.CacheInfo, error) {
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
	cutoff := time.Now().Add(-24 * time.Hour) 

	err := filepath.Walk(c.baseDir, func(path string, fileInfo os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		if !fileInfo.IsDir() && strings.HasSuffix(path, ".json") {
			if fileInfo.ModTime().Before(cutoff) {
				if err := os.Remove(path); err != nil {
					return err
				}
			}
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to prune cache: %v", err)
	}

	return c.removeEmptyDirs(c.baseDir)
}

func (c *Cache) getEntryPath(key string) string {
	return filepath.Join(c.getEntryDir(key), key+".json")
}

func (c *Cache) getEntryDir(key string) string {
	hash := sha256.Sum256([]byte(key))
	hashStr := fmt.Sprintf("%x", hash)
	return filepath.Join(c.baseDir, hashStr[:2], hashStr[2:4])
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