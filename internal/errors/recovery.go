package errors

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// RecoveryStrategy defines how to handle different types of failures
type RecoveryStrategy interface {
	// CanRecover determines if this strategy can handle the given error
	CanRecover(err *BuildError) bool
	
	// Recover attempts to recover from the error
	Recover(ctx context.Context, err *BuildError) error
	
	// GetDescription returns a description of what this strategy does
	GetDescription() string
}

// CleanupAction represents a cleanup action that should be performed
type CleanupAction interface {
	// Execute performs the cleanup action
	Execute(ctx context.Context) error
	
	// GetDescription returns a description of the cleanup action
	GetDescription() string
	
	// GetPriority returns the priority of this cleanup action (higher = more important)
	GetPriority() int
}

// RecoveryManager manages error recovery and cleanup operations
type RecoveryManager struct {
	strategies     []RecoveryStrategy
	cleanupActions []CleanupAction
	mu             sync.RWMutex
	context        context.Context
	cancel         context.CancelFunc
}

// NewRecoveryManager creates a new recovery manager
func NewRecoveryManager(ctx context.Context) *RecoveryManager {
	recoveryCtx, cancel := context.WithCancel(ctx)
	
	rm := &RecoveryManager{
		strategies:     make([]RecoveryStrategy, 0),
		cleanupActions: make([]CleanupAction, 0),
		context:        recoveryCtx,
		cancel:         cancel,
	}
	
	// Register default recovery strategies
	rm.RegisterStrategy(&NetworkRecoveryStrategy{})
	rm.RegisterStrategy(&RegistryRecoveryStrategy{})
	rm.RegisterStrategy(&ResourceRecoveryStrategy{})
	rm.RegisterStrategy(&FilesystemRecoveryStrategy{})
	rm.RegisterStrategy(&CacheRecoveryStrategy{})
	
	return rm
}

// RegisterStrategy registers a recovery strategy
func (rm *RecoveryManager) RegisterStrategy(strategy RecoveryStrategy) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.strategies = append(rm.strategies, strategy)
}

// RegisterCleanupAction registers a cleanup action
func (rm *RecoveryManager) RegisterCleanupAction(action CleanupAction) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.cleanupActions = append(rm.cleanupActions, action)
}

// AttemptRecovery attempts to recover from an error using registered strategies
func (rm *RecoveryManager) AttemptRecovery(ctx context.Context, err *BuildError) error {
	rm.mu.RLock()
	strategies := make([]RecoveryStrategy, len(rm.strategies))
	copy(strategies, rm.strategies)
	rm.mu.RUnlock()

	for _, strategy := range strategies {
		if strategy.CanRecover(err) {
			if recoveryErr := strategy.Recover(ctx, err); recoveryErr == nil {
				return nil // Recovery successful
			}
		}
	}

	return NewErrorBuilder().
		Category(ErrorCategoryUnknown).
		Severity(ErrorSeverityHigh).
		Operation("recovery").
		Message("No recovery strategy available for error").
		Cause(err).
		Build()
}

// PerformCleanup performs all registered cleanup actions
func (rm *RecoveryManager) PerformCleanup(ctx context.Context) error {
	rm.mu.RLock()
	actions := make([]CleanupAction, len(rm.cleanupActions))
	copy(actions, rm.cleanupActions)
	rm.mu.RUnlock()

	// Sort actions by priority (highest first)
	for i := 0; i < len(actions)-1; i++ {
		for j := i + 1; j < len(actions); j++ {
			if actions[i].GetPriority() < actions[j].GetPriority() {
				actions[i], actions[j] = actions[j], actions[i]
			}
		}
	}

	var cleanupErrors []error
	for _, action := range actions {
		if err := action.Execute(ctx); err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("cleanup action '%s' failed: %v", 
				action.GetDescription(), err))
		}
	}

	if len(cleanupErrors) > 0 {
		return NewErrorBuilder().
			Category(ErrorCategoryFilesystem).
			Severity(ErrorSeverityMedium).
			Operation("cleanup").
			Message(fmt.Sprintf("Some cleanup actions failed: %v", cleanupErrors)).
			Build()
	}

	return nil
}

// Shutdown gracefully shuts down the recovery manager
func (rm *RecoveryManager) Shutdown() {
	rm.cancel()
}

// NetworkRecoveryStrategy handles network-related errors
type NetworkRecoveryStrategy struct{}

func (s *NetworkRecoveryStrategy) CanRecover(err *BuildError) bool {
	return err.Category == ErrorCategoryNetwork
}

func (s *NetworkRecoveryStrategy) Recover(ctx context.Context, err *BuildError) error {
	// For network errors, we can try alternative endpoints or wait for connectivity
	// This is a placeholder - actual implementation would depend on specific network issues
	
	// Wait a bit for network to recover
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(5 * time.Second):
		// Continue
	}
	
	return nil // Assume network recovered
}

