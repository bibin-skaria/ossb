package errors

import (
	"fmt"
	"runtime"
	"strings"
	"time"
)

// ErrorCategory represents different categories of errors for better handling
type ErrorCategory string

const (
	ErrorCategoryBuild        ErrorCategory = "build"
	ErrorCategoryRegistry     ErrorCategory = "registry"
	ErrorCategoryAuth         ErrorCategory = "auth"
	ErrorCategoryNetwork      ErrorCategory = "network"
	ErrorCategoryFilesystem   ErrorCategory = "filesystem"
	ErrorCategoryCache        ErrorCategory = "cache"
	ErrorCategoryValidation   ErrorCategory = "validation"
	ErrorCategoryResource     ErrorCategory = "resource"
	ErrorCategoryTimeout      ErrorCategory = "timeout"
	ErrorCategoryPermission   ErrorCategory = "permission"
	ErrorCategoryConfiguration ErrorCategory = "configuration"
	ErrorCategoryManifest     ErrorCategory = "manifest"
	ErrorCategoryLayer        ErrorCategory = "layer"
	ErrorCategoryExecutor     ErrorCategory = "executor"
	ErrorCategoryUnknown      ErrorCategory = "unknown"
)

// ErrorSeverity represents the severity level of an error
type ErrorSeverity string

const (
	ErrorSeverityLow      ErrorSeverity = "low"
	ErrorSeverityMedium   ErrorSeverity = "medium"
	ErrorSeverityHigh     ErrorSeverity = "high"
	ErrorSeverityCritical ErrorSeverity = "critical"
)

