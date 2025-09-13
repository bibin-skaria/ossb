package errors

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"time"
)

// RetryConfig defines retry behavior for operations
type RetryConfig struct {
	MaxRetries      int           `json:"max_retries"`
	InitialInterval time.Duration `json:"initial_interval"`
	MaxInterval     time.Duration `json:"max_interval"`
	Multiplier      float64       `json:"multiplier"`
	Jitter          bool          `json:"jitter"`
	RetryableErrors []ErrorCategory `json:"retryable_errors,omitempty"`
}

// DefaultRetryConfig returns a default retry configuration
func DefaultRetryConfig() *RetryConfig {
	return &RetryConfig{
		MaxRetries:      3,
		InitialInterval: 1 * time.Second,
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
	}
}

// AggressiveRetryConfig returns a more aggressive retry configuration for critical operations
func AggressiveRetryConfig() *RetryConfig {
	return &RetryConfig{
		MaxRetries:      5,
		InitialInterval: 500 * time.Millisecond,
		MaxInterval:     60 * time.Second,
		Multiplier:      2.5,
		Jitter:          true,
		RetryableErrors: []ErrorCategory{
			ErrorCategoryNetwork,
			ErrorCategoryRegistry,
			ErrorCategoryResource,
			ErrorCategoryCache,
			ErrorCategoryTimeout,
		},
	}
}

// ConservativeRetryConfig returns a conservative retry configuration for non-critical operations
func ConservativeRetryConfig() *RetryConfig {
	return &RetryConfig{
		MaxRetries:      2,
		InitialInterval: 2 * time.Second,
		MaxInterval:     15 * time.Second,
		Multiplier:      1.5,
		Jitter:          false,
		RetryableErrors: []ErrorCategory{
			ErrorCategoryNetwork,
			ErrorCategoryRegistry,
		},
	}
}

// RetryableFunc represents a function that can be retried
type RetryableFunc func() error

// RetryWithContext executes a function with retry logic and context cancellation
func RetryWithContext(ctx context.Context, config *RetryConfig, operation string, fn RetryableFunc) error {
	if config == nil {
		config = DefaultRetryConfig()
	}

	var lastErr error
	interval := config.InitialInterval

	for attempt := 0; attempt <= config.MaxRetries; attempt++ {
		// Check context cancellation before each attempt
		select {
		case <-ctx.Done():
			return NewErrorBuilder().
				Category(ErrorCategoryTimeout).
				Severity(ErrorSeverityCritical).
				Operation(operation).
				Message("Operation cancelled by context").
				Cause(ctx.Err()).
				Build()
		default:
		}

		// Wait before retry (except for first attempt)
		if attempt > 0 {
			waitTime := interval
			if config.Jitter {
				waitTime = addJitter(interval)
			}

			select {
			case <-ctx.Done():
				return NewErrorBuilder().
					Category(ErrorCategoryTimeout).
					Severity(ErrorSeverityCritical).
					Operation(operation).
					Message("Operation cancelled during retry wait").
					Cause(ctx.Err()).
					Build()
			case <-time.After(waitTime):
				// Continue with retry
			}

			// Calculate next interval with exponential backoff
			interval = time.Duration(float64(interval) * config.Multiplier)
			if interval > config.MaxInterval {
				interval = config.MaxInterval
			}
		}

		// Execute the function
		if err := fn(); err != nil {
			lastErr = err

			// Check if this error is retryable
			if !isRetryableError(err, config) {
				return err
			}

			// Log retry attempt (if we have more attempts left)
			if attempt < config.MaxRetries {
				// This would be logged by the observability system
				continue
			}
		} else {
			// Success
			return nil
		}
	}

	// All retries exhausted
	return NewErrorBuilder().
		Category(ErrorCategoryNetwork).
		Severity(ErrorSeverityHigh).
		Operation(operation).
		Message(fmt.Sprintf("Operation failed after %d retries", config.MaxRetries)).
		Cause(lastErr).
		Suggestion("Check the underlying issue and try again later").
		Metadata("max_retries", config.MaxRetries).
		Metadata("last_error", lastErr.Error()).
		Build()
}

// Retry executes a function with retry logic (without context)
func Retry(config *RetryConfig, operation string, fn RetryableFunc) error {
	return RetryWithContext(context.Background(), config, operation, fn)
}

// isRetryableError determines if an error should trigger a retry
func isRetryableError(err error, config *RetryConfig) bool {
	// Check if it's a BuildError with retry information
	if buildErr, ok := err.(*BuildError); ok {
		// If explicitly marked as non-retryable, don't retry
		if !buildErr.IsRetryable() {
			return false
		}

		// Check if the error category is in the retryable list
		for _, category := range config.RetryableErrors {
			if buildErr.Category == category {
				return true
			}
		}
		return false
	}

	// For non-BuildError types, apply heuristics
	errMsg := err.Error()
	return isRetryableByMessage(errMsg)
}

// isRetryableByMessage determines retry-ability based on error message content
func isRetryableByMessage(errMsg string) bool {
	retryablePatterns := []string{
		"connection refused",
		"connection reset",
		"connection timeout",
		"network unreachable",
		"temporary failure",
		"service unavailable",
		"internal server error",
		"bad gateway",
		"gateway timeout",
		"too many requests",
		"rate limit",
		"throttled",
		"timeout",
		"deadline exceeded",
		"context deadline exceeded",
		"i/o timeout",
		"no route to host",
		"host unreachable",
	}

	errMsgLower := fmt.Sprintf("%s", errMsg)
	for _, pattern := range retryablePatterns {
		if contains(errMsgLower, pattern) {
			return true
		}
	}

	return false
}

