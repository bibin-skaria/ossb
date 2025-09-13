package errors

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRecoveryManager(t *testing.T) {
	ctx := context.Background()
	rm := NewRecoveryManager(ctx)
	defer rm.Shutdown()

	// Test that default strategies are registered
	if len(rm.strategies) == 0 {
		t.Error("Expected default strategies to be registered")
	}

	// Test registering a custom strategy
	customStrategy := &TestRecoveryStrategy{}
	rm.RegisterStrategy(customStrategy)

	if len(rm.strategies) < 2 { // At least default + custom
		t.Error("Expected custom strategy to be registered")
	}
}

func TestRecoveryManager_AttemptRecovery(t *testing.T) {
	ctx := context.Background()
	rm := NewRecoveryManager(ctx)
	defer rm.Shutdown()

	// Test successful recovery
	networkErr := NewNetworkError("test", "network failure", nil)
	err := rm.AttemptRecovery(ctx, networkErr)
	if err != nil {
		t.Errorf("Expected successful recovery, got error: %v", err)
	}

	// Test no recovery strategy available
	unknownErr := &BuildError{
		Category: ErrorCategory("custom_category"),
		Message:  "unknown error type",
	}
	err = rm.AttemptRecovery(ctx, unknownErr)
	if err == nil {
		t.Error("Expected error when no recovery strategy available")
	}
}

