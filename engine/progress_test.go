package engine

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/bibin-skaria/ossb/internal/types"
)

func TestProgressTracker_BasicFlow(t *testing.T) {
	var output bytes.Buffer
	platforms := []types.Platform{
		{OS: "linux", Architecture: "amd64"},
		{OS: "linux", Architecture: "arm64"},
	}
	
	tracker := NewProgressTracker("test-build", platforms, &output, true)
	ctx := context.Background()

	// Test starting a stage
	tracker.StartStage(ctx, "build", "linux/amd64", 5)
	
	// Verify stage was created
	stage := tracker.GetStageProgress("build", "linux/amd64")
	if stage == nil {
		t.Fatal("Expected stage to be created")
	}
	
	if stage.Status != StageStatusRunning {
		t.Errorf("Expected stage status to be running, got %s", stage.Status)
	}
	
	if stage.Operations != 5 {
		t.Errorf("Expected 5 operations, got %d", stage.Operations)
	}

	// Test updating progress
	tracker.UpdateStageProgress(ctx, "build", "linux/amd64", 50.0, "Half way done")
	
	stage = tracker.GetStageProgress("build", "linux/amd64")
	if stage.Progress != 50.0 {
		t.Errorf("Expected progress to be 50.0, got %f", stage.Progress)
	}

	// Test operation updates
	tracker.UpdateOperation(ctx, "build", "linux/amd64", "exec", true, time.Second)
	tracker.UpdateOperation(ctx, "build", "linux/amd64", "file", false, time.Millisecond*500)
	
	stage = tracker.GetStageProgress("build", "linux/amd64")
	if stage.CompletedOps != 2 {
		t.Errorf("Expected 2 completed operations, got %d", stage.CompletedOps)
	}
	
	if stage.CacheHits != 1 {
		t.Errorf("Expected 1 cache hit, got %d", stage.CacheHits)
	}

	// Test completing stage
	tracker.CompleteStage(ctx, "build", "linux/amd64", true, "")
	
	stage = tracker.GetStageProgress("build", "linux/amd64")
	if stage.Status != StageStatusCompleted {
		t.Errorf("Expected stage status to be completed, got %s", stage.Status)
	}
	
	if stage.Progress != 100.0 {
		t.Errorf("Expected progress to be 100.0, got %f", stage.Progress)
	}

	// Test overall progress
	overallProgress := tracker.GetOverallProgress()
	if overallProgress != 100.0 {
		t.Errorf("Expected overall progress to be 100.0, got %f", overallProgress)
	}
}

func TestProgressTracker_MultipleStages(t *testing.T) {
	var output bytes.Buffer
	platforms := []types.Platform{{OS: "linux", Architecture: "amd64"}}
	
	tracker := NewProgressTracker("test-build", platforms, &output, false)
	ctx := context.Background()

	// Start multiple stages
	tracker.StartStage(ctx, "parse", "", 1)
	tracker.StartStage(ctx, "build", "linux/amd64", 3)
	tracker.StartStage(ctx, "export", "", 1)

	// Complete first stage
	tracker.CompleteStage(ctx, "parse", "", true, "")

	// Partially complete second stage
	tracker.UpdateOperation(ctx, "build", "linux/amd64", "pull", false, time.Second)
	tracker.UpdateStageProgress(ctx, "build", "linux/amd64", 33.3, "1/3 operations done")

	// Get summary
	summary := tracker.GetSummary()
	
	if summary.TotalStages != 3 {
		t.Errorf("Expected 3 total stages, got %d", summary.TotalStages)
	}
	
	if summary.CompletedStages != 1 {
		t.Errorf("Expected 1 completed stage, got %d", summary.CompletedStages)
	}
	
	// Overall progress should be partial
	if summary.OverallProgress < 30.0 || summary.OverallProgress > 50.0 {
		t.Errorf("Expected overall progress between 30-50%%, got %f", summary.OverallProgress)
	}
}

func TestProgressTracker_SubStages(t *testing.T) {
	var output bytes.Buffer
	platforms := []types.Platform{{OS: "linux", Architecture: "amd64"}}
	
	tracker := NewProgressTracker("test-build", platforms, &output, true)
	ctx := context.Background()

	// Start main stage
	tracker.StartStage(ctx, "build", "linux/amd64", 5)
	
	// Add sub-stages
	tracker.AddSubStage(ctx, "build", "linux/amd64", "pull", 2)
	tracker.AddSubStage(ctx, "build", "linux/amd64", "execute", 3)

	// Verify sub-stages were added
	stage := tracker.GetStageProgress("build", "linux/amd64")
	if len(stage.SubStages) != 2 {
		t.Errorf("Expected 2 sub-stages, got %d", len(stage.SubStages))
	}
	
	if _, exists := stage.SubStages["pull"]; !exists {
		t.Error("Expected 'pull' sub-stage to exist")
	}
	
	if _, exists := stage.SubStages["execute"]; !exists {
		t.Error("Expected 'execute' sub-stage to exist")
	}
}

