package engine

import (
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/bibin-skaria/ossb/internal/types"
)

// ObservabilityManager manages comprehensive build observability
type ObservabilityManager struct {
	logger           *StructuredLogger
	progressTracker  *ProgressTracker
	metricsCollector *MetricsCollector
	buildID          string
	config           *types.BuildConfig
	startTime        time.Time
	context          context.Context
}

// StructuredLogger provides enhanced structured logging with error categorization
type StructuredLogger struct {
	logger    *logrus.Logger
	buildID   string
	context   map[string]interface{}
	output    io.Writer
}

// ErrorCategory represents different categories of errors
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
	ErrorCategoryUnknown      ErrorCategory = "unknown"
)

// LogLevel represents different log levels
type LogLevel string

const (
	LogLevelTrace LogLevel = "trace"
	LogLevelDebug LogLevel = "debug"
	LogLevelInfo  LogLevel = "info"
	LogLevelWarn  LogLevel = "warn"
	LogLevelError LogLevel = "error"
	LogLevelFatal LogLevel = "fatal"
)

// BuildEvent represents a structured build event
type BuildEvent struct {
	Type        string                 `json:"type"`
	Level       LogLevel               `json:"level"`
	Timestamp   time.Time              `json:"timestamp"`
	BuildID     string                 `json:"build_id"`
	Stage       string                 `json:"stage,omitempty"`
	Platform    string                 `json:"platform,omitempty"`
	Operation   string                 `json:"operation,omitempty"`
	Message     string                 `json:"message"`
	Duration    time.Duration          `json:"duration,omitempty"`
	Error       *ErrorInfo             `json:"error,omitempty"`
	Progress    float64                `json:"progress,omitempty"`
	CacheHit    bool                   `json:"cache_hit,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	Context     map[string]interface{} `json:"context,omitempty"`
	StackTrace  string                 `json:"stack_trace,omitempty"`
}

// ErrorInfo provides detailed error information
type ErrorInfo struct {
	Category    ErrorCategory          `json:"category"`
	Code        string                 `json:"code,omitempty"`
	Message     string                 `json:"message"`
	Cause       string                 `json:"cause,omitempty"`
	Suggestion  string                 `json:"suggestion,omitempty"`
	Retryable   bool                   `json:"retryable"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	StackTrace  string                 `json:"stack_trace,omitempty"`
}

// BuildReport represents a comprehensive build report
type BuildReport struct {
	BuildID         string                 `json:"build_id"`
	StartTime       time.Time              `json:"start_time"`
	EndTime         *time.Time             `json:"end_time,omitempty"`
	Duration        time.Duration          `json:"duration"`
	Success         bool                   `json:"success"`
	Config          *types.BuildConfig     `json:"config"`
	Progress        *ProgressSummary       `json:"progress"`
	Metrics         *BuildMetrics          `json:"metrics"`
	Events          []BuildEvent           `json:"events"`
	Errors          []ErrorInfo            `json:"errors"`
	Warnings        []string               `json:"warnings"`
	Summary         *BuildSummary          `json:"summary"`
	Recommendations []string               `json:"recommendations"`
	Metadata        map[string]interface{} `json:"metadata"`
}

// BuildSummary provides a high-level summary of the build
type BuildSummary struct {
	TotalStages      int           `json:"total_stages"`
	CompletedStages  int           `json:"completed_stages"`
	FailedStages     int           `json:"failed_stages"`
	TotalOperations  int           `json:"total_operations"`
	CacheHitRate     float64       `json:"cache_hit_rate"`
	AverageStageTime time.Duration `json:"average_stage_time"`
	PeakMemoryMB     int64         `json:"peak_memory_mb"`
	TotalErrors      int           `json:"total_errors"`
	TotalWarnings    int           `json:"total_warnings"`
	Platforms        []string      `json:"platforms"`
	OutputSize       int64         `json:"output_size,omitempty"`
}

