package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/bibin-skaria/ossb/executors"
	"github.com/bibin-skaria/ossb/exporters"
	"github.com/bibin-skaria/ossb/frontends"
	"github.com/bibin-skaria/ossb/internal/types"
)

type Builder struct {
	config           *types.BuildConfig
	cache            *Cache
	solver           *GraphSolver
	executor         executors.Executor
	exporter         exporters.Exporter
	frontend         frontends.Frontend
	workDir          string
	progressOut      io.Writer
	observability    *ObservabilityManager
	resourceManager  *ResourceManager
	parallelExecutor *ParallelExecutor
	concurrentBuilder *ConcurrentBuilder
}

func NewBuilder(config *types.BuildConfig) (*Builder, error) {
	if config.CacheDir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %v", err)
		}
		config.CacheDir = filepath.Join(homeDir, ".ossb", "cache")
	}

	if err := os.MkdirAll(config.CacheDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %v", err)
	}

	workDir := filepath.Join(config.CacheDir, "work", fmt.Sprintf("build-%d", time.Now().Unix()))
	if err := os.MkdirAll(workDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create work directory: %v", err)
	}

	var cache *Cache
	if config.Rootless {
		cache = NewRootlessCache(config.CacheDir)
	} else {
		cache = NewCache(config.CacheDir)
	}
	solver := NewGraphSolver()

	frontend, err := frontends.GetFrontend(config.Frontend)
	if err != nil {
		return nil, fmt.Errorf("failed to get frontend: %v", err)
	}

	executorType := "local"
	if config.Rootless {
		executorType = "rootless"
	} else if len(config.Platforms) > 1 || (len(config.Platforms) == 1 && config.Platforms[0].String() != types.GetHostPlatform().String()) {
		executorType = "container"
	}

	executor, err := executors.GetExecutor(executorType)
	if err != nil {
		return nil, fmt.Errorf("failed to get executor %s: %v", executorType, err)
	}

	exporter, err := exporters.GetExporter(config.Output)
	if err != nil {
		return nil, fmt.Errorf("failed to get exporter: %v", err)
	}

	// Create observability manager
	buildID := fmt.Sprintf("build-%d", time.Now().Unix())
	observability := NewObservabilityManager(buildID, config, os.Stdout, config.Progress)

	// Create resource manager
	resourceManager := NewResourceManager(config.ResourceLimits)

	// Create parallel executor
	parallelConfig := &ParallelExecutionConfig{
		MaxConcurrentOps:    4,
		MaxConcurrentStages: 2,
		OpTimeout:           10 * time.Minute,
		StageTimeout:        30 * time.Minute,
		EnablePipelining:    true,
		EnableSpeculation:   false,
		ResourceThreshold:   0.8,
	}
	parallelExecutor := NewParallelExecutor(resourceManager, parallelConfig)

	// Create concurrent builder for multi-platform builds
	concurrentConfig := &ConcurrentBuildConfig{
		MaxWorkers:            len(config.Platforms),
		QueueSize:             100,
		DefaultTimeout:        30 * time.Minute,
		ResourceCheckInterval: 10 * time.Second,
		EnablePrioritization:  true,
		EnableLoadBalancing:   true,
		WorkerIdleTimeout:     5 * time.Minute,
	}
	concurrentBuilder := NewConcurrentBuilder(resourceManager, concurrentConfig)

	return &Builder{
		config:            config,
		cache:             cache,
		solver:            solver,
		executor:          executor,
		exporter:          exporter,
		frontend:          frontend,
		workDir:           workDir,
		progressOut:       os.Stdout,
		observability:     observability,
		resourceManager:   resourceManager,
		parallelExecutor:  parallelExecutor,
		concurrentBuilder: concurrentBuilder,
	}, nil
}

func (b *Builder) SetProgressOutput(w io.Writer) {
	b.progressOut = w
}

