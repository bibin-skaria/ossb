package errors

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ErrorHandler provides centralized error handling with recovery and observability
type ErrorHandler struct {
	recoveryManager    *RecoveryManager
	gracefulDegradation *GracefulDegradation
	retryTracker       *RetryTracker
	errorCollector     *ErrorCollector
	circuitBreakers    map[string]*CircuitBreaker
	mu                 sync.RWMutex
	config             *ErrorHandlerConfig
}

// ErrorHandlerConfig configures the error handler behavior
type ErrorHandlerConfig struct {
	// Retry configuration
	DefaultRetryConfig *RetryConfig `json:"default_retry_config"`
	
	// Circuit breaker configuration
	CircuitBreakerEnabled    bool          `json:"circuit_breaker_enabled"`
	CircuitBreakerMaxFailures int          `json:"circuit_breaker_max_failures"`
	CircuitBreakerResetTimeout time.Duration `json:"circuit_breaker_reset_timeout"`
	
	// Recovery configuration
	RecoveryEnabled bool `json:"recovery_enabled"`
	CleanupEnabled  bool `json:"cleanup_enabled"`
	
	// Degradation configuration
	DegradationEnabled bool `json:"degradation_enabled"`
	
	// Observability configuration
	TrackRetryMetrics bool `json:"track_retry_metrics"`
	CollectErrors     bool `json:"collect_errors"`
}

// DefaultErrorHandlerConfig returns a default error handler configuration
func DefaultErrorHandlerConfig() *ErrorHandlerConfig {
	return &ErrorHandlerConfig{
		DefaultRetryConfig:         DefaultRetryConfig(),
		CircuitBreakerEnabled:      true,
		CircuitBreakerMaxFailures:  5,
		CircuitBreakerResetTimeout: 30 * time.Second,
		RecoveryEnabled:            true,
		CleanupEnabled:             true,
		DegradationEnabled:         true,
		TrackRetryMetrics:          true,
		CollectErrors:              true,
	}
}

// NewErrorHandler creates a new error handler
func NewErrorHandler(ctx context.Context, config *ErrorHandlerConfig) *ErrorHandler {
	if config == nil {
		config = DefaultErrorHandlerConfig()
	}

	eh := &ErrorHandler{
		recoveryManager:     NewRecoveryManager(ctx),
		gracefulDegradation: NewGracefulDegradation(),
		retryTracker:        NewRetryTracker(),
		errorCollector:      NewErrorCollector(),
		circuitBreakers:     make(map[string]*CircuitBreaker),
		config:              config,
	}

	return eh
}

// HandleError provides comprehensive error handling with retry, recovery, and degradation
func (eh *ErrorHandler) HandleError(ctx context.Context, err error, operation string, options ...ErrorHandleOption) error {
	if err == nil {
		return nil
	}

	// Apply options
	opts := &ErrorHandleOptions{
		Stage:        "",
		Platform:     "",
		RetryConfig:  eh.config.DefaultRetryConfig,
		AllowRetry:   true,
		AllowRecovery: eh.config.RecoveryEnabled,
		AllowDegradation: eh.config.DegradationEnabled,
	}
	for _, option := range options {
		option(opts)
	}

	// Convert to BuildError if needed
	buildErr := eh.ensureBuildError(err, operation, opts)

	// Collect error if enabled
	if eh.config.CollectErrors {
		eh.errorCollector.AddError(buildErr)
	}

	// Try recovery first if enabled and error is critical
	if opts.AllowRecovery && buildErr.IsCritical() {
		if recoveryErr := eh.attemptRecovery(ctx, buildErr); recoveryErr == nil {
			return nil // Recovery successful
		}
	}

	// Try retry if enabled and error is retryable
	if opts.AllowRetry && buildErr.IsRetryable() {
		if retryErr := eh.attemptRetry(ctx, buildErr, operation, opts); retryErr == nil {
			return nil // Retry successful
		}
	}

	// Try graceful degradation if enabled
	if opts.AllowDegradation {
		if degradationErr := eh.attemptDegradation(ctx, buildErr); degradationErr == nil {
			// Degradation successful, add warning
			eh.errorCollector.AddWarning(fmt.Sprintf("Degraded functionality due to: %s", buildErr.Message))
			return nil
		}
	}

	// All handling strategies failed, return the original error
	return buildErr
}

// HandleErrorWithRetry handles an error with retry logic
func (eh *ErrorHandler) HandleErrorWithRetry(ctx context.Context, operation string, fn RetryableFunc, options ...ErrorHandleOption) error {
	opts := &ErrorHandleOptions{
		RetryConfig: eh.config.DefaultRetryConfig,
		AllowRetry:  true,
	}
	for _, option := range options {
		option(opts)
	}

	// Use circuit breaker if enabled
	if eh.config.CircuitBreakerEnabled {
		cb := eh.getOrCreateCircuitBreaker(operation)
		return RetryWithCircuitBreaker(ctx, opts.RetryConfig, cb, operation, fn)
	}

	return RetryWithContext(ctx, opts.RetryConfig, operation, fn)
}

