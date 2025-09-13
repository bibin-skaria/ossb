package k8s

import (
	"context"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/bibin-skaria/ossb/internal/types"
)

func TestNewJobLifecycleManager(t *testing.T) {
	buildID := "test-build-123"
	manager := NewJobLifecycleManager(buildID)

	if manager == nil {
		t.Fatal("NewJobLifecycleManager() returned nil")
	}

	if manager.buildID != buildID {
		t.Errorf("buildID = %s, expected %s", manager.buildID, buildID)
	}

	if manager.status != JobStatusRunning {
		t.Errorf("initial status = %s, expected %s", manager.status, JobStatusRunning)
	}

	if manager.integration == nil {
		t.Error("integration is nil")
	}

	if manager.logger == nil {
		t.Error("logger is nil")
	}
}

func TestJobLifecycleManager_Start(t *testing.T) {
	manager := NewJobLifecycleManager("test-build")
	ctx := context.Background()

	newCtx, err := manager.Start(ctx)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if newCtx == nil {
		t.Error("Start() returned nil context")
	}

	// Verify context is cancellable
	select {
	case <-newCtx.Done():
		t.Error("Context should not be cancelled initially")
	default:
		// Expected
	}

	if manager.cancelFunc == nil {
		t.Error("cancelFunc should be set after Start()")
	}
}

func TestJobLifecycleManager_Complete(t *testing.T) {
	tests := []struct {
		name           string
		buildResult    *types.BuildResult
		expectedStatus JobStatus
		expectedExit   ExitCode
	}{
		{
			name: "successful build",
			buildResult: &types.BuildResult{
				Success:    true,
				Operations: 10,
				CacheHits:  5,
				Duration:   "30s",
			},
			expectedStatus: JobStatusSucceeded,
			expectedExit:   ExitCodeSuccess,
		},
		{
			name: "failed build",
			buildResult: &types.BuildResult{
				Success:    false,
				Error:      "build failed",
				Operations: 8,
				CacheHits:  3,
				Duration:   "20s",
			},
			expectedStatus: JobStatusFailed,
			expectedExit:   ExitCodeBuildError,
		},
		{
			name: "registry error",
			buildResult: &types.BuildResult{
				Success: false,
				Error:   "failed to push to registry",
			},
			expectedStatus: JobStatusFailed,
			expectedExit:   ExitCodeRegistryError,
		},
		{
			name: "auth error",
			buildResult: &types.BuildResult{
				Success: false,
				Error:   "authentication failed",
			},
			expectedStatus: JobStatusFailed,
			expectedExit:   ExitCodeAuthError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := NewJobLifecycleManager("test-build")
			ctx := context.Background()

			exitCode := manager.Complete(ctx, tt.buildResult)

			if exitCode != tt.expectedExit {
				t.Errorf("Complete() exit code = %v, expected %v", exitCode, tt.expectedExit)
			}

			if manager.GetStatus() != tt.expectedStatus {
				t.Errorf("status = %s, expected %s", manager.GetStatus(), tt.expectedStatus)
			}
		})
	}
}

func TestJobLifecycleManager_Fail(t *testing.T) {
	tests := []struct {
		name         string
		error        string
		stage        string
		expectedExit ExitCode
	}{
		{
			name:         "general build error",
			error:        "build failed",
			stage:        "build",
			expectedExit: ExitCodeBuildError,
		},
		{
			name:         "registry error",
			error:        "failed to push image to registry",
			stage:        "push",
			expectedExit: ExitCodeRegistryError,
		},
		{
			name:         "config error",
			error:        "invalid configuration",
			stage:        "init",
			expectedExit: ExitCodeConfigError,
		},
		{
			name:         "resource error",
			error:        "out of memory",
			stage:        "build",
			expectedExit: ExitCodeResourceError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := NewJobLifecycleManager("test-build")
			ctx := context.Background()

			exitCode := manager.Fail(ctx, &testError{tt.error}, tt.stage)

			if exitCode != tt.expectedExit {
				t.Errorf("Fail() exit code = %v, expected %v", exitCode, tt.expectedExit)
			}

			if manager.GetStatus() != JobStatusFailed {
				t.Errorf("status = %s, expected %s", manager.GetStatus(), JobStatusFailed)
			}
		})
	}
}

func TestJobLifecycleManager_Cancel(t *testing.T) {
	manager := NewJobLifecycleManager("test-build")
	ctx := context.Background()

	// Start the manager to set up cancellation
	newCtx, err := manager.Start(ctx)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	reason := "user requested cancellation"
	exitCode := manager.Cancel(newCtx, reason)

	if exitCode != ExitCodeCancelledError {
		t.Errorf("Cancel() exit code = %v, expected %v", exitCode, ExitCodeCancelledError)
	}

	if manager.GetStatus() != JobStatusFailed {
		t.Errorf("status = %s, expected %s", manager.GetStatus(), JobStatusFailed)
	}

	if !manager.IsCancelled() {
		t.Error("IsCancelled() should return true after Cancel()")
	}

	// Verify context was cancelled
	select {
	case <-newCtx.Done():
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Error("Context should be cancelled after Cancel()")
	}
}

func TestJobLifecycleManager_ReportProgress(t *testing.T) {
	manager := NewJobLifecycleManager("test-build")
	ctx := context.Background()

	// Should not panic or error
	manager.ReportProgress(ctx, "build", 50.0, "Building layer 1", "linux/amd64", "RUN", true)

	// Test with cancelled manager
	manager.Cancel(ctx, "test")
	manager.ReportProgress(ctx, "build", 75.0, "Building layer 2", "linux/amd64", "COPY", false)
	// Should handle gracefully when cancelled
}