func (b *Builder) Build() (*types.BuildResult, error) {
	start := time.Now()
	ctx := context.Background()
	
	// Start observability
	ctx = b.observability.StartBuild(ctx)
	
	result := &types.BuildResult{
		Success:         false,
		Metadata:        make(map[string]string),
		PlatformResults: make(map[string]*types.PlatformResult),
	}

	if len(b.config.Platforms) == 0 {
		b.config.Platforms = []types.Platform{types.GetHostPlatform()}
	}

	result.MultiArch = len(b.config.Platforms) > 1

	if b.config.Progress && b.progressOut != nil {
		if result.MultiArch {
			fmt.Fprintf(b.progressOut, "Starting multi-arch build for %d platforms...\n", len(b.config.Platforms))
		} else {
			fmt.Fprintf(b.progressOut, "Starting build for %s...\n", b.config.Platforms[0].String())
		}
	}

	dockerfilePath := filepath.Join(b.config.Context, b.config.Dockerfile)
	dockerfileContent, err := os.ReadFile(dockerfilePath)
	if err != nil {
		result.Error = fmt.Sprintf("failed to read Dockerfile: %v", err)
		b.observability.RecordError(ctx, err, "read_dockerfile", "init", "")
		return result, nil
	}

	// Start parsing stage
	b.observability.StartStage(ctx, "parse", "", 1)
	
	if b.config.Progress && b.progressOut != nil {
		fmt.Fprintf(b.progressOut, "Parsing Dockerfile...\n")
	}

	totalCacheHits := 0
	allSuccess := true
	
	// Complete parsing stage
	operations, err := b.frontend.Parse(string(dockerfileContent), b.config)
	if err != nil {
		b.observability.CompleteStage(ctx, "parse", "", false, err.Error())
		result.Error = fmt.Sprintf("failed to parse Dockerfile: %v", err)
		return result, nil
	}
	b.observability.CompleteStage(ctx, "parse", "", true, "")

	// Use concurrent execution for multi-platform builds
	if len(b.config.Platforms) > 1 {
		// Execute platforms concurrently
		var wg sync.WaitGroup
		platformResults := make(chan *types.PlatformResult, len(b.config.Platforms))
		
		for _, platform := range b.config.Platforms {
			wg.Add(1)
			go func(p types.Platform) {
				defer wg.Done()
				
				platformResult := b.buildPlatform(ctx, p, operations)
				platformResults <- platformResult
				
				if platformResult.Success {
					totalCacheHits += platformResult.CacheHits
				} else {
					allSuccess = false
				}
			}(platform)
		}
		
		// Wait for all platforms to complete
		go func() {
			wg.Wait()
			close(platformResults)
		}()
		
		// Collect results
		for platformResult := range platformResults {
			result.PlatformResults[platformResult.Platform.String()] = platformResult
		}
	} else {
		// Single platform build - use parallel execution within platform
		platform := b.config.Platforms[0]
		platformResult := b.buildPlatformParallel(ctx, platform, operations)
		result.PlatformResults[platform.String()] = platformResult
		
		if platformResult.Success {
			totalCacheHits += platformResult.CacheHits
		} else {
			allSuccess = false
		}
	}

	result.Operations = len(operations) * len(b.config.Platforms) // Total operations across all platforms
	result.CacheHits = totalCacheHits
	result.Success = allSuccess

	if !allSuccess {
		var failedPlatforms []string
		for platformStr, platformResult := range result.PlatformResults {
			if !platformResult.Success {
				failedPlatforms = append(failedPlatforms, platformStr)
			}
		}
		result.Error = fmt.Sprintf("build failed for platforms: %s", strings.Join(failedPlatforms, ", "))
	}

	if result.Success {
		// Start export stage
		b.observability.StartStage(ctx, "export", "", 1)
		
		if b.config.Progress && b.progressOut != nil {
			fmt.Fprintf(b.progressOut, "Exporting result...\n")
		}

		if err := b.exporter.Export(result, b.config, b.workDir); err != nil {
			result.Error = fmt.Sprintf("failed to export result: %v", err)
			result.Success = false
			b.observability.RecordError(ctx, err, "export", "export", "")
			b.observability.CompleteStage(ctx, "export", "", false, err.Error())
			return result, nil
		}
		
		b.observability.CompleteStage(ctx, "export", "", true, "")
	}

	result.Duration = time.Since(start).String()

	// Update cache metrics in observability
	if cacheMetrics, err := b.GetCacheMetrics(); err == nil {
		b.observability.metricsCollector.RecordCacheMetrics(cacheMetrics)
	}

	// Generate comprehensive build report
	buildReport := b.observability.FinishBuild(ctx, result)
	
	// Add build report to result metadata
	if reportJSON, err := json.Marshal(buildReport); err == nil {
		result.Metadata["build_report"] = string(reportJSON)
	}

	if b.config.Progress && b.progressOut != nil {
		if result.Success {
			fmt.Fprintf(b.progressOut, "Build completed successfully in %s\n", result.Duration)
			if result.MultiArch {
				successfulBuilds := 0
				for _, platformResult := range result.PlatformResults {
					if platformResult.Success {
						successfulBuilds++
					}
				}
				fmt.Fprintf(b.progressOut, "Successfully built %d/%d platforms\n", successfulBuilds, len(b.config.Platforms))
			}
		} else {
			fmt.Fprintf(b.progressOut, "Build failed: %s\n", result.Error)
		}
		fmt.Fprintf(b.progressOut, "Cache hits: %d operations\n", totalCacheHits)
		
		// Display build summary
		if buildReport.Summary != nil {
			fmt.Fprintf(b.progressOut, "\nBuild Summary:\n")
			fmt.Fprintf(b.progressOut, "  Stages: %d completed, %d failed\n", buildReport.Summary.CompletedStages, buildReport.Summary.FailedStages)
			fmt.Fprintf(b.progressOut, "  Cache hit rate: %.1f%%\n", buildReport.Summary.CacheHitRate)
			fmt.Fprintf(b.progressOut, "  Peak memory: %d MB\n", buildReport.Summary.PeakMemoryMB)
			
			if len(buildReport.Recommendations) > 0 {
				fmt.Fprintf(b.progressOut, "\nRecommendations:\n")
				for _, rec := range buildReport.Recommendations {
					fmt.Fprintf(b.progressOut, "  • %s\n", rec)
				}
			}
		}
	}

	return result, nil
}

