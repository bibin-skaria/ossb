package errors

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// BuildErrorHandler provides error handling specifically for build operations
type BuildErrorHandler struct {
	*ErrorHandler
	buildID   string
	workDir   string
	tempFiles []string
	processes []int
}

// NewBuildErrorHandler creates a new build-specific error handler
func NewBuildErrorHandler(ctx context.Context, buildID, workDir string) *BuildErrorHandler {
	config := &ErrorHandlerConfig{
		DefaultRetryConfig: &RetryConfig{
			MaxRetries:      3,
			InitialInterval: 2 * time.Second,
			MaxInterval:     30 * time.Second,
			Multiplier:      2.0,
			Jitter:          true,
			RetryableErrors: []ErrorCategory{
				ErrorCategoryNetwork,
				ErrorCategoryRegistry,
				ErrorCategoryResource,
				ErrorCategoryCache,
				ErrorCategoryTimeout,
			},
		},
		CircuitBreakerEnabled:      true,
		CircuitBreakerMaxFailures:  5,
		CircuitBreakerResetTimeout: 60 * time.Second,
		RecoveryEnabled:            true,
		CleanupEnabled:             true,
		DegradationEnabled:         true,
		TrackRetryMetrics:          true,
		CollectErrors:              true,
	}

	eh := NewErrorHandler(ctx, config)
	
	beh := &BuildErrorHandler{
		ErrorHandler: eh,
		buildID:      buildID,
		workDir:      workDir,
		tempFiles:    make([]string, 0),
		processes:    make([]int, 0),
	}

	// Register build-specific recovery strategies
	beh.registerBuildRecoveryStrategies()
	
	return beh
}

// RegisterTempFile registers a temporary file for cleanup
func (beh *BuildErrorHandler) RegisterTempFile(path string) {
	beh.tempFiles = append(beh.tempFiles, path)
	beh.RegisterCleanupAction(NewTempFileCleanupAction(path))
}

// RegisterProcess registers a process for cleanup
func (beh *BuildErrorHandler) RegisterProcess(pid int) {
	beh.processes = append(beh.processes, pid)
	beh.RegisterCleanupAction(NewProcessCleanupAction(pid))
}

// RegisterContainer registers a container for cleanup
func (beh *BuildErrorHandler) RegisterContainer(containerID string) {
	beh.RegisterCleanupAction(NewContainerCleanupAction(containerID))
}

// HandleBuildError handles build-specific errors with context
func (beh *BuildErrorHandler) HandleBuildError(ctx context.Context, err error, operation, stage, platform string) error {
	return beh.HandleError(ctx, err, operation,
		WithStage(stage),
		WithPlatform(platform),
	)
}

// HandleRegistryError handles registry-specific errors with appropriate retry logic
func (beh *BuildErrorHandler) HandleRegistryError(ctx context.Context, err error, operation string) error {
	retryConfig := &RetryConfig{
		MaxRetries:      5,
		InitialInterval: 1 * time.Second,
		MaxInterval:     60 * time.Second,
		Multiplier:      2.0,
		Jitter:          true,
		RetryableErrors: []ErrorCategory{
			ErrorCategoryNetwork,
			ErrorCategoryRegistry,
			ErrorCategoryTimeout,
		},
	}

	return beh.HandleError(ctx, err, operation,
		WithRetryConfig(retryConfig),
	)
}

// HandleExecutorError handles executor-specific errors
func (beh *BuildErrorHandler) HandleExecutorError(ctx context.Context, err error, operation, stage, platform string) error {
	// Executor errors might need different handling
	retryConfig := &RetryConfig{
		MaxRetries:      2,
		InitialInterval: 5 * time.Second,
		MaxInterval:     30 * time.Second,
		Multiplier:      1.5,
		Jitter:          false,
		RetryableErrors: []ErrorCategory{
			ErrorCategoryResource,
			ErrorCategoryFilesystem,
			ErrorCategoryTimeout,
		},
	}

	return beh.HandleError(ctx, err, operation,
		WithStage(stage),
		WithPlatform(platform),
		WithRetryConfig(retryConfig),
	)
}

// PerformBuildCleanup performs comprehensive build cleanup
func (beh *BuildErrorHandler) PerformBuildCleanup(ctx context.Context) error {
	// Add build-specific cleanup actions
	if beh.workDir != "" {
		beh.RegisterCleanupAction(NewTempFileCleanupAction(
			filepath.Join(beh.workDir, "*.tmp"),
			filepath.Join(beh.workDir, "build-*"),
			filepath.Join(beh.workDir, "layer-*"),
		))
	}

	return beh.PerformCleanup(ctx)
}

// GetBuildReport generates a comprehensive build error report
func (beh *BuildErrorHandler) GetBuildReport() *BuildErrorReport {
	summary := beh.GetErrorSummary()
	cbStatus := beh.GetCircuitBreakerStatus()

	return &BuildErrorReport{
		BuildID:             beh.buildID,
		WorkDir:             beh.workDir,
		ErrorSummary:        summary,
		CircuitBreakerStatus: cbStatus,
		TempFiles:           beh.tempFiles,
		Processes:           beh.processes,
		Timestamp:           time.Now(),
	}
}

