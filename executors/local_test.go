package executors

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bibin-skaria/ossb/internal/types"
	"github.com/bibin-skaria/ossb/registry"
)

func TestLocalExecutor_ExecuteSource_Scratch(t *testing.T) {
	executor := NewLocalExecutor()
	tempDir, err := ioutil.TempDir("", "ossb-test-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	operation := &types.Operation{
		Type: types.OperationTypeSource,
		Metadata: map[string]string{
			"image": "scratch",
		},
		Outputs: []string{"base"},
	}

	result, err := executor.Execute(operation, tempDir)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !result.Success {
		t.Fatalf("Expected success, got error: %s", result.Error)
	}

	// Check that base directory was created
	baseDir := filepath.Join(tempDir, "base")
	if _, err := os.Stat(baseDir); os.IsNotExist(err) {
		t.Fatalf("Base directory was not created")
	}

	// Check environment variables
	if result.Environment["PATH"] == "" {
		t.Fatalf("PATH environment variable not set")
	}
}

func TestLocalExecutor_ExecuteExec_SimpleCommand(t *testing.T) {
	executor := NewLocalExecutor()
	tempDir, err := ioutil.TempDir("", "ossb-test-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// First create a base filesystem
	baseDir := filepath.Join(tempDir, "base")
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		t.Fatalf("Failed to create base dir: %v", err)
	}

	// Create basic filesystem structure
	binDir := filepath.Join(baseDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("Failed to create bin dir: %v", err)
	}

	// Create a simple shell script for testing
	shPath := filepath.Join(binDir, "sh")
	shContent := `#!/bin/bash
echo "test output"
`
	if err := ioutil.WriteFile(shPath, []byte(shContent), 0755); err != nil {
		t.Fatalf("Failed to create sh: %v", err)
	}

	operation := &types.Operation{
		Type:    types.OperationTypeExec,
		Command: []string{"echo", "hello world"},
		Environment: map[string]string{
			"PATH": "/bin:/usr/bin",
		},
		Outputs: []string{"layer1"},
	}

	result, err := executor.Execute(operation, tempDir)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !result.Success {
		t.Fatalf("Expected success, got error: %s", result.Error)
	}

	// Check that layer was created
	if len(result.Outputs) == 0 {
		t.Fatalf("No outputs created")
	}

	layerPath := result.Outputs[0]
	if _, err := os.Stat(layerPath); os.IsNotExist(err) {
		t.Fatalf("Layer file was not created: %s", layerPath)
	}
}

func TestLocalExecutor_ExecuteFile_Copy(t *testing.T) {
	executor := NewLocalExecutor()
	tempDir, err := ioutil.TempDir("", "ossb-test-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create source file
	srcDir := filepath.Join(tempDir, "src")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatalf("Failed to create src dir: %v", err)
	}

	srcFile := filepath.Join(srcDir, "test.txt")
	if err := ioutil.WriteFile(srcFile, []byte("test content"), 0644); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	operation := &types.Operation{
		Type:    types.OperationTypeFile,
		Command: []string{"copy"},
		Inputs:  []string{"copy", srcFile},
		Metadata: map[string]string{
			"dest": "/app/test.txt",
		},
		Outputs: []string{"layer1"},
	}

	result, err := executor.Execute(operation, tempDir)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !result.Success {
		t.Fatalf("Expected success, got error: %s", result.Error)
	}
}

func TestLocalExecutor_ExecutePull(t *testing.T) {
	// Skip this test if we don't have network access or registry credentials
	if testing.Short() {
		t.Skip("Skipping pull test in short mode")
	}

	executor := NewLocalExecutor()
	tempDir, err := ioutil.TempDir("", "ossb-test-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	operation := &types.Operation{
		Type: types.OperationTypePull,
		Metadata: map[string]string{
			"image": "alpine:latest",
		},
		Platform: types.Platform{
			OS:           "linux",
			Architecture: "amd64",
		},
		Outputs: []string{"manifest"},
	}

	result, err := executor.Execute(operation, tempDir)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !result.Success {
		t.Fatalf("Expected success, got error: %s", result.Error)
	}

	// Check that manifest was created
	if len(result.Outputs) == 0 {
		t.Fatalf("No outputs created")
	}

	manifestPath := result.Outputs[0]
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		t.Fatalf("Manifest file was not created: %s", manifestPath)
	}

	// Verify manifest content
	manifestData, err := ioutil.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("Failed to read manifest: %v", err)
	}

	var manifest registry.ImageManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatalf("Failed to parse manifest: %v", err)
	}

	if manifest.SchemaVersion == 0 {
		t.Fatalf("Invalid manifest schema version")
	}
}

