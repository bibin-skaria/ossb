package errors

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestNewErrorHandler(t *testing.T) {
	ctx := context.Background()
	
	// Test with default config
	eh := NewErrorHandler(ctx, nil)
	if eh == nil {
		t.Fatal("Expected error handler to be created")
	}
	if eh.config == nil {
		t.Error("Expected default config to be set")
	}
	
	// Test with custom config
	config := &ErrorHandlerConfig{
		DefaultRetryConfig: ConservativeRetryConfig(),
		RecoveryEnabled:    false,
	}
	eh2 := NewErrorHandler(ctx, config)
	if eh2.config.RecoveryEnabled {
		t.Error("Expected recovery to be disabled")
	}
}

func TestErrorHandler_HandleError_Success(t *testing.T) {
	ctx := context.Background()
	eh := NewErrorHandler(ctx, nil)
	defer eh.Shutdown()

	// Test handling nil error
	err := eh.HandleError(ctx, nil, "test_operation")
	if err != nil {
		t.Errorf("Expected nil error to be handled successfully, got: %v", err)
	}
}

func TestErrorHandler_HandleError_BuildError(t *testing.T) {
	ctx := context.Background()
	eh := NewErrorHandler(ctx, nil)
	defer eh.Shutdown()

	// Test handling BuildError
	buildErr := NewNetworkError("test", "network failure", nil)
	err := eh.HandleError(ctx, buildErr, "test_operation")
	
	// Should return the error since we can't actually retry in this test
	if err == nil {
		t.Error("Expected error to be returned when handling fails")
	}
}

func TestErrorHandler_HandleError_RegularError(t *testing.T) {
	ctx := context.Background()
	eh := NewErrorHandler(ctx, nil)
	defer eh.Shutdown()

	// Test handling regular error
	regularErr := fmt.Errorf("regular error")
	err := eh.HandleError(ctx, regularErr, "test_operation")
	
	buildErr, ok := err.(*BuildError)
	if !ok {
		t.Errorf("Expected BuildError, got %T", err)
	} else {
		if buildErr.Operation != "test_operation" {
			t.Errorf("Expected operation 'test_operation', got %v", buildErr.Operation)
		}
		if buildErr.Cause != regularErr {
			t.Error("Expected cause to be original error")
		}
	}
}

func TestErrorHandler_HandleError_WithOptions(t *testing.T) {
	ctx := context.Background()
	eh := NewErrorHandler(ctx, nil)
	defer eh.Shutdown()

	regularErr := fmt.Errorf("test error")
	err := eh.HandleError(ctx, regularErr, "test_operation",
		WithStage("test_stage"),
		WithPlatform("linux/amd64"),
		WithoutRetry(),
	)
	
	buildErr, ok := err.(*BuildError)
	if !ok {
		t.Fatalf("Expected BuildError, got %T", err)
	}
	
	if buildErr.Stage != "test_stage" {
		t.Errorf("Expected stage 'test_stage', got %v", buildErr.Stage)
	}
	if buildErr.Platform != "linux/amd64" {
		t.Errorf("Expected platform 'linux/amd64', got %v", buildErr.Platform)
	}
}

func TestErrorHandler_HandleErrorWithRetry(t *testing.T) {
	ctx := context.Background()
	config := &ErrorHandlerConfig{
		DefaultRetryConfig:    ConservativeRetryConfig(),
		CircuitBreakerEnabled: false,
	}
	eh := NewErrorHandler(ctx, config)
	defer eh.Shutdown()

	attempts := 0
	err := eh.HandleErrorWithRetry(ctx, "test_operation", func() error {
		attempts++
		if attempts < 2 {
			return NewNetworkError("test", "temporary failure", nil)
		}
		return nil
	})

	if err != nil {
		t.Errorf("Expected success after retry, got error: %v", err)
	}
	if attempts != 2 {
		t.Errorf("Expected 2 attempts, got %d", attempts)
	}
}

func TestErrorHandler_HandleErrorWithRetry_CircuitBreaker(t *testing.T) {
	ctx := context.Background()
	config := &ErrorHandlerConfig{
		DefaultRetryConfig:         ConservativeRetryConfig(),
		CircuitBreakerEnabled:      true,
		CircuitBreakerMaxFailures:  2,
		CircuitBreakerResetTimeout: 100 * time.Millisecond,
	}
	eh := NewErrorHandler(ctx, config)
	defer eh.Shutdown()

	// Make multiple calls to trigger circuit breaker
	for i := 0; i < 3; i++ {
		err := eh.HandleErrorWithRetry(ctx, "test_operation", func() error {
			return NewNetworkError("test", "persistent failure", nil)
		})
		if err == nil {
			t.Error("Expected error from failed retries")
		}
	}

	// Check circuit breaker status
	status := eh.GetCircuitBreakerStatus()
	if cbStatus, exists := status["test_operation"]; !exists {
		t.Error("Expected circuit breaker to be created for operation")
	} else if cbStatus.State != CircuitStateOpen {
		t.Errorf("Expected circuit state %v, got %v", CircuitStateOpen, cbStatus.State)
	}
}