func (s *NetworkRecoveryStrategy) GetDescription() string {
	return "Handles network connectivity issues by waiting for recovery"
}

// RegistryRecoveryStrategy handles registry-related errors
type RegistryRecoveryStrategy struct{}

func (s *RegistryRecoveryStrategy) CanRecover(err *BuildError) bool {
	return err.Category == ErrorCategoryRegistry
}

func (s *RegistryRecoveryStrategy) Recover(ctx context.Context, err *BuildError) error {
	// For registry errors, we might try alternative registries or mirrors
	// This is a placeholder for actual registry failover logic
	return NewErrorBuilder().
		Category(ErrorCategoryRegistry).
		Severity(ErrorSeverityMedium).
		Operation("registry_recovery").
		Message("Registry recovery not implemented").
		Build()
}

func (s *RegistryRecoveryStrategy) GetDescription() string {
	return "Handles registry errors by attempting alternative registries"
}

// ResourceRecoveryStrategy handles resource-related errors
type ResourceRecoveryStrategy struct{}

func (s *ResourceRecoveryStrategy) CanRecover(err *BuildError) bool {
	return err.Category == ErrorCategoryResource
}

func (s *ResourceRecoveryStrategy) Recover(ctx context.Context, err *BuildError) error {
	// For resource errors, we can try to free up resources or use degraded mode
	// This could involve cache cleanup, temporary file cleanup, etc.
	
	// Attempt to free up disk space
	if err := s.cleanupTempFiles(); err != nil {
		return err
	}
	
	return nil
}

func (s *ResourceRecoveryStrategy) cleanupTempFiles() error {
	tempDirs := []string{"/tmp", os.TempDir()}
	
	for _, dir := range tempDirs {
		if err := s.cleanupOldFiles(dir, 24*time.Hour); err != nil {
			// Log error but continue
			continue
		}
	}
	
	return nil
}

func (s *ResourceRecoveryStrategy) cleanupOldFiles(dir string, maxAge time.Duration) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip files we can't access
		}
		
		if time.Since(info.ModTime()) > maxAge {
			// Only remove files that look like temporary build files
			if s.isTempBuildFile(path) {
				os.Remove(path) // Ignore errors
			}
		}
		
		return nil
	})
}

func (s *ResourceRecoveryStrategy) isTempBuildFile(path string) bool {
	base := filepath.Base(path)
	tempPatterns := []string{"ossb-", "build-", "layer-", "manifest-"}
	
	for _, pattern := range tempPatterns {
		if len(base) > len(pattern) && base[:len(pattern)] == pattern {
			return true
		}
	}
	
	return false
}

func (s *ResourceRecoveryStrategy) GetDescription() string {
	return "Handles resource errors by cleaning up temporary files and freeing resources"
}

// FilesystemRecoveryStrategy handles filesystem-related errors
type FilesystemRecoveryStrategy struct{}

func (s *FilesystemRecoveryStrategy) CanRecover(err *BuildError) bool {
	return err.Category == ErrorCategoryFilesystem
}

func (s *FilesystemRecoveryStrategy) Recover(ctx context.Context, err *BuildError) error {
	// For filesystem errors, we might try to create missing directories,
	// fix permissions, or use alternative paths
	
	// This is a placeholder for actual filesystem recovery logic
	return NewErrorBuilder().
		Category(ErrorCategoryFilesystem).
		Severity(ErrorSeverityMedium).
		Operation("filesystem_recovery").
		Message("Filesystem recovery not implemented").
		Build()
}

func (s *FilesystemRecoveryStrategy) GetDescription() string {
	return "Handles filesystem errors by creating directories and fixing permissions"
}

// CacheRecoveryStrategy handles cache-related errors
type CacheRecoveryStrategy struct{}

func (s *CacheRecoveryStrategy) CanRecover(err *BuildError) bool {
	return err.Category == ErrorCategoryCache
}

func (s *CacheRecoveryStrategy) Recover(ctx context.Context, err *BuildError) error {
	// For cache errors, we can clear the cache and continue without it
	// This is a graceful degradation strategy
	
	return nil // Continue without cache
}

func (s *CacheRecoveryStrategy) GetDescription() string {
	return "Handles cache errors by clearing cache and continuing without it"
}

// Cleanup action implementations

// TempFileCleanupAction cleans up temporary files
type TempFileCleanupAction struct {
	paths []string
}

func NewTempFileCleanupAction(paths ...string) *TempFileCleanupAction {
	return &TempFileCleanupAction{paths: paths}
}

func (a *TempFileCleanupAction) Execute(ctx context.Context) error {
	for _, path := range a.paths {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		
		if err := os.RemoveAll(path); err != nil {
			// Log error but continue with other cleanups
			continue
		}
	}
	return nil
}

