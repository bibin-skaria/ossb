package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bibin-skaria/ossb/internal/types"
)

func TestObservabilityManager_BasicFlow(t *testing.T) {
	var output bytes.Buffer
	config := &types.BuildConfig{
		Context:    "/test",
		Dockerfile: "Dockerfile",
		Tags:       []string{"test:latest"},
		Platforms:  []types.Platform{{OS: "linux", Architecture: "amd64"}},
		Progress:   true,
	}
	
	manager := NewObservabilityManager("test-build", config, &output, true)
	ctx := context.Background()

	// Start build
	ctx = manager.StartBuild(ctx)
	
	// Start stage
	manager.StartStage(ctx, "build", "linux/amd64", 3)
	
	// Update progress
	manager.UpdateStageProgress(ctx, "build", "linux/amd64", 33.3, "1/3 operations complete")
	
	// Record operation
	operation := &types.Operation{
		Type:     types.OperationTypeExec,
		Command:  []string{"echo", "test"},
		Platform: types.Platform{OS: "linux", Architecture: "amd64"},
	}
	
	result := &types.OperationResult{
		Operation: operation,
		Success:   true,
		CacheHit:  true,
	}
	
	manager.RecordOperation(ctx, operation, result, time.Second)
	
	// Complete stage
	manager.CompleteStage(ctx, "build", "linux/amd64", true, "")
	
	// Finish build
	buildResult := &types.BuildResult{
		Success:     true,
		Operations:  3,
		CacheHits:   1,
		Duration:    "2s",
		PlatformResults: map[string]*types.PlatformResult{
			"linux/amd64": {
				Platform: types.Platform{OS: "linux", Architecture: "amd64"},
				Success:  true,
			},
		},
	}
	
	report := manager.FinishBuild(ctx, buildResult)
	
	// Verify report
	if report.BuildID != "test-build" {
		t.Errorf("Expected build ID 'test-build', got '%s'", report.BuildID)
	}
	
	if !report.Success {
		t.Error("Expected build to be successful")
	}
	
	if report.Summary == nil {
		t.Fatal("Expected build summary to be present")
	}
	
	if report.Summary.TotalStages != 1 {
		t.Errorf("Expected 1 total stage, got %d", report.Summary.TotalStages)
	}
	
	if report.Summary.CompletedStages != 1 {
		t.Errorf("Expected 1 completed stage, got %d", report.Summary.CompletedStages)
	}
}

func TestObservabilityManager_ErrorHandling(t *testing.T) {
	var output bytes.Buffer
	config := &types.BuildConfig{
		Context:    "/test",
		Dockerfile: "Dockerfile",
		Tags:       []string{"test:latest"},
		Platforms:  []types.Platform{{OS: "linux", Architecture: "amd64"}},
		Progress:   true,
	}
	
	manager := NewObservabilityManager("test-build", config, &output, true)
	ctx := context.Background()

	// Test different error categories
	testErrors := []struct {
		err      error
		expected ErrorCategory
	}{
		{errors.New("registry connection failed"), ErrorCategoryRegistry},
		{errors.New("authentication failed"), ErrorCategoryAuth},
		{errors.New("network timeout"), ErrorCategoryNetwork},
		{errors.New("file not found"), ErrorCategoryFilesystem},
		{errors.New("cache corruption"), ErrorCategoryCache},
		{errors.New("permission denied"), ErrorCategoryPermission},
		{errors.New("invalid config"), ErrorCategoryConfiguration},
		{errors.New("dockerfile syntax error"), ErrorCategoryBuild},
		{errors.New("unknown error"), ErrorCategoryUnknown},
	}
	
	for _, test := range testErrors {
		manager.RecordError(ctx, test.err, "test_op", "test_stage", "linux/amd64")
		
		// Verify error was categorized correctly
		errorInfo := manager.categorizeError(test.err, "test_op", "test_stage")
		if errorInfo.Category != test.expected {
			t.Errorf("Expected error category %s for error '%s', got %s", 
				test.expected, test.err.Error(), errorInfo.Category)
		}
	}
}

