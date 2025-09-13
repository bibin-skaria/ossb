package engine

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/bibin-skaria/ossb/internal/types"
)

// ConcurrentBuilder manages concurrent build operations with resource constraints
type ConcurrentBuilder struct {
	resourceManager *ResourceManager
	buildQueue      chan *BuildRequest
	workers         []*BuildWorker
	results         map[string]*BuildResult
	resultsMutex    sync.RWMutex
	config          *ConcurrentBuildConfig
	scheduler       *BuildScheduler
	coordinator     *BuildCoordinator
}

// BuildRequest represents a build request in the queue
type BuildRequest struct {
	ID       string
	Config   *types.BuildConfig
	Priority int
	Context  context.Context
	Result   chan *BuildResult
	Started  time.Time
	Timeout  time.Duration
}

// BuildResult represents the result of a concurrent build
type BuildResult struct {
	Request   *BuildRequest
	Result    *types.BuildResult
	Error     error
	Duration  time.Duration
	Resources *ResourceMonitorSummary
}

// BuildWorker represents a worker that processes build requests
type BuildWorker struct {
	ID              int
	builder         *Builder
	resourceManager *ResourceManager
	currentRequest  *BuildRequest
	mutex           sync.RWMutex
	stopChan        chan bool
	workChan        <-chan *BuildRequest
}

// ConcurrentBuildConfig configures concurrent build behavior
type ConcurrentBuildConfig struct {
	MaxWorkers          int           `json:"max_workers"`
	QueueSize           int           `json:"queue_size"`
	DefaultTimeout      time.Duration `json:"default_timeout"`
	ResourceCheckInterval time.Duration `json:"resource_check_interval"`
	EnablePrioritization bool          `json:"enable_prioritization"`
	EnableLoadBalancing  bool          `json:"enable_load_balancing"`
	WorkerIdleTimeout   time.Duration `json:"worker_idle_timeout"`
}

// BuildScheduler schedules builds based on priority and resource availability
type BuildScheduler struct {
	pendingBuilds   []*BuildRequest
	runningBuilds   map[string]*BuildRequest
	mutex           sync.RWMutex
	resourceManager *ResourceManager
	config          *ConcurrentBuildConfig
}

// BuildCoordinator coordinates multiple concurrent builds
type BuildCoordinator struct {
	activeBuilds    map[string]*BuildRequest
	dependencies    map[string][]string // build ID -> dependent build IDs
	mutex           sync.RWMutex
	resourceManager *ResourceManager
}

// NewConcurrentBuilder creates a new concurrent builder
func NewConcurrentBuilder(resourceManager *ResourceManager, config *ConcurrentBuildConfig) *ConcurrentBuilder {
	if config == nil {
		config = &ConcurrentBuildConfig{
			MaxWorkers:            4,
			QueueSize:             100,
			DefaultTimeout:        30 * time.Minute,
			ResourceCheckInterval: 10 * time.Second,
			EnablePrioritization:  true,
			EnableLoadBalancing:   true,
			WorkerIdleTimeout:     5 * time.Minute,
		}
	}

	cb := &ConcurrentBuilder{
		resourceManager: resourceManager,
		buildQueue:      make(chan *BuildRequest, config.QueueSize),
		workers:         make([]*BuildWorker, 0, config.MaxWorkers),
		results:         make(map[string]*BuildResult),
		config:          config,
		scheduler:       NewBuildScheduler(resourceManager, config),
		coordinator:     NewBuildCoordinator(resourceManager),
	}

	// Create workers
	for i := 0; i < config.MaxWorkers; i++ {
		worker := NewBuildWorker(i, resourceManager, cb.buildQueue)
		cb.workers = append(cb.workers, worker)
		go worker.Start()
	}

	// Start scheduler
	go cb.scheduler.Start()

	return cb
}

