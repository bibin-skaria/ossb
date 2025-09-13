package errors

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestDefaultRetryConfig(t *testing.T) {
	config := DefaultRetryConfig()
	
	if config.MaxRetries != 3 {
		t.Errorf("Expected MaxRetries 3, got %d", config.MaxRetries)
	}
	if config.InitialInterval != 1*time.Second {
		t.Errorf("Expected InitialInterval 1s, got %v", config.InitialInterval)
	}
	if config.MaxInterval != 30*time.Second {
		t.Errorf("Expected MaxInterval 30s, got %v", config.MaxInterval)
	}
	if config.Multiplier != 2.0 {
		t.Errorf("Expected Multiplier 2.0, got %f", config.Multiplier)
	}
	if !config.Jitter {
		t.Error("Expected Jitter to be true")
	}
}

func TestAggressiveRetryConfig(t *testing.T) {
	config := AggressiveRetryConfig()
	
	if config.MaxRetries != 5 {
		t.Errorf("Expected MaxRetries 5, got %d", config.MaxRetries)
	}
	if config.InitialInterval != 500*time.Millisecond {
		t.Errorf("Expected InitialInterval 500ms, got %v", config.InitialInterval)
	}
	if config.Multiplier != 2.5 {
		t.Errorf("Expected Multiplier 2.5, got %f", config.Multiplier)
	}
}

func TestConservativeRetryConfig(t *testing.T) {
	config := ConservativeRetryConfig()
	
	if config.MaxRetries != 2 {
		t.Errorf("Expected MaxRetries 2, got %d", config.MaxRetries)
	}
	if config.Jitter {
		t.Error("Expected Jitter to be false")
	}
}

func TestRetryWithContext_Success(t *testing.T) {
	config := &RetryConfig{
		MaxRetries:      3,
		InitialInterval: 1 * time.Millisecond,
		MaxInterval:     10 * time.Millisecond,
		Multiplier:      2.0,
		Jitter:          false,
		RetryableErrors: []ErrorCategory{ErrorCategoryNetwork},
	}

	attempts := 0
	fn := func() error {
		attempts++
		if attempts < 3 {
			return NewNetworkError("test", "temporary failure", nil)
		}
		return nil
	}

	ctx := context.Background()
	err := RetryWithContext(ctx, config, "test_operation", fn)

	if err != nil {
		t.Errorf("Expected success, got error: %v", err)
	}
	if attempts != 3 {
		t.Errorf("Expected 3 attempts, got %d", attempts)
	}
}

func TestRetryWithContext_MaxRetriesExceeded(t *testing.T) {
	config := &RetryConfig{
		MaxRetries:      2,
		InitialInterval: 1 * time.Millisecond,
		MaxInterval:     10 * time.Millisecond,
		Multiplier:      2.0,
		Jitter:          false,
		RetryableErrors: []ErrorCategory{ErrorCategoryNetwork},
	}

	attempts := 0
	fn := func() error {
		attempts++
		return NewNetworkError("test", "persistent failure", nil)
	}

	ctx := context.Background()
	err := RetryWithContext(ctx, config, "test_operation", fn)

	if err == nil {
		t.Error("Expected error after max retries exceeded")
	}
	if attempts != 3 { // Initial attempt + 2 retries
		t.Errorf("Expected 3 attempts, got %d", attempts)
	}

	buildErr, ok := err.(*BuildError)
	if !ok {
		t.Errorf("Expected BuildError, got %T", err)
	} else {
		if buildErr.Category != ErrorCategoryNetwork {
			t.Errorf("Expected category %v, got %v", ErrorCategoryNetwork, buildErr.Category)
		}
	}
}

func TestRetryWithContext_NonRetryableError(t *testing.T) {
	config := &RetryConfig{
		MaxRetries:      3,
		InitialInterval: 1 * time.Millisecond,
		MaxInterval:     10 * time.Millisecond,
		Multiplier:      2.0,
		RetryableErrors: []ErrorCategory{ErrorCategoryNetwork},
	}

	attempts := 0
	fn := func() error {
		attempts++
		return NewAuthError("test", "authentication failed", nil)
	}

	ctx := context.Background()
	err := RetryWithContext(ctx, config, "test_operation", fn)

	if err == nil {
		t.Error("Expected error for non-retryable error")
	}
	if attempts != 1 {
		t.Errorf("Expected 1 attempt for non-retryable error, got %d", attempts)
	}
}

