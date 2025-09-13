package layers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDetectChanges(t *testing.T) {
	// Create temporary directories for testing
	tempDir, err := os.MkdirTemp("", "changes_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	oldDir := filepath.Join(tempDir, "old")
	newDir := filepath.Join(tempDir, "new")

	// Create old directory structure
	if err := os.MkdirAll(oldDir, 0755); err != nil {
		t.Fatalf("Failed to create old dir: %v", err)
	}

	// Create some files in old directory
	oldFile1 := filepath.Join(oldDir, "file1.txt")
	if err := os.WriteFile(oldFile1, []byte("original content"), 0644); err != nil {
		t.Fatalf("Failed to create old file1: %v", err)
	}

	oldFile2 := filepath.Join(oldDir, "file2.txt")
	if err := os.WriteFile(oldFile2, []byte("will be deleted"), 0644); err != nil {
		t.Fatalf("Failed to create old file2: %v", err)
	}

	oldSubdir := filepath.Join(oldDir, "subdir")
	if err := os.MkdirAll(oldSubdir, 0755); err != nil {
		t.Fatalf("Failed to create old subdir: %v", err)
	}

	// Create new directory structure
	if err := os.MkdirAll(newDir, 0755); err != nil {
		t.Fatalf("Failed to create new dir: %v", err)
	}

	// Modified file
	newFile1 := filepath.Join(newDir, "file1.txt")
	if err := os.WriteFile(newFile1, []byte("modified content"), 0644); err != nil {
		t.Fatalf("Failed to create new file1: %v", err)
	}

	// New file
	newFile3 := filepath.Join(newDir, "file3.txt")
	if err := os.WriteFile(newFile3, []byte("new content"), 0644); err != nil {
		t.Fatalf("Failed to create new file3: %v", err)
	}

	// Keep subdir but add a file in it
	newSubdir := filepath.Join(newDir, "subdir")
	if err := os.MkdirAll(newSubdir, 0755); err != nil {
		t.Fatalf("Failed to create new subdir: %v", err)
	}

	newSubfile := filepath.Join(newSubdir, "subfile.txt")
	if err := os.WriteFile(newSubfile, []byte("sub content"), 0644); err != nil {
		t.Fatalf("Failed to create subfile: %v", err)
	}

	// file2.txt is deleted (not present in new directory)

	config := LayerConfig{
		Compression: CompressionNone,
	}
	lm := NewLayerManager(config)

	changes, err := lm.DetectChanges(oldDir, newDir)
	if err != nil {
		t.Fatalf("DetectChanges failed: %v", err)
	}

	// Analyze changes
	changesByType := GroupChangesByType(changes)

	// Should have 2 modifications (file1.txt and subdir - modified because we added a file to it)
	if len(changesByType[ChangeTypeModify]) != 2 {
		t.Errorf("Expected 2 modifications, got %d", len(changesByType[ChangeTypeModify]))
	}

	// Should have 2 additions (file3.txt and subdir/subfile.txt)
	if len(changesByType[ChangeTypeAdd]) != 2 {
		t.Errorf("Expected 2 additions, got %d", len(changesByType[ChangeTypeAdd]))
	}

	// Should have 1 deletion (file2.txt)
	if len(changesByType[ChangeTypeDelete]) != 1 {
		t.Errorf("Expected 1 deletion, got %d", len(changesByType[ChangeTypeDelete]))
	}

	// Verify specific changes
	foundModify := false
	foundAdd := false
	foundDelete := false

	for _, change := range changes {
		switch change.Type {
		case ChangeTypeModify:
			if change.Path == "/file1.txt" {
				foundModify = true
			}
		case ChangeTypeAdd:
			if change.Path == "/file3.txt" || change.Path == "/subdir/subfile.txt" {
				foundAdd = true
			}
		case ChangeTypeDelete:
			if change.Path == "/file2.txt" {
				foundDelete = true
			}
		}
	}

	if !foundModify {
		t.Error("Expected to find modification of file1.txt")
	}
	if !foundAdd {
		t.Error("Expected to find addition of new files")
	}
	if !foundDelete {
		t.Error("Expected to find deletion of file2.txt")
	}
}

func TestDetectChangesWithSymlinks(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "symlink_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	oldDir := filepath.Join(tempDir, "old")
	newDir := filepath.Join(tempDir, "new")

	// Create directories
	if err := os.MkdirAll(oldDir, 0755); err != nil {
		t.Fatalf("Failed to create old dir: %v", err)
	}
	if err := os.MkdirAll(newDir, 0755); err != nil {
		t.Fatalf("Failed to create new dir: %v", err)
	}

	// Create target file
	targetFile := filepath.Join(newDir, "target.txt")
	if err := os.WriteFile(targetFile, []byte("target content"), 0644); err != nil {
		t.Fatalf("Failed to create target file: %v", err)
	}

	// Create symlink in new directory
	symlinkPath := filepath.Join(newDir, "link.txt")
	if err := os.Symlink("target.txt", symlinkPath); err != nil {
		t.Fatalf("Failed to create symlink: %v", err)
	}

	config := LayerConfig{
		Compression: CompressionNone,
	}
	lm := NewLayerManager(config)

	changes, err := lm.DetectChanges(oldDir, newDir)
	if err != nil {
		t.Fatalf("DetectChanges failed: %v", err)
	}

	// Should detect both the target file and symlink as additions
	if len(changes) != 2 {
		t.Errorf("Expected 2 changes, got %d", len(changes))
	}

	// Find the symlink change
	var symlinkChange *FileChange
	for _, change := range changes {
		if change.Path == "/link.txt" {
			symlinkChange = &change
			break
		}
	}

	if symlinkChange == nil {
		t.Fatal("Symlink change not found")
	}

	if symlinkChange.Type != ChangeTypeAdd {
		t.Errorf("Expected symlink to be added, got %v", symlinkChange.Type)
	}

	if symlinkChange.Linkname != "target.txt" {
		t.Errorf("Expected linkname 'target.txt', got '%s'", symlinkChange.Linkname)
	}

	if symlinkChange.Mode&os.ModeSymlink == 0 {
		t.Error("Expected symlink mode to be set")
	}
}

func TestApplyChanges(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "apply_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	baseDir := filepath.Join(tempDir, "base")
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		t.Fatalf("Failed to create base dir: %v", err)
	}

	// Create existing file to be modified
	existingFile := filepath.Join(baseDir, "existing.txt")
	if err := os.WriteFile(existingFile, []byte("original"), 0644); err != nil {
		t.Fatalf("Failed to create existing file: %v", err)
	}

	// Create file to be deleted
	deleteFile := filepath.Join(baseDir, "delete.txt")
	if err := os.WriteFile(deleteFile, []byte("to be deleted"), 0644); err != nil {
		t.Fatalf("Failed to create delete file: %v", err)
	}

	config := LayerConfig{
		Compression: CompressionNone,
	}
	lm := NewLayerManager(config)

	// Define changes to apply
	changes := []FileChange{
		{
			Path:      "/existing.txt",
			Type:      ChangeTypeModify,
			Mode:      0644,
			Size:      8,
			Content:   strings.NewReader("modified"),
			Timestamp: time.Now(),
		},
		{
			Path:      "/new.txt",
			Type:      ChangeTypeAdd,
			Mode:      0644,
			Size:      11,
			Content:   strings.NewReader("new content"),
			Timestamp: time.Now(),
		},
		{
			Path:      "/newdir",
			Type:      ChangeTypeAdd,
			Mode:      os.ModeDir | 0755,
			Size:      0,
			Timestamp: time.Now(),
		},
		{
			Path:      "/delete.txt",
			Type:      ChangeTypeDelete,
			Mode:      0644,
			Timestamp: time.Now(),
		},
	}

	// Apply changes
	if err := lm.ApplyChanges(baseDir, changes); err != nil {
		t.Fatalf("ApplyChanges failed: %v", err)
	}

	// Verify modifications
	modifiedContent, err := os.ReadFile(existingFile)
	if err != nil {
		t.Fatalf("Failed to read modified file: %v", err)
	}
	if string(modifiedContent) != "modified" {
		t.Errorf("Expected 'modified', got '%s'", string(modifiedContent))
	}

	// Verify new file
	newFile := filepath.Join(baseDir, "new.txt")
	newContent, err := os.ReadFile(newFile)
	if err != nil {
		t.Fatalf("Failed to read new file: %v", err)
	}
	if string(newContent) != "new content" {
		t.Errorf("Expected 'new content', got '%s'", string(newContent))
	}

	// Verify new directory
	newDir := filepath.Join(baseDir, "newdir")
	if stat, err := os.Stat(newDir); err != nil {
		t.Fatalf("New directory not created: %v", err)
	} else if !stat.IsDir() {
		t.Error("Expected newdir to be a directory")
	}

	// Verify deletion
	if _, err := os.Stat(deleteFile); !os.IsNotExist(err) {
		t.Error("Expected delete.txt to be deleted")
	}
}