// NewObservabilityManager creates a new observability manager
func NewObservabilityManager(buildID string, config *types.BuildConfig, output io.Writer, verbose bool) *ObservabilityManager {
	logger := NewStructuredLogger(buildID, output)
	progressTracker := NewProgressTracker(buildID, config.Platforms, output, verbose)
	metricsCollector := NewMetricsCollector(buildID, config.Platforms)

	return &ObservabilityManager{
		logger:           logger,
		progressTracker:  progressTracker,
		metricsCollector: metricsCollector,
		buildID:          buildID,
		config:           config,
		startTime:        time.Now(),
		context:          context.Background(),
	}
}

// NewStructuredLogger creates a new structured logger
func NewStructuredLogger(buildID string, output io.Writer) *StructuredLogger {
	logger := logrus.New()
	
	// Configure JSON formatter for structured logging
	logger.SetFormatter(&logrus.JSONFormatter{
		TimestampFormat: time.RFC3339Nano,
		FieldMap: logrus.FieldMap{
			logrus.FieldKeyTime:  "timestamp",
			logrus.FieldKeyLevel: "level",
			logrus.FieldKeyMsg:   "message",
		},
	})

	// Set log level from environment or default to info
	if level := os.Getenv("LOG_LEVEL"); level != "" {
		if logLevel, err := logrus.ParseLevel(level); err == nil {
			logger.SetLevel(logLevel)
		}
	} else {
		logger.SetLevel(logrus.InfoLevel)
	}

	if output != nil {
		logger.SetOutput(output)
	}

	return &StructuredLogger{
		logger:  logger,
		buildID: buildID,
		context: make(map[string]interface{}),
		output:  output,
	}
}

// StartBuild starts build observability
func (o *ObservabilityManager) StartBuild(ctx context.Context) context.Context {
	o.context = ctx
	
	// Log build start
	o.logger.LogBuildStart(ctx, o.config)
	
	// Start metrics collection
	o.metricsCollector.startResourceSampling()
	
	return ctx
}

// StartStage starts observability for a build stage
func (o *ObservabilityManager) StartStage(ctx context.Context, stageName string, platform string, expectedOps int) {
	o.progressTracker.StartStage(ctx, stageName, platform, expectedOps)
	o.metricsCollector.StartStage(stageName, platform)
	
	o.logger.LogStageStart(ctx, stageName, platform, expectedOps)
}

// UpdateStageProgress updates stage progress
func (o *ObservabilityManager) UpdateStageProgress(ctx context.Context, stageName string, platform string, progress float64, message string) {
	o.progressTracker.UpdateStageProgress(ctx, stageName, platform, progress, message)
}

// CompleteStage completes a build stage
func (o *ObservabilityManager) CompleteStage(ctx context.Context, stageName string, platform string, success bool, errorMsg string) {
	o.progressTracker.CompleteStage(ctx, stageName, platform, success, errorMsg)
	o.metricsCollector.EndStage(stageName, platform, success)
	
	o.logger.LogStageComplete(ctx, stageName, platform, success, errorMsg)
}

// RecordOperation records an operation
func (o *ObservabilityManager) RecordOperation(ctx context.Context, operation *types.Operation, result *types.OperationResult, duration time.Duration) {
	o.progressTracker.UpdateOperation(ctx, "current", operation.Platform.String(), string(operation.Type), result.CacheHit, duration)
	o.metricsCollector.RecordOperation(ctx, operation, result, duration)
	
	o.logger.LogOperation(ctx, operation, result, duration)
}

// RecordError records an error with categorization
func (o *ObservabilityManager) RecordError(ctx context.Context, err error, operation string, stage string, platform string) {
	errorInfo := o.categorizeError(err, operation, stage)
	
	o.logger.LogError(ctx, errorInfo, operation, stage, platform)
	o.metricsCollector.RecordError(string(errorInfo.Category), errorInfo.Message)
}

// FinishBuild completes build observability
func (o *ObservabilityManager) FinishBuild(ctx context.Context, result *types.BuildResult) *BuildReport {
	success := result != nil && result.Success
	
	o.progressTracker.Finish(ctx, success)
	o.metricsCollector.Finish(success)
	
	o.logger.LogBuildComplete(ctx, result)
	
	return o.GenerateReport(result)
}