func TestStructuredLogger_EventLogging(t *testing.T) {
	var output bytes.Buffer
	logger := NewStructuredLogger("test-build", &output)
	ctx := context.Background()

	config := &types.BuildConfig{
		Context:    "/test",
		Dockerfile: "Dockerfile",
		Tags:       []string{"test:latest"},
		Platforms:  []types.Platform{{OS: "linux", Architecture: "amd64"}},
	}

	// Test build start logging
	logger.LogBuildStart(ctx, config)
	
	// Test stage logging
	logger.LogStageStart(ctx, "build", "linux/amd64", 3)
	logger.LogStageComplete(ctx, "build", "linux/amd64", true, "")
	
	// Test operation logging
	operation := &types.Operation{
		Type:     types.OperationTypeExec,
		Platform: types.Platform{OS: "linux", Architecture: "amd64"},
	}
	
	result := &types.OperationResult{
		Operation: operation,
		Success:   true,
		CacheHit:  true,
	}
	
	logger.LogOperation(ctx, operation, result, time.Second)
	
	// Test build completion logging
	buildResult := &types.BuildResult{
		Success:   true,
		Duration:  "2s",
		Operations: 3,
		CacheHits: 1,
	}
	
	logger.LogBuildComplete(ctx, buildResult)
	
	// Verify output contains JSON logs
	outputStr := output.String()
	if outputStr == "" {
		t.Error("Expected log output, got empty string")
	}
	
	// Should contain structured JSON logs
	if !bytes.Contains(output.Bytes(), []byte(`"component":"ossb"`)) {
		t.Error("Expected structured log with component field")
	}
	
	if !bytes.Contains(output.Bytes(), []byte(`"build_id":"test-build"`)) {
		t.Error("Expected build ID in logs")
	}
}

func TestObservabilityManager_RecommendationGeneration(t *testing.T) {
	var output bytes.Buffer
	config := &types.BuildConfig{
		Context:    "/test",
		Dockerfile: "Dockerfile",
		Tags:       []string{"test:latest"},
		Platforms:  []types.Platform{{OS: "linux", Architecture: "amd64"}},
		Progress:   true,
	}
	
	manager := NewObservabilityManager("test-build", config, &output, false)

	// Create metrics with various issues
	metrics := &BuildMetrics{
		BuildID:         "test-build",
		CacheHitRate:    25.0, // Low cache hit rate
		PeakMemoryMB:    8192, // High memory usage
		Duration:        15 * time.Minute, // Long build time
		ErrorCount:      3,    // Has errors
		RegistryMetrics: &RegistryMetrics{
			NetworkErrors: 2, // Network issues
		},
	}
	
	progress := &ProgressSummary{
		TotalStages:     3,
		CompletedStages: 2,
		FailedStages:    1,
	}
	
	recommendations := manager.generateRecommendations(metrics, progress)
	
	// Should generate multiple recommendations
	if len(recommendations) == 0 {
		t.Error("Expected recommendations to be generated")
	}
	
	// Check for specific recommendations
	hasCacheRecommendation := false
	hasMemoryRecommendation := false
	hasTimeRecommendation := false
	hasErrorRecommendation := false
	hasNetworkRecommendation := false
	
	for _, rec := range recommendations {
		if bytes.Contains([]byte(rec), []byte("cache")) {
			hasCacheRecommendation = true
		}
		if bytes.Contains([]byte(rec), []byte("memory")) {
			hasMemoryRecommendation = true
		}
		if bytes.Contains([]byte(rec), []byte("time")) {
			hasTimeRecommendation = true
		}
		if bytes.Contains([]byte(rec), []byte("error")) {
			hasErrorRecommendation = true
		}
		if bytes.Contains([]byte(strings.ToLower(rec)), []byte("network")) {
			hasNetworkRecommendation = true
		}
	}
	
	if !hasCacheRecommendation {
		t.Error("Expected cache optimization recommendation")
	}
	if !hasMemoryRecommendation {
		t.Error("Expected memory optimization recommendation")
	}
	if !hasTimeRecommendation {
		t.Error("Expected build time optimization recommendation")
	}
	if !hasErrorRecommendation {
		t.Error("Expected error handling recommendation")
	}
	if !hasNetworkRecommendation {
		t.Error("Expected network optimization recommendation")
	}
}

func TestErrorInfo_Categorization(t *testing.T) {
	var output bytes.Buffer
	config := &types.BuildConfig{
		Platforms: []types.Platform{{OS: "linux", Architecture: "amd64"}},
	}
	
	manager := NewObservabilityManager("test-build", config, &output, false)

	testCases := []struct {
		error    string
		category ErrorCategory
		retryable bool
	}{
		{"registry pull failed", ErrorCategoryRegistry, true},
		{"authentication required", ErrorCategoryAuth, false},
		{"connection timeout", ErrorCategoryNetwork, true},
		{"file not found", ErrorCategoryFilesystem, false},
		{"cache miss", ErrorCategoryCache, true},
		{"permission denied", ErrorCategoryPermission, false},
		{"invalid dockerfile", ErrorCategoryBuild, false},
		{"memory limit exceeded", ErrorCategoryResource, false},
		{"random error", ErrorCategoryUnknown, false},
	}
	
	for _, tc := range testCases {
		err := errors.New(tc.error)
		errorInfo := manager.categorizeError(err, "test_op", "test_stage")
		
		if errorInfo.Category != tc.category {
			t.Errorf("Expected category %s for error '%s', got %s", 
				tc.category, tc.error, errorInfo.Category)
		}
		
		if errorInfo.Retryable != tc.retryable {
			t.Errorf("Expected retryable=%t for error '%s', got %t", 
				tc.retryable, tc.error, errorInfo.Retryable)
		}
		
		if errorInfo.Message != tc.error {
			t.Errorf("Expected message '%s', got '%s'", tc.error, errorInfo.Message)
		}
		
		if errorInfo.Suggestion == "" {
			t.Errorf("Expected suggestion for error '%s'", tc.error)
		}
		
		if errorInfo.StackTrace == "" {
			t.Errorf("Expected stack trace for error '%s'", tc.error)
		}
	}
}

