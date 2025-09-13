package engine

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/bibin-skaria/ossb/internal/types"
)

// ProgressTracker tracks build progress with stage-level granularity
type ProgressTracker struct {
	mutex           sync.RWMutex
	stages          map[string]*StageProgress
	totalStages     int
	completedStages int
	currentStage    string
	startTime       time.Time
	output          io.Writer
	buildID         string
	platforms       []types.Platform
	verbose         bool
}

// StageProgress represents progress for a single build stage
type StageProgress struct {
	Name            string                       `json:"name"`
	Platform        string                       `json:"platform,omitempty"`
	StartTime       time.Time                    `json:"start_time"`
	EndTime         *time.Time                   `json:"end_time,omitempty"`
	Duration        time.Duration                `json:"duration"`
	Progress        float64                      `json:"progress"`
	Status          StageStatus                  `json:"status"`
	Operations      int                          `json:"operations"`
	CompletedOps    int                          `json:"completed_operations"`
	CacheHits       int                          `json:"cache_hits"`
	Error           string                       `json:"error,omitempty"`
	Metadata        map[string]interface{}       `json:"metadata,omitempty"`
	SubStages       map[string]*StageProgress    `json:"sub_stages,omitempty"`
}

// StageStatus represents the status of a build stage
type StageStatus string

const (
	StageStatusPending    StageStatus = "pending"
	StageStatusRunning    StageStatus = "running"
	StageStatusCompleted  StageStatus = "completed"
	StageStatusFailed     StageStatus = "failed"
	StageStatusSkipped    StageStatus = "skipped"
)

