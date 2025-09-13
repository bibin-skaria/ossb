package executors

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bibin-skaria/ossb/internal/types"
)

// TestLocalExecutor_CompleteDockerfileBuild tests a complete Dockerfile build scenario
func TestLocalExecutor_CompleteDockerfileBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping complete build test in short mode")
	}

	executor := NewLocalExecutor()
	tempDir, err := ioutil.TempDir("", "ossb-complete-build-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Simulate a Dockerfile build:
	// FROM alpine:latest
	// WORKDIR /app
	// COPY app.txt /app/
	// RUN echo "Building application" > /app/build.log
	// CMD ["cat", "/app/app.txt"]

	t.Logf("Starting complete Dockerfile build simulation in %s", tempDir)

	// Step 1: FROM alpine:latest (source operation pulls and extracts)
	t.Log("Step 1: Setting up base image (alpine:latest)")
	sourceOp := &types.Operation{
		Type: types.OperationTypeSource,
		Metadata: map[string]string{
			"image": "alpine:latest",
		},
		Platform: types.Platform{
			OS:           "linux",
			Architecture: "amd64",
		},
		Outputs: []string{"base"},
	}

	result, err := executor.Execute(sourceOp, tempDir)
	if err != nil {
		t.Fatalf("Source operation failed: %v", err)
	}
	if !result.Success {
		t.Fatalf("Source operation failed: %s", result.Error)
	}
	t.Logf("✓ Base image pulled and extracted successfully")

	// Step 2: WORKDIR /app (create working directory)
	t.Log("Step 2: Setting up working directory")
	workdirOp := &types.Operation{
		Type:    types.OperationTypeExec,
		Command: []string{"mkdir", "-p", "/app"},
		Environment: map[string]string{
			"PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		},
		WorkDir: "/",
		Outputs: []string{"workdir-layer"},
	}

	result, err = executor.Execute(workdirOp, tempDir)
	if err != nil {
		t.Fatalf("WORKDIR operation failed: %v", err)
	}
	if !result.Success {
		t.Fatalf("WORKDIR operation failed: %s", result.Error)
	}
	t.Logf("✓ Working directory created, layer: %s", result.Outputs[0])

	// Step 3: COPY app.txt /app/
	t.Log("Step 3: Copying application file")
	
	// Create source file in build context
	contextDir := filepath.Join(tempDir, "context")
	if err := os.MkdirAll(contextDir, 0755); err != nil {
		t.Fatalf("Failed to create context dir: %v", err)
	}

	appFile := filepath.Join(contextDir, "app.txt")
	appContent := "Hello from OSSB!\nThis is a test application.\n"
	if err := ioutil.WriteFile(appFile, []byte(appContent), 0644); err != nil {
		t.Fatalf("Failed to create app file: %v", err)
	}

	copyOp := &types.Operation{
		Type:    types.OperationTypeFile,
		Command: []string{"copy"},
		Inputs:  []string{"copy", appFile},
		Metadata: map[string]string{
			"dest": "/app/app.txt",
		},
		Outputs: []string{"copy-layer"},
	}

	result, err = executor.Execute(copyOp, tempDir)
	if err != nil {
		t.Fatalf("COPY operation failed: %v", err)
	}
	if !result.Success {
		t.Fatalf("COPY operation failed: %s", result.Error)
	}
	t.Logf("✓ File copied, layer created")

	// Step 4: RUN echo "Building application" > /app/build.log
	t.Log("Step 4: Running build command")
	runOp := &types.Operation{
		Type:    types.OperationTypeExec,
		Command: []string{"echo", "Building application"},
		Environment: map[string]string{
			"PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		},
		WorkDir: "/app",
		Outputs: []string{"run-layer"},
	}

	result, err = executor.Execute(runOp, tempDir)
	if err != nil {
		t.Fatalf("RUN operation failed: %v", err)
	}
	if !result.Success {
		t.Fatalf("RUN operation failed: %s", result.Error)
	}
	t.Logf("✓ Build command executed, layer: %s", result.Outputs[0])

	// Step 5: Create final layer from all changes
	t.Log("Step 5: Creating final image layer")
	layerOp := &types.Operation{
		Type: types.OperationTypeLayer,
		Metadata: map[string]string{
			"source": filepath.Join(tempDir, "layers", "layer-2"), // Last layer created
		},
		Outputs: []string{"final-layer"},
	}

	result, err = executor.Execute(layerOp, tempDir)
	if err != nil {
		t.Fatalf("Layer creation failed: %v", err)
	}
	if !result.Success {
		t.Fatalf("Layer creation failed: %s", result.Error)
	}
	t.Logf("✓ Final layer created: %s", result.Outputs[0])
	t.Logf("✓ Layer digest: %s", result.Environment["layer_digest"])

	// Verify the build results
	t.Log("Verifying build results...")
	
	// Check that all expected directories exist
	expectedDirs := []string{
		filepath.Join(tempDir, "base"),
		filepath.Join(tempDir, "layers"),
		filepath.Join(tempDir, "rootfs"),
	}

	for _, dir := range expectedDirs {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			t.Errorf("Expected directory does not exist: %s", dir)
		}
	}

	// Check that layer files were created
	layersDir := filepath.Join(tempDir, "layers")
	entries, err := os.ReadDir(layersDir)
	if err != nil {
		t.Fatalf("Failed to read layers directory: %v", err)
	}

	layerCount := 0
	for _, entry := range entries {
		if entry.IsDir() && filepath.Ext(entry.Name()) == "" {
			layerCount++
		}
	}

	if layerCount < 2 {
		t.Errorf("Expected at least 2 layers, found %d", layerCount)
	}

	t.Logf("✓ Build completed successfully with %d layers", layerCount)
	t.Log("✓ Complete Dockerfile build simulation passed!")
}

// TestLocalExecutor_MultiStageDockerfile tests multi-stage build capabilities
func TestLocalExecutor_MultiStageDockerfile(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping multi-stage test in short mode")
	}

	executor := NewLocalExecutor()
	tempDir, err := ioutil.TempDir("", "ossb-multistage-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Simulate a multi-stage Dockerfile:
	// FROM alpine:latest AS builder
	// WORKDIR /build
	// COPY source.txt /build/
	// RUN echo "compiled" > /build/app
	//
	// FROM alpine:latest AS runtime
	// WORKDIR /app
	// COPY --from=builder /build/app /app/
	// CMD ["./app"]

	t.Logf("Starting multi-stage build simulation in %s", tempDir)

	// Stage 1: Builder stage
	t.Log("Stage 1: Builder stage")
	
	// Pull base image for builder
	pullOp := &types.Operation{
		Type: types.OperationTypePull,
		Metadata: map[string]string{
			"image": "alpine:latest",
		},
		Platform: types.Platform{
			OS:           "linux",
			Architecture: "amd64",
		},
		Outputs: []string{"builder-manifest"},
	}

	result, err := executor.Execute(pullOp, tempDir)
	if err != nil {
		t.Fatalf("Builder pull failed: %v", err)
	}
	if !result.Success {
		t.Fatalf("Builder pull failed: %s", result.Error)
	}

	// Create build context
	contextDir := filepath.Join(tempDir, "context")
	if err := os.MkdirAll(contextDir, 0755); err != nil {
		t.Fatalf("Failed to create context dir: %v", err)
	}

	sourceFile := filepath.Join(contextDir, "source.txt")
	if err := ioutil.WriteFile(sourceFile, []byte("source code"), 0644); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	// WORKDIR /build
	workdirOp := &types.Operation{
		Type:    types.OperationTypeExec,
		Command: []string{"mkdir", "-p", "/build"},
		Environment: map[string]string{
			"PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		},
		Outputs: []string{"builder-workdir"},
	}

	result, err = executor.Execute(workdirOp, tempDir)
	if err != nil {
		t.Fatalf("Builder WORKDIR failed: %v", err)
	}
	if !result.Success {
		t.Fatalf("Builder WORKDIR failed: %s", result.Error)
	}

	// COPY source.txt /build/
	copyOp := &types.Operation{
		Type:    types.OperationTypeFile,
		Command: []string{"copy"},
		Inputs:  []string{"copy", sourceFile},
		Metadata: map[string]string{
			"dest": "/build/source.txt",
		},
		Outputs: []string{"builder-copy"},
	}

	result, err = executor.Execute(copyOp, tempDir)
	if err != nil {
		t.Fatalf("Builder COPY failed: %v", err)
	}
	if !result.Success {
		t.Fatalf("Builder COPY failed: %s", result.Error)
	}

	// RUN echo "compiled" > /build/app
	runOp := &types.Operation{
		Type:    types.OperationTypeExec,
		Command: []string{"echo", "compiled application"},
		Environment: map[string]string{
			"PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		},
		WorkDir: "/build",
		Outputs: []string{"builder-run"},
	}

	result, err = executor.Execute(runOp, tempDir)
	if err != nil {
		t.Fatalf("Builder RUN failed: %v", err)
	}
	if !result.Success {
		t.Fatalf("Builder RUN failed: %s", result.Error)
	}

	t.Log("✓ Builder stage completed")

	// Stage 2: Runtime stage (simplified - in real implementation we'd start fresh)
	t.Log("Stage 2: Runtime stage")

	// For this test, we'll simulate copying from the builder stage
	// In a real implementation, this would involve managing multiple filesystem contexts

	// Find the last created layer
	layersDir := filepath.Join(tempDir, "layers")
	entries, err := os.ReadDir(layersDir)
	if err != nil {
		t.Fatalf("Failed to read layers directory: %v", err)
	}

	var lastLayer string
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "layer-") {
			lastLayer = entry.Name()
		}
	}

	if lastLayer == "" {
		t.Fatalf("No layers found in builder stage")
	}

	// Create runtime layer
	runtimeOp := &types.Operation{
		Type: types.OperationTypeLayer,
		Metadata: map[string]string{
			"source": filepath.Join(tempDir, "layers", lastLayer), // Builder's final layer
		},
		Outputs: []string{"runtime-layer"},
	}

	result, err = executor.Execute(runtimeOp, tempDir)
	if err != nil {
		t.Fatalf("Runtime layer creation failed: %v", err)
	}
	if !result.Success {
		t.Fatalf("Runtime layer creation failed: %s", result.Error)
	}

	t.Log("✓ Multi-stage build simulation completed")
	t.Logf("✓ Final runtime layer: %s", result.Outputs[0])
}

// TestLocalExecutor_ErrorHandling tests error handling scenarios
func TestLocalExecutor_ErrorHandling(t *testing.T) {
	executor := NewLocalExecutor()
	tempDir, err := ioutil.TempDir("", "ossb-error-test-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Test 1: Invalid image reference
	t.Log("Testing invalid image reference")
	invalidPullOp := &types.Operation{
		Type: types.OperationTypePull,
		Metadata: map[string]string{
			"image": "invalid::image::reference",
		},
		Platform: types.Platform{
			OS:           "linux",
			Architecture: "amd64",
		},
	}

	result, err := executor.Execute(invalidPullOp, tempDir)
	if err != nil {
		t.Fatalf("Execute should not return error, but operation should fail: %v", err)
	}
	if result.Success {
		t.Fatalf("Expected operation to fail with invalid image reference")
	}
	t.Logf("✓ Invalid image reference handled correctly: %s", result.Error)

	// Test 2: Command execution failure
	t.Log("Testing command execution failure")
	
	// Create base filesystem first
	baseDir := filepath.Join(tempDir, "base")
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		t.Fatalf("Failed to create base dir: %v", err)
	}

	failingOp := &types.Operation{
		Type:    types.OperationTypeExec,
		Command: []string{"nonexistent-command", "arg1", "arg2"},
		Environment: map[string]string{
			"PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		},
	}

	result, err = executor.Execute(failingOp, tempDir)
	if err != nil {
		t.Fatalf("Execute should not return error, but operation should fail: %v", err)
	}
	if result.Success {
		t.Fatalf("Expected operation to fail with nonexistent command")
	}
	t.Logf("✓ Command execution failure handled correctly: %s", result.Error)

	// Test 3: Missing metadata
	t.Log("Testing missing metadata")
	missingMetaOp := &types.Operation{
		Type:     types.OperationTypePull,
		Metadata: map[string]string{}, // Missing image
	}

	result, err = executor.Execute(missingMetaOp, tempDir)
	if err != nil {
		t.Fatalf("Execute should not return error, but operation should fail: %v", err)
	}
	if result.Success {
		t.Fatalf("Expected operation to fail with missing metadata")
	}
	t.Logf("✓ Missing metadata handled correctly: %s", result.Error)

	t.Log("✓ All error handling tests passed")
}

// TestLocalExecutor_Performance tests performance characteristics
func TestLocalExecutor_Performance(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	executor := NewLocalExecutor()
	tempDir, err := ioutil.TempDir("", "ossb-perf-test-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create base filesystem
	baseDir := filepath.Join(tempDir, "base")
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		t.Fatalf("Failed to create base dir: %v", err)
	}

	// Test multiple file operations
	t.Log("Testing performance with multiple file operations")
	
	contextDir := filepath.Join(tempDir, "context")
	if err := os.MkdirAll(contextDir, 0755); err != nil {
		t.Fatalf("Failed to create context dir: %v", err)
	}

	// Create multiple test files
	numFiles := 50
	for i := 0; i < numFiles; i++ {
		testFile := filepath.Join(contextDir, fmt.Sprintf("file%d.txt", i))
		content := fmt.Sprintf("Content of file %d\nLine 2\nLine 3\n", i)
		if err := ioutil.WriteFile(testFile, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create test file %d: %v", i, err)
		}
	}

	// Execute multiple copy operations
	for i := 0; i < numFiles; i++ {
		srcFile := filepath.Join(contextDir, fmt.Sprintf("file%d.txt", i))
		copyOp := &types.Operation{
			Type:    types.OperationTypeFile,
			Command: []string{"copy"},
			Inputs:  []string{"copy", srcFile},
			Metadata: map[string]string{
				"dest": fmt.Sprintf("/app/file%d.txt", i),
			},
			Outputs: []string{fmt.Sprintf("copy-layer-%d", i)},
		}

		result, err := executor.Execute(copyOp, tempDir)
		if err != nil {
			t.Fatalf("Copy operation %d failed: %v", i, err)
		}
		if !result.Success {
			t.Fatalf("Copy operation %d failed: %s", i, result.Error)
		}
	}

	t.Logf("✓ Successfully processed %d file operations", numFiles)

	// Test layer creation performance
	layersDir := filepath.Join(tempDir, "layers")
	entries, err := os.ReadDir(layersDir)
	if err != nil {
		t.Fatalf("Failed to read layers directory: %v", err)
	}

	layerCount := 0
	for _, entry := range entries {
		if entry.IsDir() {
			layerCount++
		}
	}

	t.Logf("✓ Created %d layers", layerCount)
	
	if layerCount < 1 {
		t.Errorf("Expected at least 1 layer, got %d", layerCount)
	}

	t.Log("✓ Performance test completed successfully")
}