// GenerateReport generates a comprehensive build report
func (o *ObservabilityManager) GenerateReport(result *types.BuildResult) *BuildReport {
	endTime := time.Now()
	duration := endTime.Sub(o.startTime)
	
	progressSummary := o.progressTracker.GetSummary()
	metrics := o.metricsCollector.GetMetrics()
	
	report := &BuildReport{
		BuildID:   o.buildID,
		StartTime: o.startTime,
		EndTime:   &endTime,
		Duration:  duration,
		Success:   result != nil && result.Success,
		Config:    o.config,
		Progress:  &progressSummary,
		Metrics:   metrics,
		Events:    make([]BuildEvent, 0),
		Errors:    make([]ErrorInfo, 0),
		Warnings:  make([]string, 0),
		Metadata:  make(map[string]interface{}),
	}

	// Generate summary
	report.Summary = &BuildSummary{
		TotalStages:      progressSummary.TotalStages,
		CompletedStages:  progressSummary.CompletedStages,
		FailedStages:     progressSummary.FailedStages,
		TotalOperations:  metrics.TotalOperations,
		CacheHitRate:     metrics.CacheHitRate,
		PeakMemoryMB:     metrics.PeakMemoryMB,
		TotalErrors:      metrics.ErrorCount,
		TotalWarnings:    metrics.WarningCount,
		Platforms:        metrics.Platforms,
	}

	if progressSummary.TotalStages > 0 {
		totalStageDuration := time.Duration(0)
		for _, stage := range progressSummary.Stages {
			totalStageDuration += stage.Duration
		}
		report.Summary.AverageStageTime = totalStageDuration / time.Duration(progressSummary.TotalStages)
	}

	// Generate recommendations
	report.Recommendations = o.generateRecommendations(metrics, &progressSummary)

	return report
}

// LogBuildStart logs the start of a build
func (s *StructuredLogger) LogBuildStart(ctx context.Context, config *types.BuildConfig) {
	event := BuildEvent{
		Type:      "build_start",
		Level:     LogLevelInfo,
		Timestamp: time.Now(),
		BuildID:   s.buildID,
		Message:   "Starting container image build",
		Metadata: map[string]interface{}{
			"platforms":  config.Platforms,
			"tags":       config.Tags,
			"dockerfile": config.Dockerfile,
			"context":    config.Context,
			"rootless":   config.Rootless,
		},
	}

	s.logEvent(event)
}

// LogBuildComplete logs build completion
func (s *StructuredLogger) LogBuildComplete(ctx context.Context, result *types.BuildResult) {
	level := LogLevelInfo
	message := "Build completed successfully"
	
	if result == nil || !result.Success {
		level = LogLevelError
		message = "Build failed"
	}

	event := BuildEvent{
		Type:      "build_complete",
		Level:     level,
		Timestamp: time.Now(),
		BuildID:   s.buildID,
		Message:   message,
		Progress:  100.0,
	}

	if result != nil {
		event.Duration, _ = time.ParseDuration(result.Duration)
		event.Metadata = map[string]interface{}{
			"success":         result.Success,
			"operations":      result.Operations,
			"cache_hits":      result.CacheHits,
			"multi_arch":      result.MultiArch,
			"platform_results": result.PlatformResults,
		}
		
		if !result.Success {
			event.Error = &ErrorInfo{
				Category: ErrorCategoryBuild,
				Message:  result.Error,
			}
		}
	}

	s.logEvent(event)
}

// LogStageStart logs the start of a stage
func (s *StructuredLogger) LogStageStart(ctx context.Context, stageName string, platform string, expectedOps int) {
	event := BuildEvent{
		Type:      "stage_start",
		Level:     LogLevelInfo,
		Timestamp: time.Now(),
		BuildID:   s.buildID,
		Stage:     stageName,
		Platform:  platform,
		Message:   fmt.Sprintf("Starting stage: %s", stageName),
		Metadata: map[string]interface{}{
			"expected_operations": expectedOps,
		},
	}

	s.logEvent(event)
}