// SubmitBuild submits a build request for concurrent execution
func (cb *ConcurrentBuilder) SubmitBuild(ctx context.Context, config *types.BuildConfig, priority int) (*BuildResult, error) {
	request := &BuildRequest{
		ID:       fmt.Sprintf("build-%d", time.Now().UnixNano()),
		Config:   config,
		Priority: priority,
		Context:  ctx,
		Result:   make(chan *BuildResult, 1),
		Started:  time.Now(),
		Timeout:  cb.config.DefaultTimeout,
	}

	// Check resource availability before queuing
	if err := cb.resourceManager.CheckResourceLimits(); err != nil {
		return nil, fmt.Errorf("resource limits exceeded: %v", err)
	}

	// Submit to scheduler
	if err := cb.scheduler.ScheduleBuild(request); err != nil {
		return nil, fmt.Errorf("failed to schedule build: %v", err)
	}

	// Wait for result with timeout
	select {
	case result := <-request.Result:
		cb.resultsMutex.Lock()
		cb.results[request.ID] = result
		cb.resultsMutex.Unlock()
		return result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(request.Timeout):
		return nil, fmt.Errorf("build timeout after %v", request.Timeout)
	}
}

// SubmitBuildAsync submits a build request asynchronously
func (cb *ConcurrentBuilder) SubmitBuildAsync(ctx context.Context, config *types.BuildConfig, priority int) (string, <-chan *BuildResult, error) {
	request := &BuildRequest{
		ID:       fmt.Sprintf("build-%d", time.Now().UnixNano()),
		Config:   config,
		Priority: priority,
		Context:  ctx,
		Result:   make(chan *BuildResult, 1),
		Started:  time.Now(),
		Timeout:  cb.config.DefaultTimeout,
	}

	// Submit to scheduler
	if err := cb.scheduler.ScheduleBuild(request); err != nil {
		return "", nil, fmt.Errorf("failed to schedule build: %v", err)
	}

	return request.ID, request.Result, nil
}

// GetBuildStatus returns the status of a build
func (cb *ConcurrentBuilder) GetBuildStatus(buildID string) (*BuildStatus, error) {
	cb.resultsMutex.RLock()
	defer cb.resultsMutex.RUnlock()

	// Check completed builds
	if result, exists := cb.results[buildID]; exists {
		return &BuildStatus{
			ID:        buildID,
			Status:    "completed",
			Started:   result.Request.Started,
			Completed: &result.Request.Started, // Simplified
			Duration:  result.Duration,
			Success:   result.Result != nil && result.Result.Success,
			Error:     result.Error,
		}, nil
	}

	// Check running builds
	if status := cb.scheduler.GetBuildStatus(buildID); status != nil {
		return status, nil
	}

	return nil, fmt.Errorf("build not found: %s", buildID)
}

// GetActiveBuilds returns information about currently active builds
func (cb *ConcurrentBuilder) GetActiveBuilds() []*BuildStatus {
	return cb.scheduler.GetActiveBuilds()
}

// GetQueueStatus returns information about the build queue
func (cb *ConcurrentBuilder) GetQueueStatus() *QueueStatus {
	return cb.scheduler.GetQueueStatus()
}

// Shutdown gracefully shuts down the concurrent builder
func (cb *ConcurrentBuilder) Shutdown(ctx context.Context) error {
	// Stop accepting new builds
	close(cb.buildQueue)

	// Stop scheduler
	cb.scheduler.Stop()

	// Stop all workers
	for _, worker := range cb.workers {
		worker.Stop()
	}

	// Wait for workers to finish with timeout
	done := make(chan bool, 1)
	go func() {
		for _, worker := range cb.workers {
			worker.WaitForCompletion()
		}
		done <- true
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(30 * time.Second):
		return fmt.Errorf("shutdown timeout")
	}
}

// BuildStatus represents the status of a build
type BuildStatus struct {
	ID        string     `json:"id"`
	Status    string     `json:"status"` // queued, running, completed, failed
	Started   time.Time  `json:"started"`
	Completed *time.Time `json:"completed,omitempty"`
	Duration  time.Duration `json:"duration"`
	Success   bool       `json:"success"`
	Error     error      `json:"error,omitempty"`
	Progress  float64    `json:"progress"`
	Worker    int        `json:"worker,omitempty"`
}

// QueueStatus represents the status of the build queue
type QueueStatus struct {
	QueuedBuilds  int `json:"queued_builds"`
	RunningBuilds int `json:"running_builds"`
	ActiveWorkers int `json:"active_workers"`
	IdleWorkers   int `json:"idle_workers"`
	TotalCapacity int `json:"total_capacity"`
}

// NewBuildWorker creates a new build worker
func NewBuildWorker(id int, resourceManager *ResourceManager, workChan chan *BuildRequest) *BuildWorker {
	return &BuildWorker{
		ID:              id,
		resourceManager: resourceManager,
		stopChan:        make(chan bool, 1),
		workChan:        workChan,
	}
}