func (b *Builder) executeOperation(operation *types.Operation) (*types.OperationResult, error) {
	if !b.config.NoCache {
		cacheKey := operation.CacheKey()
		
		// Compute context hashes for intelligent caching
		dockerfileHash, _ := b.cache.ComputeDockerfileHash(filepath.Join(b.config.Context, b.config.Dockerfile))
		buildContextHash, _ := b.cache.ComputeBuildContextHash(b.config.Context, []string{".git", "node_modules", ".ossb"})
		
		if cachedResult, hit := b.cache.GetWithContext(cacheKey, operation.Platform, dockerfileHash, buildContextHash); hit {
			return cachedResult, nil
		}
	}

	result, err := b.executor.Execute(operation, b.workDir)
	if err != nil {
		return nil, err
	}

	if !b.config.NoCache && result.Success {
		// Compute context hashes for storage
		dockerfileHash, _ := b.cache.ComputeDockerfileHash(filepath.Join(b.config.Context, b.config.Dockerfile))
		buildContextHash, _ := b.cache.ComputeBuildContextHash(b.config.Context, []string{".git", "node_modules", ".ossb"})
		
		// Determine dependencies (simplified for now)
		var dependencies []string
		if operation.Type == types.OperationTypeExec && len(operation.Inputs) > 0 {
			dependencies = operation.Inputs
		}
		
		if err := b.cache.SetWithContext(operation.CacheKey(), result, operation.Platform, dockerfileHash, buildContextHash, dependencies); err != nil {
			if b.config.Progress && b.progressOut != nil {
				fmt.Fprintf(b.progressOut, "Warning: failed to cache result: %v\n", err)
			}
		}
	}

	return result, nil
}

func (b *Builder) updateResultMetadata(result *types.BuildResult, operation *types.Operation, opResult *types.OperationResult) {
	if operation.Type == types.OperationTypeMeta && operation.Metadata != nil {
		for key, value := range operation.Metadata {
			result.Metadata[key] = value
		}
	}

	if opResult.Environment != nil {
		for key, value := range opResult.Environment {
			result.Metadata["env."+key] = value
		}
	}
}

func (b *Builder) GetCacheInfo() (*types.CacheInfo, error) {
	return b.cache.Info()
}

func (b *Builder) GetCacheMetrics() (*types.CacheMetrics, error) {
	metrics, err := b.cache.GetMetrics()
	if err != nil {
		return nil, err
	}
	
	// Convert internal metrics to types.CacheMetrics
	typesMetrics := &types.CacheMetrics{
		TotalHits:         metrics.TotalHits,
		TotalMisses:       metrics.TotalMisses,
		HitRate:           metrics.HitRate,
		TotalSize:         metrics.TotalSize,
		TotalFiles:        metrics.TotalFiles,
		InvalidationCount: metrics.InvalidationCount,
		PruningCount:      metrics.PruningCount,
		SharedEntries:     metrics.SharedEntries,
		PlatformStats:     make(map[string]*types.PlatformCacheStats),
	}
	
	// Convert platform stats
	for platform, stats := range metrics.PlatformStats {
		typesMetrics.PlatformStats[platform] = &types.PlatformCacheStats{
			Hits:        stats.Hits,
			Misses:      stats.Misses,
			TotalSize:   stats.TotalSize,
			TotalFiles:  stats.TotalFiles,
			LastUpdated: stats.LastUpdated,
		}
	}
	
	return typesMetrics, nil
}

func (b *Builder) PruneCache() error {
	return b.cache.Prune()
}

