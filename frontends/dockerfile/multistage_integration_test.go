package dockerfile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bibin-skaria/ossb/executors"
	"github.com/bibin-skaria/ossb/internal/types"
)

func TestMultiStageIntegration(t *testing.T) {
	// Create temporary directory for test
	tempDir, err := os.MkdirTemp("", "ossb-multistage-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create test context with source files
	contextDir := filepath.Join(tempDir, "context")
	if err := os.MkdirAll(contextDir, 0755); err != nil {
		t.Fatalf("failed to create context dir: %v", err)
	}

	// Create a simple source file
	sourceFile := filepath.Join(contextDir, "app.txt")
	if err := os.WriteFile(sourceFile, []byte("Hello from app"), 0644); err != nil {
		t.Fatalf("failed to create source file: %v", err)
	}

	// Create a simple multi-stage Dockerfile
	dockerfile := `FROM scratch AS builder
COPY app.txt /build/app.txt
RUN echo "built" > /build/status.txt

FROM scratch
COPY --from=builder /build/app.txt /app/app.txt
COPY --from=builder /build/status.txt /app/status.txt`

	config := &types.BuildConfig{
		Context:    contextDir,
		Dockerfile: "Dockerfile",
		BuildArgs:  make(map[string]string),
	}

	// Parse the Dockerfile
	parser := &Parser{
		config:    config,
		buildArgs: config.BuildArgs,
	}

	operations, err := parser.Parse(dockerfile)
	if err != nil {
		t.Fatalf("failed to parse Dockerfile: %v", err)
	}

	if len(operations) == 0 {
		t.Fatalf("no operations generated")
	}

	// Verify we have multi-stage context
	if parser.multiStageContext == nil {
		t.Fatalf("multi-stage context not created")
	}

	if len(parser.multiStageContext.Stages) != 2 {
		t.Fatalf("expected 2 stages, got %d", len(parser.multiStageContext.Stages))
	}

	// Check stage names
	expectedStages := []string{"builder", "stage-1"}
	for i, expectedName := range expectedStages {
		if parser.multiStageContext.Stages[i].Name != expectedName {
			t.Errorf("stage %d: expected name '%s', got '%s'", i, expectedName, parser.multiStageContext.Stages[i].Name)
		}
	}

	// Check final stage dependencies
	finalStage := parser.multiStageContext.FinalStage
	if finalStage == nil {
		t.Fatalf("final stage not found")
	}

	if len(finalStage.Dependencies) != 1 || finalStage.Dependencies[0] != "builder" {
		t.Errorf("final stage dependencies: expected ['builder'], got %v", finalStage.Dependencies)
	}

	// Check that operations have stage metadata
	stageOperations := make(map[string]int)
	for _, op := range operations {
		if op.Metadata != nil {
			if stage, exists := op.Metadata["stage"]; exists {
				stageOperations[stage]++
			}
		}
	}

	if len(stageOperations) < 2 {
		t.Errorf("expected operations from multiple stages, got: %v", stageOperations)
	}

	// Test with executor (mock execution)
	workDir := filepath.Join(tempDir, "work")
	if err := os.MkdirAll(workDir, 0755); err != nil {
		t.Fatalf("failed to create work dir: %v", err)
	}

	// Create a local executor for testing
	executor := executors.NewLocalExecutor()
	executor.SetWorkspaceDir(workDir)

	// Execute a few operations to test the multi-stage functionality
	for i, op := range operations {
		if i >= 3 { // Just test first few operations
			break
		}

		result, err := executor.Execute(op, workDir)
		if err != nil {
			t.Logf("Operation %d execution error (expected for scratch images): %v", i, err)
			continue
		}

		if !result.Success && result.Error != "" {
			t.Logf("Operation %d failed (expected for scratch images): %s", i, result.Error)
		}
	}
}

func TestMultiStageWithRealImages(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Create temporary directory for test
	tempDir, err := os.MkdirTemp("", "ossb-multistage-real-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create test context
	contextDir := filepath.Join(tempDir, "context")
	if err := os.MkdirAll(contextDir, 0755); err != nil {
		t.Fatalf("failed to create context dir: %v", err)
	}

	// Create a simple source file
	sourceFile := filepath.Join(contextDir, "hello.txt")
	if err := os.WriteFile(sourceFile, []byte("Hello World"), 0644); err != nil {
		t.Fatalf("failed to create source file: %v", err)
	}

	// Multi-stage Dockerfile with real images
	dockerfile := `FROM alpine:3.18 AS builder
COPY hello.txt /tmp/hello.txt
RUN echo "Built at $(date)" > /tmp/build-info.txt

FROM alpine:3.18
COPY --from=builder /tmp/hello.txt /app/hello.txt
COPY --from=builder /tmp/build-info.txt /app/build-info.txt
CMD ["cat", "/app/hello.txt"]`

	config := &types.BuildConfig{
		Context:    contextDir,
		Dockerfile: "Dockerfile",
		BuildArgs:  make(map[string]string),
	}

	// Parse the Dockerfile
	parser := &Parser{
		config:    config,
		buildArgs: config.BuildArgs,
	}

	operations, err := parser.Parse(dockerfile)
	if err != nil {
		t.Fatalf("failed to parse Dockerfile: %v", err)
	}

	// Verify multi-stage structure
	if parser.multiStageContext == nil {
		t.Fatalf("multi-stage context not created")
	}

	if len(parser.multiStageContext.Stages) != 2 {
		t.Fatalf("expected 2 stages, got %d", len(parser.multiStageContext.Stages))
	}

	// Check that we have COPY --from operations
	foundCopyFrom := false
	for _, op := range operations {
		if op.Type == types.OperationTypeFile && op.Metadata != nil {
			if fromStage, exists := op.Metadata["from_stage"]; exists && fromStage == "builder" {
				foundCopyFrom = true
				break
			}
		}
	}

	if !foundCopyFrom {
		t.Errorf("no COPY --from operations found")
	}

	// Verify stage dependencies
	finalStage := parser.multiStageContext.FinalStage
	if finalStage == nil {
		t.Fatalf("final stage not found")
	}

	if len(finalStage.Dependencies) != 1 || finalStage.Dependencies[0] != "builder" {
		t.Errorf("final stage dependencies: expected ['builder'], got %v", finalStage.Dependencies)
	}
}

func TestMultiStageNumericReferences(t *testing.T) {
	dockerfile := `FROM alpine:3.18 AS base
RUN echo "base stage"

FROM alpine:3.18 AS builder  
COPY --from=0 /etc/alpine-release /tmp/base-release
RUN echo "builder stage"

FROM alpine:3.18
COPY --from=1 /tmp/base-release /app/base-release
COPY --from=builder /etc/alpine-release /app/builder-release`

	config := &types.BuildConfig{
		Context:    "/tmp/test",
		Dockerfile: "Dockerfile",
		BuildArgs:  make(map[string]string),
	}

	parser := &Parser{
		config:    config,
		buildArgs: config.BuildArgs,
	}

	operations, err := parser.Parse(dockerfile)
	if err != nil {
		t.Fatalf("failed to parse Dockerfile: %v", err)
	}

	// Verify stages
	if len(parser.multiStageContext.Stages) != 3 {
		t.Fatalf("expected 3 stages, got %d", len(parser.multiStageContext.Stages))
	}

	// Check dependencies - final stage should depend on builder only
	// (it copies from stage 1 which is builder, and from builder directly)
	finalStage := parser.multiStageContext.FinalStage
	if finalStage == nil {
		t.Fatalf("final stage not found")
	}

	expectedDeps := []string{"builder"}
	if len(finalStage.Dependencies) != len(expectedDeps) {
		t.Errorf("final stage: expected %d dependencies, got %d", len(expectedDeps), len(finalStage.Dependencies))
	}

	for _, expectedDep := range expectedDeps {
		found := false
		for _, actualDep := range finalStage.Dependencies {
			if actualDep == expectedDep {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("final stage missing dependency: %s", expectedDep)
		}
	}
	
	// Check builder stage dependencies - should depend on base (stage 0)
	builderStage := parser.multiStageContext.StagesByName["builder"]
	if builderStage == nil {
		t.Fatalf("builder stage not found")
	}
	
	expectedBuilderDeps := []string{"base"}
	if len(builderStage.Dependencies) != len(expectedBuilderDeps) {
		t.Errorf("builder stage: expected %d dependencies, got %d", len(expectedBuilderDeps), len(builderStage.Dependencies))
	}

	for _, expectedDep := range expectedBuilderDeps {
		found := false
		for _, actualDep := range builderStage.Dependencies {
			if actualDep == expectedDep {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("builder stage missing dependency: %s", expectedDep)
		}
	}

	// Check that numeric references were resolved
	for _, op := range operations {
		if op.Type == types.OperationTypeFile && op.Metadata != nil {
			if fromStage, exists := op.Metadata["from_stage"]; exists {
				// Should not contain numeric references
				if fromStage == "0" || fromStage == "1" {
					t.Errorf("numeric stage reference not resolved: %s", fromStage)
				}
			}
		}
	}
}

func TestMultiStageComplexDependencies(t *testing.T) {
	dockerfile := `FROM alpine:3.18 AS base
RUN apk add --no-cache ca-certificates

FROM base AS deps
RUN apk add --no-cache curl
RUN curl -o /tmp/data.txt https://example.com/data || echo "mock data" > /tmp/data.txt

FROM base AS builder
COPY --from=deps /tmp/data.txt /build/data.txt
RUN echo "processing data" > /build/processed.txt

FROM base AS assets
RUN echo "building assets" > /assets/bundle.js

FROM base
COPY --from=builder /build/processed.txt /app/processed.txt
COPY --from=assets /assets/bundle.js /app/bundle.js
CMD ["cat", "/app/processed.txt"]`

	config := &types.BuildConfig{
		Context:    "/tmp/test",
		Dockerfile: "Dockerfile",
		BuildArgs:  make(map[string]string),
	}

	parser := &Parser{
		config:    config,
		buildArgs: config.BuildArgs,
	}

	operations, err := parser.Parse(dockerfile)
	if err != nil {
		t.Fatalf("failed to parse Dockerfile: %v", err)
	}

	// Should have 5 stages
	if len(parser.multiStageContext.Stages) != 5 {
		t.Fatalf("expected 5 stages, got %d", len(parser.multiStageContext.Stages))
	}

	// Check stage dependencies
	expectedDependencies := map[string][]string{
		"base":     {},
		"deps":     {"base"},        // FROM base
		"builder":  {"base", "deps"}, // FROM base + COPY --from=deps
		"assets":   {"base"},        // FROM base
		"stage-4": {"base", "builder", "assets"}, // FROM base + COPY --from=builder + COPY --from=assets
	}

	for stageName, expectedDeps := range expectedDependencies {
		stage, exists := parser.multiStageContext.StagesByName[stageName]
		if !exists {
			t.Errorf("stage %s not found", stageName)
			continue
		}

		if len(stage.Dependencies) != len(expectedDeps) {
			t.Errorf("stage %s: expected %d dependencies, got %d", stageName, len(expectedDeps), len(stage.Dependencies))
			continue
		}

		for _, expectedDep := range expectedDeps {
			found := false
			for _, actualDep := range stage.Dependencies {
				if actualDep == expectedDep {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("stage %s: missing dependency %s", stageName, expectedDep)
			}
		}
	}

	// Verify operations were generated for all stages
	stageOperations := make(map[string]int)
	for _, op := range operations {
		if op.Metadata != nil {
			if stage, exists := op.Metadata["stage"]; exists {
				stageOperations[stage]++
			}
		}
	}

	if len(stageOperations) != 5 {
		t.Errorf("expected operations from 5 stages, got operations from %d stages: %v", len(stageOperations), stageOperations)
	}
}