// registerBuildRecoveryStrategies registers build-specific recovery strategies
func (beh *BuildErrorHandler) registerBuildRecoveryStrategies() {
	// Docker daemon recovery strategy
	beh.RegisterRecoveryStrategy(&DockerDaemonRecoveryStrategy{})
	
	// Disk space recovery strategy
	beh.RegisterRecoveryStrategy(&DiskSpaceRecoveryStrategy{workDir: beh.workDir})
	
	// Build context recovery strategy
	beh.RegisterRecoveryStrategy(&BuildContextRecoveryStrategy{})
}

// BuildErrorReport represents a comprehensive build error report
type BuildErrorReport struct {
	BuildID              string                       `json:"build_id"`
	WorkDir              string                       `json:"work_dir"`
	ErrorSummary         *ErrorSummary                `json:"error_summary"`
	CircuitBreakerStatus map[string]CircuitBreakerStatus `json:"circuit_breaker_status"`
	TempFiles            []string                     `json:"temp_files"`
	Processes            []int                        `json:"processes"`
	Timestamp            time.Time                    `json:"timestamp"`
}

// Build-specific recovery strategies

// DockerDaemonRecoveryStrategy handles Docker daemon connectivity issues
type DockerDaemonRecoveryStrategy struct{}

func (s *DockerDaemonRecoveryStrategy) CanRecover(err *BuildError) bool {
	return err.Category == ErrorCategoryExecutor && 
		   contains(err.Message, "docker") || contains(err.Message, "daemon")
}

func (s *DockerDaemonRecoveryStrategy) Recover(ctx context.Context, err *BuildError) error {
	// Try to restart Docker daemon or switch to alternative executor
	// This is a placeholder for actual Docker daemon recovery logic
	return NewErrorBuilder().
		Category(ErrorCategoryExecutor).
		Severity(ErrorSeverityMedium).
		Operation("docker_daemon_recovery").
		Message("Docker daemon recovery not implemented").
		Build()
}

func (s *DockerDaemonRecoveryStrategy) GetDescription() string {
	return "Handles Docker daemon connectivity issues"
}

// DiskSpaceRecoveryStrategy handles disk space issues
type DiskSpaceRecoveryStrategy struct {
	workDir string
}

func (s *DiskSpaceRecoveryStrategy) CanRecover(err *BuildError) bool {
	return err.Category == ErrorCategoryResource && 
		   (contains(err.Message, "disk") || contains(err.Message, "space") || contains(err.Message, "no space"))
}

func (s *DiskSpaceRecoveryStrategy) Recover(ctx context.Context, err *BuildError) error {
	// Try to free up disk space
	if err := s.cleanupTempFiles(); err != nil {
		return err
	}
	
	if err := s.cleanupCache(); err != nil {
		return err
	}
	
	return nil
}

func (s *DiskSpaceRecoveryStrategy) cleanupTempFiles() error {
	if s.workDir == "" {
		return nil
	}
	
	// Clean up temporary files in work directory
	tempPatterns := []string{
		filepath.Join(s.workDir, "*.tmp"),
		filepath.Join(s.workDir, "build-*"),
		filepath.Join(s.workDir, "layer-*"),
		filepath.Join(s.workDir, "manifest-*"),
	}
	
	for _, pattern := range tempPatterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		
		for _, match := range matches {
			os.RemoveAll(match) // Ignore errors
		}
	}
	
	return nil
}

func (s *DiskSpaceRecoveryStrategy) cleanupCache() error {
	// Clean up build cache
	cacheDir := filepath.Join(s.workDir, ".cache")
	if _, err := os.Stat(cacheDir); err == nil {
		// Remove old cache entries (older than 24 hours)
		return filepath.Walk(cacheDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			
			if time.Since(info.ModTime()) > 24*time.Hour {
				os.RemoveAll(path)
			}
			
			return nil
		})
	}
	
	return nil
}

func (s *DiskSpaceRecoveryStrategy) GetDescription() string {
	return "Handles disk space issues by cleaning up temporary files and cache"
}

// BuildContextRecoveryStrategy handles build context issues
type BuildContextRecoveryStrategy struct{}

func (s *BuildContextRecoveryStrategy) CanRecover(err *BuildError) bool {
	return err.Category == ErrorCategoryFilesystem && 
		   (contains(err.Message, "context") || contains(err.Message, "dockerfile"))
}

func (s *BuildContextRecoveryStrategy) Recover(ctx context.Context, err *BuildError) error {
	// Try to recover build context issues
	// This could involve re-downloading context, fixing permissions, etc.
	return NewErrorBuilder().
		Category(ErrorCategoryFilesystem).
		Severity(ErrorSeverityMedium).
		Operation("build_context_recovery").
		Message("Build context recovery not implemented").
		Build()
}

func (s *BuildContextRecoveryStrategy) GetDescription() string {
	return "Handles build context and Dockerfile issues"
}

