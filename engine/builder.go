package engine

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/bibin-skaria/ossb/executors"
	"github.com/bibin-skaria/ossb/exporters"
	"github.com/bibin-skaria/ossb/frontends"
	"github.com/bibin-skaria/ossb/internal/types"
)

type Builder struct {
	config      *types.BuildConfig
	cache       *Cache
	solver      *GraphSolver
	executor    executors.Executor
	exporter    exporters.Exporter
	frontend    frontends.Frontend
	workDir     string
	progressOut io.Writer
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

	cache := NewCache(config.CacheDir)
	solver := NewGraphSolver()

	frontend, err := frontends.GetFrontend(config.Frontend)
	if err != nil {
		return nil, fmt.Errorf("failed to get frontend: %v", err)
	}

	executor, err := executors.GetExecutor("local")
	if err != nil {
		return nil, fmt.Errorf("failed to get executor: %v", err)
	}

	exporter, err := exporters.GetExporter(config.Output)
	if err != nil {
		return nil, fmt.Errorf("failed to get exporter: %v", err)
	}

	return &Builder{
		config:      config,
		cache:       cache,
		solver:      solver,
		executor:    executor,
		exporter:    exporter,
		frontend:    frontend,
		workDir:     workDir,
		progressOut: os.Stdout,
	}, nil
}

func (b *Builder) SetProgressOutput(w io.Writer) {
	b.progressOut = w
}

func (b *Builder) Build() (*types.BuildResult, error) {
	start := time.Now()
	
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
		return result, nil
	}

	if b.config.Progress && b.progressOut != nil {
		fmt.Fprintf(b.progressOut, "Parsing Dockerfile...\n")
	}

	totalCacheHits := 0
	allSuccess := true
	
	for _, platform := range b.config.Platforms {
		platformResult := &types.PlatformResult{
			Platform: platform,
			Success:  false,
		}
		result.PlatformResults[platform.String()] = platformResult

		if b.config.Progress && b.progressOut != nil {
			fmt.Fprintf(b.progressOut, "\nBuilding for platform %s...\n", platform.String())
		}

		operations, err := b.frontend.Parse(string(dockerfileContent), b.config)
		if err != nil {
			platformResult.Error = fmt.Sprintf("failed to parse Dockerfile: %v", err)
			allSuccess = false
			continue
		}

		for _, op := range operations {
			op.Platform = platform
		}

		if b.config.Progress && b.progressOut != nil {
			fmt.Fprintf(b.progressOut, "Building dependency graph for %d operations on %s...\n", len(operations), platform.String())
		}

		solver := NewGraphSolver()
		if err := solver.BuildGraph(operations); err != nil {
			platformResult.Error = fmt.Sprintf("failed to build dependency graph: %v", err)
			allSuccess = false
			continue
		}

		executionOrder, err := solver.GetExecutionOrder()
		if err != nil {
			platformResult.Error = fmt.Sprintf("failed to get execution order: %v", err)
			allSuccess = false
			continue
		}

		if b.config.Progress && b.progressOut != nil {
			fmt.Fprintf(b.progressOut, "Executing %d operations for %s...\n", len(executionOrder), platform.String())
		}

		cacheHits := 0
		for i, nodeID := range executionOrder {
			operation := solver.GetOperation(nodeID)
			if operation == nil {
				platformResult.Error = fmt.Sprintf("operation not found for node %s", nodeID)
				allSuccess = false
				break
			}

			if b.config.Progress && b.progressOut != nil {
				fmt.Fprintf(b.progressOut, "[%s %d/%d] Executing %s operation...\n", platform.String(), i+1, len(executionOrder), operation.Type)
			}

			opResult, err := b.executeOperation(operation)
			if err != nil {
				platformResult.Error = fmt.Sprintf("failed to execute operation: %v", err)
				allSuccess = false
				break
			}

			if !opResult.Success {
				platformResult.Error = fmt.Sprintf("operation failed: %s", opResult.Error)
				allSuccess = false
				break
			}

			if opResult.CacheHit {
				cacheHits++
			}

			b.updateResultMetadata(result, operation, opResult)
		}

		if platformResult.Error == "" {
			platformResult.Success = true
			platformResult.ImageID = fmt.Sprintf("%s-%s", b.config.Tags[0], platform.String())
			totalCacheHits += cacheHits
		}
	}

	result.Operations = len(b.config.Platforms) * result.Operations // Multiply by platform count
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
		if b.config.Progress && b.progressOut != nil {
			fmt.Fprintf(b.progressOut, "Exporting result...\n")
		}

		if err := b.exporter.Export(result, b.config, b.workDir); err != nil {
			result.Error = fmt.Sprintf("failed to export result: %v", err)
			result.Success = false
			return result, nil
		}
	}

	result.Duration = time.Since(start).String()

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
	}

	return result, nil
}

func (b *Builder) executeOperation(operation *types.Operation) (*types.OperationResult, error) {
	if !b.config.NoCache {
		cacheKey := operation.CacheKey()
		if cachedResult, hit := b.cache.Get(cacheKey); hit {
			return cachedResult, nil
		}
	}

	result, err := b.executor.Execute(operation, b.workDir)
	if err != nil {
		return nil, err
	}

	if !b.config.NoCache && result.Success {
		if err := b.cache.Set(operation.CacheKey(), result); err != nil {
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

func (b *Builder) PruneCache() error {
	return b.cache.Prune()
}

func (b *Builder) ClearCache() error {
	return b.cache.Clear()
}

func (b *Builder) Cleanup() error {
	if b.workDir != "" {
		return os.RemoveAll(b.workDir)
	}
	return nil
}