func TestRetryWithContext_ContextCancellation(t *testing.T) {
	config := &RetryConfig{
		MaxRetries:      5,
		InitialInterval: 100 * time.Millisecond,
		MaxInterval:     1 * time.Second,
		Multiplier:      2.0,
		RetryableErrors: []ErrorCategory{ErrorCategoryNetwork},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	fn := func() error {
		return NewNetworkError("test", "network failure", nil)
	}

	err := RetryWithContext(ctx, config, "test_operation", fn)

	if err == nil {
		t.Error("Expected error due to context cancellation")
	}

	buildErr, ok := err.(*BuildError)
	if !ok {
		t.Errorf("Expected BuildError, got %T", err)
	} else {
		if buildErr.Category != ErrorCategoryTimeout {
			t.Errorf("Expected category %v, got %v", ErrorCategoryTimeout, buildErr.Category)
		}
	}
}

func TestRetryWithContext_NilConfig(t *testing.T) {
	attempts := 0
	fn := func() error {
		attempts++
		if attempts == 1 {
			return NewNetworkError("test", "first attempt fails", nil)
		}
		return nil
	}

	ctx := context.Background()
	err := RetryWithContext(ctx, nil, "test_operation", fn)

	// With nil config, should use default config and retry
	if err != nil {
		t.Errorf("Expected success with default config, got error: %v", err)
	}
}

func TestIsRetryableError(t *testing.T) {
	config := &RetryConfig{
		RetryableErrors: []ErrorCategory{ErrorCategoryNetwork, ErrorCategoryRegistry},
	}

	tests := []struct {
		name      string
		error     error
		retryable bool
	}{
		{
			name:      "retryable BuildError",
			error:     NewNetworkError("test", "network error", nil),
			retryable: true,
		},
		{
			name:      "non-retryable BuildError",
			error:     NewAuthError("test", "auth error", nil),
			retryable: false,
		},
		{
			name: "explicitly non-retryable BuildError",
			error: &BuildError{
				Category:  ErrorCategoryNetwork,
				Retryable: false,
			},
			retryable: false,
		},
		{
			name:      "regular error with retryable message",
			error:     fmt.Errorf("connection timeout"),
			retryable: true,
		},
		{
			name:      "regular error with non-retryable message",
			error:     fmt.Errorf("invalid input"),
			retryable: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRetryableError(tt.error, config); got != tt.retryable {
				t.Errorf("isRetryableError() = %v, want %v", got, tt.retryable)
			}
		})
	}
}

func TestIsRetryableByMessage(t *testing.T) {
	tests := []struct {
		name      string
		message   string
		retryable bool
	}{
		{
			name:      "connection refused",
			message:   "connection refused",
			retryable: true,
		},
		{
			name:      "connection timeout",
			message:   "connection timeout",
			retryable: true,
		},
		{
			name:      "network unreachable",
			message:   "network unreachable",
			retryable: true,
		},
		{
			name:      "service unavailable",
			message:   "service unavailable",
			retryable: true,
		},
		{
			name:      "rate limit exceeded",
			message:   "rate limit exceeded",
			retryable: true,
		},
		{
			name:      "invalid input",
			message:   "invalid input",
			retryable: false,
		},
		{
			name:      "authentication failed",
			message:   "authentication failed",
			retryable: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRetryableByMessage(tt.message); got != tt.retryable {
				t.Errorf("isRetryableByMessage() = %v, want %v", got, tt.retryable)
			}
		})
	}
}