func TestErrorHandler_RegisterCleanupAction(t *testing.T) {
	ctx := context.Background()
	eh := NewErrorHandler(ctx, nil)
	defer eh.Shutdown()

	action := NewTempFileCleanupAction("/tmp/test")
	eh.RegisterCleanupAction(action)

	// We can't easily test that the action was registered without exposing internals,
	// but we can test that cleanup doesn't fail
	err := eh.PerformCleanup(ctx)
	if err != nil {
		t.Errorf("Expected cleanup to succeed, got error: %v", err)
	}
}

func TestErrorHandler_RegisterRecoveryStrategy(t *testing.T) {
	ctx := context.Background()
	eh := NewErrorHandler(ctx, nil)
	defer eh.Shutdown()

	strategy := &TestRecoveryStrategy{}
	eh.RegisterRecoveryStrategy(strategy)

	// Test that the strategy is used
	customErr := &BuildError{
		Category: ErrorCategory("test_category"),
		Severity: ErrorSeverityCritical,
		Message:  "test error",
	}

	err := eh.HandleError(ctx, customErr, "test_operation")
	// Should succeed because our test strategy always succeeds
	if err != nil {
		t.Errorf("Expected recovery to succeed, got error: %v", err)
	}
}

func TestErrorHandler_GetErrorSummary(t *testing.T) {
	ctx := context.Background()
	eh := NewErrorHandler(ctx, nil)
	defer eh.Shutdown()

	// Handle some errors
	eh.HandleError(ctx, NewNetworkError("test1", "network error", nil), "op1")
	eh.HandleError(ctx, NewAuthError("test2", "auth error", nil), "op2")
	eh.HandleError(ctx, fmt.Errorf("regular error"), "op3")

	summary := eh.GetErrorSummary()
	if summary.TotalErrors != 3 {
		t.Errorf("Expected 3 total errors, got %d", summary.TotalErrors)
	}
	if summary.CriticalErrors != 1 { // Auth error is critical
		t.Errorf("Expected 1 critical error, got %d", summary.CriticalErrors)
	}
	if summary.Categories[ErrorCategoryNetwork] != 1 {
		t.Errorf("Expected 1 network error, got %d", summary.Categories[ErrorCategoryNetwork])
	}
	if summary.Categories[ErrorCategoryAuth] != 1 {
		t.Errorf("Expected 1 auth error, got %d", summary.Categories[ErrorCategoryAuth])
	}
}

func TestErrorHandler_GetErrorSummary_Methods(t *testing.T) {
	ctx := context.Background()
	eh := NewErrorHandler(ctx, nil)
	defer eh.Shutdown()

	// Handle some errors
	eh.HandleError(ctx, NewAuthError("test", "auth error", nil), "op1")
	eh.HandleError(ctx, NewNetworkError("test", "network error", nil), "op2")
	eh.HandleError(ctx, NewNetworkError("test", "another network error", nil), "op3")

	summary := eh.GetErrorSummary()

	// Test HasCriticalErrors
	if !summary.HasCriticalErrors() {
		t.Error("Expected summary to have critical errors")
	}

	// Test HasErrors
	if !summary.HasErrors() {
		t.Error("Expected summary to have errors")
	}

	// Test GetMostCommonCategory
	mostCommon := summary.GetMostCommonCategory()
	if mostCommon != ErrorCategoryNetwork {
		t.Errorf("Expected most common category to be %v, got %v", ErrorCategoryNetwork, mostCommon)
	}

	// Test GetRecommendations
	recommendations := summary.GetRecommendations()
	if len(recommendations) == 0 {
		t.Error("Expected recommendations to be generated")
	}

	// Should have critical error recommendation
	foundCritical := false
	for _, rec := range recommendations {
		if contains(rec, "Critical errors") {
			foundCritical = true
			break
		}
	}
	if !foundCritical {
		t.Error("Expected critical error recommendation")
	}
}

func TestErrorHandler_PerformCleanup_Disabled(t *testing.T) {
	ctx := context.Background()
	config := &ErrorHandlerConfig{
		CleanupEnabled: false,
	}
	eh := NewErrorHandler(ctx, config)
	defer eh.Shutdown()

	err := eh.PerformCleanup(ctx)
	if err != nil {
		t.Errorf("Expected cleanup to succeed when disabled, got error: %v", err)
	}
}

func TestErrorHandleOptions(t *testing.T) {
	opts := &ErrorHandleOptions{}

	// Test WithStage
	WithStage("test_stage")(opts)
	if opts.Stage != "test_stage" {
		t.Errorf("Expected stage 'test_stage', got %v", opts.Stage)
	}

	// Test WithPlatform
	WithPlatform("linux/amd64")(opts)
	if opts.Platform != "linux/amd64" {
		t.Errorf("Expected platform 'linux/amd64', got %v", opts.Platform)
	}

	// Test WithRetryConfig
	config := ConservativeRetryConfig()
	WithRetryConfig(config)(opts)
	if opts.RetryConfig != config {
		t.Error("Expected retry config to be set")
	}

	// Test WithoutRetry
	opts.AllowRetry = true
	WithoutRetry()(opts)
	if opts.AllowRetry {
		t.Error("Expected retry to be disabled")
	}

	// Test WithoutRecovery
	opts.AllowRecovery = true
	WithoutRecovery()(opts)
	if opts.AllowRecovery {
		t.Error("Expected recovery to be disabled")
	}

	// Test WithoutDegradation
	opts.AllowDegradation = true
	WithoutDegradation()(opts)
	if opts.AllowDegradation {
		t.Error("Expected degradation to be disabled")
	}
}

