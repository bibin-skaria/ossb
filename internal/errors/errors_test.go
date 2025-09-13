package errors

import (
	"fmt"
	"testing"
	"time"
)

func TestBuildError_Error(t *testing.T) {
	tests := []struct {
		name     string
		error    *BuildError
		expected string
	}{
		{
			name: "full context error",
			error: &BuildError{
				Category:  ErrorCategoryRegistry,
				Severity:  ErrorSeverityHigh,
				Operation: "pull_image",
				Stage:     "base_image",
				Platform:  "linux/amd64",
				Message:   "failed to pull image",
			},
			expected: "[registry:high] pull_image in stage base_image (platform linux/amd64): failed to pull image",
		},
		{
			name: "stage only error",
			error: &BuildError{
				Category:  ErrorCategoryBuild,
				Severity:  ErrorSeverityMedium,
				Operation: "dockerfile_parse",
				Stage:     "parse",
				Message:   "syntax error",
			},
			expected: "[build:medium] dockerfile_parse in stage parse: syntax error",
		},
		{
			name: "operation only error",
			error: &BuildError{
				Category:  ErrorCategoryNetwork,
				Severity:  ErrorSeverityLow,
				Operation: "network_check",
				Message:   "connection timeout",
			},
			expected: "[network:low] network_check operation: connection timeout",
		},
		{
			name: "minimal error",
			error: &BuildError{
				Category: ErrorCategoryUnknown,
				Severity: ErrorSeverityMedium,
				Message:  "unknown error",
			},
			expected: "[unknown:medium] unknown error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.error.Error(); got != tt.expected {
				t.Errorf("BuildError.Error() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestBuildError_IsRetryable(t *testing.T) {
	tests := []struct {
		name      string
		error     *BuildError
		retryable bool
	}{
		{
			name: "explicitly retryable",
			error: &BuildError{
				Category:  ErrorCategoryNetwork,
				Retryable: true,
			},
			retryable: true,
		},
		{
			name: "explicitly not retryable",
			error: &BuildError{
				Category:  ErrorCategoryAuth,
				Retryable: false,
			},
			retryable: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.error.IsRetryable(); got != tt.retryable {
				t.Errorf("BuildError.IsRetryable() = %v, want %v", got, tt.retryable)
			}
		})
	}
}

func TestBuildError_IsCritical(t *testing.T) {
	tests := []struct {
		name     string
		error    *BuildError
		critical bool
	}{
		{
			name: "critical error",
			error: &BuildError{
				Severity: ErrorSeverityCritical,
			},
			critical: true,
		},
		{
			name: "non-critical error",
			error: &BuildError{
				Severity: ErrorSeverityMedium,
			},
			critical: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.error.IsCritical(); got != tt.critical {
				t.Errorf("BuildError.IsCritical() = %v, want %v", got, tt.critical)
			}
		})
	}
}

func TestErrorBuilder(t *testing.T) {
	err := NewErrorBuilder().
		Category(ErrorCategoryRegistry).
		Severity(ErrorSeverityHigh).
		Code("REG001").
		Message("failed to pull image").
		Operation("pull_image").
		Stage("base_image").
		Platform("linux/amd64").
		Retryable(true).
		Suggestion("Check registry connectivity").
		Metadata("registry", "docker.io").
		Context("timeout", "30s").
		Build()

	if err.Category != ErrorCategoryRegistry {
		t.Errorf("Expected category %v, got %v", ErrorCategoryRegistry, err.Category)
	}
	if err.Severity != ErrorSeverityHigh {
		t.Errorf("Expected severity %v, got %v", ErrorSeverityHigh, err.Severity)
	}
	if err.Code != "REG001" {
		t.Errorf("Expected code REG001, got %v", err.Code)
	}
	if err.Message != "failed to pull image" {
		t.Errorf("Expected message 'failed to pull image', got %v", err.Message)
	}
	if !err.Retryable {
		t.Error("Expected error to be retryable")
	}
	if err.Metadata["registry"] != "docker.io" {
		t.Errorf("Expected metadata registry=docker.io, got %v", err.Metadata["registry"])
	}
	if err.Context["timeout"] != "30s" {
		t.Errorf("Expected context timeout=30s, got %v", err.Context["timeout"])
	}
}

func TestCategorizeError(t *testing.T) {
	tests := []struct {
		name      string
		message   string
		operation string
		expected  ErrorCategory
	}{
		{
			name:      "dockerfile error",
			message:   "dockerfile syntax error on line 5",
			operation: "parse",
			expected:  ErrorCategoryBuild,
		},
		{
			name:      "registry operation",
			message:   "failed to connect",
			operation: "registry_pull",
			expected:  ErrorCategoryRegistry,
		},
		{
			name:      "auth error",
			message:   "unauthorized access",
			operation: "pull_image",
			expected:  ErrorCategoryAuth,
		},
		{
			name:      "network error",
			message:   "connection timeout",
			operation: "download",
			expected:  ErrorCategoryNetwork,
		},
		{
			name:      "cache error",
			message:   "cache corruption detected",
			operation: "build",
			expected:  ErrorCategoryCache,
		},
		{
			name:      "resource error",
			message:   "out of memory",
			operation: "build",
			expected:  ErrorCategoryResource,
		},
		{
			name:      "permission error",
			message:   "permission denied",
			operation: "file_write",
			expected:  ErrorCategoryPermission,
		},
		{
			name:      "config error",
			message:   "invalid configuration",
			operation: "init",
			expected:  ErrorCategoryConfiguration,
		},
		{
			name:      "filesystem error",
			message:   "no such file or directory",
			operation: "read",
			expected:  ErrorCategoryFilesystem,
		},
		{
			name:      "unknown error",
			message:   "something went wrong",
			operation: "unknown",
			expected:  ErrorCategoryUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := categorizeError(tt.message, tt.operation); got != tt.expected {
				t.Errorf("categorizeError() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestDetermineSeverity(t *testing.T) {
	tests := []struct {
		name     string
		category ErrorCategory
		message  string
		expected ErrorSeverity
	}{
		{
			name:     "auth error is critical",
			category: ErrorCategoryAuth,
			message:  "authentication failed",
			expected: ErrorSeverityCritical,
		},
		{
			name:     "dockerfile validation is critical",
			category: ErrorCategoryValidation,
			message:  "dockerfile syntax error",
			expected: ErrorSeverityCritical,
		},
		{
			name:     "config validation is high",
			category: ErrorCategoryValidation,
			message:  "invalid config value",
			expected: ErrorSeverityHigh,
		},
		{
			name:     "network error is medium",
			category: ErrorCategoryNetwork,
			message:  "connection failed",
			expected: ErrorSeverityMedium,
		},
		{
			name:     "fatal message is critical",
			category: ErrorCategoryUnknown,
			message:  "fatal error occurred",
			expected: ErrorSeverityCritical,
		},
		{
			name:     "default is low",
			category: ErrorCategoryUnknown,
			message:  "some error",
			expected: ErrorSeverityLow,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := determineSeverity(tt.category, tt.message); got != tt.expected {
				t.Errorf("determineSeverity() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestIsRetryableCategory(t *testing.T) {
	tests := []struct {
		name      string
		category  ErrorCategory
		retryable bool
	}{
		{
			name:      "network is retryable",
			category:  ErrorCategoryNetwork,
			retryable: true,
		},
		{
			name:      "registry is retryable",
			category:  ErrorCategoryRegistry,
			retryable: true,
		},
		{
			name:      "auth is not retryable",
			category:  ErrorCategoryAuth,
			retryable: false,
		},
		{
			name:      "validation is not retryable",
			category:  ErrorCategoryValidation,
			retryable: false,
		},
		{
			name:      "unknown is retryable by default",
			category:  ErrorCategoryUnknown,
			retryable: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRetryableCategory(tt.category); got != tt.retryable {
				t.Errorf("isRetryableCategory() = %v, want %v", got, tt.retryable)
			}
		})
	}
}

func TestCommonErrorConstructors(t *testing.T) {
	cause := fmt.Errorf("underlying error")

	t.Run("NewNetworkError", func(t *testing.T) {
		err := NewNetworkError("connect", "connection failed", cause)
		if err.Category != ErrorCategoryNetwork {
			t.Errorf("Expected category %v, got %v", ErrorCategoryNetwork, err.Category)
		}
		if err.Severity != ErrorSeverityMedium {
			t.Errorf("Expected severity %v, got %v", ErrorSeverityMedium, err.Severity)
		}
		if !err.Retryable {
			t.Error("Expected network error to be retryable")
		}
	})

	t.Run("NewRegistryError", func(t *testing.T) {
		err := NewRegistryError("pull", "pull failed", cause)
		if err.Category != ErrorCategoryRegistry {
			t.Errorf("Expected category %v, got %v", ErrorCategoryRegistry, err.Category)
		}
		if !err.Retryable {
			t.Error("Expected registry error to be retryable")
		}
	})

	t.Run("NewAuthError", func(t *testing.T) {
		err := NewAuthError("authenticate", "auth failed", cause)
		if err.Category != ErrorCategoryAuth {
			t.Errorf("Expected category %v, got %v", ErrorCategoryAuth, err.Category)
		}
		if err.Severity != ErrorSeverityCritical {
			t.Errorf("Expected severity %v, got %v", ErrorSeverityCritical, err.Severity)
		}
		if err.Retryable {
			t.Error("Expected auth error to not be retryable")
		}
	})

	t.Run("NewValidationError", func(t *testing.T) {
		err := NewValidationError("validate", "validation failed", cause)
		if err.Category != ErrorCategoryValidation {
			t.Errorf("Expected category %v, got %v", ErrorCategoryValidation, err.Category)
		}
		if err.Retryable {
			t.Error("Expected validation error to not be retryable")
		}
	})
}

func TestWrapError(t *testing.T) {
	t.Run("wrap nil error", func(t *testing.T) {
		wrapped := WrapError(nil, "test")
		if wrapped != nil {
			t.Error("Expected nil when wrapping nil error")
		}
	})

	t.Run("wrap existing BuildError", func(t *testing.T) {
		original := NewNetworkError("test", "test error", nil)
		wrapped := WrapError(original, "wrap")
		if wrapped != original {
			t.Error("Expected same BuildError when wrapping BuildError")
		}
	})

	t.Run("wrap regular error", func(t *testing.T) {
		original := fmt.Errorf("regular error")
		wrapped := WrapError(original, "test_operation")
		if wrapped == nil {
			t.Fatal("Expected wrapped error to not be nil")
		}
		if wrapped.Message != "regular error" {
			t.Errorf("Expected message 'regular error', got %v", wrapped.Message)
		}
		if wrapped.Operation != "test_operation" {
			t.Errorf("Expected operation 'test_operation', got %v", wrapped.Operation)
		}
		if wrapped.Cause != original {
			t.Error("Expected cause to be original error")
		}
	})
}

func TestErrorCollector(t *testing.T) {
	collector := NewErrorCollector()

	// Test empty collector
	if collector.HasErrors() {
		t.Error("Expected no errors in new collector")
	}
	if collector.HasCriticalErrors() {
		t.Error("Expected no critical errors in new collector")
	}

	// Add some errors
	err1 := NewNetworkError("test1", "network error", nil)
	err2 := NewAuthError("test2", "auth error", nil)
	
	collector.AddError(err1)
	collector.AddError(err2)
	collector.AddWarning("test warning")
	collector.AddContext("build_id", "test-123")

	// Test collector state
	if !collector.HasErrors() {
		t.Error("Expected collector to have errors")
	}
	if !collector.HasCriticalErrors() {
		t.Error("Expected collector to have critical errors (auth error)")
	}

	errors := collector.GetErrors()
	if len(errors) != 2 {
		t.Errorf("Expected 2 errors, got %d", len(errors))
	}

	warnings := collector.GetWarnings()
	if len(warnings) != 1 {
		t.Errorf("Expected 1 warning, got %d", len(warnings))
	}
	if warnings[0] != "test warning" {
		t.Errorf("Expected warning 'test warning', got %v", warnings[0])
	}

	context := collector.GetContext()
	if context["build_id"] != "test-123" {
		t.Errorf("Expected build_id 'test-123', got %v", context["build_id"])
	}

	// Test ToError
	compositeErr := collector.ToError()
	if compositeErr == nil {
		t.Error("Expected composite error to not be nil")
	}
}

func TestErrorCollector_SingleError(t *testing.T) {
	collector := NewErrorCollector()
	err := NewNetworkError("test", "single error", nil)
	collector.AddError(err)

	compositeErr := collector.ToError()
	if compositeErr != err {
		t.Error("Expected single error to be returned as-is")
	}
}

func TestErrorCollector_NoErrors(t *testing.T) {
	collector := NewErrorCollector()
	compositeErr := collector.ToError()
	if compositeErr != nil {
		t.Error("Expected nil error when no errors collected")
	}
}

func TestBuildError_GetUserFriendlyMessage(t *testing.T) {
	tests := []struct {
		name     string
		error    *BuildError
		expected string
	}{
		{
			name: "error with suggestion",
			error: &BuildError{
				Message:    "Connection failed",
				Suggestion: "Check your network connection",
			},
			expected: "Connection failed\n\nSuggestion: Check your network connection",
		},
		{
			name: "error without suggestion",
			error: &BuildError{
				Message: "Something went wrong",
			},
			expected: "Something went wrong",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.error.GetUserFriendlyMessage(); got != tt.expected {
				t.Errorf("GetUserFriendlyMessage() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestBuildError_Timestamp(t *testing.T) {
	before := time.Now()
	err := NewErrorBuilder().
		Message("test error").
		Build()
	after := time.Now()

	if err.Timestamp.Before(before) || err.Timestamp.After(after) {
		t.Error("Expected timestamp to be set during Build()")
	}
}

func TestBuildError_StackTrace(t *testing.T) {
	err := NewErrorBuilder().
		Message("test error").
		Build()

	if err.StackTrace == "" {
		t.Error("Expected stack trace to be set")
	}

	// Stack trace should contain this test function
	if !contains(err.StackTrace, "TestBuildError_StackTrace") {
		t.Error("Expected stack trace to contain test function name")
	}
}