func TestObservabilityManager_MultiStageFlow(t *testing.T) {
	var output bytes.Buffer
	config := &types.BuildConfig{
		Context:    "/test",
		Dockerfile: "Dockerfile",
		Tags:       []string{"test:latest"},
		Platforms:  []types.Platform{
			{OS: "linux", Architecture: "amd64"},
			{OS: "linux", Architecture: "arm64"},
		},
		Progress: true,
	}
	
	manager := NewObservabilityManager("test-build", config, &output, true)
	ctx := context.Background()

	// Start build
	ctx = manager.StartBuild(ctx)
	
	stages := []struct {
		name     string
		platform string
		ops      int
		success  bool
	}{
		{"parse", "", 1, true},
		{"build", "linux/amd64", 3, true},
		{"build", "linux/arm64", 3, false}, // Failed stage
		{"export", "", 1, true},
	}
	
	for _, stage := range stages {
		manager.StartStage(ctx, stage.name, stage.platform, stage.ops)
		
		for i := 0; i < stage.ops; i++ {
			progress := float64(i+1) / float64(stage.ops) * 100.0
			manager.UpdateStageProgress(ctx, stage.name, stage.platform, progress, 
				fmt.Sprintf("Operation %d/%d", i+1, stage.ops))
			
			operation := &types.Operation{
				Type:     types.OperationTypeExec,
				Platform: types.ParsePlatform(stage.platform),
			}
			
			result := &types.OperationResult{
				Operation: operation,
				Success:   stage.success,
				CacheHit:  i%2 == 0,
			}
			
			if !stage.success {
				result.Error = "Test error"
			}
			
			manager.RecordOperation(ctx, operation, result, time.Millisecond*100)
		}
		
		errorMsg := ""
		if !stage.success {
			errorMsg = "Stage failed"
		}
		
		manager.CompleteStage(ctx, stage.name, stage.platform, stage.success, errorMsg)
	}
	
	// Finish build
	buildResult := &types.BuildResult{
		Success:     false, // Overall failure due to arm64 failure
		Operations:  8,
		CacheHits:   4,
		Duration:    "5s",
		Error:       "Build failed for platform linux/arm64",
		PlatformResults: map[string]*types.PlatformResult{
			"linux/amd64": {
				Platform: types.Platform{OS: "linux", Architecture: "amd64"},
				Success:  true,
			},
			"linux/arm64": {
				Platform: types.Platform{OS: "linux", Architecture: "arm64"},
				Success:  false,
				Error:    "Test error",
			},
		},
	}
	
	report := manager.FinishBuild(ctx, buildResult)
	
	// Verify report reflects multi-stage, multi-platform build
	if report.Summary.TotalStages != 4 {
		t.Errorf("Expected 4 total stages, got %d", report.Summary.TotalStages)
	}
	
	if report.Summary.FailedStages != 1 {
		t.Errorf("Expected 1 failed stage, got %d", report.Summary.FailedStages)
	}
	
	if report.Success {
		t.Error("Expected overall build to be marked as failed")
	}
	
	if len(report.Recommendations) == 0 {
		t.Error("Expected recommendations for failed build")
	}
}

func BenchmarkObservabilityManager_RecordOperation(b *testing.B) {
	var output bytes.Buffer
	config := &types.BuildConfig{
		Platforms: []types.Platform{{OS: "linux", Architecture: "amd64"}},
	}
	
	manager := NewObservabilityManager("bench-build", config, &output, false)
	ctx := context.Background()
	
	ctx = manager.StartBuild(ctx)
	manager.StartStage(ctx, "build", "linux/amd64", b.N)
	
	operation := &types.Operation{
		Type:     types.OperationTypeExec,
		Platform: types.Platform{OS: "linux", Architecture: "amd64"},
	}
	
	result := &types.OperationResult{
		Operation: operation,
		Success:   true,
		CacheHit:  true,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		manager.RecordOperation(ctx, operation, result, time.Microsecond*100)
	}
}