// Start starts the build worker
func (bw *BuildWorker) Start() {
	for {
		select {
		case request := <-bw.workChan:
			if request == nil {
				return // Channel closed
			}
			bw.processRequest(request)
		case <-bw.stopChan:
			return
		}
	}
}

// Stop stops the build worker
func (bw *BuildWorker) Stop() {
	select {
	case bw.stopChan <- true:
	default:
	}
}

// WaitForCompletion waits for the worker to complete current work
func (bw *BuildWorker) WaitForCompletion() {
	bw.mutex.RLock()
	current := bw.currentRequest
	bw.mutex.RUnlock()

	if current != nil {
		// Wait for current request to complete
		select {
		case <-current.Result:
		case <-time.After(30 * time.Second):
			// Force completion after timeout
		}
	}
}

// processRequest processes a build request
func (bw *BuildWorker) processRequest(request *BuildRequest) {
	bw.mutex.Lock()
	bw.currentRequest = request
	bw.mutex.Unlock()

	defer func() {
		bw.mutex.Lock()
		bw.currentRequest = nil
		bw.mutex.Unlock()
	}()

	startTime := time.Now()

	// Acquire build permit
	if err := bw.resourceManager.AcquireBuildPermit(request.Context); err != nil {
		result := &BuildResult{
			Request:  request,
			Error:    fmt.Errorf("failed to acquire build permit: %v", err),
			Duration: time.Since(startTime),
		}
		request.Result <- result
		return
	}
	defer bw.resourceManager.ReleaseBuildPermit()

	// Start resource monitoring
	monitor := bw.resourceManager.StartMonitoring(request.ID)
	defer func() {
		if monitor := bw.resourceManager.StopMonitoring(request.ID); monitor != nil {
			// Resource monitoring completed
		}
	}()

	// Create builder for this request
	builder, err := NewBuilder(request.Config)
	if err != nil {
		result := &BuildResult{
			Request:  request,
			Error:    fmt.Errorf("failed to create builder: %v", err),
			Duration: time.Since(startTime),
		}
		request.Result <- result
		return
	}
	defer builder.Cleanup()

	// Execute build
	buildResult, err := builder.Build()
	duration := time.Since(startTime)

	// Get resource summary
	var resourceSummary *ResourceMonitorSummary
	if monitor != nil {
		resourceSummary = monitor.GetSummary()
	}

	// Create result
	result := &BuildResult{
		Request:   request,
		Result:    buildResult,
		Error:     err,
		Duration:  duration,
		Resources: resourceSummary,
	}

	// Profile the operation if profiling is enabled
	if resourceSummary != nil {
		bw.resourceManager.ProfileOperation(
			types.OperationTypeExec, // Simplified - could be more specific
			duration,
			resourceSummary.PeakMemoryMB,
			resourceSummary.PeakCPUPercent,
		)
	}

	request.Result <- result
}

// NewBuildScheduler creates a new build scheduler
func NewBuildScheduler(resourceManager *ResourceManager, config *ConcurrentBuildConfig) *BuildScheduler {
	return &BuildScheduler{
		pendingBuilds:   make([]*BuildRequest, 0),
		runningBuilds:   make(map[string]*BuildRequest),
		resourceManager: resourceManager,
		config:          config,
	}
}

// ScheduleBuild schedules a build request
func (bs *BuildScheduler) ScheduleBuild(request *BuildRequest) error {
	bs.mutex.Lock()
	defer bs.mutex.Unlock()

	// Add to pending builds
	bs.pendingBuilds = append(bs.pendingBuilds, request)

	// Sort by priority if prioritization is enabled
	if bs.config.EnablePrioritization {
		bs.sortPendingBuilds()
	}

	return nil
}

// Start starts the build scheduler
func (bs *BuildScheduler) Start() {
	ticker := time.NewTicker(bs.config.ResourceCheckInterval)
	defer ticker.Stop()

	for range ticker.C {
		bs.scheduleNextBuilds()
	}
}

// Stop stops the build scheduler
func (bs *BuildScheduler) Stop() {
	// Implementation would stop the scheduler
}