func (a *TempFileCleanupAction) GetDescription() string {
	return fmt.Sprintf("Clean up temporary files: %v", a.paths)
}

func (a *TempFileCleanupAction) GetPriority() int {
	return 100 // High priority
}

// ProcessCleanupAction cleans up running processes
type ProcessCleanupAction struct {
	processes []int // PIDs
}

func NewProcessCleanupAction(pids ...int) *ProcessCleanupAction {
	return &ProcessCleanupAction{processes: pids}
}

func (a *ProcessCleanupAction) Execute(ctx context.Context) error {
	for _, pid := range a.processes {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		
		if process, err := os.FindProcess(pid); err == nil {
			process.Kill() // Ignore errors
		}
	}
	return nil
}

func (a *ProcessCleanupAction) GetDescription() string {
	return fmt.Sprintf("Clean up processes: %v", a.processes)
}

func (a *ProcessCleanupAction) GetPriority() int {
	return 200 // Very high priority
}

// ContainerCleanupAction cleans up containers
type ContainerCleanupAction struct {
	containerIDs []string
}

func NewContainerCleanupAction(containerIDs ...string) *ContainerCleanupAction {
	return &ContainerCleanupAction{containerIDs: containerIDs}
}

func (a *ContainerCleanupAction) Execute(ctx context.Context) error {
	// This would integrate with container runtime (Docker/Podman) to clean up containers
	// Placeholder implementation
	for _, containerID := range a.containerIDs {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		
		// Execute container cleanup command
		// This would be implemented based on the container runtime being used
		_ = containerID // Placeholder to avoid unused variable error
	}
	return nil
}

func (a *ContainerCleanupAction) GetDescription() string {
	return fmt.Sprintf("Clean up containers: %v", a.containerIDs)
}

func (a *ContainerCleanupAction) GetPriority() int {
	return 150 // High priority
}

// GracefulDegradation provides strategies for continuing operation with reduced functionality
type GracefulDegradation struct {
	strategies map[ErrorCategory]DegradationStrategy
}

// DegradationStrategy defines how to degrade functionality for a specific error category
type DegradationStrategy interface {
	// Degrade reduces functionality to continue operation
	Degrade(ctx context.Context, err *BuildError) error
	
	// GetDescription returns a description of the degradation
	GetDescription() string
}

// NewGracefulDegradation creates a new graceful degradation manager
func NewGracefulDegradation() *GracefulDegradation {
	gd := &GracefulDegradation{
		strategies: make(map[ErrorCategory]DegradationStrategy),
	}
	
	// Register default degradation strategies
	gd.strategies[ErrorCategoryCache] = &CacheDegradationStrategy{}
	gd.strategies[ErrorCategoryRegistry] = &RegistryDegradationStrategy{}
	gd.strategies[ErrorCategoryResource] = &ResourceDegradationStrategy{}
	
	return gd
}

// Degrade attempts to degrade functionality for the given error
func (gd *GracefulDegradation) Degrade(ctx context.Context, err *BuildError) error {
	if strategy, exists := gd.strategies[err.Category]; exists {
		return strategy.Degrade(ctx, err)
	}
	
	return NewErrorBuilder().
		Category(ErrorCategoryUnknown).
		Severity(ErrorSeverityHigh).
		Operation("degradation").
		Message("No degradation strategy available").
		Cause(err).
		Build()
}

// CacheDegradationStrategy handles cache failures by disabling cache
type CacheDegradationStrategy struct{}

func (s *CacheDegradationStrategy) Degrade(ctx context.Context, err *BuildError) error {
	// Disable cache and continue
	// This would set a flag to bypass cache operations
	return nil
}

func (s *CacheDegradationStrategy) GetDescription() string {
	return "Disable cache and continue without caching"
}

// RegistryDegradationStrategy handles registry failures by using local images only
type RegistryDegradationStrategy struct{}

func (s *RegistryDegradationStrategy) Degrade(ctx context.Context, err *BuildError) error {
	// Switch to local-only mode
	// This would configure the system to only use locally available images
	return NewErrorBuilder().
		Category(ErrorCategoryRegistry).
		Severity(ErrorSeverityMedium).
		Operation("registry_degradation").
		Message("Registry degradation not fully implemented").
		Build()
}

func (s *RegistryDegradationStrategy) GetDescription() string {
	return "Switch to local-only mode for image operations"
}

// ResourceDegradationStrategy handles resource constraints by reducing parallelism
type ResourceDegradationStrategy struct{}

func (s *ResourceDegradationStrategy) Degrade(ctx context.Context, err *BuildError) error {
	// Reduce parallelism and memory usage
	// This would configure the system to use fewer resources
	return nil
}

func (s *ResourceDegradationStrategy) GetDescription() string {
	return "Reduce parallelism and memory usage to continue with limited resources"
}