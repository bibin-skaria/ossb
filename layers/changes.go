package layers

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// FileInfo represents extended file information
type FileInfo struct {
	Path     string
	Mode     os.FileMode
	Size     int64
	ModTime  time.Time
	UID      int
	GID      int
	Linkname string
	Exists   bool
}

// DetectChanges compares two directory trees and returns the differences
func (lm *DefaultLayerManager) DetectChanges(oldPath, newPath string) ([]FileChange, error) {
	oldFiles, err := lm.scanDirectory(oldPath)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to scan old directory: %v", err)
	}

	newFiles, err := lm.scanDirectory(newPath)
	if err != nil {
		return nil, fmt.Errorf("failed to scan new directory: %v", err)
	}

	return lm.compareFileMaps(oldFiles, newFiles, newPath)
}

// scanDirectory recursively scans a directory and returns a map of file information
func (lm *DefaultLayerManager) scanDirectory(rootPath string) (map[string]*FileInfo, error) {
	files := make(map[string]*FileInfo)

	if _, err := os.Stat(rootPath); os.IsNotExist(err) {
		return files, nil // Empty map for non-existent directory
	}

	err := filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Get relative path
		relPath, err := filepath.Rel(rootPath, path)
		if err != nil {
			return err
		}

		// Skip the root directory itself
		if relPath == "." {
			return nil
		}

		// Normalize path separators
		relPath = filepath.ToSlash(relPath)
		if !strings.HasPrefix(relPath, "/") {
			relPath = "/" + relPath
		}

		fileInfo := &FileInfo{
			Path:    relPath,
			Mode:    info.Mode(),
			Size:    info.Size(),
			ModTime: info.ModTime(),
			Exists:  true,
		}

		// Get UID/GID on Unix systems
		if stat, ok := info.Sys().(*syscall.Stat_t); ok {
			fileInfo.UID = int(stat.Uid)
			fileInfo.GID = int(stat.Gid)
		}

		// Handle symlinks
		if info.Mode()&os.ModeSymlink != 0 {
			linkTarget, err := os.Readlink(path)
			if err != nil {
				return fmt.Errorf("failed to read symlink %s: %v", path, err)
			}
			fileInfo.Linkname = linkTarget
		}

		files[relPath] = fileInfo
		return nil
	})

	return files, err
}

// compareFileMaps compares two file maps and generates change list
func (lm *DefaultLayerManager) compareFileMaps(oldFiles, newFiles map[string]*FileInfo, newBasePath string) ([]FileChange, error) {
	var changes []FileChange

	// Find added and modified files
	for path, newInfo := range newFiles {
		oldInfo, existed := oldFiles[path]

		if !existed {
			// File was added
			change, err := lm.createFileChange(path, ChangeTypeAdd, newInfo, newBasePath)
			if err != nil {
				return nil, err
			}
			changes = append(changes, *change)
		} else if lm.fileChanged(oldInfo, newInfo) {
			// File was modified
			change, err := lm.createFileChange(path, ChangeTypeModify, newInfo, newBasePath)
			if err != nil {
				return nil, err
			}
			changes = append(changes, *change)
		}
	}

	// Find deleted files
	for path, oldInfo := range oldFiles {
		if _, exists := newFiles[path]; !exists {
			change := &FileChange{
				Path:      path,
				Type:      ChangeTypeDelete,
				Mode:      oldInfo.Mode,
				Size:      0,
				Timestamp: time.Now(),
				UID:       oldInfo.UID,
				GID:       oldInfo.GID,
			}
			changes = append(changes, *change)
		}
	}

	return changes, nil
}

