package exporters

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bibin-skaria/ossb/internal/types"
)

func TestLocalExporter_Export(t *testing.T) {
	workDir := setupTestWorkDir(t)
	defer os.RemoveAll(workDir)
	
	result := createTestBuildResult()
	config := createTestBuildConfig()
	
	exporter := &LocalExporter{}
	
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
	
	// Verify local extraction structure
	verifyLocalExtraction(t, result.OutputPath)
}

func TestLocalExporter_ExportWithoutTags(t *testing.T) {
	workDir := setupTestWorkDir(t)
	defer os.RemoveAll(workDir)
	
	result := createTestBuildResult()
	config := createTestBuildConfig()
	config.Tags = []string{} // No tags
	
	exporter := &LocalExporter{}
	
	err := exporter.Export(result, config, workDir)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}
	
	// Should use default name
	expectedName := "image"
	if !strings.HasSuffix(result.OutputPath, expectedName) {
		t.Errorf("Expected output path to end with %s, got %s", expectedName, result.OutputPath)
	}
	
	verifyLocalExtraction(t, result.OutputPath)
}

func TestLocalExporter_ExportWithComplexTag(t *testing.T) {
	workDir := setupTestWorkDir(t)
	defer os.RemoveAll(workDir)
	
	result := createTestBuildResult()
	config := createTestBuildConfig()
	config.Tags = []string{"registry.example.com/namespace/image:v1.0.0"}
	
	exporter := &LocalExporter{}
	
	err := exporter.Export(result, config, workDir)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}
	
	// Should sanitize tag name for filesystem
	expectedName := "registry.example.com_namespace_image_v1.0.0"
	if !strings.HasSuffix(result.OutputPath, expectedName) {
		t.Errorf("Expected output path to end with %s, got %s", expectedName, result.OutputPath)
	}
	
	verifyLocalExtraction(t, result.OutputPath)
}

func TestLocalExporter_ExportWithMultiplePlatforms(t *testing.T) {
	workDir := setupTestWorkDir(t)
	defer os.RemoveAll(workDir)
	
	result := createTestBuildResult()
	config := createTestBuildConfig()
	config.Platforms = []types.Platform{
		{OS: "linux", Architecture: "amd64"},
		{OS: "linux", Architecture: "arm64"},
	}
	
	exporter := &LocalExporter{}
	
	err := exporter.Export(result, config, workDir)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}
	
	// Should use first platform
	verifyLocalExtraction(t, result.OutputPath)
}

func TestLocalExporter_SaveImageMetadata(t *testing.T) {
	workDir := setupTestWorkDir(t)
	defer os.RemoveAll(workDir)
	
	outputPath := filepath.Join(workDir, "test-output")
	if err := os.MkdirAll(outputPath, 0755); err != nil {
		t.Fatalf("Failed to create output dir: %v", err)
	}
	
	result := createTestBuildResult()
	config := createTestBuildConfig()
	
	exporter := &LocalExporter{}
	
	// Collect layers first
	layerObjects, err := exporter.collectAndProcessLayers(filepath.Join(workDir, "layers"))
	if err != nil {
		t.Fatalf("Failed to collect layers: %v", err)
	}
	
	err = exporter.saveImageMetadata(result, config, outputPath, layerObjects)
	if err != nil {
		t.Fatalf("Failed to save metadata: %v", err)
	}
	
	// Verify metadata files exist
	metadataFiles := []string{"config.json", "manifest.json", "ossb-metadata.json"}
	for _, file := range metadataFiles {
		filePath := filepath.Join(outputPath, file)
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			t.Errorf("Metadata file %s not found", file)
		}
	}
	
	// Verify ossb-metadata.json content
	metadataPath := filepath.Join(outputPath, "ossb-metadata.json")
	metadataData, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Fatalf("Failed to read metadata: %v", err)
	}
	
	var metadata map[string]interface{}
	if err := json.Unmarshal(metadataData, &metadata); err != nil {
		t.Fatalf("Failed to parse metadata: %v", err)
	}
	
	// Verify metadata fields
	if exporter, ok := metadata["exporter"]; !ok || exporter != "local" {
		t.Errorf("Expected exporter 'local', got %v", exporter)
	}
	
	if _, ok := metadata["exported_at"]; !ok {
		t.Error("Missing exported_at field")
	}
	
	if _, ok := metadata["build_result"]; !ok {
		t.Error("Missing build_result field")
	}
	
	if _, ok := metadata["build_config"]; !ok {
		t.Error("Missing build_config field")
	}
}