// BuildError represents a comprehensive error with categorization and recovery information
type BuildError struct {
	Category    ErrorCategory          `json:"category"`
	Severity    ErrorSeverity          `json:"severity"`
	Code        string                 `json:"code,omitempty"`
	Message     string                 `json:"message"`
	Cause       error                  `json:"-"`
	Operation   string                 `json:"operation,omitempty"`
	Stage       string                 `json:"stage,omitempty"`
	Platform    string                 `json:"platform,omitempty"`
	Timestamp   time.Time              `json:"timestamp"`
	Retryable   bool                   `json:"retryable"`
	Suggestion  string                 `json:"suggestion,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	StackTrace  string                 `json:"stack_trace,omitempty"`
	Context     map[string]interface{} `json:"context,omitempty"`
}

// Error implements the error interface
func (e *BuildError) Error() string {
	if e.Stage != "" && e.Platform != "" {
		return fmt.Sprintf("[%s:%s] %s in stage %s (platform %s): %s", 
			e.Category, e.Severity, e.Operation, e.Stage, e.Platform, e.Message)
	} else if e.Stage != "" {
		return fmt.Sprintf("[%s:%s] %s in stage %s: %s", 
			e.Category, e.Severity, e.Operation, e.Stage, e.Message)
	} else if e.Operation != "" {
		return fmt.Sprintf("[%s:%s] %s operation: %s", 
			e.Category, e.Severity, e.Operation, e.Message)
	}
	return fmt.Sprintf("[%s:%s] %s", e.Category, e.Severity, e.Message)
}

// Unwrap returns the underlying error
func (e *BuildError) Unwrap() error {
	return e.Cause
}

// IsRetryable returns true if the error might succeed on retry
func (e *BuildError) IsRetryable() bool {
	return e.Retryable
}

// IsCritical returns true if the error is critical and should stop the build
func (e *BuildError) IsCritical() bool {
	return e.Severity == ErrorSeverityCritical
}

// GetUserFriendlyMessage returns a user-friendly error message with suggestions
func (e *BuildError) GetUserFriendlyMessage() string {
	msg := e.Message
	if e.Suggestion != "" {
		msg += "\n\nSuggestion: " + e.Suggestion
	}
	return msg
}

// ErrorBuilder helps construct BuildError instances with proper categorization
type ErrorBuilder struct {
	category   ErrorCategory
	severity   ErrorSeverity
	code       string
	message    string
	cause      error
	operation  string
	stage      string
	platform   string
	retryable  bool
	suggestion string
	metadata   map[string]interface{}
	context    map[string]interface{}
}

// NewErrorBuilder creates a new error builder
func NewErrorBuilder() *ErrorBuilder {
	return &ErrorBuilder{
		metadata: make(map[string]interface{}),
		context:  make(map[string]interface{}),
	}
}

// Category sets the error category
func (b *ErrorBuilder) Category(category ErrorCategory) *ErrorBuilder {
	b.category = category
	return b
}

// Severity sets the error severity
func (b *ErrorBuilder) Severity(severity ErrorSeverity) *ErrorBuilder {
	b.severity = severity
	return b
}

// Code sets the error code
func (b *ErrorBuilder) Code(code string) *ErrorBuilder {
	b.code = code
	return b
}

// Message sets the error message
func (b *ErrorBuilder) Message(message string) *ErrorBuilder {
	b.message = message
	return b
}

// Messagef sets the error message with formatting
func (b *ErrorBuilder) Messagef(format string, args ...interface{}) *ErrorBuilder {
	b.message = fmt.Sprintf(format, args...)
	return b
}

// Cause sets the underlying error
func (b *ErrorBuilder) Cause(err error) *ErrorBuilder {
	b.cause = err
	return b
}

// Operation sets the operation context
func (b *ErrorBuilder) Operation(operation string) *ErrorBuilder {
	b.operation = operation
	return b
}

// Stage sets the build stage context
func (b *ErrorBuilder) Stage(stage string) *ErrorBuilder {
	b.stage = stage
	return b
}

// Platform sets the platform context
func (b *ErrorBuilder) Platform(platform string) *ErrorBuilder {
	b.platform = platform
	return b
}

// Retryable sets whether the error is retryable
func (b *ErrorBuilder) Retryable(retryable bool) *ErrorBuilder {
	b.retryable = retryable
	return b
}

// Suggestion sets a user-friendly suggestion
func (b *ErrorBuilder) Suggestion(suggestion string) *ErrorBuilder {
	b.suggestion = suggestion
	return b
}

// Metadata adds metadata to the error
func (b *ErrorBuilder) Metadata(key string, value interface{}) *ErrorBuilder {
	b.metadata[key] = value
	return b
}

// Context adds context information to the error
func (b *ErrorBuilder) Context(key string, value interface{}) *ErrorBuilder {
	b.context[key] = value
	return b
}

// Build creates the BuildError instance
func (b *ErrorBuilder) Build() *BuildError {
	// Auto-categorize if not set
	if b.category == "" {
		b.category = categorizeError(b.message, b.operation)
	}

	// Auto-set severity if not set
	if b.severity == "" {
		b.severity = determineSeverity(b.category, b.message)
	}

	// Auto-set retryable if not explicitly set
	if b.category != "" && !b.retryable {
		b.retryable = isRetryableCategory(b.category)
	}

	// Generate stack trace
	buf := make([]byte, 2048)
	n := runtime.Stack(buf, false)
	stackTrace := string(buf[:n])

	return &BuildError{
		Category:   b.category,
		Severity:   b.severity,
		Code:       b.code,
		Message:    b.message,
		Cause:      b.cause,
		Operation:  b.operation,
		Stage:      b.stage,
		Platform:   b.platform,
		Timestamp:  time.Now(),
		Retryable:  b.retryable,
		Suggestion: b.suggestion,
		Metadata:   b.metadata,
		StackTrace: stackTrace,
		Context:    b.context,
	}
}

// categorizeError automatically categorizes an error based on its content
func categorizeError(message, operation string) ErrorCategory {
	msgLower := strings.ToLower(message)
	opLower := strings.ToLower(operation)

	// Check message content first for auth-related errors (high priority)
	if strings.Contains(msgLower, "auth") || strings.Contains(msgLower, "credential") || strings.Contains(msgLower, "unauthorized") {
		return ErrorCategoryAuth
	}

	// Check operation context
	switch {
	case strings.Contains(opLower, "registry") || strings.Contains(opLower, "pull") || strings.Contains(opLower, "push"):
		return ErrorCategoryRegistry
	case strings.Contains(opLower, "auth") || strings.Contains(opLower, "credential"):
		return ErrorCategoryAuth
	case strings.Contains(opLower, "manifest"):
		return ErrorCategoryManifest
	case strings.Contains(opLower, "layer"):
		return ErrorCategoryLayer
	case strings.Contains(opLower, "executor") || strings.Contains(opLower, "execute"):
		return ErrorCategoryExecutor
	}

	// Check message content
	switch {
	case strings.Contains(msgLower, "dockerfile") || strings.Contains(msgLower, "instruction") || strings.Contains(msgLower, "syntax"):
		return ErrorCategoryBuild
	case strings.Contains(msgLower, "registry") || strings.Contains(msgLower, "pull") || strings.Contains(msgLower, "push"):
		return ErrorCategoryRegistry
	case strings.Contains(msgLower, "network") || strings.Contains(msgLower, "connection") || strings.Contains(msgLower, "timeout"):
		return ErrorCategoryNetwork
	case strings.Contains(msgLower, "cache"):
		return ErrorCategoryCache
	case strings.Contains(msgLower, "memory") || strings.Contains(msgLower, "disk") || strings.Contains(msgLower, "resource"):
		return ErrorCategoryResource
	case strings.Contains(msgLower, "permission") || strings.Contains(msgLower, "denied"):
		return ErrorCategoryPermission
	case strings.Contains(msgLower, "config") || strings.Contains(msgLower, "invalid") || strings.Contains(msgLower, "parse"):
		return ErrorCategoryConfiguration
	case strings.Contains(msgLower, "file") || strings.Contains(msgLower, "directory") || strings.Contains(msgLower, "no such"):
		return ErrorCategoryFilesystem
	case strings.Contains(msgLower, "manifest"):
		return ErrorCategoryManifest
	case strings.Contains(msgLower, "layer"):
		return ErrorCategoryLayer
	default:
		return ErrorCategoryUnknown
	}
}

// determineSeverity determines the severity of an error based on category and message
func determineSeverity(category ErrorCategory, message string) ErrorSeverity {
	msgLower := strings.ToLower(message)

	// Critical errors that should stop the build immediately
	switch category {
	case ErrorCategoryAuth:
		return ErrorSeverityCritical
	case ErrorCategoryValidation:
		if strings.Contains(msgLower, "dockerfile") || strings.Contains(msgLower, "syntax") {
			return ErrorSeverityCritical
		}
		return ErrorSeverityHigh
	case ErrorCategoryConfiguration:
		return ErrorSeverityHigh
	case ErrorCategoryPermission:
		return ErrorSeverityHigh
	}

	// Check for critical keywords in message
	if strings.Contains(msgLower, "fatal") || strings.Contains(msgLower, "critical") || 
	   strings.Contains(msgLower, "panic") || strings.Contains(msgLower, "abort") {
		return ErrorSeverityCritical
	}

	// Network and resource errors are usually medium severity (retryable)
	switch category {
	case ErrorCategoryNetwork, ErrorCategoryRegistry, ErrorCategoryResource, ErrorCategoryCache:
		return ErrorSeverityMedium
	case ErrorCategoryFilesystem, ErrorCategoryLayer, ErrorCategoryManifest:
		return ErrorSeverityMedium
	default:
		return ErrorSeverityLow
	}
}

// isRetryableCategory determines if an error category is generally retryable
func isRetryableCategory(category ErrorCategory) bool {
	switch category {
	case ErrorCategoryNetwork, ErrorCategoryRegistry, ErrorCategoryResource, 
		 ErrorCategoryCache, ErrorCategoryTimeout:
		return true
	case ErrorCategoryAuth, ErrorCategoryValidation, ErrorCategoryConfiguration, 
		 ErrorCategoryPermission:
		return false
	default:
		return true // Default to retryable for unknown categories
	}
}

// Common error constructors for frequently used error types

// NewNetworkError creates a network-related error
func NewNetworkError(operation, message string, cause error) *BuildError {
	return NewErrorBuilder().
		Category(ErrorCategoryNetwork).
		Severity(ErrorSeverityMedium).
		Operation(operation).
		Message(message).
		Cause(cause).
		Retryable(true).
		Suggestion("Check network connectivity and retry").
		Build()
}

// NewRegistryError creates a registry-related error
func NewRegistryError(operation, message string, cause error) *BuildError {
	return NewErrorBuilder().
		Category(ErrorCategoryRegistry).
		Severity(ErrorSeverityMedium).
		Operation(operation).
		Message(message).
		Cause(cause).
		Retryable(true).
		Suggestion("Check registry connectivity and credentials").
		Build()
}

// NewAuthError creates an authentication-related error
func NewAuthError(operation, message string, cause error) *BuildError {
	return NewErrorBuilder().
		Category(ErrorCategoryAuth).
		Severity(ErrorSeverityCritical).
		Operation(operation).
		Message(message).
		Cause(cause).
		Retryable(false).
		Suggestion("Verify registry credentials and permissions").
		Build()
}

// NewValidationError creates a validation-related error
func NewValidationError(operation, message string, cause error) *BuildError {
	return NewErrorBuilder().
		Category(ErrorCategoryValidation).
		Severity(ErrorSeverityHigh).
		Operation(operation).
		Message(message).
		Cause(cause).
		Retryable(false).
		Suggestion("Check input syntax and format").
		Build()
}

// NewResourceError creates a resource-related error
func NewResourceError(operation, message string, cause error) *BuildError {
	return NewErrorBuilder().
		Category(ErrorCategoryResource).
		Severity(ErrorSeverityMedium).
		Operation(operation).
		Message(message).
		Cause(cause).
		Retryable(true).
		Suggestion("Increase available resources or optimize build").
		Build()
}

// NewFilesystemError creates a filesystem-related error
func NewFilesystemError(operation, message string, cause error) *BuildError {
	return NewErrorBuilder().
		Category(ErrorCategoryFilesystem).
		Severity(ErrorSeverityMedium).
		Operation(operation).
		Message(message).
		Cause(cause).
		Retryable(false).
		Suggestion("Check file paths and permissions").
		Build()
}

// WrapError wraps an existing error with BuildError categorization
func WrapError(err error, operation string) *BuildError {
	if err == nil {
		return nil
	}

	// If it's already a BuildError, return as-is
	if buildErr, ok := err.(*BuildError); ok {
		return buildErr
	}

	return NewErrorBuilder().
		Message(err.Error()).
		Cause(err).
		Operation(operation).
		Build()
}

// ErrorCollector collects multiple errors during build operations
type ErrorCollector struct {
	errors   []*BuildError
	warnings []string
	context  map[string]interface{}
}

// NewErrorCollector creates a new error collector
func NewErrorCollector() *ErrorCollector {
	return &ErrorCollector{
		errors:  make([]*BuildError, 0),
		warnings: make([]string, 0),
		context: make(map[string]interface{}),
	}
}

// AddError adds an error to the collector
func (c *ErrorCollector) AddError(err *BuildError) {
	if err != nil {
		c.errors = append(c.errors, err)
	}
}

// AddWarning adds a warning to the collector
func (c *ErrorCollector) AddWarning(message string) {
	c.warnings = append(c.warnings, message)
}

// AddContext adds context information
func (c *ErrorCollector) AddContext(key string, value interface{}) {
	c.context[key] = value
}

// HasErrors returns true if there are any errors
func (c *ErrorCollector) HasErrors() bool {
	return len(c.errors) > 0
}

// HasCriticalErrors returns true if there are any critical errors
func (c *ErrorCollector) HasCriticalErrors() bool {
	for _, err := range c.errors {
		if err.IsCritical() {
			return true
		}
	}
	return false
}

// GetErrors returns all collected errors
func (c *ErrorCollector) GetErrors() []*BuildError {
	return c.errors
}

// GetWarnings returns all collected warnings
func (c *ErrorCollector) GetWarnings() []string {
	return c.warnings
}

// GetContext returns the context information
func (c *ErrorCollector) GetContext() map[string]interface{} {
	return c.context
}

// ToError converts the collector to a single error if there are errors
func (c *ErrorCollector) ToError() error {
	if len(c.errors) == 0 {
		return nil
	}

	if len(c.errors) == 1 {
		return c.errors[0]
	}

	// Create a composite error
	messages := make([]string, len(c.errors))
	for i, err := range c.errors {
		messages[i] = err.Error()
	}

	return NewErrorBuilder().
		Category(ErrorCategoryBuild).
		Severity(ErrorSeverityHigh).
		Message(fmt.Sprintf("Multiple errors occurred: %s", strings.Join(messages, "; "))).
		Suggestion("Review individual errors and fix them one by one").
		Build()
}