func TestLocalExecutor_ExecuteLayer(t *testing.T) {
	executor := NewLocalExecutor()
	tempDir, err := ioutil.TempDir("", "ossb-test-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create source directory with some files
	srcDir := filepath.Join(tempDir, "source")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatalf("Failed to create source dir: %v", err)
	}

	testFile := filepath.Join(srcDir, "test.txt")
	if err := ioutil.WriteFile(testFile, []byte("test content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	operation := &types.Operation{
		Type: types.OperationTypeLayer,
		Metadata: map[string]string{
			"source": srcDir,
		},
		Outputs: []string{"layer"},
	}

	result, err := executor.Execute(operation, tempDir)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !result.Success {
		t.Fatalf("Expected success, got error: %s", result.Error)
	}

	// Check that layer was created
	if len(result.Outputs) == 0 {
		t.Fatalf("No outputs created")
	}

	layerPath := result.Outputs[0]
	if _, err := os.Stat(layerPath); os.IsNotExist(err) {
		t.Fatalf("Layer file was not created: %s", layerPath)
	}

	// Check that digest was calculated
	if result.Environment["layer_digest"] == "" {
		t.Fatalf("Layer digest not calculated")
	}
}

func TestLocalExecutor_BuildEnvironment(t *testing.T) {
	executor := NewLocalExecutor()

	env := map[string]string{
		"CUSTOM_VAR": "custom_value",
		"PATH":       "/custom/path",
	}

	result := executor.buildEnvironment(env)

	// Check that custom variables are included
	found := false
	for _, envVar := range result {
		if envVar == "CUSTOM_VAR=custom_value" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Custom environment variable not found")
	}

	// Check that PATH is overridden
	found = false
	for _, envVar := range result {
		if envVar == "PATH=/custom/path" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("PATH override not found")
	}
}

func TestLocalExecutor_ParseUser(t *testing.T) {
	executor := NewLocalExecutor()

	tests := []struct {
		user        string
		expectedUID uint32
		expectedGID uint32
	}{
		{"1000", 1000, 1000},
		{"1000:1001", 1000, 1001},
		{"root", 1000, 1000}, // fallback for non-numeric
		{"", 1000, 1000},     // fallback for empty
	}

	for _, test := range tests {
		uid, gid, err := executor.parseUser(test.user)
		if err != nil {
			t.Fatalf("parseUser failed for %s: %v", test.user, err)
		}

		if uid != test.expectedUID {
			t.Fatalf("Expected UID %d, got %d for user %s", test.expectedUID, uid, test.user)
		}

		if gid != test.expectedGID {
			t.Fatalf("Expected GID %d, got %d for user %s", test.expectedGID, gid, test.user)
		}
	}
}

func TestLocalExecutor_CalculateLayerDigest(t *testing.T) {
	executor := NewLocalExecutor()
	tempDir, err := ioutil.TempDir("", "ossb-test-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a test file
	testFile := filepath.Join(tempDir, "test.tar.gz")
	if err := ioutil.WriteFile(testFile, []byte("test content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	digest, err := executor.calculateLayerDigest(testFile)
	if err != nil {
		t.Fatalf("calculateLayerDigest failed: %v", err)
	}

	if digest == "" {
		t.Fatalf("Empty digest returned")
	}

	if !strings.HasPrefix(digest, "sha256:") {
		t.Fatalf("Digest should start with sha256:, got: %s", digest)
	}
}

// Integration test with a real Dockerfile scenario
func TestLocalExecutor_IntegrationTest(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	executor := NewLocalExecutor()
	tempDir, err := ioutil.TempDir("", "ossb-integration-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Test scenario: FROM scratch, COPY file, RUN command
	
	// Step 1: Source operation (scratch)
	sourceOp := &types.Operation{
		Type: types.OperationTypeSource,
		Metadata: map[string]string{
			"image": "scratch",
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

	// Step 2: File operation (copy a test file)
	srcDir := filepath.Join(tempDir, "context")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatalf("Failed to create context dir: %v", err)
	}

	testFile := filepath.Join(srcDir, "app.txt")
	if err := ioutil.WriteFile(testFile, []byte("Hello World"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	fileOp := &types.Operation{
		Type:    types.OperationTypeFile,
		Command: []string{"copy"},
		Inputs:  []string{"copy", testFile},
		Metadata: map[string]string{
			"dest": "/app/app.txt",
		},
		Outputs: []string{"layer1"},
	}

	result, err = executor.Execute(fileOp, tempDir)
	if err != nil {
		t.Fatalf("File operation failed: %v", err)
	}
	if !result.Success {
		t.Fatalf("File operation failed: %s", result.Error)
	}

	t.Logf("Integration test completed successfully")
}

// Benchmark tests
func BenchmarkLocalExecutor_ExecuteSource(b *testing.B) {
	executor := NewLocalExecutor()
	tempDir, err := ioutil.TempDir("", "ossb-bench-")
	if err != nil {
		b.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	operation := &types.Operation{
		Type: types.OperationTypeSource,
		Metadata: map[string]string{
			"image": "scratch",
		},
		Outputs: []string{"base"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := executor.Execute(operation, tempDir)
		if err != nil {
			b.Fatalf("Execute failed: %v", err)
		}
	}
}

func BenchmarkLocalExecutor_CreateLayer(b *testing.B) {
	executor := NewLocalExecutor()
	tempDir, err := ioutil.TempDir("", "ossb-bench-")
	if err != nil {
		b.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create source directory with test files
	srcDir := filepath.Join(tempDir, "source")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		b.Fatalf("Failed to create source dir: %v", err)
	}

	for i := 0; i < 100; i++ {
		testFile := filepath.Join(srcDir, fmt.Sprintf("file%d.txt", i))
		if err := ioutil.WriteFile(testFile, []byte("test content"), 0644); err != nil {
			b.Fatalf("Failed to create test file: %v", err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := executor.createLayerFromChanges(srcDir)
		if err != nil {
			b.Fatalf("createLayerFromChanges failed: %v", err)
		}
	}
}