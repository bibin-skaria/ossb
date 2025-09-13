package k8s

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func TestNewStructuredLogger(t *testing.T) {
	buildID := "test-build-123"
	logger := NewStructuredLogger(buildID)

	if logger == nil {
		t.Fatal("NewStructuredLogger() returned nil")
	}

	if logger.buildID != buildID {
		t.Errorf("buildID = %s, expected %s", logger.buildID, buildID)
	}

	if logger.logger == nil {
		t.Error("logger.logger is nil")
	}
}

func TestStructuredLogger_WithContext(t *testing.T) {
	// Set up environment variables
	os.Setenv("JOB_NAME", "test-job")
	os.Setenv("POD_NAME", "test-pod")
	os.Setenv("POD_NAMESPACE", "test-namespace")
	defer func() {
		os.Unsetenv("JOB_NAME")
		os.Unsetenv("POD_NAME")
		os.Unsetenv("POD_NAMESPACE")
	}()

	logger := NewStructuredLogger("test-build")
	ctx := context.WithValue(context.Background(), "trace_id", "trace-123")

	entry := logger.WithContext(ctx)

	// Verify fields are set
	expectedFields := map[string]interface{}{
		"component": "ossb",
		"build_id":  "test-build",
		"job_name":  "test-job",
		"pod_name":  "test-pod",
		"namespace": "test-namespace",
		"trace_id":  "trace-123",
	}

	for key, expectedValue := range expectedFields {
		if value, exists := entry.Data[key]; !exists {
			t.Errorf("Expected field %s not found", key)
		} else if value != expectedValue {
			t.Errorf("Field %s = %v, expected %v", key, value, expectedValue)
		}
	}
}

func TestStructuredLogger_LogBuildStart(t *testing.T) {
	var buf bytes.Buffer
	logger := NewStructuredLogger("test-build")
	logger.logger.SetOutput(&buf)

	ctx := context.Background()
	platforms := []string{"linux/amd64", "linux/arm64"}
	tags := []string{"myapp:latest", "myapp:v1.0"}

	logger.LogBuildStart(ctx, platforms, tags)

	// Parse log output
	var logEntry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("Failed to parse log output: %v", err)
	}

	// Verify log fields
	if logEntry["event"] != "build_start" {
		t.Errorf("event = %v, expected build_start", logEntry["event"])
	}

	if logEntry["message"] != "Starting container image build" {
		t.Errorf("message = %v, expected 'Starting container image build'", logEntry["message"])
	}

	// Verify platforms and tags are logged
	if platforms, ok := logEntry["platforms"].([]interface{}); !ok || len(platforms) != 2 {
		t.Errorf("platforms = %v, expected 2 platforms", logEntry["platforms"])
	}

	if tags, ok := logEntry["tags"].([]interface{}); !ok || len(tags) != 2 {
		t.Errorf("tags = %v, expected 2 tags", logEntry["tags"])
	}
}

func TestStructuredLogger_LogBuildComplete(t *testing.T) {
	var buf bytes.Buffer
	logger := NewStructuredLogger("test-build")
	logger.logger.SetOutput(&buf)

	ctx := context.Background()
	duration := 30 * time.Second

	tests := []struct {
		name       string
		success    bool
		operations int
		cacheHits  int
		expectedMsg string
		expectedLevel string
	}{
		{
			name:        "successful build",
			success:     true,
			operations:  10,
			cacheHits:   5,
			expectedMsg: "Container image build completed successfully",
			expectedLevel: "info",
		},
		{
			name:        "failed build",
			success:     false,
			operations:  8,
			cacheHits:   3,
			expectedMsg: "Container image build failed",
			expectedLevel: "error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf.Reset()
			logger.LogBuildComplete(ctx, tt.success, duration, tt.operations, tt.cacheHits)

			var logEntry map[string]interface{}
			if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
				t.Fatalf("Failed to parse log output: %v", err)
			}

			if logEntry["event"] != "build_complete" {
				t.Errorf("event = %v, expected build_complete", logEntry["event"])
			}

			if logEntry["message"] != tt.expectedMsg {
				t.Errorf("message = %v, expected %s", logEntry["message"], tt.expectedMsg)
			}

			if logEntry["level"] != tt.expectedLevel {
				t.Errorf("level = %v, expected %s", logEntry["level"], tt.expectedLevel)
			}

			if logEntry["success"] != tt.success {
				t.Errorf("success = %v, expected %v", logEntry["success"], tt.success)
			}

			if logEntry["duration"] != duration.String() {
				t.Errorf("duration = %v, expected %s", logEntry["duration"], duration.String())
			}

			if operations, ok := logEntry["operations"].(float64); !ok || int(operations) != tt.operations {
				t.Errorf("operations = %v, expected %d", logEntry["operations"], tt.operations)
			}

			if cacheHits, ok := logEntry["cache_hits"].(float64); !ok || int(cacheHits) != tt.cacheHits {
				t.Errorf("cache_hits = %v, expected %d", logEntry["cache_hits"], tt.cacheHits)
			}
		})
	}
}