// Utility functions for integration with existing OSSB components

// WrapRegistryError wraps registry errors with proper categorization
func WrapRegistryError(err error, operation, registry string) *BuildError {
	if err == nil {
		return nil
	}

	builder := NewErrorBuilder().
		Category(ErrorCategoryRegistry).
		Operation(operation).
		Cause(err).
		Metadata("registry", registry)

	errMsg := err.Error()
	switch {
	case contains(errMsg, "unauthorized") || contains(errMsg, "authentication"):
		return builder.
			Severity(ErrorSeverityCritical).
			Message("Registry authentication failed").
			Suggestion("Verify registry credentials and permissions").
			Retryable(false).
			Build()
			
	case contains(errMsg, "not found") || contains(errMsg, "404"):
		return builder.
			Severity(ErrorSeverityHigh).
			Message("Registry resource not found").
			Suggestion("Verify image name and tag").
			Retryable(false).
			Build()
			
	case contains(errMsg, "timeout") || contains(errMsg, "connection"):
		return builder.
			Severity(ErrorSeverityMedium).
			Message("Registry connection failed").
			Suggestion("Check network connectivity and retry").
			Retryable(true).
			Build()
			
	default:
		return builder.
			Message(errMsg).
			Build()
	}
}

// WrapExecutorError wraps executor errors with proper categorization
func WrapExecutorError(err error, operation, executor string) *BuildError {
	if err == nil {
		return nil
	}

	builder := NewErrorBuilder().
		Category(ErrorCategoryExecutor).
		Operation(operation).
		Cause(err).
		Metadata("executor", executor)

	errMsg := err.Error()
	switch {
	case contains(errMsg, "permission") || contains(errMsg, "denied"):
		return builder.
			Severity(ErrorSeverityHigh).
			Message("Executor permission denied").
			Suggestion("Check file permissions or try rootless mode").
			Retryable(false).
			Build()
			
	case contains(errMsg, "memory") || contains(errMsg, "oom"):
		return builder.
			Severity(ErrorSeverityMedium).
			Message("Executor out of memory").
			Suggestion("Increase available memory or use smaller base images").
			Retryable(true).
			Build()
			
	case contains(errMsg, "timeout"):
		return builder.
			Severity(ErrorSeverityMedium).
			Message("Executor operation timeout").
			Suggestion("Increase timeout or check for hanging processes").
			Retryable(true).
			Build()
			
	default:
		return builder.
			Message(errMsg).
			Build()
	}
}

// WrapLayerError wraps layer errors with proper categorization
func WrapLayerError(err error, operation, layerID string) *BuildError {
	if err == nil {
		return nil
	}

	return NewErrorBuilder().
		Category(ErrorCategoryLayer).
		Operation(operation).
		Message(err.Error()).
		Cause(err).
		Metadata("layer_id", layerID).
		Build()
}

// WrapManifestError wraps manifest errors with proper categorization
func WrapManifestError(err error, operation string) *BuildError {
	if err == nil {
		return nil
	}

	return NewErrorBuilder().
		Category(ErrorCategoryManifest).
		Operation(operation).
		Message(err.Error()).
		Cause(err).
		Build()
}

// Example usage functions

// ExampleRegistryOperation shows how to use error handling with registry operations
func ExampleRegistryOperation(ctx context.Context, beh *BuildErrorHandler) error {
	return beh.HandleErrorWithRetry(ctx, "pull_image", func() error {
		// Simulate registry operation
		// In real code, this would be the actual registry call
		return fmt.Errorf("connection timeout")
	})
}

// ExampleExecutorOperation shows how to use error handling with executor operations
func ExampleExecutorOperation(ctx context.Context, beh *BuildErrorHandler, stage, platform string) error {
	return beh.HandleExecutorError(ctx, 
		fmt.Errorf("command execution failed"), 
		"run_command", stage, platform)
}

// ExampleBuildWithErrorHandling shows a complete build example with error handling
func ExampleBuildWithErrorHandling(ctx context.Context, buildID, workDir string) error {
	// Create build error handler
	beh := NewBuildErrorHandler(ctx, buildID, workDir)
	defer func() {
		// Always perform cleanup
		if cleanupErr := beh.PerformBuildCleanup(ctx); cleanupErr != nil {
			fmt.Printf("Cleanup failed: %v\n", cleanupErr)
		}
		beh.Shutdown()
	}()

	// Register temporary files and processes for cleanup
	tempFile := filepath.Join(workDir, "build.tmp")
	beh.RegisterTempFile(tempFile)

	// Simulate build operations with error handling
	if err := ExampleRegistryOperation(ctx, beh); err != nil {
		return err
	}

	if err := ExampleExecutorOperation(ctx, beh, "build", "linux/amd64"); err != nil {
		return err
	}

	// Generate build report
	report := beh.GetBuildReport()
	if report.ErrorSummary.HasCriticalErrors() {
		return fmt.Errorf("build failed with critical errors")
	}

	return nil
}