func TestProgressTracker_FailedStage(t *testing.T) {
	var output bytes.Buffer
	platforms := []types.Platform{{OS: "linux", Architecture: "amd64"}}
	
	tracker := NewProgressTracker("test-build", platforms, &output, true)
	ctx := context.Background()

	// Start and fail a stage
	tracker.StartStage(ctx, "build", "linux/amd64", 3)
	tracker.UpdateOperation(ctx, "build", "linux/amd64", "pull", false, time.Second)
	tracker.CompleteStage(ctx, "build", "linux/amd64", false, "Network error")

	// Verify stage failure
	stage := tracker.GetStageProgress("build", "linux/amd64")
	if stage.Status != StageStatusFailed {
		t.Errorf("Expected stage status to be failed, got %s", stage.Status)
	}
	
	if stage.Error != "Network error" {
		t.Errorf("Expected error message 'Network error', got '%s'", stage.Error)
	}

	// Verify summary reflects failure
	summary := tracker.GetSummary()
	if summary.FailedStages != 1 {
		t.Errorf("Expected 1 failed stage, got %d", summary.FailedStages)
	}
}

func TestProgressTracker_Finish(t *testing.T) {
	var output bytes.Buffer
	platforms := []types.Platform{{OS: "linux", Architecture: "amd64"}}
	
	tracker := NewProgressTracker("test-build", platforms, &output, true)
	ctx := context.Background()

	// Start and complete a stage
	tracker.StartStage(ctx, "build", "linux/amd64", 1)
	tracker.UpdateOperation(ctx, "build", "linux/amd64", "exec", true, time.Second)
	tracker.CompleteStage(ctx, "build", "linux/amd64", true, "")

	// Finish successfully
	tracker.Finish(ctx, true)

	// Check output contains success message
	outputStr := output.String()
	if !bytes.Contains(output.Bytes(), []byte("Build completed successfully")) {
		t.Errorf("Expected success message in output, got: %s", outputStr)
	}
}

func TestProgressTracker_GetStageKey(t *testing.T) {
	var output bytes.Buffer
	platforms := []types.Platform{{OS: "linux", Architecture: "amd64"}}
	
	tracker := NewProgressTracker("test-build", platforms, &output, false)

	// Test stage key generation
	key1 := tracker.getStageKey("build", "linux/amd64")
	key2 := tracker.getStageKey("build", "")
	key3 := tracker.getStageKey("parse", "linux/amd64")

	if key1 != "build@linux/amd64" {
		t.Errorf("Expected 'build@linux/amd64', got '%s'", key1)
	}
	
	if key2 != "build" {
		t.Errorf("Expected 'build', got '%s'", key2)
	}
	
	if key3 != "parse@linux/amd64" {
		t.Errorf("Expected 'parse@linux/amd64', got '%s'", key3)
	}
}

func TestProgressTracker_ConcurrentAccess(t *testing.T) {
	var output bytes.Buffer
	platforms := []types.Platform{
		{OS: "linux", Architecture: "amd64"},
		{OS: "linux", Architecture: "arm64"},
	}
	
	tracker := NewProgressTracker("test-build", platforms, &output, false)
	ctx := context.Background()

	// Test concurrent access to tracker
	done := make(chan bool, 2)
	
	// Goroutine 1: amd64 platform
	go func() {
		tracker.StartStage(ctx, "build", "linux/amd64", 3)
		for i := 0; i < 3; i++ {
			tracker.UpdateOperation(ctx, "build", "linux/amd64", "exec", i%2 == 0, time.Millisecond*100)
			time.Sleep(time.Millisecond * 10)
		}
		tracker.CompleteStage(ctx, "build", "linux/amd64", true, "")
		done <- true
	}()
	
	// Goroutine 2: arm64 platform
	go func() {
		tracker.StartStage(ctx, "build", "linux/arm64", 2)
		for i := 0; i < 2; i++ {
			tracker.UpdateOperation(ctx, "build", "linux/arm64", "file", i%2 == 1, time.Millisecond*150)
			time.Sleep(time.Millisecond * 15)
		}
		tracker.CompleteStage(ctx, "build", "linux/arm64", true, "")
		done <- true
	}()

	// Wait for both goroutines to complete
	<-done
	<-done

	// Verify both stages completed
	summary := tracker.GetSummary()
	if summary.TotalStages != 2 {
		t.Errorf("Expected 2 total stages, got %d", summary.TotalStages)
	}
	
	if summary.CompletedStages != 2 {
		t.Errorf("Expected 2 completed stages, got %d", summary.CompletedStages)
	}
}

func BenchmarkProgressTracker_UpdateOperation(b *testing.B) {
	var output bytes.Buffer
	platforms := []types.Platform{{OS: "linux", Architecture: "amd64"}}
	
	tracker := NewProgressTracker("bench-build", platforms, &output, false)
	ctx := context.Background()
	
	tracker.StartStage(ctx, "build", "linux/amd64", b.N)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tracker.UpdateOperation(ctx, "build", "linux/amd64", "exec", i%2 == 0, time.Microsecond*100)
	}
}

func BenchmarkProgressTracker_GetSummary(b *testing.B) {
	var output bytes.Buffer
	platforms := []types.Platform{{OS: "linux", Architecture: "amd64"}}
	
	tracker := NewProgressTracker("bench-build", platforms, &output, false)
	ctx := context.Background()
	
	// Set up some stages
	for i := 0; i < 10; i++ {
		stageName := fmt.Sprintf("stage-%d", i)
		tracker.StartStage(ctx, stageName, "linux/amd64", 5)
		for j := 0; j < 5; j++ {
			tracker.UpdateOperation(ctx, stageName, "linux/amd64", "exec", j%2 == 0, time.Microsecond*100)
		}
		tracker.CompleteStage(ctx, stageName, "linux/amd64", true, "")
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tracker.GetSummary()
	}
}