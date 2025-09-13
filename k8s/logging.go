package k8s

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/sirupsen/logrus"
)

// StructuredLogger provides Kubernetes-compatible structured logging
type StructuredLogger struct {
	logger    *logrus.Logger
	jobName   string
	podName   string
	namespace string
	buildID   string
}

// LogLevel represents log levels compatible with Kubernetes
type LogLevel string

const (
	LogLevelDebug LogLevel = "debug"
	LogLevelInfo  LogLevel = "info"
	LogLevelWarn  LogLevel = "warn"
	LogLevelError LogLevel = "error"
	LogLevelFatal LogLevel = "fatal"
)

// BuildEvent represents a build-related event
type BuildEvent struct {
	Type        string                 `json:"type"`
	Stage       string                 `json:"stage,omitempty"`
	Platform    string                 `json:"platform,omitempty"`
	Operation   string                 `json:"operation,omitempty"`
	Message     string                 `json:"message"`
	Duration    time.Duration          `json:"duration,omitempty"`
	Error       string                 `json:"error,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	CacheHit    bool                   `json:"cache_hit,omitempty"`
	Progress    float64                `json:"progress,omitempty"`
}

// NewStructuredLogger creates a new structured logger for Kubernetes
func NewStructuredLogger(buildID string) *StructuredLogger {
	logger := logrus.New()
	
	// Configure JSON formatter for Kubernetes log aggregation
	logger.SetFormatter(&logrus.JSONFormatter{
		TimestampFormat: time.RFC3339Nano,
		FieldMap: logrus.FieldMap{
			logrus.FieldKeyTime:  "timestamp",
			logrus.FieldKeyLevel: "level",
			logrus.FieldKeyMsg:   "message",
		},
	})

	// Set log level from environment
	if level := os.Getenv("LOG_LEVEL"); level != "" {
		if logLevel, err := logrus.ParseLevel(level); err == nil {
			logger.SetLevel(logLevel)
		}
	} else {
		logger.SetLevel(logrus.InfoLevel)
	}

	return &StructuredLogger{
		logger:    logger,
		jobName:   os.Getenv("JOB_NAME"),
		podName:   os.Getenv("POD_NAME"),
		namespace: os.Getenv("POD_NAMESPACE"),
		buildID:   buildID,
	}
}

// WithContext adds context information to log entries
func (s *StructuredLogger) WithContext(ctx context.Context) *logrus.Entry {
	entry := s.logger.WithFields(logrus.Fields{
		"component": "ossb",
		"build_id":  s.buildID,
	})

	if s.jobName != "" {
		entry = entry.WithField("job_name", s.jobName)
	}
	if s.podName != "" {
		entry = entry.WithField("pod_name", s.podName)
	}
	if s.namespace != "" {
		entry = entry.WithField("namespace", s.namespace)
	}

	// Add trace ID if available in context
	if traceID := ctx.Value("trace_id"); traceID != nil {
		entry = entry.WithField("trace_id", traceID)
	}

	return entry
}

// LogBuildStart logs the start of a build
func (s *StructuredLogger) LogBuildStart(ctx context.Context, platforms []string, tags []string) {
	s.WithContext(ctx).WithFields(logrus.Fields{
		"event":     "build_start",
		"platforms": platforms,
		"tags":      tags,
	}).Info("Starting container image build")
}

// LogBuildComplete logs the completion of a build
func (s *StructuredLogger) LogBuildComplete(ctx context.Context, success bool, duration time.Duration, operations int, cacheHits int) {
	entry := s.WithContext(ctx).WithFields(logrus.Fields{
		"event":      "build_complete",
		"success":    success,
		"duration":   duration.String(),
		"operations": operations,
		"cache_hits": cacheHits,
	})

	if success {
		entry.Info("Container image build completed successfully")
	} else {
		entry.Error("Container image build failed")
	}
}

// LogStageStart logs the start of a build stage
func (s *StructuredLogger) LogStageStart(ctx context.Context, stage string, platform string) {
	s.WithContext(ctx).WithFields(logrus.Fields{
		"event":    "stage_start",
		"stage":    stage,
		"platform": platform,
	}).Info(fmt.Sprintf("Starting build stage: %s", stage))
}

// LogStageComplete logs the completion of a build stage
func (s *StructuredLogger) LogStageComplete(ctx context.Context, stage string, platform string, duration time.Duration, success bool) {
	entry := s.WithContext(ctx).WithFields(logrus.Fields{
		"event":    "stage_complete",
		"stage":    stage,
		"platform": platform,
		"duration": duration.String(),
		"success":  success,
	})

	if success {
		entry.Info(fmt.Sprintf("Completed build stage: %s", stage))
	} else {
		entry.Error(fmt.Sprintf("Failed build stage: %s", stage))
	}
}

// LogOperation logs a build operation
func (s *StructuredLogger) LogOperation(ctx context.Context, operation string, platform string, cacheHit bool, duration time.Duration) {
	s.WithContext(ctx).WithFields(logrus.Fields{
		"event":     "operation",
		"operation": operation,
		"platform":  platform,
		"cache_hit": cacheHit,
		"duration":  duration.String(),
	}).Debug(fmt.Sprintf("Executed operation: %s", operation))
}

// LogProgress logs build progress
func (s *StructuredLogger) LogProgress(ctx context.Context, stage string, progress float64, message string) {
	s.WithContext(ctx).WithFields(logrus.Fields{
		"event":    "progress",
		"stage":    stage,
		"progress": progress,
	}).Info(message)
}

// LogError logs an error with context
func (s *StructuredLogger) LogError(ctx context.Context, err error, operation string, stage string) {
	s.WithContext(ctx).WithFields(logrus.Fields{
		"event":     "error",
		"operation": operation,
		"stage":     stage,
		"error":     err.Error(),
	}).Error(fmt.Sprintf("Operation failed: %s", operation))
}

// LogRegistryOperation logs registry operations
func (s *StructuredLogger) LogRegistryOperation(ctx context.Context, operation string, registry string, image string, success bool, duration time.Duration) {
	entry := s.WithContext(ctx).WithFields(logrus.Fields{
		"event":     "registry_operation",
		"operation": operation,
		"registry":  registry,
		"image":     image,
		"success":   success,
		"duration":  duration.String(),
	})

	if success {
		entry.Info(fmt.Sprintf("Registry operation completed: %s", operation))
	} else {
		entry.Error(fmt.Sprintf("Registry operation failed: %s", operation))
	}
}

// LogCacheOperation logs cache operations
func (s *StructuredLogger) LogCacheOperation(ctx context.Context, operation string, key string, hit bool, size int64) {
	s.WithContext(ctx).WithFields(logrus.Fields{
		"event":     "cache_operation",
		"operation": operation,
		"key":       key,
		"hit":       hit,
		"size":      size,
	}).Debug(fmt.Sprintf("Cache operation: %s", operation))
}

// LogResourceUsage logs resource usage information
func (s *StructuredLogger) LogResourceUsage(ctx context.Context, memoryMB int64, cpuPercent float64, diskMB int64) {
	s.WithContext(ctx).WithFields(logrus.Fields{
		"event":       "resource_usage",
		"memory_mb":   memoryMB,
		"cpu_percent": cpuPercent,
		"disk_mb":     diskMB,
	}).Debug("Resource usage")
}

// LogSecurityEvent logs security-related events
func (s *StructuredLogger) LogSecurityEvent(ctx context.Context, event string, details map[string]interface{}) {
	entry := s.WithContext(ctx).WithFields(logrus.Fields{
		"event":    "security",
		"security_event": event,
	})

	for key, value := range details {
		entry = entry.WithField(key, value)
	}

	entry.Info(fmt.Sprintf("Security event: %s", event))
}

// LogEvent logs a custom build event
func (s *StructuredLogger) LogEvent(ctx context.Context, event BuildEvent) {
	entry := s.WithContext(ctx).WithFields(logrus.Fields{
		"event": event.Type,
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
	if event.Error != "" {
		entry = entry.WithField("error", event.Error)
	}
	if event.CacheHit {
		entry = entry.WithField("cache_hit", event.CacheHit)
	}
	if event.Progress > 0 {
		entry = entry.WithField("progress", event.Progress)
	}

	for key, value := range event.Metadata {
		entry = entry.WithField(key, value)
	}

	if event.Error != "" {
		entry.Error(event.Message)
	} else {
		entry.Info(event.Message)
	}
}

// SetLogLevel sets the log level
func (s *StructuredLogger) SetLogLevel(level LogLevel) {
	switch level {
	case LogLevelDebug:
		s.logger.SetLevel(logrus.DebugLevel)
	case LogLevelInfo:
		s.logger.SetLevel(logrus.InfoLevel)
	case LogLevelWarn:
		s.logger.SetLevel(logrus.WarnLevel)
	case LogLevelError:
		s.logger.SetLevel(logrus.ErrorLevel)
	case LogLevelFatal:
		s.logger.SetLevel(logrus.FatalLevel)
	}
}

// GetLogger returns the underlying logrus logger
func (s *StructuredLogger) GetLogger() *logrus.Logger {
	return s.logger
}