// ProgressEvent represents a progress event
type ProgressEvent struct {
	Type        string                 `json:"type"`
	Stage       string                 `json:"stage"`
	Platform    string                 `json:"platform,omitempty"`
	Operation   string                 `json:"operation,omitempty"`
	Progress    float64                `json:"progress"`
	Message     string                 `json:"message"`
	Timestamp   time.Time              `json:"timestamp"`
	Duration    time.Duration          `json:"duration,omitempty"`
	CacheHit    bool                   `json:"cache_hit,omitempty"`
	Error       string                 `json:"error,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// NewProgressTracker creates a new progress tracker
func NewProgressTracker(buildID string, platforms []types.Platform, output io.Writer, verbose bool) *ProgressTracker {
	return &ProgressTracker{
		stages:      make(map[string]*StageProgress),
		startTime:   time.Now(),
		output:      output,
		buildID:     buildID,
		platforms:   platforms,
		verbose:     verbose,
	}
}

// StartStage starts tracking a new stage
func (p *ProgressTracker) StartStage(ctx context.Context, stageName string, platform string, expectedOps int) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	stageKey := p.getStageKey(stageName, platform)
	
	stage := &StageProgress{
		Name:         stageName,
		Platform:     platform,
		StartTime:    time.Now(),
		Status:       StageStatusRunning,
		Operations:   expectedOps,
		CompletedOps: 0,
		CacheHits:    0,
		Progress:     0.0,
		Metadata:     make(map[string]interface{}),
		SubStages:    make(map[string]*StageProgress),
	}

	p.stages[stageKey] = stage
	p.currentStage = stageKey
	p.totalStages++

	// Emit progress event
	event := ProgressEvent{
		Type:      "stage_start",
		Stage:     stageName,
		Platform:  platform,
		Progress:  0.0,
		Message:   fmt.Sprintf("Starting stage: %s", stageName),
		Timestamp: time.Now(),
		Metadata: map[string]interface{}{
			"expected_operations": expectedOps,
		},
	}

	p.emitEvent(ctx, event)
	p.updateDisplay()
}

// UpdateStageProgress updates progress for a stage
func (p *ProgressTracker) UpdateStageProgress(ctx context.Context, stageName string, platform string, progress float64, message string) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	stageKey := p.getStageKey(stageName, platform)
	stage, exists := p.stages[stageKey]
	if !exists {
		return
	}

	stage.Progress = progress
	stage.Duration = time.Since(stage.StartTime)

	// Emit progress event
	event := ProgressEvent{
		Type:      "stage_progress",
		Stage:     stageName,
		Platform:  platform,
		Progress:  progress,
		Message:   message,
		Timestamp: time.Now(),
		Duration:  stage.Duration,
	}

	p.emitEvent(ctx, event)
	p.updateDisplay()
}

// CompleteStage marks a stage as completed
func (p *ProgressTracker) CompleteStage(ctx context.Context, stageName string, platform string, success bool, errorMsg string) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	stageKey := p.getStageKey(stageName, platform)
	stage, exists := p.stages[stageKey]
	if !exists {
		return
	}

	endTime := time.Now()
	stage.EndTime = &endTime
	stage.Duration = endTime.Sub(stage.StartTime)
	stage.Progress = 100.0

	if success {
		stage.Status = StageStatusCompleted
		p.completedStages++
	} else {
		stage.Status = StageStatusFailed
		stage.Error = errorMsg
	}

	// Emit completion event
	event := ProgressEvent{
		Type:      "stage_complete",
		Stage:     stageName,
		Platform:  platform,
		Progress:  100.0,
		Message:   fmt.Sprintf("Stage %s: %s", stageName, stage.Status),
		Timestamp: time.Now(),
		Duration:  stage.Duration,
		Error:     errorMsg,
		Metadata: map[string]interface{}{
			"operations":   stage.Operations,
			"completed":    stage.CompletedOps,
			"cache_hits":   stage.CacheHits,
			"success":      success,
		},
	}

	p.emitEvent(ctx, event)
	p.updateDisplay()
}

// UpdateOperation updates progress for an operation within a stage
func (p *ProgressTracker) UpdateOperation(ctx context.Context, stageName string, platform string, operation string, cacheHit bool, duration time.Duration) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	stageKey := p.getStageKey(stageName, platform)
	stage, exists := p.stages[stageKey]
	if !exists {
		return
	}

	stage.CompletedOps++
	if cacheHit {
		stage.CacheHits++
	}

	// Update stage progress based on completed operations
	if stage.Operations > 0 {
		stage.Progress = float64(stage.CompletedOps) / float64(stage.Operations) * 100.0
	}

	stage.Duration = time.Since(stage.StartTime)

	// Emit operation event
	event := ProgressEvent{
		Type:      "operation",
		Stage:     stageName,
		Platform:  platform,
		Operation: operation,
		Progress:  stage.Progress,
		Message:   fmt.Sprintf("Executed %s operation", operation),
		Timestamp: time.Now(),
		Duration:  duration,
		CacheHit:  cacheHit,
		Metadata: map[string]interface{}{
			"completed_ops": stage.CompletedOps,
			"total_ops":     stage.Operations,
		},
	}

	p.emitEvent(ctx, event)
	
	if p.verbose {
		p.updateDisplay()
	}
}

// AddSubStage adds a sub-stage to an existing stage
func (p *ProgressTracker) AddSubStage(ctx context.Context, parentStage string, platform string, subStageName string, expectedOps int) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	parentKey := p.getStageKey(parentStage, platform)
	parent, exists := p.stages[parentKey]
	if !exists {
		return
	}

	subStage := &StageProgress{
		Name:         subStageName,
		Platform:     platform,
		StartTime:    time.Now(),
		Status:       StageStatusRunning,
		Operations:   expectedOps,
		CompletedOps: 0,
		CacheHits:    0,
		Progress:     0.0,
		Metadata:     make(map[string]interface{}),
	}

	parent.SubStages[subStageName] = subStage

	// Emit sub-stage start event
	event := ProgressEvent{
		Type:      "substage_start",
		Stage:     fmt.Sprintf("%s.%s", parentStage, subStageName),
		Platform:  platform,
		Progress:  0.0,
		Message:   fmt.Sprintf("Starting sub-stage: %s", subStageName),
		Timestamp: time.Now(),
		Metadata: map[string]interface{}{
			"parent_stage":        parentStage,
			"expected_operations": expectedOps,
		},
	}

	p.emitEvent(ctx, event)
}

// GetOverallProgress returns the overall build progress
func (p *ProgressTracker) GetOverallProgress() float64 {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	if p.totalStages == 0 {
		return 0.0
	}

	totalProgress := 0.0
	for _, stage := range p.stages {
		totalProgress += stage.Progress
	}

	return totalProgress / float64(p.totalStages)
}

// GetStageProgress returns progress for a specific stage
func (p *ProgressTracker) GetStageProgress(stageName string, platform string) *StageProgress {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	stageKey := p.getStageKey(stageName, platform)
	if stage, exists := p.stages[stageKey]; exists {
		// Return a copy to avoid race conditions
		stageCopy := *stage
		return &stageCopy
	}
	return nil
}

// GetAllStages returns all stage progress information
func (p *ProgressTracker) GetAllStages() map[string]*StageProgress {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	stages := make(map[string]*StageProgress)
	for key, stage := range p.stages {
		stageCopy := *stage
		stages[key] = &stageCopy
	}
	return stages
}

// GetSummary returns a progress summary
func (p *ProgressTracker) GetSummary() ProgressSummary {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	summary := ProgressSummary{
		BuildID:         p.buildID,
		StartTime:       p.startTime,
		Duration:        time.Since(p.startTime),
		TotalStages:     p.totalStages,
		CompletedStages: p.completedStages,
		OverallProgress: p.GetOverallProgress(),
		Platforms:       len(p.platforms),
		Stages:          make([]StageSummary, 0, len(p.stages)),
	}

	totalOps := 0
	totalCacheHits := 0
	failedStages := 0

	for _, stage := range p.stages {
		totalOps += stage.Operations
		totalCacheHits += stage.CacheHits
		
		if stage.Status == StageStatusFailed {
			failedStages++
		}

		stageSummary := StageSummary{
			Name:         stage.Name,
			Platform:     stage.Platform,
			Status:       stage.Status,
			Progress:     stage.Progress,
			Duration:     stage.Duration,
			Operations:   stage.Operations,
			CompletedOps: stage.CompletedOps,
			CacheHits:    stage.CacheHits,
			Error:        stage.Error,
		}

		summary.Stages = append(summary.Stages, stageSummary)
	}

	summary.TotalOperations = totalOps
	summary.TotalCacheHits = totalCacheHits
	summary.FailedStages = failedStages

	return summary
}

// ProgressSummary represents a summary of build progress
type ProgressSummary struct {
	BuildID          string          `json:"build_id"`
	StartTime        time.Time       `json:"start_time"`
	Duration         time.Duration   `json:"duration"`
	TotalStages      int             `json:"total_stages"`
	CompletedStages  int             `json:"completed_stages"`
	FailedStages     int             `json:"failed_stages"`
	OverallProgress  float64         `json:"overall_progress"`
	Platforms        int             `json:"platforms"`
	TotalOperations  int             `json:"total_operations"`
	TotalCacheHits   int             `json:"total_cache_hits"`
	Stages           []StageSummary  `json:"stages"`
}

// StageSummary represents a summary of stage progress
type StageSummary struct {
	Name         string        `json:"name"`
	Platform     string        `json:"platform,omitempty"`
	Status       StageStatus   `json:"status"`
	Progress     float64       `json:"progress"`
	Duration     time.Duration `json:"duration"`
	Operations   int           `json:"operations"`
	CompletedOps int           `json:"completed_operations"`
	CacheHits    int           `json:"cache_hits"`
	Error        string        `json:"error,omitempty"`
}

// getStageKey generates a unique key for a stage
func (p *ProgressTracker) getStageKey(stageName string, platform string) string {
	if platform != "" {
		return fmt.Sprintf("%s@%s", stageName, platform)
	}
	return stageName
}

// emitEvent emits a progress event
func (p *ProgressTracker) emitEvent(ctx context.Context, event ProgressEvent) {
	// This could be extended to send events to external systems
	// For now, we just store the event for potential logging
	if p.verbose && p.output != nil {
		fmt.Fprintf(p.output, "[%s] %s: %s (%.1f%%)\n", 
			event.Timestamp.Format("15:04:05"), 
			event.Stage, 
			event.Message, 
			event.Progress)
	}
}

// updateDisplay updates the progress display (must be called with mutex held)
func (p *ProgressTracker) updateDisplay() {
	if p.output == nil {
		return
	}

	// Calculate overall progress without acquiring lock (already held)
	overallProgress := 0.0
	if p.totalStages > 0 {
		totalProgress := 0.0
		for _, stage := range p.stages {
			totalProgress += stage.Progress
		}
		overallProgress = totalProgress / float64(p.totalStages)
	}
	
	if !p.verbose {
		// Simple progress line
		fmt.Fprintf(p.output, "\rProgress: %.1f%% (%d/%d stages)", 
			overallProgress, p.completedStages, p.totalStages)
	}
}

// Finish completes the progress tracking
func (p *ProgressTracker) Finish(ctx context.Context, success bool) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	duration := time.Since(p.startTime)
	
	event := ProgressEvent{
		Type:      "build_complete",
		Progress:  100.0,
		Message:   fmt.Sprintf("Build completed in %s", duration),
		Timestamp: time.Now(),
		Duration:  duration,
		Metadata: map[string]interface{}{
			"success":           success,
			"total_stages":      p.totalStages,
			"completed_stages":  p.completedStages,
			"failed_stages":     p.totalStages - p.completedStages,
		},
	}

	if !success {
		event.Error = "Build failed"
	}

	p.emitEvent(ctx, event)

	if p.output != nil {
		if success {
			fmt.Fprintf(p.output, "\n✓ Build completed successfully in %s\n", duration)
		} else {
			fmt.Fprintf(p.output, "\n✗ Build failed after %s\n", duration)
		}
	}
}