// LogStageComplete logs stage completion
func (s *StructuredLogger) LogStageComplete(ctx context.Context, stageName string, platform string, success bool, errorMsg string) {
	level := LogLevelInfo
	message := fmt.Sprintf("Stage %s completed successfully", stageName)
	
	if !success {
		level = LogLevelError
		message = fmt.Sprintf("Stage %s failed", stageName)
	}

	event := BuildEvent{
		Type:      "stage_complete",
		Level:     level,
		Timestamp: time.Now(),
		BuildID:   s.buildID,
		Stage:     stageName,
		Platform:  platform,
		Message:   message,
		Progress:  100.0,
	}

	if !success && errorMsg != "" {
		event.Error = &ErrorInfo{
			Category: ErrorCategoryBuild,
			Message:  errorMsg,
		}
	}

	s.logEvent(event)
}

// LogOperation logs an operation
func (s *StructuredLogger) LogOperation(ctx context.Context, operation *types.Operation, result *types.OperationResult, duration time.Duration) {
	level := LogLevelDebug
	if !result.Success {
		level = LogLevelError
	}

	event := BuildEvent{
		Type:      "operation",
		Level:     level,
		Timestamp: time.Now(),
		BuildID:   s.buildID,
		Operation: string(operation.Type),
		Platform:  operation.Platform.String(),
		Message:   fmt.Sprintf("Executed %s operation", operation.Type),
		Duration:  duration,
		CacheHit:  result.CacheHit,
		Metadata: map[string]interface{}{
			"inputs":  operation.Inputs,
			"outputs": result.Outputs,
		},
	}

	if !result.Success {
		event.Error = &ErrorInfo{
			Category: ErrorCategoryBuild,
			Message:  result.Error,
		}
	}

	s.logEvent(event)
}

// LogError logs an error with detailed information
func (s *StructuredLogger) LogError(ctx context.Context, errorInfo *ErrorInfo, operation string, stage string, platform string) {
	event := BuildEvent{
		Type:      "error",
		Level:     LogLevelError,
		Timestamp: time.Now(),
		BuildID:   s.buildID,
		Stage:     stage,
		Platform:  platform,
		Operation: operation,
		Message:   fmt.Sprintf("Error in %s: %s", operation, errorInfo.Message),
		Error:     errorInfo,
	}

	s.logEvent(event)
}

// logEvent logs a structured event
func (s *StructuredLogger) logEvent(event BuildEvent) {
	entry := s.logger.WithFields(logrus.Fields{
		"component": "ossb",
		"build_id":  event.BuildID,
		"type":      event.Type,
	})

	if event.Stage != "" {
		entry = entry.WithField("stage", event.Stage)
	}
	if event.Platform != "" {
		entry = entry.WithField("platform", event.Platform)
	}
	if event.Operation != "" {
		entry = entry.WithField("operation", event.Operation)
	}
	if event.Duration > 0 {
		entry = entry.WithField("duration", event.Duration.String())
	}
	if event.Progress > 0 {
		entry = entry.WithField("progress", event.Progress)
	}
	if event.CacheHit {
		entry = entry.WithField("cache_hit", event.CacheHit)
	}
	if event.Error != nil {
		entry = entry.WithField("error_category", event.Error.Category)
		entry = entry.WithField("error_code", event.Error.Code)
		entry = entry.WithField("retryable", event.Error.Retryable)
	}

	for key, value := range event.Metadata {
		entry = entry.WithField(key, value)
	}

	switch event.Level {
	case LogLevelTrace:
		entry.Trace(event.Message)
	case LogLevelDebug:
		entry.Debug(event.Message)
	case LogLevelInfo:
		entry.Info(event.Message)
	case LogLevelWarn:
		entry.Warn(event.Message)
	case LogLevelError:
		entry.Error(event.Message)
	case LogLevelFatal:
		entry.Fatal(event.Message)
	}
}