// RegisterCleanupAction registers a cleanup action
func (eh *ErrorHandler) RegisterCleanupAction(action CleanupAction) {
	eh.recoveryManager.RegisterCleanupAction(action)
}

// RegisterRecoveryStrategy registers a recovery strategy
func (eh *ErrorHandler) RegisterRecoveryStrategy(strategy RecoveryStrategy) {
	eh.recoveryManager.RegisterStrategy(strategy)
}

// PerformCleanup performs all registered cleanup actions
func (eh *ErrorHandler) PerformCleanup(ctx context.Context) error {
	if !eh.config.CleanupEnabled {
		return nil
	}
	return eh.recoveryManager.PerformCleanup(ctx)
}

// GetErrorSummary returns a summary of collected errors
func (eh *ErrorHandler) GetErrorSummary() *ErrorSummary {
	errors := eh.errorCollector.GetErrors()
	warnings := eh.errorCollector.GetWarnings()
	context := eh.errorCollector.GetContext()

	summary := &ErrorSummary{
		TotalErrors:    len(errors),
		CriticalErrors: 0,
		HighErrors:     0,
		MediumErrors:   0,
		LowErrors:      0,
		TotalWarnings:  len(warnings),
		Categories:     make(map[ErrorCategory]int),
		RetryMetrics:   eh.retryTracker.GetAllMetrics(),
		Context:        context,
		Errors:         errors,
		Warnings:       warnings,
	}

	// Categorize errors
	for _, err := range errors {
		summary.Categories[err.Category]++
		
		switch err.Severity {
		case ErrorSeverityCritical:
			summary.CriticalErrors++
		case ErrorSeverityHigh:
			summary.HighErrors++
		case ErrorSeverityMedium:
			summary.MediumErrors++
		case ErrorSeverityLow:
			summary.LowErrors++
		}
	}

	return summary
}

// GetCircuitBreakerStatus returns the status of all circuit breakers
func (eh *ErrorHandler) GetCircuitBreakerStatus() map[string]CircuitBreakerStatus {
	eh.mu.RLock()
	defer eh.mu.RUnlock()

	status := make(map[string]CircuitBreakerStatus)
	for operation, cb := range eh.circuitBreakers {
		status[operation] = CircuitBreakerStatus{
			State:        cb.GetState(),
			FailureCount: cb.GetFailureCount(),
		}
	}

	return status
}

// Shutdown gracefully shuts down the error handler
func (eh *ErrorHandler) Shutdown() {
	eh.recoveryManager.Shutdown()
}

// Private methods

func (eh *ErrorHandler) ensureBuildError(err error, operation string, opts *ErrorHandleOptions) *BuildError {
	if buildErr, ok := err.(*BuildError); ok {
		// Create a copy to avoid modifying the original
		newErr := *buildErr
		
		// Update context if not set
		if newErr.Operation == "" {
			newErr.Operation = operation
		}
		if newErr.Stage == "" && opts.Stage != "" {
			newErr.Stage = opts.Stage
		}
		if newErr.Platform == "" && opts.Platform != "" {
			newErr.Platform = opts.Platform
		}
		return &newErr
	}

	// Convert regular error to BuildError
	return NewErrorBuilder().
		Message(err.Error()).
		Cause(err).
		Operation(operation).
		Stage(opts.Stage).
		Platform(opts.Platform).
		Build()
}

func (eh *ErrorHandler) attemptRecovery(ctx context.Context, err *BuildError) error {
	return eh.recoveryManager.AttemptRecovery(ctx, err)
}

func (eh *ErrorHandler) attemptRetry(ctx context.Context, err *BuildError, operation string, opts *ErrorHandleOptions) error {
	if opts.RetryConfig == nil {
		return err
	}

	// For the error handler, we don't actually retry the operation here
	// This method is used when we already have an error and want to determine
	// if it should be retried. The actual retry logic is handled by HandleErrorWithRetry.
	
	// Just return the error since we can't retry an already-failed operation
	return err
}

func (eh *ErrorHandler) attemptDegradation(ctx context.Context, err *BuildError) error {
	return eh.gracefulDegradation.Degrade(ctx, err)
}