func TestLocalExporter_CreateExtractionManifest(t *testing.T) {
	workDir := setupTestWorkDir(t)
	defer os.RemoveAll(workDir)
	
	outputPath := filepath.Join(workDir, "test-output")
	if err := os.MkdirAll(outputPath, 0755); err != nil {
		t.Fatalf("Failed to create output dir: %v", err)
	}
	
	config := createTestBuildConfig()
	
	exporter := &LocalExporter{}
	
	// Collect layers first
	layerObjects, err := exporter.collectAndProcessLayers(filepath.Join(workDir, "layers"))
	if err != nil {
		t.Fatalf("Failed to collect layers: %v", err)
	}
	
	err = exporter.createExtractionManifest(outputPath, layerObjects, config)
	if err != nil {
		t.Fatalf("Failed to create extraction manifest: %v", err)
	}
	
	// Verify extraction manifest exists
	manifestPath := filepath.Join(outputPath, "extraction-manifest.json")
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		t.Error("Extraction manifest not found")
	}
	
	// Verify manifest content
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("Failed to read extraction manifest: %v", err)
	}
	
	var manifest map[string]interface{}
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatalf("Failed to parse extraction manifest: %v", err)
	}
	
	// Verify manifest fields
	if version, ok := manifest["version"]; !ok || version != "1.0" {
		t.Errorf("Expected version '1.0', got %v", version)
	}
	
	if format, ok := manifest["format"]; !ok || format != "local-filesystem" {
		t.Errorf("Expected format 'local-filesystem', got %v", format)
	}
	
	if rootfsPath, ok := manifest["rootfs_path"]; !ok || rootfsPath != "rootfs" {
		t.Errorf("Expected rootfs_path 'rootfs', got %v", rootfsPath)
	}
	
	if imageName, ok := manifest["image_name"]; !ok || imageName != "test:latest" {
		t.Errorf("Expected image_name 'test:latest', got %v", imageName)
	}
	
	if _, ok := manifest["created"]; !ok {
		t.Error("Missing created field")
	}
	
	if layers, ok := manifest["layers"]; !ok {
		t.Error("Missing layers field")
	} else if layersArray, ok := layers.([]interface{}); !ok {
		t.Error("Layers field is not an array")
	} else if len(layersArray) != len(layerObjects) {
		t.Errorf("Expected %d layers, got %d", len(layerObjects), len(layersArray))
	}
}

func TestLocalExporter_CreateInstructionsFromMetadata(t *testing.T) {
	exporter := &LocalExporter{}
	
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
}

func TestLocalExporter_CreateInstructionsFromEmptyMetadata(t *testing.T) {
	exporter := &LocalExporter{}
	
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

func TestLocalExporter_ExportWithEmptyLayers(t *testing.T) {
	workDir, err := os.MkdirTemp("", "ossb-local-empty-test-*")
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
	
	exporter := &LocalExporter{}
	
	err = exporter.Export(result, config, workDir)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}
	
	// Should handle empty layers gracefully
	verifyLocalExtraction(t, result.OutputPath)
}

func TestLocalExporter_CopyFileWithPermissions(t *testing.T) {
	workDir, err := os.MkdirTemp("", "ossb-local-copy-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp work dir: %v", err)
	}
	defer os.RemoveAll(workDir)
	
	// Create source file with specific permissions
	srcFile := filepath.Join(workDir, "source.txt")
	content := "test content"
	if err := os.WriteFile(srcFile, []byte(content), 0755); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}
	
	// Copy file
	destFile := filepath.Join(workDir, "dest", "destination.txt")
	exporter := &LocalExporter{}
	
	err = exporter.copyFile(srcFile, destFile, 0755)
	if err != nil {
		t.Fatalf("Failed to copy file: %v", err)
	}
	
	// Verify destination file exists
	if _, err := os.Stat(destFile); os.IsNotExist(err) {
		t.Error("Destination file does not exist")
	}
	
	// Verify content
	destContent, err := os.ReadFile(destFile)
	if err != nil {
		t.Fatalf("Failed to read destination file: %v", err)
	}
	
	if string(destContent) != content {
		t.Errorf("Expected content '%s', got '%s'", content, string(destContent))
	}
	
	// Verify permissions
	destInfo, err := os.Stat(destFile)
	if err != nil {
		t.Fatalf("Failed to stat destination file: %v", err)
	}
	
	expectedMode := os.FileMode(0755)
	if destInfo.Mode().Perm() != expectedMode {
		t.Errorf("Expected permissions %v, got %v", expectedMode, destInfo.Mode().Perm())
	}
}