// categorizeError categorizes an error based on its content and context
func (o *ObservabilityManager) categorizeError(err error, operation string, stage string) *ErrorInfo {
	errorMsg := err.Error()
	errorMsgLower := strings.ToLower(errorMsg)
	
	errorInfo := &ErrorInfo{
		Message:   errorMsg,
		Retryable: false,
		Metadata:  make(map[string]interface{}),
	}

	// Add stack trace for debugging
	buf := make([]byte, 1024)
	n := runtime.Stack(buf, false)
	errorInfo.StackTrace = string(buf[:n])

	// Categorize based on error content (order matters - more specific first)
	switch {
	case strings.Contains(errorMsgLower, "dockerfile") || strings.Contains(errorMsgLower, "instruction") || strings.Contains(errorMsgLower, "syntax error"):
		errorInfo.Category = ErrorCategoryBuild
		errorInfo.Suggestion = "Check Dockerfile syntax and instructions"
		
	case strings.Contains(errorMsgLower, "registry") || strings.Contains(errorMsgLower, "pull") || strings.Contains(errorMsgLower, "push"):
		errorInfo.Category = ErrorCategoryRegistry
		errorInfo.Retryable = true
		errorInfo.Suggestion = "Check registry connectivity and credentials"
		
	case strings.Contains(errorMsgLower, "auth") || strings.Contains(errorMsgLower, "credential") || strings.Contains(errorMsgLower, "unauthorized"):
		errorInfo.Category = ErrorCategoryAuth
		errorInfo.Suggestion = "Verify registry credentials and permissions"
		
	case strings.Contains(errorMsgLower, "network") || strings.Contains(errorMsgLower, "connection") || strings.Contains(errorMsgLower, "timeout"):
		errorInfo.Category = ErrorCategoryNetwork
		errorInfo.Retryable = true
		errorInfo.Suggestion = "Check network connectivity and retry"
		
	case strings.Contains(errorMsgLower, "cache"):
		errorInfo.Category = ErrorCategoryCache
		errorInfo.Retryable = true
		errorInfo.Suggestion = "Try clearing cache with --no-cache flag"
		
	case strings.Contains(errorMsgLower, "memory") || strings.Contains(errorMsgLower, "disk") || strings.Contains(errorMsgLower, "resource"):
		errorInfo.Category = ErrorCategoryResource
		errorInfo.Suggestion = "Increase available resources or use smaller base images"
		
	case strings.Contains(errorMsgLower, "permission") || strings.Contains(errorMsgLower, "denied"):
		errorInfo.Category = ErrorCategoryPermission
		errorInfo.Suggestion = "Check file permissions or try rootless mode"
		
	case strings.Contains(errorMsgLower, "config") || strings.Contains(errorMsgLower, "invalid") || strings.Contains(errorMsgLower, "parse"):
		errorInfo.Category = ErrorCategoryConfiguration
		errorInfo.Suggestion = "Check configuration syntax and values"
		
	case strings.Contains(errorMsgLower, "file") || strings.Contains(errorMsgLower, "directory") || strings.Contains(errorMsgLower, "no such"):
		errorInfo.Category = ErrorCategoryFilesystem
		errorInfo.Suggestion = "Check file paths and permissions"
		
	default:
		errorInfo.Category = ErrorCategoryUnknown
		errorInfo.Suggestion = "Check logs for more details"
	}

	// Add context metadata
	errorInfo.Metadata["operation"] = operation
	errorInfo.Metadata["stage"] = stage
	errorInfo.Metadata["timestamp"] = time.Now()

	return errorInfo
}

// generateRecommendations generates build optimization recommendations
func (o *ObservabilityManager) generateRecommendations(metrics *BuildMetrics, progress *ProgressSummary) []string {
	recommendations := make([]string, 0)

	// Cache hit rate recommendations
	if metrics.CacheHitRate < 50.0 {
		recommendations = append(recommendations, "Consider optimizing Dockerfile to improve cache hit rate")
	}

	// Memory usage recommendations
	if metrics.PeakMemoryMB > 4096 {
		recommendations = append(recommendations, "High memory usage detected - consider using smaller base images")
	}

	// Build time recommendations
	if metrics.Duration > 10*time.Minute {
		recommendations = append(recommendations, "Long build time - consider using multi-stage builds and layer caching")
	}

	// Error rate recommendations
	if metrics.ErrorCount > 0 {
		recommendations = append(recommendations, "Errors detected - review error logs and fix issues")
	}

	// Registry operation recommendations
	if metrics.RegistryMetrics != nil && metrics.RegistryMetrics.NetworkErrors > 0 {
		recommendations = append(recommendations, "Network errors during registry operations - check connectivity")
	}

	return recommendations
}