// fileChanged determines if a file has been modified
func (lm *DefaultLayerManager) fileChanged(oldInfo, newInfo *FileInfo) bool {
	// Check basic attributes
	if oldInfo.Mode != newInfo.Mode ||
		oldInfo.Size != newInfo.Size ||
		oldInfo.UID != newInfo.UID ||
		oldInfo.GID != newInfo.GID ||
		oldInfo.Linkname != newInfo.Linkname {
		return true
	}

	// For regular files, check modification time
	if newInfo.Mode.IsRegular() && !oldInfo.ModTime.Equal(newInfo.ModTime) {
		return true
	}

	return false
}

// createFileChange creates a FileChange from FileInfo
func (lm *DefaultLayerManager) createFileChange(path string, changeType ChangeType, info *FileInfo, basePath string) (*FileChange, error) {
	change := &FileChange{
		Path:      path,
		Type:      changeType,
		Mode:      info.Mode,
		Size:      info.Size,
		Timestamp: info.ModTime,
		UID:       info.UID,
		GID:       info.GID,
		Linkname:  info.Linkname,
	}

	// For regular files, open content reader
	if info.Mode.IsRegular() && info.Size > 0 {
		fullPath := filepath.Join(basePath, strings.TrimPrefix(path, "/"))
		file, err := os.Open(fullPath)
		if err != nil {
			return nil, fmt.Errorf("failed to open file %s: %v", fullPath, err)
		}
		change.Content = file
	}

	return change, nil
}

// ApplyChanges applies a list of file changes to a base directory
func (lm *DefaultLayerManager) ApplyChanges(basePath string, changes []FileChange) error {
	for _, change := range changes {
		if err := lm.applyChange(basePath, change); err != nil {
			return fmt.Errorf("failed to apply change %s: %v", change.Path, err)
		}
	}
	return nil
}

// applyChange applies a single file change to the filesystem
func (lm *DefaultLayerManager) applyChange(basePath string, change FileChange) error {
	targetPath := filepath.Join(basePath, strings.TrimPrefix(change.Path, "/"))

	switch change.Type {
	case ChangeTypeAdd, ChangeTypeModify:
		// Ensure parent directory exists
		if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
			return err
		}

		if change.Mode.IsDir() {
			return os.MkdirAll(targetPath, change.Mode)
		} else if change.Mode&os.ModeSymlink != 0 {
			// Remove existing file/symlink if it exists
			os.Remove(targetPath)
			return os.Symlink(change.Linkname, targetPath)
		} else if change.Mode.IsRegular() {
			file, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, change.Mode)
			if err != nil {
				return err
			}
			defer file.Close()

			if change.Content != nil {
				if _, err := io.Copy(file, change.Content); err != nil {
					return err
				}
			}

			// Set timestamps
			return os.Chtimes(targetPath, change.Timestamp, change.Timestamp)
		}

	case ChangeTypeDelete:
		return os.RemoveAll(targetPath)

	default:
		return fmt.Errorf("unknown change type: %v", change.Type)
	}

	return nil
}

// CloseFileChanges closes any open file readers in the changes
func CloseFileChanges(changes []FileChange) {
	for _, change := range changes {
		if change.Content != nil {
			if closer, ok := change.Content.(io.Closer); ok {
				closer.Close()
			}
		}
	}
}

// CalculateChangesSize calculates the total size of changes
func CalculateChangesSize(changes []FileChange) int64 {
	var totalSize int64
	for _, change := range changes {
		if change.Type != ChangeTypeDelete {
			totalSize += change.Size
		}
	}
	return totalSize
}

// FilterChanges filters changes based on a predicate function
func FilterChanges(changes []FileChange, predicate func(FileChange) bool) []FileChange {
	var filtered []FileChange
	for _, change := range changes {
		if predicate(change) {
			filtered = append(filtered, change)
		}
	}
	return filtered
}

// GroupChangesByType groups changes by their type
func GroupChangesByType(changes []FileChange) map[ChangeType][]FileChange {
	groups := make(map[ChangeType][]FileChange)
	for _, change := range changes {
		groups[change.Type] = append(groups[change.Type], change)
	}
	return groups
}