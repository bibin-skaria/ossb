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
		Success:  false,
		Metadata: make(map[string]string),
	}

	if b.config.Progress && b.progressOut != nil {
		fmt.Fprintf(b.progressOut, "Starting build...\n")
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

	operations, err := b.frontend.Parse(string(dockerfileContent), b.config)
	if err != nil {
		result.Error = fmt.Sprintf("failed to parse Dockerfile: %v", err)
		return result, nil
	}

	result.Operations = len(operations)

	if b.config.Progress && b.progressOut != nil {
		fmt.Fprintf(b.progressOut, "Building dependency graph for %d operations...\n", len(operations))
	}

	if err := b.solver.BuildGraph(operations); err != nil {
		result.Error = fmt.Sprintf("failed to build dependency graph: %v", err)
		return result, nil
	}

	executionOrder, err := b.solver.GetExecutionOrder()
	if err != nil {
		result.Error = fmt.Sprintf("failed to get execution order: %v", err)
		return result, nil
	}

	if b.config.Progress && b.progressOut != nil {
		fmt.Fprintf(b.progressOut, "Executing operations...\n")
	}

	cacheHits := 0
	for i, nodeID := range executionOrder {
		operation := b.solver.GetOperation(nodeID)
		if operation == nil {
			result.Error = fmt.Sprintf("operation not found for node %s", nodeID)
			return result, nil
		}

		if b.config.Progress && b.progressOut != nil {
			fmt.Fprintf(b.progressOut, "[%d/%d] Executing %s operation...\n", i+1, len(executionOrder), operation.Type)
		}

		opResult, err := b.executeOperation(operation)
		if err != nil {
			result.Error = fmt.Sprintf("failed to execute operation: %v", err)
			return result, nil
		}

		if !opResult.Success {
			result.Error = fmt.Sprintf("operation failed: %s", opResult.Error)
			return result, nil
		}

		if opResult.CacheHit {
			cacheHits++
		}

		b.updateResultMetadata(result, operation, opResult)
	}

	result.CacheHits = cacheHits

	if b.config.Progress && b.progressOut != nil {
		fmt.Fprintf(b.progressOut, "Exporting result...\n")
	}

	if err := b.exporter.Export(result, b.config, b.workDir); err != nil {
		result.Error = fmt.Sprintf("failed to export result: %v", err)
		return result, nil
	}

	result.Success = true
	result.Duration = time.Since(start).String()

	if b.config.Progress && b.progressOut != nil {
		fmt.Fprintf(b.progressOut, "Build completed successfully in %s\n", result.Duration)
		fmt.Fprintf(b.progressOut, "Cache hits: %d/%d operations\n", cacheHits, len(operations))
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