func TestStructuredLogger_LogStageStart(t *testing.T) {
	var buf bytes.Buffer
	logger := NewStructuredLogger("test-build")
	logger.logger.SetOutput(&buf)

	ctx := context.Background()
	stage := "build-stage"
	platform := "linux/amd64"

	logger.LogStageStart(ctx, stage, platform)

	var logEntry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("Failed to parse log output: %v", err)
	}

	if logEntry["event"] != "stage_start" {
		t.Errorf("event = %v, expected stage_start", logEntry["event"])
	}

	if logEntry["stage"] != stage {
		t.Errorf("stage = %v, expected %s", logEntry["stage"], stage)
	}

	if logEntry["platform"] != platform {
		t.Errorf("platform = %v, expected %s", logEntry["platform"], platform)
	}

	expectedMsg := "Starting build stage: build-stage"
	if logEntry["message"] != expectedMsg {
		t.Errorf("message = %v, expected %s", logEntry["message"], expectedMsg)
	}
}

func TestStructuredLogger_LogOperation(t *testing.T) {
	var buf bytes.Buffer
	logger := NewStructuredLogger("test-build")
	logger.logger.SetOutput(&buf)
	logger.logger.SetLevel(logrus.DebugLevel) // Enable debug logging

	ctx := context.Background()
	operation := "RUN apt-get update"
	platform := "linux/amd64"
	cacheHit := true
	duration := 5 * time.Second

	logger.LogOperation(ctx, operation, platform, cacheHit, duration)

	var logEntry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("Failed to parse log output: %v", err)
	}

	if logEntry["event"] != "operation" {
		t.Errorf("event = %v, expected operation", logEntry["event"])
	}

	if logEntry["operation"] != operation {
		t.Errorf("operation = %v, expected %s", logEntry["operation"], operation)
	}

	if logEntry["platform"] != platform {
		t.Errorf("platform = %v, expected %s", logEntry["platform"], platform)
	}

	if logEntry["cache_hit"] != cacheHit {
		t.Errorf("cache_hit = %v, expected %v", logEntry["cache_hit"], cacheHit)
	}

	if logEntry["duration"] != duration.String() {
		t.Errorf("duration = %v, expected %s", logEntry["duration"], duration.String())
	}
}

func TestStructuredLogger_LogRegistryOperation(t *testing.T) {
	var buf bytes.Buffer
	logger := NewStructuredLogger("test-build")
	logger.logger.SetOutput(&buf)

	ctx := context.Background()
	operation := "push"
	registry := "docker.io"
	image := "myapp:latest"
	success := true
	duration := 10 * time.Second

	logger.LogRegistryOperation(ctx, operation, registry, image, success, duration)

	var logEntry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("Failed to parse log output: %v", err)
	}

	if logEntry["event"] != "registry_operation" {
		t.Errorf("event = %v, expected registry_operation", logEntry["event"])
	}

	if logEntry["operation"] != operation {
		t.Errorf("operation = %v, expected %s", logEntry["operation"], operation)
	}

	if logEntry["registry"] != registry {
		t.Errorf("registry = %v, expected %s", logEntry["registry"], registry)
	}

	if logEntry["image"] != image {
		t.Errorf("image = %v, expected %s", logEntry["image"], image)
	}

	if logEntry["success"] != success {
		t.Errorf("success = %v, expected %v", logEntry["success"], success)
	}

	if logEntry["duration"] != duration.String() {
		t.Errorf("duration = %v, expected %s", logEntry["duration"], duration.String())
	}
}