func TestCalculateChangesSize(t *testing.T) {
	changes := []FileChange{
		{
			Path: "/file1.txt",
			Type: ChangeTypeAdd,
			Size: 100,
		},
		{
			Path: "/file2.txt",
			Type: ChangeTypeModify,
			Size: 200,
		},
		{
			Path: "/file3.txt",
			Type: ChangeTypeDelete,
			Size: 50, // Should not be counted
		},
	}

	totalSize := CalculateChangesSize(changes)
	expectedSize := int64(300) // 100 + 200, deletion not counted

	if totalSize != expectedSize {
		t.Errorf("Expected total size %d, got %d", expectedSize, totalSize)
	}
}

func TestFilterChanges(t *testing.T) {
	changes := []FileChange{
		{Path: "/file1.txt", Type: ChangeTypeAdd},
		{Path: "/file2.txt", Type: ChangeTypeModify},
		{Path: "/file3.txt", Type: ChangeTypeDelete},
		{Path: "/file4.txt", Type: ChangeTypeAdd},
	}

	// Filter only additions
	additions := FilterChanges(changes, func(c FileChange) bool {
		return c.Type == ChangeTypeAdd
	})

	if len(additions) != 2 {
		t.Errorf("Expected 2 additions, got %d", len(additions))
	}

	for _, change := range additions {
		if change.Type != ChangeTypeAdd {
			t.Errorf("Expected ChangeTypeAdd, got %v", change.Type)
		}
	}
}