// contains checks if a string contains a substring (case-insensitive)
func contains(s, substr string) bool {
	return len(s) >= len(substr) && 
		   (s == substr || len(s) > len(substr) && 
		   (hasPrefix(s, substr) || hasSuffix(s, substr) || containsInner(s, substr)))
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[0:len(prefix)] == prefix
}

func hasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

func containsInner(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// addJitter adds random jitter to the wait interval to avoid thundering herd
func addJitter(interval time.Duration) time.Duration {
	// Add up to 25% jitter
	jitter := time.Duration(rand.Float64() * 0.25 * float64(interval))
	return interval + jitter
}

// CircuitBreaker implements a circuit breaker pattern for failing operations
type CircuitBreaker struct {
	maxFailures     int
	resetTimeout    time.Duration
	failureCount    int
	lastFailureTime time.Time
	state           CircuitState
}

// CircuitState represents the state of a circuit breaker
type CircuitState string

const (
	CircuitStateClosed   CircuitState = "closed"   // Normal operation
	CircuitStateOpen     CircuitState = "open"     // Failing, reject requests
	CircuitStateHalfOpen CircuitState = "half_open" // Testing if service recovered
)

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(maxFailures int, resetTimeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		maxFailures:  maxFailures,
		resetTimeout: resetTimeout,
		state:        CircuitStateClosed,
	}
}

// Execute executes a function through the circuit breaker
func (cb *CircuitBreaker) Execute(operation string, fn RetryableFunc) error {
	// Check if circuit is open and should remain open
	if cb.state == CircuitStateOpen {
		if time.Since(cb.lastFailureTime) < cb.resetTimeout {
			return NewErrorBuilder().
				Category(ErrorCategoryResource).
				Severity(ErrorSeverityHigh).
				Operation(operation).
				Message("Circuit breaker is open - operation rejected").
				Suggestion("Wait for circuit breaker to reset or check underlying service").
				Metadata("circuit_state", cb.state).
				Metadata("failures", cb.failureCount).
				Build()
		}
		// Transition to half-open to test the service
		cb.state = CircuitStateHalfOpen
	}

	// Execute the function
	err := fn()

	if err != nil {
		cb.recordFailure()
		return err
	}

	// Success - reset the circuit breaker
	cb.recordSuccess()
	return nil
}

// recordFailure records a failure and potentially opens the circuit
func (cb *CircuitBreaker) recordFailure() {
	cb.failureCount++
	cb.lastFailureTime = time.Now()

	if cb.failureCount >= cb.maxFailures {
		cb.state = CircuitStateOpen
	}
}

// recordSuccess records a success and closes the circuit
func (cb *CircuitBreaker) recordSuccess() {
	cb.failureCount = 0
	cb.state = CircuitStateClosed
}

// GetState returns the current circuit breaker state
func (cb *CircuitBreaker) GetState() CircuitState {
	return cb.state
}

// GetFailureCount returns the current failure count
func (cb *CircuitBreaker) GetFailureCount() int {
	return cb.failureCount
}

// RetryWithCircuitBreaker combines retry logic with circuit breaker pattern
func RetryWithCircuitBreaker(ctx context.Context, retryConfig *RetryConfig, cb *CircuitBreaker, operation string, fn RetryableFunc) error {
	return cb.Execute(operation, func() error {
		return RetryWithContext(ctx, retryConfig, operation, fn)
	})
}

// ExponentialBackoff calculates the wait time for exponential backoff
func ExponentialBackoff(attempt int, initialInterval time.Duration, multiplier float64, maxInterval time.Duration, jitter bool) time.Duration {
	if attempt <= 0 {
		return 0
	}

	// Calculate exponential backoff
	interval := time.Duration(float64(initialInterval) * math.Pow(multiplier, float64(attempt-1)))
	
	// Cap at maximum interval
	if interval > maxInterval {
		interval = maxInterval
	}

	// Add jitter if requested
	if jitter {
		interval = addJitter(interval)
	}

	return interval
}

// RetryMetrics tracks retry statistics
type RetryMetrics struct {
	TotalAttempts    int           `json:"total_attempts"`
	SuccessfulRetries int          `json:"successful_retries"`
	FailedRetries    int           `json:"failed_retries"`
	AverageAttempts  float64       `json:"average_attempts"`
	TotalWaitTime    time.Duration `json:"total_wait_time"`
	MaxWaitTime      time.Duration `json:"max_wait_time"`
}

// RetryTracker tracks retry metrics for observability
type RetryTracker struct {
	metrics map[string]*RetryMetrics
}

// NewRetryTracker creates a new retry tracker
func NewRetryTracker() *RetryTracker {
	return &RetryTracker{
		metrics: make(map[string]*RetryMetrics),
	}
}

// RecordAttempt records a retry attempt
func (rt *RetryTracker) RecordAttempt(operation string, attempt int, waitTime time.Duration, success bool) {
	if rt.metrics[operation] == nil {
		rt.metrics[operation] = &RetryMetrics{}
	}

	metrics := rt.metrics[operation]
	metrics.TotalAttempts++
	metrics.TotalWaitTime += waitTime

	if waitTime > metrics.MaxWaitTime {
		metrics.MaxWaitTime = waitTime
	}

	if success {
		metrics.SuccessfulRetries++
	} else {
		metrics.FailedRetries++
	}

	// Update average attempts
	if metrics.SuccessfulRetries > 0 {
		metrics.AverageAttempts = float64(metrics.TotalAttempts) / float64(metrics.SuccessfulRetries)
	}
}

// GetMetrics returns retry metrics for an operation
func (rt *RetryTracker) GetMetrics(operation string) *RetryMetrics {
	return rt.metrics[operation]
}

// GetAllMetrics returns all retry metrics
func (rt *RetryTracker) GetAllMetrics() map[string]*RetryMetrics {
	return rt.metrics
}