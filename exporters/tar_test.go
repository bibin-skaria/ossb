package exporters

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bibin-skaria/ossb/internal/types"
)

func TestTarExporter_Export(t *testing.T) {
	workDir := setupTestWorkDir(t)
	defer os.RemoveAll(workDir)
	
	result := createTestBuildResult()
	config := createTestBuildConfig()
	
	exporter := &TarExporter{}
	
	err := exporter.Export(result, config, workDir)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}
	
	// Verify output path is set
	if result.OutputPath == "" {
		t.Error("Output path not set")
	}
	
	// Verify tar file exists
	if _, err := os.Stat(result.OutputPath); os.IsNotExist(err) {
		t.Errorf("Tar file does not exist: %s", result.OutputPath)
	}
	
	// Verify tar file has expected name
	expectedName := "test_latest.tar"
	if !strings.HasSuffix(result.OutputPath, expectedName) {
		t.Errorf("Expected tar file to end with %s, got %s", expectedName, result.OutputPath)
	}
	
	// Verify tar content
	verifyTarContent(t, result.OutputPath)
}

func TestTarExporter_ExportWithoutTags(t *testing.T) {
	workDir := setupTestWorkDir(t)
	defer os.RemoveAll(workDir)
	
	result := createTestBuildResult()
	config := createTestBuildConfig()
	config.Tags = []string{} // No tags
	
	exporter := &TarExporter{}
	
	err := exporter.Export(result, config, workDir)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}
	
	// Should use default name
	expectedName := "image.tar"
	if !strings.HasSuffix(result.OutputPath, expectedName) {
		t.Errorf("Expected tar file to end with %s, got %s", expectedName, result.OutputPath)
	}
}

func TestTarExporter_ExportWithCompression(t *testing.T) {
	workDir := setupTestWorkDir(t)
	defer os.RemoveAll(workDir)
	
	result := createTestBuildResult()
	config := createTestBuildConfig()
	config.Output = "compressed.tar.gz" // Request compression
	
	exporter := &TarExporter{}
	
	err := exporter.Export(result, config, workDir)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}
	
	// Should create compressed tar
	if !strings.HasSuffix(result.OutputPath, ".tar.gz") {
		t.Errorf("Expected compressed tar file to end with .tar.gz, got %s", result.OutputPath)
	}
}

func TestTarExporter_ExportWithComplexTag(t *testing.T) {
	workDir := setupTestWorkDir(t)
	defer os.RemoveAll(workDir)
	
	result := createTestBuildResult()
	config := createTestBuildConfig()
	config.Tags = []string{"registry.example.com/namespace/image:v1.0.0"}
	
	exporter := &TarExporter{}
	
	err := exporter.Export(result, config, workDir)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}
	
	// Should sanitize tag name for filesystem
	expectedName := "registry.example.com_namespace_image_v1.0.0.tar"
	if !strings.HasSuffix(result.OutputPath, expectedName) {
		t.Errorf("Expected tar file to end with %s, got %s", expectedName, result.OutputPath)
	}
}

func TestTarExporter_ShouldCompress(t *testing.T) {
	exporter := &TarExporter{}
	
	tests := []struct {
		name     string
		config   *types.BuildConfig
		expected bool
	}{
		{
			name: "output with .gz suffix",
			config: &types.BuildConfig{
				Output: "image.tar.gz",
			},
			expected: true,
		},
		{
			name: "output with compressed keyword",
			config: &types.BuildConfig{
				Output: "compressed-image.tar",
			},
			expected: true,
		},
		{
			name: "regular output",
			config: &types.BuildConfig{
				Output: "image.tar",
			},
			expected: false,
		},
		{
			name: "empty output",
			config: &types.BuildConfig{
				Output: "",
			},
			expected: false,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := exporter.shouldCompress(tt.config)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestTarExporter_CreateInstructionsFromMetadata(t *testing.T) {
	exporter := &TarExporter{}
	
	metadata := map[string]string{
		"workdir":    "/app",
		"user":       "appuser",
		"cmd":        "echo hello",
		"entrypoint": "/bin/sh",
	}
	
	instructions := exporter.createInstructionsFromMetadata(metadata)
	
	if len(instructions) == 0 {
		t.Error("No instructions created")
	}
	
	// Verify FROM instruction is first
	if instructions[0].Command != "FROM" {
		t.Errorf("Expected FROM instruction first, got %s", instructions[0].Command)
	}
	
	// Verify expected instructions exist
	expectedCommands := []string{"FROM", "WORKDIR", "USER", "CMD", "ENTRYPOINT"}
	if len(instructions) != len(expectedCommands) {
		t.Errorf("Expected %d instructions, got %d", len(expectedCommands), len(instructions))
	}
	
	// Verify specific instructions
	commandMap := make(map[string]string)
	for _, instruction := range instructions {
		commandMap[instruction.Command] = instruction.Value
	}
	
	if commandMap["WORKDIR"] != "/app" {
		t.Errorf("Expected WORKDIR /app, got %s", commandMap["WORKDIR"])
	}
	
	if commandMap["USER"] != "appuser" {
		t.Errorf("Expected USER appuser, got %s", commandMap["USER"])
	}
}

func TestTarExporter_CreateInstructionsFromEmptyMetadata(t *testing.T) {
	exporter := &TarExporter{}
	
	// Test with nil metadata
	instructions := exporter.createInstructionsFromMetadata(nil)
	if len(instructions) != 0 {
		t.Errorf("Expected no instructions for nil metadata, got %d", len(instructions))
	}
	
	// Test with empty metadata
	instructions = exporter.createInstructionsFromMetadata(map[string]string{})
	if len(instructions) != 1 || instructions[0].Command != "FROM" {
		t.Errorf("Expected only FROM instruction for empty metadata, got %d instructions", len(instructions))
	}
}

func TestTarExporter_ExportWithEmptyLayers(t *testing.T) {
	workDir, err := os.MkdirTemp("", "ossb-tar-empty-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp work dir: %v", err)
	}
	defer os.RemoveAll(workDir)
	
	// Create empty layers directory
	layersDir := filepath.Join(workDir, "layers")
	if err := os.MkdirAll(layersDir, 0755); err != nil {
		t.Fatalf("Failed to create layers dir: %v", err)
	}
	
	result := createTestBuildResult()
	config := createTestBuildConfig()
	
	exporter := &TarExporter{}
	
	err = exporter.Export(result, config, workDir)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}
	
	// Should handle empty layers gracefully
	verifyTarContent(t, result.OutputPath)
}

func TestTarExporter_ExportWithMultiplePlatforms(t *testing.T) {
	workDir := setupTestWorkDir(t)
	defer os.RemoveAll(workDir)
	
	result := createTestBuildResult()
	config := createTestBuildConfig()
	config.Platforms = []types.Platform{
		{OS: "linux", Architecture: "amd64"},
		{OS: "linux", Architecture: "arm64"},
	}
	
	exporter := &TarExporter{}
	
	err := exporter.Export(result, config, workDir)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}
	
	// Should use first platform
	verifyTarContent(t, result.OutputPath)
}