func (b *Builder) PruneCacheWithCustomStrategy(maxSize int64, maxAge time.Duration, maxFiles int, lruEnabled bool) error {
	strategy := PruningStrategy{
		MaxSize:       maxSize,
		MaxAge:        maxAge,
		MaxFiles:      maxFiles,
		LRUEnabled:    lruEnabled,
		PlatformQuota: maxSize / 4, // 25% per platform by default
		OrphanCleanup: true,
	}
	return b.cache.PruneWithStrategy(strategy)
}

func (b *Builder) InvalidateCacheByDockerfile() error {
	dockerfileHash, err := b.cache.ComputeDockerfileHash(filepath.Join(b.config.Context, b.config.Dockerfile))
	if err != nil {
		return err
	}
	return b.cache.InvalidateByDockerfile(dockerfileHash)
}

func (b *Builder) InvalidateCacheByBuildContext() error {
	buildContextHash, err := b.cache.ComputeBuildContextHash(b.config.Context, []string{".git", "node_modules", ".ossb"})
	if err != nil {
		return err
	}
	return b.cache.InvalidateByBuildContext(buildContextHash)
}

func (b *Builder) GetSharedCacheEntriesCount() (int, error) {
	entries, err := b.cache.GetSharedCacheEntries()
	if err != nil {
		return 0, err
	}
	return len(entries), nil
}

func (b *Builder) ClearCache() error {
	return b.cache.Clear()
}

// buildPlatform builds for a single platform using traditional sequential execution
func (b *Builder) buildPlatform(ctx context.Context, platform types.Platform, operations []*types.Operation) *types.PlatformResult {
	platformResult := &types.PlatformResult{
		Platform: platform,
		Success:  false,
	}

	if b.config.Progress && b.progressOut != nil {
		fmt.Fprintf(b.progressOut, "\nBuilding for platform %s...\n", platform.String())
	}

	// Start platform-specific build stage
	b.observability.StartStage(ctx, "build", platform.String(), len(operations))

	// Acquire build permit for resource management
	if err := b.resourceManager.AcquireBuildPermit(ctx); err != nil {
		platformResult.Error = fmt.Sprintf("failed to acquire build permit: %v", err)
		b.observability.CompleteStage(ctx, "build", platform.String(), false, err.Error())
		return platformResult
	}
	defer b.resourceManager.ReleaseBuildPermit()

	// Set platform for all operations
	for _, op := range operations {
		op.Platform = platform
	}

	if b.config.Progress && b.progressOut != nil {
		fmt.Fprintf(b.progressOut, "Building dependency graph for %d operations on %s...\n", len(operations), platform.String())
	}

	solver := NewGraphSolver()
	if err := solver.BuildGraph(operations); err != nil {
		platformResult.Error = fmt.Sprintf("failed to build dependency graph: %v", err)
		b.observability.RecordError(ctx, err, "build_graph", "build", platform.String())
		b.observability.CompleteStage(ctx, "build", platform.String(), false, err.Error())
		return platformResult
	}

	executionOrder, err := solver.GetExecutionOrder()
	if err != nil {
		platformResult.Error = fmt.Sprintf("failed to get execution order: %v", err)
		b.observability.RecordError(ctx, err, "execution_order", "build", platform.String())
		b.observability.CompleteStage(ctx, "build", platform.String(), false, err.Error())
		return platformResult
	}

	if b.config.Progress && b.progressOut != nil {
		fmt.Fprintf(b.progressOut, "Executing %d operations for %s...\n", len(executionOrder), platform.String())
	}

	cacheHits := 0
	for i, nodeID := range executionOrder {
		operation := solver.GetOperation(nodeID)
		if operation == nil {
			platformResult.Error = fmt.Sprintf("operation not found for node %s", nodeID)
			b.observability.CompleteStage(ctx, "build", platform.String(), false, platformResult.Error)
			return platformResult
		}

		if b.config.Progress && b.progressOut != nil {
			fmt.Fprintf(b.progressOut, "[%s %d/%d] Executing %s operation...\n", platform.String(), i+1, len(executionOrder), operation.Type)
		}

		// Update progress
		progress := float64(i) / float64(len(executionOrder)) * 100.0
		b.observability.UpdateStageProgress(ctx, "build", platform.String(), progress, fmt.Sprintf("Executing %s operation", operation.Type))

		opStart := time.Now()
		opResult, err := b.executeOperation(operation)
		opDuration := time.Since(opStart)
		
		if err != nil {
			platformResult.Error = fmt.Sprintf("failed to execute operation: %v", err)
			b.observability.RecordError(ctx, err, string(operation.Type), "build", platform.String())
			b.observability.CompleteStage(ctx, "build", platform.String(), false, err.Error())
			return platformResult
		}

		if !opResult.Success {
			platformResult.Error = fmt.Sprintf("operation failed: %s", opResult.Error)
			opErr := fmt.Errorf(opResult.Error)
			b.observability.RecordError(ctx, opErr, string(operation.Type), "build", platform.String())
			b.observability.CompleteStage(ctx, "build", platform.String(), false, opResult.Error)
			return platformResult
		}

		// Record operation metrics
		b.observability.RecordOperation(ctx, operation, opResult, opDuration)

		if opResult.CacheHit {
			cacheHits++
		}
	}

	platformResult.Success = true
	platformResult.ImageID = fmt.Sprintf("%s-%s", b.config.Tags[0], platform.String())
	platformResult.CacheHits = cacheHits
	b.observability.CompleteStage(ctx, "build", platform.String(), true, "")

	return platformResult
}