func (eh *ErrorHandler) getOrCreateCircuitBreaker(operation string) *CircuitBreaker {
	eh.mu.Lock()
	defer eh.mu.Unlock()

	if cb, exists := eh.circuitBreakers[operation]; exists {
		return cb
	}

	cb := NewCircuitBreaker(
		eh.config.CircuitBreakerMaxFailures,
		eh.config.CircuitBreakerResetTimeout,
	)
	eh.circuitBreakers[operation] = cb
	return cb
}

// ErrorHandleOptions configures error handling behavior
type ErrorHandleOptions struct {
	Stage            string
	Platform         string
	RetryConfig      *RetryConfig
	AllowRetry       bool
	AllowRecovery    bool
	AllowDegradation bool
}

// ErrorHandleOption is a function that configures ErrorHandleOptions
type ErrorHandleOption func(*ErrorHandleOptions)

// WithStage sets the build stage context
func WithStage(stage string) ErrorHandleOption {
	return func(opts *ErrorHandleOptions) {
		opts.Stage = stage
	}
}

// WithPlatform sets the platform context
func WithPlatform(platform string) ErrorHandleOption {
	return func(opts *ErrorHandleOptions) {
		opts.Platform = platform
	}
}

// WithRetryConfig sets the retry configuration
func WithRetryConfig(config *RetryConfig) ErrorHandleOption {
	return func(opts *ErrorHandleOptions) {
		opts.RetryConfig = config
	}
}

// WithoutRetry disables retry for this error
func WithoutRetry() ErrorHandleOption {
	return func(opts *ErrorHandleOptions) {
		opts.AllowRetry = false
	}
}

// WithoutRecovery disables recovery for this error
func WithoutRecovery() ErrorHandleOption {
	return func(opts *ErrorHandleOptions) {
		opts.AllowRecovery = false
	}
}

// WithoutDegradation disables degradation for this error
func WithoutDegradation() ErrorHandleOption {
	return func(opts *ErrorHandleOptions) {
		opts.AllowDegradation = false
	}
}

// ErrorSummary provides a summary of errors and their handling
type ErrorSummary struct {
	TotalErrors    int                        `json:"total_errors"`
	CriticalErrors int                        `json:"critical_errors"`
	HighErrors     int                        `json:"high_errors"`
	MediumErrors   int                        `json:"medium_errors"`
	LowErrors      int                        `json:"low_errors"`
	TotalWarnings  int                        `json:"total_warnings"`
	Categories     map[ErrorCategory]int      `json:"categories"`
	RetryMetrics   map[string]*RetryMetrics   `json:"retry_metrics"`
	Context        map[string]interface{}     `json:"context"`
	Errors         []*BuildError              `json:"errors"`
	Warnings       []string                   `json:"warnings"`
}

// CircuitBreakerStatus represents the status of a circuit breaker
type CircuitBreakerStatus struct {
	State        CircuitState `json:"state"`
	FailureCount int          `json:"failure_count"`
}

// HasCriticalErrors returns true if there are any critical errors
func (s *ErrorSummary) HasCriticalErrors() bool {
	return s.CriticalErrors > 0
}

// HasErrors returns true if there are any errors
func (s *ErrorSummary) HasErrors() bool {
	return s.TotalErrors > 0
}

// GetMostCommonCategory returns the most common error category
func (s *ErrorSummary) GetMostCommonCategory() ErrorCategory {
	var maxCategory ErrorCategory
	maxCount := 0

	for category, count := range s.Categories {
		if count > maxCount {
			maxCount = count
			maxCategory = category
		}
	}

	return maxCategory
}

// GetRecommendations returns recommendations based on the error summary
func (s *ErrorSummary) GetRecommendations() []string {
	recommendations := make([]string, 0)

	// Critical errors
	if s.CriticalErrors > 0 {
		recommendations = append(recommendations, "Critical errors detected - build cannot continue without fixing these issues")
	}

	// Most common category recommendations
	mostCommon := s.GetMostCommonCategory()
	switch mostCommon {
	case ErrorCategoryNetwork:
		recommendations = append(recommendations, "Network issues detected - check connectivity and retry")
	case ErrorCategoryRegistry:
		recommendations = append(recommendations, "Registry issues detected - verify credentials and registry availability")
	case ErrorCategoryAuth:
		recommendations = append(recommendations, "Authentication issues detected - verify credentials and permissions")
	case ErrorCategoryResource:
		recommendations = append(recommendations, "Resource issues detected - increase available memory/disk or optimize build")
	case ErrorCategoryCache:
		recommendations = append(recommendations, "Cache issues detected - consider clearing cache with --no-cache")
	}

	// Retry metrics recommendations
	for operation, metrics := range s.RetryMetrics {
		if metrics.FailedRetries > metrics.SuccessfulRetries {
			recommendations = append(recommendations, 
				fmt.Sprintf("Operation '%s' has high failure rate - investigate underlying issues", operation))
		}
	}

	return recommendations
}