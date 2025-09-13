package k8s

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/bibin-skaria/ossb/internal/types"
)

// JobLifecycleManager manages the lifecycle of OSSB jobs in Kubernetes
type JobLifecycleManager struct {
	integration *KubernetesIntegration
	logger      *StructuredLogger
	buildID     string
	startTime   time.Time
	mutex       sync.RWMutex
	status      JobStatus
	cancelled   bool
	cancelFunc  context.CancelFunc
	cleanup     []func() error
}

// ExitCode represents job exit codes
type ExitCode int

const (
	ExitCodeSuccess         ExitCode = 0
	ExitCodeGeneralError    ExitCode = 1
	ExitCodeBuildError      ExitCode = 2
	ExitCodeRegistryError   ExitCode = 3
	ExitCodeAuthError       ExitCode = 4
	ExitCodeConfigError     ExitCode = 5
	ExitCodeResourceError   ExitCode = 6
	ExitCodeTimeoutError    ExitCode = 7
	ExitCodeCancelledError  ExitCode = 8
)

// NewJobLifecycleManager creates a new job lifecycle manager
func NewJobLifecycleManager(buildID string) *JobLifecycleManager {
	integration := NewKubernetesIntegration()
	logger := NewStructuredLogger(buildID)

	return &JobLifecycleManager{
		integration: integration,
		logger:      logger,
		buildID:     buildID,
		startTime:   time.Now(),
		status:      JobStatusRunning,
		cleanup:     make([]func() error, 0),
	}
}

// Start initializes the job lifecycle
func (j *JobLifecycleManager) Start(ctx context.Context) (context.Context, error) {
	j.logger.WithContext(ctx).WithFields(map[string]interface{}{
		"build_id": j.buildID,
		"job_info": j.integration.GetJobInfo(),
	}).Info("Starting OSSB job lifecycle")

	// Create cancellable context
	ctx, cancel := context.WithCancel(ctx)
	j.cancelFunc = cancel

	// Set up signal handling for graceful shutdown
	j.setupSignalHandling(ctx)

	// Report job start
	if err := j.integration.SetJobStatus(JobStatusRunning, "Job started", nil); err != nil {
		j.logger.LogError(ctx, err, "set_job_status", "start")
	}

	// Set up workspace if running in Kubernetes
	if j.integration.IsRunningInKubernetes() {
		if err := j.integration.SetupWorkspace("10Gi"); err != nil {
			j.logger.LogError(ctx, err, "setup_workspace", "start")
			return ctx, fmt.Errorf("failed to setup workspace: %v", err)
		}
	}

	return ctx, nil
}

// ReportProgress reports build progress
func (j *JobLifecycleManager) ReportProgress(ctx context.Context, stage string, progress float64, message string, platform string, operation string, cacheHit bool) {
	j.mutex.RLock()
	defer j.mutex.RUnlock()

	if j.cancelled {
		return
	}

	// Log progress
	j.logger.LogProgress(ctx, stage, progress, message)

	// Report to Kubernetes integration
	if err := j.integration.ReportProgress(stage, progress, message, platform, operation, cacheHit); err != nil {
		j.logger.LogError(ctx, err, "report_progress", stage)
	}
}

// Complete marks the job as completed
func (j *JobLifecycleManager) Complete(ctx context.Context, result *types.BuildResult) ExitCode {
	j.mutex.Lock()
	defer j.mutex.Unlock()

	duration := time.Since(j.startTime)
	
	var exitCode ExitCode
	var status JobStatus
	var message string

	if result.Success {
		exitCode = ExitCodeSuccess
		status = JobStatusSucceeded
		message = fmt.Sprintf("Build completed successfully in %s", duration)
		j.logger.LogBuildComplete(ctx, true, duration, result.Operations, result.CacheHits)
	} else {
		exitCode = j.determineExitCode(result.Error)
		status = JobStatusFailed
		message = fmt.Sprintf("Build failed after %s: %s", duration, result.Error)
		j.logger.LogBuildComplete(ctx, false, duration, result.Operations, result.CacheHits)
	}

	j.status = status

	// Report final status
	if err := j.integration.SetJobStatus(status, message, result); err != nil {
		j.logger.LogError(ctx, err, "set_job_status", "complete")
	}

	// Run cleanup functions
	j.runCleanup(ctx)

	return exitCode
}

// Fail marks the job as failed
func (j *JobLifecycleManager) Fail(ctx context.Context, err error, stage string) ExitCode {
	j.mutex.Lock()
	defer j.mutex.Unlock()

	duration := time.Since(j.startTime)
	exitCode := j.determineExitCode(err.Error())
	message := fmt.Sprintf("Build failed in stage %s after %s: %v", stage, duration, err)

	j.status = JobStatusFailed
	j.logger.LogError(ctx, err, "build_failure", stage)

	// Create failed build result
	result := &types.BuildResult{
		Success:   false,
		Error:     err.Error(),
		Duration:  duration.String(),
	}

	// Report failure status
	if statusErr := j.integration.SetJobStatus(JobStatusFailed, message, result); statusErr != nil {
		j.logger.LogError(ctx, statusErr, "set_job_status", "fail")
	}

	// Run cleanup functions
	j.runCleanup(ctx)

	return exitCode
}