func TestExponentialBackoff(t *testing.T) {
	tests := []struct {
		name            string
		attempt         int
		initialInterval time.Duration
		multiplier      float64
		maxInterval     time.Duration
		jitter          bool
		expectedMin     time.Duration
		expectedMax     time.Duration
	}{
		{
			name:            "first attempt",
			attempt:         0,
			initialInterval: 1 * time.Second,
			multiplier:      2.0,
			maxInterval:     30 * time.Second,
			jitter:          false,
			expectedMin:     0,
			expectedMax:     0,
		},
		{
			name:            "second attempt",
			attempt:         1,
			initialInterval: 1 * time.Second,
			multiplier:      2.0,
			maxInterval:     30 * time.Second,
			jitter:          false,
			expectedMin:     1 * time.Second,
			expectedMax:     1 * time.Second,
		},
		{
			name:            "third attempt",
			attempt:         2,
			initialInterval: 1 * time.Second,
			multiplier:      2.0,
			maxInterval:     30 * time.Second,
			jitter:          false,
			expectedMin:     2 * time.Second,
			expectedMax:     2 * time.Second,
		},
		{
			name:            "capped at max interval",
			attempt:         10,
			initialInterval: 1 * time.Second,
			multiplier:      2.0,
			maxInterval:     5 * time.Second,
			jitter:          false,
			expectedMin:     5 * time.Second,
			expectedMax:     5 * time.Second,
		},
		{
			name:            "with jitter",
			attempt:         2,
			initialInterval: 1 * time.Second,
			multiplier:      2.0,
			maxInterval:     30 * time.Second,
			jitter:          true,
			expectedMin:     2 * time.Second,
			expectedMax:     3 * time.Second, // 2s + 25% jitter
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExponentialBackoff(tt.attempt, tt.initialInterval, tt.multiplier, tt.maxInterval, tt.jitter)
			
			if result < tt.expectedMin || result > tt.expectedMax {
				t.Errorf("ExponentialBackoff() = %v, want between %v and %v", result, tt.expectedMin, tt.expectedMax)
			}
		})
	}
}

func TestCircuitBreaker(t *testing.T) {
	cb := NewCircuitBreaker(3, 1*time.Second)

	// Test initial state
	if cb.GetState() != CircuitStateClosed {
		t.Errorf("Expected initial state %v, got %v", CircuitStateClosed, cb.GetState())
	}

	// Test successful execution
	err := cb.Execute("test", func() error {
		return nil
	})
	if err != nil {
		t.Errorf("Expected success, got error: %v", err)
	}

	// Test failures leading to open circuit
	for i := 0; i < 3; i++ {
		cb.Execute("test", func() error {
			return fmt.Errorf("failure %d", i+1)
		})
	}

	if cb.GetState() != CircuitStateOpen {
		t.Errorf("Expected state %v after failures, got %v", CircuitStateOpen, cb.GetState())
	}

	// Test rejection when circuit is open
	err = cb.Execute("test", func() error {
		return nil
	})
	if err == nil {
		t.Error("Expected error when circuit is open")
	}

	buildErr, ok := err.(*BuildError)
	if !ok {
		t.Errorf("Expected BuildError, got %T", err)
	} else {
		if buildErr.Category != ErrorCategoryResource {
			t.Errorf("Expected category %v, got %v", ErrorCategoryResource, buildErr.Category)
		}
	}
}

func TestCircuitBreaker_Recovery(t *testing.T) {
	cb := NewCircuitBreaker(2, 10*time.Millisecond)

	// Cause failures to open circuit
	for i := 0; i < 2; i++ {
		cb.Execute("test", func() error {
			return fmt.Errorf("failure")
		})
	}

	if cb.GetState() != CircuitStateOpen {
		t.Error("Expected circuit to be open")
	}

	// Wait for reset timeout
	time.Sleep(15 * time.Millisecond)

	// Next call should transition to half-open and succeed
	err := cb.Execute("test", func() error {
		return nil
	})

	if err != nil {
		t.Errorf("Expected success after reset timeout, got error: %v", err)
	}

	if cb.GetState() != CircuitStateClosed {
		t.Errorf("Expected state %v after successful recovery, got %v", CircuitStateClosed, cb.GetState())
	}
}

