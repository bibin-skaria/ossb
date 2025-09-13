package exporters

import (
	"os"
	"testing"

	"github.com/bibin-skaria/ossb/internal/types"
)

func TestMultiArchExporter_Export(t *testing.T) {
	workDir := setupTestMultiArchWorkDir(t)
	defer os.RemoveAll(workDir)
	
	result := createTestMultiArchBuildResult()
	config := createTestMultiArchBuildConfig()
	
	exporter := &MultiArchExporter{}
	
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
	
	// Verify manifest list ID is set
	if result.ManifestListID == "" {
		t.Error("Manifest list ID not set")
	}
	
	// Verify image ID includes manifest list digest
	if result.ImageID == "" {
		t.Error("Image ID not set")
	}
}

func TestMultiArchExporter_ExportSingleArch(t *testing.T) {
	workDir := setupTestWorkDir(t)
	defer os.RemoveAll(workDir)
	
	// Create single-arch result (should delegate to ImageExporter)
	result := createTestBuildResult()
	result.MultiArch = false
	result.PlatformResults = map[string]*types.PlatformResult{
		"linux/amd64": {
			Platform:  types.Platform{OS: "linux", Architecture: "amd64"},
			Success:   true,
			ImageID:   "sha256:abc123",
			CacheHits: 2,
		},
	}
	
	config := createTestBuildConfig()
	
	exporter := &MultiArchExporter{}
	
	err := exporter.Export(result, config, workDir)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}
	
	// Should delegate to ImageExporter
	verifyOCILayout(t, result.OutputPath)
}

func TestMultiArchExporter_ExportWithFailedPlatforms(t *testing.T) {
	workDir := setupTestMultiArchWorkDir(t)
	defer os.RemoveAll(workDir)
	
	result := createTestMultiArchBuildResult()
	// Mark one platform as failed
	result.PlatformResults["linux/arm64"].Success = false
	result.PlatformResults["linux/arm64"].Error = "build failed"
	
	config := createTestMultiArchBuildConfig()
	
	exporter := &MultiArchExporter{}
	
	err := exporter.Export(result, config, workDir)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}
	
	// Should still work with one successful platform
	verifyOCILayout(t, result.OutputPath)
}

func TestMultiArchExporter_ExportAllPlatformsFailed(t *testing.T) {
	workDir := setupTestMultiArchWorkDir(t)
	defer os.RemoveAll(workDir)
	
	result := createTestMultiArchBuildResult()
	// Mark all platforms as failed
	for _, platformResult := range result.PlatformResults {
		platformResult.Success = false
		platformResult.Error = "build failed"
	}
	
	config := createTestMultiArchBuildConfig()
	
	exporter := &MultiArchExporter{}
	
	err := exporter.Export(result, config, workDir)
	if err == nil {
		t.Error("Expected error when all platforms failed, got nil")
	}
	
	expectedError := "no successful platform builds to export"
	if err.Error() != expectedError {
		t.Errorf("Expected error '%s', got '%s'", expectedError, err.Error())
	}
}

func TestMultiArchExporter_CreatePlatformInstructions(t *testing.T) {
	exporter := &MultiArchExporter{}
	
	platformResult := &types.PlatformResult{
		Platform: types.Platform{OS: "linux", Architecture: "arm64", Variant: "v8"},
		Success:  true,
		ImageID:  "sha256:test123",
	}
	
	platform := types.Platform{OS: "linux", Architecture: "arm64", Variant: "v8"}
	
	instructions := exporter.createPlatformInstructions(platformResult, platform)
	
	if len(instructions) == 0 {
		t.Error("No instructions created")
	}
	
	// Verify FROM instruction is first
	if instructions[0].Command != "FROM" {
		t.Errorf("Expected FROM instruction first, got %s", instructions[0].Command)
	}
	
	// Verify platform-specific labels are added
	labelCount := 0
	for _, instruction := range instructions {
		if instruction.Command == "LABEL" {
			labelCount++
		}
	}
	
	expectedLabels := 4 // platform, architecture, os, variant
	if labelCount != expectedLabels {
		t.Errorf("Expected %d LABEL instructions, got %d", expectedLabels, labelCount)
	}
}

func TestMultiArchExporter_CreatePlatformInstructionsWithoutVariant(t *testing.T) {
	exporter := &MultiArchExporter{}
	
	platformResult := &types.PlatformResult{
		Platform: types.Platform{OS: "linux", Architecture: "amd64"},
		Success:  true,
		ImageID:  "sha256:test123",
	}
	
	platform := types.Platform{OS: "linux", Architecture: "amd64"}
	
	instructions := exporter.createPlatformInstructions(platformResult, platform)
	
	// Count LABEL instructions
	labelCount := 0
	for _, instruction := range instructions {
		if instruction.Command == "LABEL" {
			labelCount++
		}
	}
	
	expectedLabels := 3 // platform, architecture, os (no variant)
	if labelCount != expectedLabels {
		t.Errorf("Expected %d LABEL instructions, got %d", expectedLabels, labelCount)
	}
}

func TestMultiArchExporter_GetImageReference(t *testing.T) {
	exporter := &MultiArchExporter{}
	
	// Test with tags
	config := &types.BuildConfig{
		Tags: []string{"myimage:multiarch", "myimage:latest"},
	}
	ref := exporter.getImageReference(config)
	if ref != "myimage:multiarch" {
		t.Errorf("Expected 'myimage:multiarch', got '%s'", ref)
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

func TestMultiArchExporter_ExportWithPushConfig(t *testing.T) {
	workDir := setupTestMultiArchWorkDir(t)
	defer os.RemoveAll(workDir)
	
	result := createTestMultiArchBuildResult()
	config := createTestMultiArchBuildConfig()
	config.Push = true
	config.Registry = "registry.example.com"
	
	exporter := &MultiArchExporter{}
	
	// This should not fail even though push will fail (no real registry)
	// The export part should still work
	err := exporter.Export(result, config, workDir)
	
	// We expect this to fail at the push stage, but export should have worked
	if err != nil && !containsString(err.Error(), "failed to push") {
		t.Fatalf("Unexpected error: %v", err)
	}
	
	// Verify export worked even if push failed
	if result.OutputPath != "" {
		verifyOCILayout(t, result.OutputPath)
	}
}

// Helper function to check if string contains substring
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && 
		(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || 
		 findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}