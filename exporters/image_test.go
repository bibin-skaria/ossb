package exporters

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bibin-skaria/ossb/internal/types"
)

func TestImageExporter_Export(t *testing.T) {
	workDir := setupTestWorkDir(t)
	defer os.RemoveAll(workDir)
	
	result := createTestBuildResult()
	config := createTestBuildConfig()
	
	exporter := &ImageExporter{}
	
	err := exporter.Export(result, config, workDir)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}
	
	// Verify output path is set
	if result.OutputPath == "" {
		t.Error("Output path not set")
	}
	
	// Verify output directory exists
	if _, err := os.Stat(result.OutputPath); os.IsNotExist(err) {
		t.Errorf("Output directory does not exist: %s", result.OutputPath)
	}
	
	// Verify OCI layout structure
	verifyOCILayout(t, result.OutputPath)
	
	// Verify image ID is set
	if result.ImageID == "" {
		t.Error("Image ID not set")
	}
}

func TestImageExporter_ExportWithoutTags(t *testing.T) {
	workDir := setupTestWorkDir(t)
	defer os.RemoveAll(workDir)
	
	result := createTestBuildResult()
	config := createTestBuildConfig()
	config.Tags = []string{} // No tags
	
	exporter := &ImageExporter{}
	
	err := exporter.Export(result, config, workDir)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}
	
	// Should still work without tags
	if result.OutputPath == "" {
		t.Error("Output path not set")
	}
	
	if result.ImageID == "" {
		t.Error("Image ID not set")
	}
}

func TestImageExporter_ExportWithMultiplePlatforms(t *testing.T) {
	workDir := setupTestWorkDir(t)
	defer os.RemoveAll(workDir)
	
	result := createTestBuildResult()
	config := createTestBuildConfig()
	config.Platforms = []types.Platform{
		{OS: "linux", Architecture: "amd64"},
		{OS: "linux", Architecture: "arm64"},
	}
	
	exporter := &ImageExporter{}
	
	err := exporter.Export(result, config, workDir)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}
	
	// Should use first platform
	verifyOCILayout(t, result.OutputPath)
}

func TestImageExporter_ExportWithEmptyLayers(t *testing.T) {
	workDir, err := os.MkdirTemp("", "ossb-image-empty-test-*")
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
	
	exporter := &ImageExporter{}
	
	err = exporter.Export(result, config, workDir)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}
	
	// Should handle empty layers gracefully
	verifyOCILayout(t, result.OutputPath)
}

func TestImageExporter_CreateInstructionsFromMetadata(t *testing.T) {
	exporter := &ImageExporter{}
	
	metadata := map[string]string{
		"workdir":       "/app",
		"user":          "appuser",
		"cmd":           "echo hello",
		"entrypoint":    "/bin/sh",
		"expose":        "8080,9090",
		"volume":        "/data,/logs",
		"label.version": "1.0.0",
		"label.author":  "test",
	}
	
	instructions := exporter.createInstructionsFromMetadata(metadata)
	
	if len(instructions) == 0 {
		t.Error("No instructions created")
	}
	
	// Verify FROM instruction is first
	if instructions[0].Command != "FROM" {
		t.Errorf("Expected FROM instruction first, got %s", instructions[0].Command)
	}
	
	// Count expected instructions
	expectedCommands := []string{"FROM", "WORKDIR", "USER", "CMD", "ENTRYPOINT", "EXPOSE", "VOLUME", "LABEL", "LABEL"}
	if len(instructions) != len(expectedCommands) {
		t.Errorf("Expected %d instructions, got %d", len(expectedCommands), len(instructions))
	}
	
	// Verify specific instructions exist
	commandMap := make(map[string]bool)
	for _, instruction := range instructions {
		commandMap[instruction.Command] = true
	}
	
	requiredCommands := []string{"WORKDIR", "USER", "CMD", "ENTRYPOINT", "EXPOSE", "VOLUME", "LABEL"}
	for _, cmd := range requiredCommands {
		if !commandMap[cmd] {
			t.Errorf("Missing required command: %s", cmd)
		}
	}
}

func TestImageExporter_CreateInstructionsFromEmptyMetadata(t *testing.T) {
	exporter := &ImageExporter{}
	
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

func TestImageExporter_GetImageReference(t *testing.T) {
	exporter := &ImageExporter{}
	
	// Test with tags
	config := &types.BuildConfig{
		Tags: []string{"myimage:latest", "myimage:v1.0"},
	}
	ref := exporter.getImageReference(config)
	if ref != "myimage:latest" {
		t.Errorf("Expected 'myimage:latest', got '%s'", ref)
	}
	
	// Test without tags
	config = &types.BuildConfig{
		Tags: []string{},
	}
	ref = exporter.getImageReference(config)
	if ref != "latest" {
		t.Errorf("Expected 'latest', got '%s'", ref)
	}
}