func TestRetryTracker(t *testing.T) {
	tracker := NewRetryTracker()

	// Record some attempts
	tracker.RecordAttempt("operation1", 1, 100*time.Millisecond, false)
	tracker.RecordAttempt("operation1", 2, 200*time.Millisecond, false)
	tracker.RecordAttempt("operation1", 3, 400*time.Millisecond, true)

	metrics := tracker.GetMetrics("operation1")
	if metrics == nil {
		t.Fatal("Expected metrics to be recorded")
	}

	if metrics.TotalAttempts != 3 {
		t.Errorf("Expected 3 total attempts, got %d", metrics.TotalAttempts)
	}
	if metrics.SuccessfulRetries != 1 {
		t.Errorf("Expected 1 successful retry, got %d", metrics.SuccessfulRetries)
	}
	if metrics.FailedRetries != 2 {
		t.Errorf("Expected 2 failed retries, got %d", metrics.FailedRetries)
	}
	if metrics.TotalWaitTime != 700*time.Millisecond {
		t.Errorf("Expected total wait time 700ms, got %v", metrics.TotalWaitTime)
	}
	if metrics.MaxWaitTime != 400*time.Millisecond {
		t.Errorf("Expected max wait time 400ms, got %v", metrics.MaxWaitTime)
	}
	if metrics.AverageAttempts != 3.0 {
		t.Errorf("Expected average attempts 3.0, got %f", metrics.AverageAttempts)
	}
}

func TestRetryTracker_MultipleOperations(t *testing.T) {
	tracker := NewRetryTracker()

	tracker.RecordAttempt("op1", 1, 100*time.Millisecond, true)
	tracker.RecordAttempt("op2", 1, 200*time.Millisecond, true)

	allMetrics := tracker.GetAllMetrics()
	if len(allMetrics) != 2 {
		t.Errorf("Expected 2 operations tracked, got %d", len(allMetrics))
	}

	if allMetrics["op1"] == nil {
		t.Error("Expected op1 metrics to exist")
	}
	if allMetrics["op2"] == nil {
		t.Error("Expected op2 metrics to exist")
	}
}

func TestRetry_WithoutContext(t *testing.T) {
	config := &RetryConfig{
		MaxRetries:      1,
		InitialInterval: 1 * time.Millisecond,
		MaxInterval:     10 * time.Millisecond,
		Multiplier:      2.0,
		RetryableErrors: []ErrorCategory{ErrorCategoryNetwork},
	}

	attempts := 0
	fn := func() error {
		attempts++
		if attempts == 1 {
			return NewNetworkError("test", "first attempt fails", nil)
		}
		return nil
	}

	err := Retry(config, "test_operation", fn)

	if err != nil {
		t.Errorf("Expected success, got error: %v", err)
	}
	if attempts != 2 {
		t.Errorf("Expected 2 attempts, got %d", attempts)
	}
}

func TestRetryWithCircuitBreaker(t *testing.T) {
	retryConfig := &RetryConfig{
		MaxRetries:      2,
		InitialInterval: 1 * time.Millisecond,
		MaxInterval:     10 * time.Millisecond,
		Multiplier:      2.0,
		RetryableErrors: []ErrorCategory{ErrorCategoryNetwork},
	}
	
	cb := NewCircuitBreaker(1, 100*time.Millisecond)
	ctx := context.Background()

	// First call should fail and open circuit
	attempts := 0
	err := RetryWithCircuitBreaker(ctx, retryConfig, cb, "test", func() error {
		attempts++
		return NewNetworkError("test", "persistent failure", nil)
	})

	if err == nil {
		t.Error("Expected error from failed retries")
	}

	// Circuit should be open now
	if cb.GetState() != CircuitStateOpen {
		t.Errorf("Expected circuit state %v, got %v", CircuitStateOpen, cb.GetState())
	}

	// Next call should be rejected immediately
	err = RetryWithCircuitBreaker(ctx, retryConfig, cb, "test", func() error {
		t.Error("Function should not be called when circuit is open")
		return nil
	})

	if err == nil {
		t.Error("Expected error when circuit is open")
	}
}