func TestStructuredLogger_LogEvent(t *testing.T) {
	var buf bytes.Buffer
	logger := NewStructuredLogger("test-build")
	logger.logger.SetOutput(&buf)

	ctx := context.Background()
	event := BuildEvent{
		Type:      "custom_event",
		Stage:     "test-stage",
		Platform:  "linux/amd64",
		Operation: "test-operation",
		Message:   "Test event message",
		Duration:  5 * time.Second,
		CacheHit:  true,
		Progress:  75.0,
		Metadata: map[string]interface{}{
			"custom_field": "custom_value",
		},
	}

	logger.LogEvent(ctx, event)

	var logEntry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("Failed to parse log output: %v", err)
	}

	// Verify all event fields are logged
	expectedFields := map[string]interface{}{
		"event":        "custom_event",
		"stage":        "test-stage",
		"platform":     "linux/amd64",
		"operation":    "test-operation",
		"message":      "Test event message",
		"duration":     "5s",
		"cache_hit":    true,
		"progress":     75.0,
		"custom_field": "custom_value",
	}

	for key, expectedValue := range expectedFields {
		if value, exists := logEntry[key]; !exists {
			t.Errorf("Expected field %s not found", key)
		} else if value != expectedValue {
			t.Errorf("Field %s = %v, expected %v", key, value, expectedValue)
		}
	}
}

func TestStructuredLogger_SetLogLevel(t *testing.T) {
	logger := NewStructuredLogger("test-build")

	tests := []struct {
		level    LogLevel
		expected logrus.Level
	}{
		{LogLevelDebug, logrus.DebugLevel},
		{LogLevelInfo, logrus.InfoLevel},
		{LogLevelWarn, logrus.WarnLevel},
		{LogLevelError, logrus.ErrorLevel},
		{LogLevelFatal, logrus.FatalLevel},
	}

	for _, tt := range tests {
		t.Run(string(tt.level), func(t *testing.T) {
			logger.SetLogLevel(tt.level)
			if logger.logger.GetLevel() != tt.expected {
				t.Errorf("Log level = %v, expected %v", logger.logger.GetLevel(), tt.expected)
			}
		})
	}
}

func TestStructuredLogger_LogLevelFromEnvironment(t *testing.T) {
	tests := []struct {
		envValue string
		expected logrus.Level
	}{
		{"debug", logrus.DebugLevel},
		{"info", logrus.InfoLevel},
		{"warn", logrus.WarnLevel},
		{"error", logrus.ErrorLevel},
		{"invalid", logrus.InfoLevel}, // Should default to info
		{"", logrus.InfoLevel},        // Should default to info
	}

	for _, tt := range tests {
		t.Run(tt.envValue, func(t *testing.T) {
			if tt.envValue != "" {
				os.Setenv("LOG_LEVEL", tt.envValue)
			} else {
				os.Unsetenv("LOG_LEVEL")
			}
			defer os.Unsetenv("LOG_LEVEL")

			logger := NewStructuredLogger("test-build")
			if logger.logger.GetLevel() != tt.expected {
				t.Errorf("Log level = %v, expected %v", logger.logger.GetLevel(), tt.expected)
			}
		})
	}
}

// Benchmark tests
func BenchmarkStructuredLogger_LogOperation(b *testing.B) {
	logger := NewStructuredLogger("test-build")
	logger.logger.SetOutput(&bytes.Buffer{}) // Discard output
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.LogOperation(ctx, "RUN apt-get update", "linux/amd64", false, time.Second)
	}
}

func BenchmarkStructuredLogger_LogEvent(b *testing.B) {
	logger := NewStructuredLogger("test-build")
	logger.logger.SetOutput(&bytes.Buffer{}) // Discard output
	ctx := context.Background()

	event := BuildEvent{
		Type:      "test_event",
		Stage:     "build",
		Platform:  "linux/amd64",
		Operation: "RUN",
		Message:   "Test message",
		Duration:  time.Second,
		CacheHit:  true,
		Progress:  50.0,
		Metadata: map[string]interface{}{
			"key1": "value1",
			"key2": "value2",
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.LogEvent(ctx, event)
	}
}