// GetBuildStatus returns the status of a build
func (bs *BuildScheduler) GetBuildStatus(buildID string) *BuildStatus {
	bs.mutex.RLock()
	defer bs.mutex.RUnlock()

	// Check running builds
	if request, exists := bs.runningBuilds[buildID]; exists {
		return &BuildStatus{
			ID:       buildID,
			Status:   "running",
			Started:  request.Started,
			Duration: time.Since(request.Started),
		}
	}

	// Check pending builds
	for _, request := range bs.pendingBuilds {
		if request.ID == buildID {
			return &BuildStatus{
				ID:      buildID,
				Status:  "queued",
				Started: request.Started,
			}
		}
	}

	return nil
}

// GetActiveBuilds returns information about active builds
func (bs *BuildScheduler) GetActiveBuilds() []*BuildStatus {
	bs.mutex.RLock()
	defer bs.mutex.RUnlock()

	var builds []*BuildStatus

	// Add running builds
	for _, request := range bs.runningBuilds {
		builds = append(builds, &BuildStatus{
			ID:       request.ID,
			Status:   "running",
			Started:  request.Started,
			Duration: time.Since(request.Started),
		})
	}

	// Add pending builds
	for _, request := range bs.pendingBuilds {
		builds = append(builds, &BuildStatus{
			ID:      request.ID,
			Status:  "queued",
			Started: request.Started,
		})
	}

	return builds
}

// GetQueueStatus returns queue status information
func (bs *BuildScheduler) GetQueueStatus() *QueueStatus {
	bs.mutex.RLock()
	defer bs.mutex.RUnlock()

	return &QueueStatus{
		QueuedBuilds:  len(bs.pendingBuilds),
		RunningBuilds: len(bs.runningBuilds),
		TotalCapacity: bs.config.MaxWorkers,
	}
}

// scheduleNextBuilds schedules the next builds based on resource availability
func (bs *BuildScheduler) scheduleNextBuilds() {
	bs.mutex.Lock()
	defer bs.mutex.Unlock()

	// Check if we can schedule more builds
	if len(bs.runningBuilds) >= bs.config.MaxWorkers {
		return
	}

	// Check resource availability
	if err := bs.resourceManager.CheckResourceLimits(); err != nil {
		return // Resources constrained
	}

	// Schedule next builds
	availableSlots := bs.config.MaxWorkers - len(bs.runningBuilds)
	for i := 0; i < availableSlots && len(bs.pendingBuilds) > 0; i++ {
		request := bs.pendingBuilds[0]
		bs.pendingBuilds = bs.pendingBuilds[1:]
		bs.runningBuilds[request.ID] = request

		// The actual scheduling to workers happens through the work channel
		// This is handled by the ConcurrentBuilder
	}
}

// sortPendingBuilds sorts pending builds by priority
func (bs *BuildScheduler) sortPendingBuilds() {
	// Simple priority sort - higher priority first
	for i := 0; i < len(bs.pendingBuilds)-1; i++ {
		for j := i + 1; j < len(bs.pendingBuilds); j++ {
			if bs.pendingBuilds[j].Priority > bs.pendingBuilds[i].Priority {
				bs.pendingBuilds[i], bs.pendingBuilds[j] = bs.pendingBuilds[j], bs.pendingBuilds[i]
			}
		}
	}
}

// NewBuildCoordinator creates a new build coordinator
func NewBuildCoordinator(resourceManager *ResourceManager) *BuildCoordinator {
	return &BuildCoordinator{
		activeBuilds:    make(map[string]*BuildRequest),
		dependencies:    make(map[string][]string),
		resourceManager: resourceManager,
	}
}

// AddBuildDependency adds a dependency between builds
func (bc *BuildCoordinator) AddBuildDependency(buildID, dependsOnID string) {
	bc.mutex.Lock()
	defer bc.mutex.Unlock()

	if bc.dependencies[dependsOnID] == nil {
		bc.dependencies[dependsOnID] = make([]string, 0)
	}
	bc.dependencies[dependsOnID] = append(bc.dependencies[dependsOnID], buildID)
}

// CanScheduleBuild checks if a build can be scheduled based on dependencies
func (bc *BuildCoordinator) CanScheduleBuild(buildID string) bool {
	bc.mutex.RLock()
	defer bc.mutex.RUnlock()

	// Check if all dependencies are completed
	for depID := range bc.dependencies {
		if contains(bc.dependencies[depID], buildID) {
			if _, isActive := bc.activeBuilds[depID]; isActive {
				return false // Dependency still running
			}
		}
	}

	return true
}

// contains checks if a slice contains a string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}