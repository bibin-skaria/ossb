package engine

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/bibin-skaria/ossb/internal/types"
)

// ParallelExecutor executes operations in parallel while respecting dependencies
type ParallelExecutor struct {
	resourceManager *ResourceManager
	config          *ParallelExecutionConfig
	dependencyGraph *DependencyGraph
	executionPool   *ExecutionPool
	resultCache     map[string]*types.OperationResult
	cacheMutex      sync.RWMutex
}

// ParallelExecutionConfig configures parallel execution behavior
type ParallelExecutionConfig struct {
	MaxConcurrentOps    int           `json:"max_concurrent_ops"`
	MaxConcurrentStages int           `json:"max_concurrent_stages"`
	OpTimeout           time.Duration `json:"op_timeout"`
	StageTimeout        time.Duration `json:"stage_timeout"`
	EnablePipelining    bool          `json:"enable_pipelining"`
	EnableSpeculation   bool          `json:"enable_speculation"`
	ResourceThreshold   float64       `json:"resource_threshold"`
}

// DependencyGraph represents operation dependencies for parallel execution
type DependencyGraph struct {
	nodes        map[string]*DependencyNode
	levels       [][]string // Operations grouped by dependency level
	mutex        sync.RWMutex
}

// DependencyNode represents a node in the dependency graph
type DependencyNode struct {
	ID           string
	Operation    *types.Operation
	Dependencies []string
	Dependents   []string
	Level        int
	Status       NodeStatus
	Result       *types.OperationResult
	StartTime    time.Time
	EndTime      time.Time
	Error        error
}

// NodeStatus represents the status of a dependency node
type NodeStatus int

const (
	NodeStatusPending NodeStatus = iota
	NodeStatusReady
	NodeStatusRunning
	NodeStatusCompleted
	NodeStatusFailed
)

// ExecutionPool manages a pool of operation executors
type ExecutionPool struct {
	workers     []*OperationWorker
	workQueue   chan *ExecutionTask
	resultQueue chan *ExecutionResult
	config      *ParallelExecutionConfig
	mutex       sync.RWMutex
}

// OperationWorker executes operations in parallel
type OperationWorker struct {
	ID              int
	resourceManager *ResourceManager
	executor        interface{} // The actual executor (local, container, etc.)
	currentTask     *ExecutionTask
	mutex           sync.RWMutex
}

// ExecutionTask represents a task to be executed
type ExecutionTask struct {
	ID        string
	Operation *types.Operation
	Context   context.Context
	WorkDir   string
	Callback  chan *ExecutionResult
}

// ExecutionResult represents the result of an execution task
type ExecutionResult struct {
	Task     *ExecutionTask
	Result   *types.OperationResult
	Error    error
	Duration time.Duration
	Worker   int
}

// ParallelBuildContext holds context for parallel build execution
type ParallelBuildContext struct {
	BuildID         string
	Operations      []*types.Operation
	WorkDir         string
	ResourceManager *ResourceManager
	Cache           interface{} // Cache interface
	Config          *types.BuildConfig
}

// NewParallelExecutor creates a new parallel executor
func NewParallelExecutor(resourceManager *ResourceManager, config *ParallelExecutionConfig) *ParallelExecutor {
	if config == nil {
		config = &ParallelExecutionConfig{
			MaxConcurrentOps:    4,
			MaxConcurrentStages: 2,
			OpTimeout:           10 * time.Minute,
			StageTimeout:        30 * time.Minute,
			EnablePipelining:    true,
			EnableSpeculation:   false,
			ResourceThreshold:   0.8, // 80% resource utilization threshold
		}
	}

	pe := &ParallelExecutor{
		resourceManager: resourceManager,
		config:          config,
		dependencyGraph: NewDependencyGraph(),
		resultCache:     make(map[string]*types.OperationResult),
	}

	pe.executionPool = NewExecutionPool(config, resourceManager)

	return pe
}