// Cancel cancels the job
func (j *JobLifecycleManager) Cancel(ctx context.Context, reason string) ExitCode {
	j.mutex.Lock()
	defer j.mutex.Unlock()

	if j.cancelled {
		return ExitCodeCancelledError
	}

	j.cancelled = true
	j.status = JobStatusFailed

	duration := time.Since(j.startTime)
	message := fmt.Sprintf("Build cancelled after %s: %s", duration, reason)

	j.logger.WithContext(ctx).WithFields(map[string]interface{}{
		"reason":   reason,
		"duration": duration.String(),
	}).Warn("Build cancelled")

	// Cancel context
	if j.cancelFunc != nil {
		j.cancelFunc()
	}

	// Create cancelled build result
	result := &types.BuildResult{
		Success:  false,
		Error:    fmt.Sprintf("Build cancelled: %s", reason),
		Duration: duration.String(),
	}

	// Report cancellation status
	if err := j.integration.SetJobStatus(JobStatusFailed, message, result); err != nil {
		j.logger.LogError(ctx, err, "set_job_status", "cancel")
	}

	// Run cleanup functions
	j.runCleanup(ctx)

	return ExitCodeCancelledError
}

// AddCleanupFunc adds a cleanup function to be called on job completion
func (j *JobLifecycleManager) AddCleanupFunc(cleanup func() error) {
	j.mutex.Lock()
	defer j.mutex.Unlock()
	j.cleanup = append(j.cleanup, cleanup)
}

// GetStatus returns the current job status
func (j *JobLifecycleManager) GetStatus() JobStatus {
	j.mutex.RLock()
	defer j.mutex.RUnlock()
	return j.status
}

// IsCancelled returns whether the job has been cancelled
func (j *JobLifecycleManager) IsCancelled() bool {
	j.mutex.RLock()
	defer j.mutex.RUnlock()
	return j.cancelled
}

// GetDuration returns the job duration
func (j *JobLifecycleManager) GetDuration() time.Duration {
	return time.Since(j.startTime)
}

// GetIntegration returns the Kubernetes integration
func (j *JobLifecycleManager) GetIntegration() *KubernetesIntegration {
	return j.integration
}

// GetLogger returns the structured logger
func (j *JobLifecycleManager) GetLogger() *StructuredLogger {
	return j.logger
}

// setupSignalHandling sets up signal handling for graceful shutdown
func (j *JobLifecycleManager) setupSignalHandling(ctx context.Context) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		select {
		case sig := <-sigChan:
			j.logger.WithContext(ctx).WithField("signal", sig.String()).Warn("Received shutdown signal")
			j.Cancel(ctx, fmt.Sprintf("received signal: %s", sig.String()))
		case <-ctx.Done():
			return
		}
	}()
}

// runCleanup runs all registered cleanup functions
func (j *JobLifecycleManager) runCleanup(ctx context.Context) {
	j.logger.WithContext(ctx).Info("Running cleanup functions")

	for i, cleanup := range j.cleanup {
		if err := cleanup(); err != nil {
			j.logger.WithContext(ctx).WithFields(map[string]interface{}{
				"cleanup_index": i,
				"error":         err.Error(),
			}).Error("Cleanup function failed")
		}
	}
}

// determineExitCode determines the appropriate exit code based on error message
func (j *JobLifecycleManager) determineExitCode(errorMsg string) ExitCode {
	errorMsg = fmt.Sprintf("%s", errorMsg) // Convert to lowercase for matching
	
	switch {
	case contains(errorMsg, "registry", "push", "pull"):
		return ExitCodeRegistryError
	case contains(errorMsg, "auth", "credential", "permission"):
		return ExitCodeAuthError
	case contains(errorMsg, "config", "invalid", "parse"):
		return ExitCodeConfigError
	case contains(errorMsg, "memory", "disk", "resource", "limit"):
		return ExitCodeResourceError
	case contains(errorMsg, "timeout", "deadline"):
		return ExitCodeTimeoutError
	case contains(errorMsg, "cancel", "interrupt"):
		return ExitCodeCancelledError
	case contains(errorMsg, "build", "dockerfile", "instruction"):
		return ExitCodeBuildError
	default:
		return ExitCodeGeneralError
	}
}

// contains checks if any of the keywords are present in the text
func contains(text string, keywords ...string) bool {
	for _, keyword := range keywords {
		if len(text) >= len(keyword) {
			for i := 0; i <= len(text)-len(keyword); i++ {
				match := true
				for j := 0; j < len(keyword); j++ {
					if text[i+j] != keyword[j] && text[i+j] != keyword[j]-32 { // Simple case-insensitive check
						match = false
						break
					}
				}
				if match {
					return true
				}
			}
		}
	}
	return false
}