func TestJobLifecycleManager_AddCleanupFunc(t *testing.T) {
	manager := NewJobLifecycleManager("test-build")
	ctx := context.Background()

	cleanupCalled := false
	cleanupFunc := func() error {
		cleanupCalled = true
		return nil
	}

	manager.AddCleanupFunc(cleanupFunc)

	// Complete the job to trigger cleanup
	result := &types.BuildResult{Success: true}
	manager.Complete(ctx, result)

	if !cleanupCalled {
		t.Error("Cleanup function was not called")
	}
}

func TestJobLifecycleManager_AddCleanupFuncWithError(t *testing.T) {
	manager := NewJobLifecycleManager("test-build")
	ctx := context.Background()

	cleanupError := &testError{"cleanup failed"}
	cleanupFunc := func() error {
		return cleanupError
	}

	manager.AddCleanupFunc(cleanupFunc)

	// Complete the job to trigger cleanup
	result := &types.BuildResult{Success: true}
	exitCode := manager.Complete(ctx, result)

	// Should still return success even if cleanup fails
	if exitCode != ExitCodeSuccess {
		t.Errorf("Complete() exit code = %v, expected %v", exitCode, ExitCodeSuccess)
	}
}

func TestJobLifecycleManager_GetDuration(t *testing.T) {
	manager := NewJobLifecycleManager("test-build")

	// Wait a small amount of time
	time.Sleep(10 * time.Millisecond)

	duration := manager.GetDuration()
	if duration < 10*time.Millisecond {
		t.Errorf("GetDuration() = %v, expected at least 10ms", duration)
	}
}

func TestJobLifecycleManager_SignalHandling(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping signal handling test in short mode")
	}

	manager := NewJobLifecycleManager("test-build")
	ctx := context.Background()

	newCtx, err := manager.Start(ctx)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Send SIGTERM to current process
	go func() {
		time.Sleep(50 * time.Millisecond)
		syscall.Kill(os.Getpid(), syscall.SIGTERM)
	}()

	// Wait for cancellation
	select {
	case <-newCtx.Done():
		// Expected - context should be cancelled due to signal
	case <-time.After(200 * time.Millisecond):
		t.Error("Context should be cancelled due to signal")
	}

	if !manager.IsCancelled() {
		t.Error("Manager should be cancelled after signal")
	}
}

func TestDetermineExitCode(t *testing.T) {
	manager := NewJobLifecycleManager("test-build")

	tests := []struct {
		error    string
		expected ExitCode
	}{
		{"registry connection failed", ExitCodeRegistryError},
		{"failed to push image", ExitCodeRegistryError},
		{"authentication failed", ExitCodeAuthError},
		{"invalid credentials", ExitCodeAuthError},
		{"invalid configuration", ExitCodeConfigError},
		{"parse error in dockerfile", ExitCodeConfigError},
		{"out of memory", ExitCodeResourceError},
		{"disk space exceeded", ExitCodeResourceError},
		{"operation timeout", ExitCodeTimeoutError},
		{"deadline exceeded", ExitCodeTimeoutError},
		{"build cancelled", ExitCodeCancelledError},
		{"interrupted by user", ExitCodeCancelledError},
		{"dockerfile instruction failed", ExitCodeBuildError},
		{"build step failed", ExitCodeBuildError},
		{"unknown error", ExitCodeGeneralError},
	}

	for _, tt := range tests {
		t.Run(tt.error, func(t *testing.T) {
			result := manager.determineExitCode(tt.error)
			if result != tt.expected {
				t.Errorf("determineExitCode(%s) = %v, expected %v", tt.error, result, tt.expected)
			}
		})
	}
}

func TestContains(t *testing.T) {
	tests := []struct {
		text     string
		keywords []string
		expected bool
	}{
		{"registry connection failed", []string{"registry"}, true},
		{"Registry Connection Failed", []string{"registry"}, true}, // Case insensitive
		{"authentication error", []string{"auth", "credential"}, true},
		{"build completed successfully", []string{"registry", "auth"}, false},
		{"timeout occurred", []string{"timeout", "deadline"}, true},
		{"", []string{"test"}, false},
		{"test", []string{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			result := contains(tt.text, tt.keywords...)
			if result != tt.expected {
				t.Errorf("contains(%s, %v) = %v, expected %v", tt.text, tt.keywords, result, tt.expected)
			}
		})
	}
}

// Helper type for testing
type testError struct {
	message string
}

func (e *testError) Error() string {
	return e.message
}

// Benchmark tests
func BenchmarkJobLifecycleManager_ReportProgress(b *testing.B) {
	manager := NewJobLifecycleManager("test-build")
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		manager.ReportProgress(ctx, "build", 50.0, "Building", "linux/amd64", "RUN", false)
	}
}

func BenchmarkDetermineExitCode(b *testing.B) {
	manager := NewJobLifecycleManager("test-build")
	errors := []string{
		"registry connection failed",
		"authentication failed",
		"invalid configuration",
		"out of memory",
		"operation timeout",
		"build cancelled",
		"dockerfile instruction failed",
		"unknown error",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		error := errors[i%len(errors)]
		manager.determineExitCode(error)
	}
}