// ExecuteParallel executes operations in parallel respecting dependencies
func (pe *ParallelExecutor) ExecuteParallel(ctx context.Context, buildCtx *ParallelBuildContext) ([]*types.OperationResult, error) {
	// Build dependency graph
	if err := pe.buildDependencyGraph(buildCtx.Operations); err != nil {
		return nil, fmt.Errorf("failed to build dependency graph: %v", err)
	}

	// Execute operations level by level
	results := make([]*types.OperationResult, 0, len(buildCtx.Operations))
	
	for level, nodeIDs := range pe.dependencyGraph.levels {
		levelResults, err := pe.executeLevel(ctx, level, nodeIDs, buildCtx)
		if err != nil {
			return nil, fmt.Errorf("failed to execute level %d: %v", level, err)
		}
		results = append(results, levelResults...)
	}

	return results, nil
}

// ExecuteWithPipelining executes operations with pipelining optimization
func (pe *ParallelExecutor) ExecuteWithPipelining(ctx context.Context, buildCtx *ParallelBuildContext) ([]*types.OperationResult, error) {
	if !pe.config.EnablePipelining {
		return pe.ExecuteParallel(ctx, buildCtx)
	}

	// Build dependency graph
	if err := pe.buildDependencyGraph(buildCtx.Operations); err != nil {
		return nil, fmt.Errorf("failed to build dependency graph: %v", err)
	}

	// Start execution pipeline
	resultChan := make(chan *ExecutionResult, len(buildCtx.Operations))
	errorChan := make(chan error, 1)

	go pe.executePipeline(ctx, buildCtx, resultChan, errorChan)

	// Collect results
	results := make([]*types.OperationResult, 0, len(buildCtx.Operations))
	completed := 0

	for completed < len(buildCtx.Operations) {
		select {
		case result := <-resultChan:
			if result.Error != nil {
				return nil, fmt.Errorf("operation %s failed: %v", result.Task.ID, result.Error)
			}
			results = append(results, result.Result)
			completed++
		case err := <-errorChan:
			return nil, err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	return results, nil
}

// buildDependencyGraph builds the dependency graph from operations
func (pe *ParallelExecutor) buildDependencyGraph(operations []*types.Operation) error {
	pe.dependencyGraph = NewDependencyGraph()

	// Add all operations as nodes
	for i, op := range operations {
		nodeID := fmt.Sprintf("op-%d", i)
		pe.dependencyGraph.AddNode(nodeID, op)
	}

	// Analyze dependencies based on operation types and inputs/outputs
	for i, op := range operations {
		nodeID := fmt.Sprintf("op-%d", i)
		
		// Find dependencies based on inputs/outputs
		for j := 0; j < i; j++ {
			prevOp := operations[j]
			prevNodeID := fmt.Sprintf("op-%d", j)
			
			if pe.hasDependency(op, prevOp) {
				pe.dependencyGraph.AddDependency(nodeID, prevNodeID)
			}
		}
	}

	// Calculate dependency levels
	return pe.dependencyGraph.CalculateLevels()
}

// hasDependency checks if one operation depends on another
func (pe *ParallelExecutor) hasDependency(op, prevOp *types.Operation) bool {
	// Sequential dependency for same-type operations
	if op.Type == prevOp.Type && op.Type == types.OperationTypeExec {
		return true
	}

	// Output-input dependency
	for _, input := range op.Inputs {
		for _, output := range prevOp.Outputs {
			if input == output {
				return true
			}
		}
	}

	// Filesystem dependency (simplified)
	if op.WorkDir != "" && prevOp.WorkDir != "" && op.WorkDir == prevOp.WorkDir {
		return true
	}

	// Multi-stage build dependencies
	if op.Type == types.OperationTypeFile && prevOp.Type == types.OperationTypeExec {
		// COPY operations depend on RUN operations in the same stage
		return true
	}

	return false
}

// executeLevel executes all operations in a dependency level
func (pe *ParallelExecutor) executeLevel(ctx context.Context, level int, nodeIDs []string, buildCtx *ParallelBuildContext) ([]*types.OperationResult, error) {
	if len(nodeIDs) == 0 {
		return nil, nil
	}

	// Check resource availability
	if err := pe.checkResourceAvailability(); err != nil {
		return nil, fmt.Errorf("insufficient resources for level %d: %v", level, err)
	}

	// Determine concurrency for this level
	maxConcurrency := pe.calculateLevelConcurrency(level, len(nodeIDs))

	// Create semaphore for level concurrency
	semaphore := make(chan struct{}, maxConcurrency)
	
	// Execute operations concurrently
	resultChan := make(chan *ExecutionResult, len(nodeIDs))
	var wg sync.WaitGroup

	for _, nodeID := range nodeIDs {
		wg.Add(1)
		go func(nID string) {
			defer wg.Done()
			
			// Acquire semaphore
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			
			result := pe.executeNode(ctx, nID, buildCtx)
			resultChan <- result
		}(nodeID)
	}

	// Wait for all operations to complete
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Collect results
	results := make([]*types.OperationResult, 0, len(nodeIDs))
	for result := range resultChan {
		if result.Error != nil {
			return nil, fmt.Errorf("operation %s failed: %v", result.Task.ID, result.Error)
		}
		results = append(results, result.Result)
	}

	return results, nil
}

// executeNode executes a single node in the dependency graph
func (pe *ParallelExecutor) executeNode(ctx context.Context, nodeID string, buildCtx *ParallelBuildContext) *ExecutionResult {
	node := pe.dependencyGraph.GetNode(nodeID)
	if node == nil {
		return &ExecutionResult{
			Error: fmt.Errorf("node not found: %s", nodeID),
		}
	}

	// Check cache first
	pe.cacheMutex.RLock()
	if cachedResult, exists := pe.resultCache[nodeID]; exists {
		pe.cacheMutex.RUnlock()
		return &ExecutionResult{
			Result: cachedResult,
		}
	}
	pe.cacheMutex.RUnlock()

	// Acquire operation permit
	if err := pe.resourceManager.AcquireOperationPermit(ctx); err != nil {
		return &ExecutionResult{
			Error: fmt.Errorf("failed to acquire operation permit: %v", err),
		}
	}
	defer pe.resourceManager.ReleaseOperationPermit()

	// Start resource monitoring
	monitor := pe.resourceManager.StartMonitoring(nodeID)
	defer pe.resourceManager.StopMonitoring(nodeID)

	// Create execution task
	task := &ExecutionTask{
		ID:        nodeID,
		Operation: node.Operation,
		Context:   ctx,
		WorkDir:   buildCtx.WorkDir,
		Callback:  make(chan *ExecutionResult, 1),
	}

	// Execute through pool
	startTime := time.Now()
	pe.executionPool.Submit(task)

	// Wait for result
	select {
	case result := <-task.Callback:
		result.Duration = time.Since(startTime)
		
		// Cache successful results
		if result.Error == nil && result.Result != nil {
			pe.cacheMutex.Lock()
			pe.resultCache[nodeID] = result.Result
			pe.cacheMutex.Unlock()
		}

		// Profile the operation
		if monitor != nil {
			summary := monitor.GetSummary()
			pe.resourceManager.ProfileOperation(
				node.Operation.Type,
				result.Duration,
				summary.PeakMemoryMB,
				summary.PeakCPUPercent,
			)
		}

		return result
	case <-ctx.Done():
		return &ExecutionResult{
			Error: ctx.Err(),
		}
	case <-time.After(pe.config.OpTimeout):
		return &ExecutionResult{
			Error: fmt.Errorf("operation timeout after %v", pe.config.OpTimeout),
		}
	}
}

// executePipeline executes operations in a pipelined manner
func (pe *ParallelExecutor) executePipeline(ctx context.Context, buildCtx *ParallelBuildContext, resultChan chan<- *ExecutionResult, errorChan chan<- error) {
	// Implementation of pipelined execution
	// This would overlap execution of independent operations across levels
	
	readyQueue := make(chan string, len(buildCtx.Operations))
	
	// Start with level 0 operations
	for _, nodeID := range pe.dependencyGraph.levels[0] {
		readyQueue <- nodeID
	}

	completed := make(map[string]bool)
	var wg sync.WaitGroup

	// Pipeline execution loop
	for len(completed) < len(buildCtx.Operations) {
		select {
		case nodeID := <-readyQueue:
			wg.Add(1)
			go func(nID string) {
				defer wg.Done()
				
				result := pe.executeNode(ctx, nID, buildCtx)
				if result.Error != nil {
					errorChan <- result.Error
					return
				}
				
				resultChan <- result
				completed[nID] = true
				
				// Check if any dependent operations are now ready
				pe.checkAndQueueDependents(nID, completed, readyQueue)
			}(nodeID)
		case <-ctx.Done():
			errorChan <- ctx.Err()
			return
		}
	}

	wg.Wait()
}

// checkAndQueueDependents checks if dependent operations are ready to execute
func (pe *ParallelExecutor) checkAndQueueDependents(completedNodeID string, completed map[string]bool, readyQueue chan<- string) {
	node := pe.dependencyGraph.GetNode(completedNodeID)
	if node == nil {
		return
	}

	for _, dependentID := range node.Dependents {
		dependentNode := pe.dependencyGraph.GetNode(dependentID)
		if dependentNode == nil {
			continue
		}

		// Check if all dependencies are completed
		allDepsCompleted := true
		for _, depID := range dependentNode.Dependencies {
			if !completed[depID] {
				allDepsCompleted = false
				break
			}
		}

		if allDepsCompleted {
			select {
			case readyQueue <- dependentID:
			default:
				// Queue is full, skip for now
			}
		}
	}
}

// checkResourceAvailability checks if resources are available for parallel execution
func (pe *ParallelExecutor) checkResourceAvailability() error {
	usage := pe.resourceManager.GetResourceUsage()
	
	// Check memory threshold
	if usage.MemoryMB > int64(float64(pe.resourceManager.config.MemoryLimitMB) * pe.config.ResourceThreshold) {
		return fmt.Errorf("memory usage too high: %d MB", usage.MemoryMB)
	}

	// Check CPU threshold
	if usage.CPUPercent > pe.config.ResourceThreshold * pe.resourceManager.config.CPULimitPercent {
		return fmt.Errorf("CPU usage too high: %.1f%%", usage.CPUPercent)
	}

	return nil
}

// calculateLevelConcurrency calculates optimal concurrency for a dependency level
func (pe *ParallelExecutor) calculateLevelConcurrency(level int, nodeCount int) int {
	// Base concurrency on available resources and node count
	maxConcurrency := pe.config.MaxConcurrentOps
	
	// Reduce concurrency for higher levels to prevent resource exhaustion
	if level > 0 {
		maxConcurrency = maxConcurrency / (level + 1)
	}
	
	// Don't exceed the number of nodes
	if maxConcurrency > nodeCount {
		maxConcurrency = nodeCount
	}
	
	// Ensure at least 1
	if maxConcurrency < 1 {
		maxConcurrency = 1
	}
	
	return maxConcurrency
}

// NewDependencyGraph creates a new dependency graph
func NewDependencyGraph() *DependencyGraph {
	return &DependencyGraph{
		nodes:  make(map[string]*DependencyNode),
		levels: make([][]string, 0),
	}
}

// AddNode adds a node to the dependency graph
func (dg *DependencyGraph) AddNode(id string, operation *types.Operation) {
	dg.mutex.Lock()
	defer dg.mutex.Unlock()

	dg.nodes[id] = &DependencyNode{
		ID:           id,
		Operation:    operation,
		Dependencies: make([]string, 0),
		Dependents:   make([]string, 0),
		Status:       NodeStatusPending,
	}
}

// AddDependency adds a dependency between nodes
func (dg *DependencyGraph) AddDependency(nodeID, dependsOnID string) {
	dg.mutex.Lock()
	defer dg.mutex.Unlock()

	if node, exists := dg.nodes[nodeID]; exists {
		node.Dependencies = append(node.Dependencies, dependsOnID)
	}

	if dependsOn, exists := dg.nodes[dependsOnID]; exists {
		dependsOn.Dependents = append(dependsOn.Dependents, nodeID)
	}
}

// GetNode returns a node by ID
func (dg *DependencyGraph) GetNode(id string) *DependencyNode {
	dg.mutex.RLock()
	defer dg.mutex.RUnlock()

	return dg.nodes[id]
}

// CalculateLevels calculates dependency levels for parallel execution
func (dg *DependencyGraph) CalculateLevels() error {
	dg.mutex.Lock()
	defer dg.mutex.Unlock()

	// Reset levels
	dg.levels = make([][]string, 0)
	
	// Calculate level for each node
	for id := range dg.nodes {
		level := dg.calculateNodeLevel(id, make(map[string]bool), make(map[string]bool))
		dg.nodes[id].Level = level
		
		// Ensure levels slice is large enough
		for len(dg.levels) <= level {
			dg.levels = append(dg.levels, make([]string, 0))
		}
		
		dg.levels[level] = append(dg.levels[level], id)
	}

	return nil
}

// calculateNodeLevel calculates the dependency level for a node
func (dg *DependencyGraph) calculateNodeLevel(nodeID string, visited, recursionStack map[string]bool) int {
	if recursionStack[nodeID] {
		// Cycle detected - return high level to break cycle
		return 1000
	}

	if visited[nodeID] {
		return dg.nodes[nodeID].Level
	}

	visited[nodeID] = true
	recursionStack[nodeID] = true

	node := dg.nodes[nodeID]
	maxDepLevel := -1

	// Calculate level based on dependencies
	for _, depID := range node.Dependencies {
		depLevel := dg.calculateNodeLevel(depID, visited, recursionStack)
		if depLevel > maxDepLevel {
			maxDepLevel = depLevel
		}
	}

	recursionStack[nodeID] = false
	level := maxDepLevel + 1
	node.Level = level

	return level
}

// NewExecutionPool creates a new execution pool
func NewExecutionPool(config *ParallelExecutionConfig, resourceManager *ResourceManager) *ExecutionPool {
	pool := &ExecutionPool{
		workers:     make([]*OperationWorker, 0, config.MaxConcurrentOps),
		workQueue:   make(chan *ExecutionTask, config.MaxConcurrentOps*2),
		resultQueue: make(chan *ExecutionResult, config.MaxConcurrentOps*2),
		config:      config,
	}

	// Create workers
	for i := 0; i < config.MaxConcurrentOps; i++ {
		worker := &OperationWorker{
			ID:              i,
			resourceManager: resourceManager,
		}
		pool.workers = append(pool.workers, worker)
		go worker.Start(pool.workQueue)
	}

	return pool
}

// Submit submits a task to the execution pool
func (ep *ExecutionPool) Submit(task *ExecutionTask) {
	ep.workQueue <- task
}

// Start starts an operation worker
func (ow *OperationWorker) Start(workQueue <-chan *ExecutionTask) {
	for task := range workQueue {
		ow.executeTask(task)
	}
}

// executeTask executes a single task
func (ow *OperationWorker) executeTask(task *ExecutionTask) {
	ow.mutex.Lock()
	ow.currentTask = task
	ow.mutex.Unlock()

	defer func() {
		ow.mutex.Lock()
		ow.currentTask = nil
		ow.mutex.Unlock()
	}()

	startTime := time.Now()

	// This would use the actual executor (local, container, etc.)
	// For now, we'll simulate execution
	result := &types.OperationResult{
		Operation: task.Operation,
		Success:   true,
		Outputs:   task.Operation.Outputs,
		CacheHit:  false,
	}

	executionResult := &ExecutionResult{
		Task:     task,
		Result:   result,
		Duration: time.Since(startTime),
		Worker:   ow.ID,
	}

	task.Callback <- executionResult
}