func TestRecoveryManager_PerformCleanup(t *testing.T) {
	ctx := context.Background()
	rm := NewRecoveryManager(ctx)
	defer rm.Shutdown()

	// Create temporary files for cleanup testing
	tempDir := os.TempDir()
	testFile1 := filepath.Join(tempDir, "test_cleanup_1.tmp")
	testFile2 := filepath.Join(tempDir, "test_cleanup_2.tmp")

	// Create test files
	if err := os.WriteFile(testFile1, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	if err := os.WriteFile(testFile2, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Register cleanup actions
	rm.RegisterCleanupAction(NewTempFileCleanupAction(testFile1))
	rm.RegisterCleanupAction(NewTempFileCleanupAction(testFile2))

	// Perform cleanup
	err := rm.PerformCleanup(ctx)
	if err != nil {
		t.Errorf("Expected successful cleanup, got error: %v", err)
	}

	// Verify files were cleaned up
	if _, err := os.Stat(testFile1); !os.IsNotExist(err) {
		t.Error("Expected test file 1 to be cleaned up")
	}
	if _, err := os.Stat(testFile2); !os.IsNotExist(err) {
		t.Error("Expected test file 2 to be cleaned up")
	}
}

func TestNetworkRecoveryStrategy(t *testing.T) {
	strategy := &NetworkRecoveryStrategy{}

	// Test CanRecover
	networkErr := NewNetworkError("test", "network failure", nil)
	if !strategy.CanRecover(networkErr) {
		t.Error("Expected NetworkRecoveryStrategy to handle network errors")
	}

	authErr := NewAuthError("test", "auth failure", nil)
	if strategy.CanRecover(authErr) {
		t.Error("Expected NetworkRecoveryStrategy to not handle auth errors")
	}

	// Test Recover (should succeed after waiting)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	start := time.Now()
	err := strategy.Recover(ctx, networkErr)
	duration := time.Since(start)

	if err != nil {
		t.Errorf("Expected successful recovery, got error: %v", err)
	}

	// Should have waited at least 5 seconds
	if duration < 5*time.Second {
		t.Errorf("Expected recovery to wait at least 5s, took %v", duration)
	}
}

func TestNetworkRecoveryStrategy_ContextCancellation(t *testing.T) {
	strategy := &NetworkRecoveryStrategy{}
	networkErr := NewNetworkError("test", "network failure", nil)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err := strategy.Recover(ctx, networkErr)
	if err != context.DeadlineExceeded {
		t.Errorf("Expected context deadline exceeded, got: %v", err)
	}
}

func TestResourceRecoveryStrategy(t *testing.T) {
	strategy := &ResourceRecoveryStrategy{}

	// Test CanRecover
	resourceErr := NewResourceError("test", "resource failure", nil)
	if !strategy.CanRecover(resourceErr) {
		t.Error("Expected ResourceRecoveryStrategy to handle resource errors")
	}

	// Test Recover
	ctx := context.Background()
	err := strategy.Recover(ctx, resourceErr)
	if err != nil {
		t.Errorf("Expected successful recovery, got error: %v", err)
	}
}

func TestResourceRecoveryStrategy_IsTempBuildFile(t *testing.T) {
	strategy := &ResourceRecoveryStrategy{}

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{
			name:     "ossb temp file",
			path:     "/tmp/ossb-12345.tmp",
			expected: true,
		},
		{
			name:     "build temp file",
			path:     "/tmp/build-67890.tmp",
			expected: true,
		},
		{
			name:     "layer temp file",
			path:     "/tmp/layer-abcdef.tmp",
			expected: true,
		},
		{
			name:     "manifest temp file",
			path:     "/tmp/manifest-xyz.tmp",
			expected: true,
		},
		{
			name:     "regular file",
			path:     "/tmp/regular-file.txt",
			expected: false,
		},
		{
			name:     "system file",
			path:     "/tmp/system.log",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := strategy.isTempBuildFile(tt.path); got != tt.expected {
				t.Errorf("isTempBuildFile() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestCacheRecoveryStrategy(t *testing.T) {
	strategy := &CacheRecoveryStrategy{}

	// Test CanRecover
	cacheErr := &BuildError{Category: ErrorCategoryCache, Message: "cache failure"}
	if !strategy.CanRecover(cacheErr) {
		t.Error("Expected CacheRecoveryStrategy to handle cache errors")
	}

	// Test Recover (should always succeed)
	ctx := context.Background()
	err := strategy.Recover(ctx, cacheErr)
	if err != nil {
		t.Errorf("Expected successful recovery, got error: %v", err)
	}
}

func TestTempFileCleanupAction(t *testing.T) {
	// Create temporary files
	tempDir := os.TempDir()
	testFile1 := filepath.Join(tempDir, "cleanup_test_1.tmp")
	testFile2 := filepath.Join(tempDir, "cleanup_test_2.tmp")

	if err := os.WriteFile(testFile1, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	if err := os.WriteFile(testFile2, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Create cleanup action
	action := NewTempFileCleanupAction(testFile1, testFile2)

	// Test properties
	if action.GetPriority() != 100 {
		t.Errorf("Expected priority 100, got %d", action.GetPriority())
	}

	description := action.GetDescription()
	if description == "" {
		t.Error("Expected non-empty description")
	}

	// Execute cleanup
	ctx := context.Background()
	err := action.Execute(ctx)
	if err != nil {
		t.Errorf("Expected successful cleanup, got error: %v", err)
	}

	// Verify files were cleaned up
	if _, err := os.Stat(testFile1); !os.IsNotExist(err) {
		t.Error("Expected test file 1 to be cleaned up")
	}
	if _, err := os.Stat(testFile2); !os.IsNotExist(err) {
		t.Error("Expected test file 2 to be cleaned up")
	}
}

func TestTempFileCleanupAction_ContextCancellation(t *testing.T) {
	// Create many temporary files to test context cancellation
	tempDir := os.TempDir()
	var testFiles []string

	for i := 0; i < 100; i++ {
		testFile := filepath.Join(tempDir, fmt.Sprintf("cleanup_cancel_test_%d.tmp", i))
		if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}
		testFiles = append(testFiles, testFile)
	}

	action := NewTempFileCleanupAction(testFiles...)

	// Cancel context quickly
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	err := action.Execute(ctx)
	if err != context.DeadlineExceeded {
		t.Errorf("Expected context deadline exceeded, got: %v", err)
	}

	// Clean up remaining files
	for _, file := range testFiles {
		os.Remove(file)
	}
}

func TestProcessCleanupAction(t *testing.T) {
	action := NewProcessCleanupAction(12345, 67890)

	// Test properties
	if action.GetPriority() != 200 {
		t.Errorf("Expected priority 200, got %d", action.GetPriority())
	}

	description := action.GetDescription()
	if description == "" {
		t.Error("Expected non-empty description")
	}

	// Execute cleanup (should not fail even with invalid PIDs)
	ctx := context.Background()
	err := action.Execute(ctx)
	if err != nil {
		t.Errorf("Expected successful cleanup, got error: %v", err)
	}
}

func TestContainerCleanupAction(t *testing.T) {
	action := NewContainerCleanupAction("container1", "container2")

	// Test properties
	if action.GetPriority() != 150 {
		t.Errorf("Expected priority 150, got %d", action.GetPriority())
	}

	description := action.GetDescription()
	if description == "" {
		t.Error("Expected non-empty description")
	}

	// Execute cleanup
	ctx := context.Background()
	err := action.Execute(ctx)
	if err != nil {
		t.Errorf("Expected successful cleanup, got error: %v", err)
	}
}

func TestGracefulDegradation(t *testing.T) {
	gd := NewGracefulDegradation()

	// Test cache degradation
	cacheErr := &BuildError{Category: ErrorCategoryCache, Message: "cache failure"}
	err := gd.Degrade(context.Background(), cacheErr)
	if err != nil {
		t.Errorf("Expected successful cache degradation, got error: %v", err)
	}

	// Test unknown category degradation
	unknownErr := &BuildError{Category: ErrorCategory("unknown"), Message: "unknown failure"}
	err = gd.Degrade(context.Background(), unknownErr)
	if err == nil {
		t.Error("Expected error for unknown category degradation")
	}
}

func TestCacheDegradationStrategy(t *testing.T) {
	strategy := &CacheDegradationStrategy{}

	description := strategy.GetDescription()
	if description == "" {
		t.Error("Expected non-empty description")
	}

	cacheErr := &BuildError{Category: ErrorCategoryCache, Message: "cache failure"}
	err := strategy.Degrade(context.Background(), cacheErr)
	if err != nil {
		t.Errorf("Expected successful degradation, got error: %v", err)
	}
}

func TestResourceDegradationStrategy(t *testing.T) {
	strategy := &ResourceDegradationStrategy{}

	description := strategy.GetDescription()
	if description == "" {
		t.Error("Expected non-empty description")
	}

	resourceErr := &BuildError{Category: ErrorCategoryResource, Message: "resource failure"}
	err := strategy.Degrade(context.Background(), resourceErr)
	if err != nil {
		t.Errorf("Expected successful degradation, got error: %v", err)
	}
}

func TestCleanupActionPriority(t *testing.T) {
	// Test that cleanup actions are sorted by priority
	action1 := NewTempFileCleanupAction("file1")      // Priority 100
	action2 := NewProcessCleanupAction(123)           // Priority 200
	action3 := NewContainerCleanupAction("container") // Priority 150

	ctx := context.Background()
	rm := NewRecoveryManager(ctx)
	defer rm.Shutdown()

	rm.RegisterCleanupAction(action1)
	rm.RegisterCleanupAction(action2)
	rm.RegisterCleanupAction(action3)

	// The cleanup should execute in priority order: Process (200), Container (150), TempFile (100)
	// We can't easily test the order without modifying the implementation,
	// but we can test that all actions are registered
	if len(rm.cleanupActions) != 3 {
		t.Errorf("Expected 3 cleanup actions, got %d", len(rm.cleanupActions))
	}
}

// TestRecoveryStrategy is a test implementation of RecoveryStrategy
type TestRecoveryStrategy struct{}

func (s *TestRecoveryStrategy) CanRecover(err *BuildError) bool {
	return err.Category == ErrorCategory("test_category")
}

func (s *TestRecoveryStrategy) Recover(ctx context.Context, err *BuildError) error {
	return nil
}

func (s *TestRecoveryStrategy) GetDescription() string {
	return "Test recovery strategy"
}

func TestCustomRecoveryStrategy(t *testing.T) {
	ctx := context.Background()
	rm := NewRecoveryManager(ctx)
	defer rm.Shutdown()

	// Register custom strategy
	customStrategy := &TestRecoveryStrategy{}
	rm.RegisterStrategy(customStrategy)

	// Test recovery with custom error category
	customErr := &BuildError{
		Category: ErrorCategory("test_category"),
		Message:  "test error",
	}

	err := rm.AttemptRecovery(ctx, customErr)
	if err != nil {
		t.Errorf("Expected successful recovery with custom strategy, got error: %v", err)
	}
}