func TestDefaultErrorHandlerConfig(t *testing.T) {
	config := DefaultErrorHandlerConfig()

	if config.DefaultRetryConfig == nil {
		t.Error("Expected default retry config to be set")
	}
	if !config.CircuitBreakerEnabled {
		t.Error("Expected circuit breaker to be enabled")
	}
	if config.CircuitBreakerMaxFailures != 5 {
		t.Errorf("Expected max failures 5, got %d", config.CircuitBreakerMaxFailures)
	}
	if config.CircuitBreakerResetTimeout != 30*time.Second {
		t.Errorf("Expected reset timeout 30s, got %v", config.CircuitBreakerResetTimeout)
	}
	if !config.RecoveryEnabled {
		t.Error("Expected recovery to be enabled")
	}
	if !config.CleanupEnabled {
		t.Error("Expected cleanup to be enabled")
	}
	if !config.DegradationEnabled {
		t.Error("Expected degradation to be enabled")
	}
	if !config.TrackRetryMetrics {
		t.Error("Expected retry metrics tracking to be enabled")
	}
	if !config.CollectErrors {
		t.Error("Expected error collection to be enabled")
	}
}

func TestCircuitBreakerStatus(t *testing.T) {
	ctx := context.Background()
	config := &ErrorHandlerConfig{
		CircuitBreakerEnabled:      true,
		CircuitBreakerMaxFailures:  1,
		CircuitBreakerResetTimeout: 100 * time.Millisecond,
	}
	eh := NewErrorHandler(ctx, config)
	defer eh.Shutdown()

	// Trigger circuit breaker
	eh.HandleErrorWithRetry(ctx, "test_op", func() error {
		return NewNetworkError("test", "failure", nil)
	})

	status := eh.GetCircuitBreakerStatus()
	if len(status) != 1 {
		t.Errorf("Expected 1 circuit breaker, got %d", len(status))
	}

	if cbStatus, exists := status["test_op"]; !exists {
		t.Error("Expected circuit breaker for test_op")
	} else {
		if cbStatus.State != CircuitStateOpen {
			t.Errorf("Expected state %v, got %v", CircuitStateOpen, cbStatus.State)
		}
		if cbStatus.FailureCount < 1 {
			t.Errorf("Expected failure count >= 1, got %d", cbStatus.FailureCount)
		}
	}
}

func TestErrorHandler_EnsureBuildError(t *testing.T) {
	ctx := context.Background()
	eh := NewErrorHandler(ctx, nil)
	defer eh.Shutdown()

	opts := &ErrorHandleOptions{
		Stage:    "test_stage",
		Platform: "linux/amd64",
	}

	// Test with existing BuildError that has no operation set
	originalErr := &BuildError{
		Category: ErrorCategoryNetwork,
		Message:  "network error",
	}
	buildErr := eh.ensureBuildError(originalErr, "test_op", opts)
	
	// Should return a copy, not the same instance
	if buildErr == originalErr {
		t.Error("Expected a copy of BuildError to be returned, not the same instance")
	}
	if buildErr.Operation != "test_op" {
		t.Errorf("Expected operation to be updated to 'test_op', got %v", buildErr.Operation)
	}

	// Test with regular error
	regularErr := fmt.Errorf("regular error")
	buildErr2 := eh.ensureBuildError(regularErr, "test_op", opts)
	
	if buildErr2.Message != "regular error" {
		t.Errorf("Expected message 'regular error', got %v", buildErr2.Message)
	}
	if buildErr2.Operation != "test_op" {
		t.Errorf("Expected operation 'test_op', got %v", buildErr2.Operation)
	}
	if buildErr2.Stage != "test_stage" {
		t.Errorf("Expected stage 'test_stage', got %v", buildErr2.Stage)
	}
	if buildErr2.Platform != "linux/amd64" {
		t.Errorf("Expected platform 'linux/amd64', got %v", buildErr2.Platform)
	}
	if buildErr2.Cause != regularErr {
		t.Error("Expected cause to be original error")
	}
}

func TestErrorSummary_EmptyState(t *testing.T) {
	summary := &ErrorSummary{
		Categories: make(map[ErrorCategory]int),
	}

	if summary.HasErrors() {
		t.Error("Expected no errors in empty summary")
	}
	if summary.HasCriticalErrors() {
		t.Error("Expected no critical errors in empty summary")
	}

	mostCommon := summary.GetMostCommonCategory()
	if mostCommon != "" {
		t.Errorf("Expected empty category for empty summary, got %v", mostCommon)
	}

	recommendations := summary.GetRecommendations()
	if len(recommendations) != 0 {
		t.Errorf("Expected no recommendations for empty summary, got %d", len(recommendations))
	}
}