// buildPlatformParallel builds for a single platform using parallel execution
func (b *Builder) buildPlatformParallel(ctx context.Context, platform types.Platform, operations []*types.Operation) *types.PlatformResult {
	platformResult := &types.PlatformResult{
		Platform: platform,
		Success:  false,
	}

	fmt.Printf("Debug: buildPlatformParallel called with %d operations\n", len(operations))

	if b.config.Progress && b.progressOut != nil {
		fmt.Fprintf(b.progressOut, "\nBuilding for platform %s with parallel execution...\n", platform.String())
	}

	// Start platform-specific build stage
	b.observability.StartStage(ctx, "build", platform.String(), len(operations))

	// Set platform for all operations
	for _, op := range operations {
		op.Platform = platform
	}

	// Create parallel build context
	buildCtx := &ParallelBuildContext{
		BuildID:         fmt.Sprintf("build-%s-%d", platform.String(), time.Now().Unix()),
		Operations:      operations,
		WorkDir:         b.workDir,
		ResourceManager: b.resourceManager,
		Config:          b.config,
	}

	fmt.Printf("Debug: Calling parallelExecutor.ExecuteWithPipelining\n")
	// Execute operations in parallel
	results, err := b.parallelExecutor.ExecuteWithPipelining(ctx, buildCtx)
	if err != nil {
		fmt.Printf("Debug: Parallel execution failed: %v\n", err)
		platformResult.Error = fmt.Sprintf("parallel execution failed: %v", err)
		b.observability.RecordError(ctx, err, "parallel_execution", "build", platform.String())
		b.observability.CompleteStage(ctx, "build", platform.String(), false, err.Error())
		return platformResult
	}

	fmt.Printf("Debug: Parallel execution returned %d results\n", len(results))

	// Count cache hits and validate results
	cacheHits := 0
	for _, result := range results {
		if result.CacheHit {
			cacheHits++
		}
		if !result.Success {
			fmt.Printf("Debug: Operation failed: %s\n", result.Error)
			platformResult.Error = fmt.Sprintf("operation failed: %s", result.Error)
			b.observability.CompleteStage(ctx, "build", platform.String(), false, result.Error)
			return platformResult
		}
	}

	fmt.Printf("Debug: All operations successful, cache hits: %d\n", cacheHits)
	platformResult.Success = true
	platformResult.ImageID = fmt.Sprintf("%s-%s", b.config.Tags[0], platform.String())
	platformResult.CacheHits = cacheHits
	b.observability.CompleteStage(ctx, "build", platform.String(), true, "")

	return platformResult
}

// GetResourceMetrics returns current resource metrics
func (b *Builder) GetResourceMetrics() (*ResourceUsage, error) {
	if b.resourceManager == nil {
		return nil, fmt.Errorf("resource manager not initialized")
	}
	return b.resourceManager.GetResourceUsage(), nil
}

// OptimizeResources triggers resource optimization
func (b *Builder) OptimizeResources() error {
	if b.resourceManager == nil {
		return fmt.Errorf("resource manager not initialized")
	}
	return b.resourceManager.OptimizeResources()
}

// GetPerformanceProfile returns performance profile for operation types
func (b *Builder) GetPerformanceProfile() map[string]*ResourceProfile {
	if b.resourceManager == nil {
		return nil
	}
	return b.resourceManager.profiler.GetAllProfiles()
}

func (b *Builder) Cleanup() error {
	// Cleanup resource manager
	if b.resourceManager != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		b.resourceManager.Shutdown(ctx)
	}

	// Cleanup concurrent builder
	if b.concurrentBuilder != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		b.concurrentBuilder.Shutdown(ctx)
	}

	// Cleanup work directory
	if b.workDir != "" {
		return os.RemoveAll(b.workDir)
	}
	return nil
}