func TestGroupChangesByType(t *testing.T) {
	changes := []FileChange{
		{Path: "/file1.txt", Type: ChangeTypeAdd},
		{Path: "/file2.txt", Type: ChangeTypeModify},
		{Path: "/file3.txt", Type: ChangeTypeDelete},
		{Path: "/file4.txt", Type: ChangeTypeAdd},
	}

	groups := GroupChangesByType(changes)

	if len(groups[ChangeTypeAdd]) != 2 {
		t.Errorf("Expected 2 additions, got %d", len(groups[ChangeTypeAdd]))
	}

	if len(groups[ChangeTypeModify]) != 1 {
		t.Errorf("Expected 1 modification, got %d", len(groups[ChangeTypeModify]))
	}

	if len(groups[ChangeTypeDelete]) != 1 {
		t.Errorf("Expected 1 deletion, got %d", len(groups[ChangeTypeDelete]))
	}
}

func TestScanDirectory(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "scan_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create test structure
	testFile := filepath.Join(tempDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	testDir := filepath.Join(tempDir, "testdir")
	if err := os.MkdirAll(testDir, 0755); err != nil {
		t.Fatalf("Failed to create test dir: %v", err)
	}

	config := LayerConfig{}
	lm := NewLayerManager(config)

	files, err := lm.scanDirectory(tempDir)
	if err != nil {
		t.Fatalf("scanDirectory failed: %v", err)
	}

	// Should find both the file and directory
	if len(files) != 2 {
		t.Errorf("Expected 2 files, got %d", len(files))
	}

	// Check file info
	fileInfo, exists := files["/test.txt"]
	if !exists {
		t.Error("test.txt not found in scan results")
	} else {
		if !fileInfo.Mode.IsRegular() {
			t.Error("Expected test.txt to be a regular file")
		}
		if fileInfo.Size != 12 {
			t.Errorf("Expected size 12, got %d", fileInfo.Size)
		}
	}

	// Check directory info
	dirInfo, exists := files["/testdir"]
	if !exists {
		t.Error("testdir not found in scan results")
	} else {
		if !dirInfo.Mode.IsDir() {
			t.Error("Expected testdir to be a directory")
		}
	}
}

func TestFileChanged(t *testing.T) {
	config := LayerConfig{}
	lm := NewLayerManager(config)

	baseTime := time.Now()

	oldInfo := &FileInfo{
		Path:    "/test.txt",
		Mode:    0644,
		Size:    100,
		ModTime: baseTime,
		UID:     1000,
		GID:     1000,
	}

	// Same file - should not be changed
	sameInfo := &FileInfo{
		Path:    "/test.txt",
		Mode:    0644,
		Size:    100,
		ModTime: baseTime,
		UID:     1000,
		GID:     1000,
	}

	if lm.fileChanged(oldInfo, sameInfo) {
		t.Error("Expected files to be the same")
	}

	// Different size - should be changed
	differentSizeInfo := &FileInfo{
		Path:    "/test.txt",
		Mode:    0644,
		Size:    200,
		ModTime: baseTime,
		UID:     1000,
		GID:     1000,
	}

	if !lm.fileChanged(oldInfo, differentSizeInfo) {
		t.Error("Expected files to be different (size)")
	}

	// Different mode - should be changed
	differentModeInfo := &FileInfo{
		Path:    "/test.txt",
		Mode:    0755,
		Size:    100,
		ModTime: baseTime,
		UID:     1000,
		GID:     1000,
	}

	if !lm.fileChanged(oldInfo, differentModeInfo) {
		t.Error("Expected files to be different (mode)")
	}

	// Different modification time - should be changed
	differentTimeInfo := &FileInfo{
		Path:    "/test.txt",
		Mode:    0644,
		Size:    100,
		ModTime: baseTime.Add(time.Hour),
		UID:     1000,
		GID:     1000,
	}

	if !lm.fileChanged(oldInfo, differentTimeInfo) {
		t.Error("Expected files to be different (time)")
	}
}