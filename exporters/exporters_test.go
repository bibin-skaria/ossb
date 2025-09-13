package exporters

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/bibin-skaria/ossb/internal/types"
)

func TestExporterRegistry(t *testing.T) {
	// Test that all exporters are registered
	expectedExporters := []string{"image", "multiarch", "tar", "local"}
	
	registeredExporters := ListExporters()
	
	if len(registeredExporters) < len(expectedExporters) {
		t.Errorf("Expected at least %d exporters, got %d", len(expectedExporters), len(registeredExporters))
	}
	
	for _, expected := range expectedExporters {
		found := false
		for _, registered := range registeredExporters {
			if registered == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected exporter %s not found in registered exporters", expected)
		}
	}
}

func TestGetExporter(t *testing.T) {
	tests := []struct {
		name        string
		exporterName string
		expectError bool
	}{
		{
			name:         "get image exporter",
			exporterName: "image",
			expectError:  false,
		},
		{
			name:         "get multiarch exporter",
			exporterName: "multiarch",
			expectError:  false,
		},
		{
			name:         "get tar exporter",
			exporterName: "tar",
			expectError:  false,
		},
		{
			name:         "get local exporter",
			exporterName: "local",
			expectError:  false,
		},
		{
			name:         "get non-existent exporter",
			exporterName: "nonexistent",
			expectError:  true,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exporter, err := GetExporter(tt.exporterName)
			
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for exporter %s, got nil", tt.exporterName)
				}
				if exporter != nil {
					t.Errorf("Expected nil exporter for %s, got %T", tt.exporterName, exporter)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for exporter %s: %v", tt.exporterName, err)
				}
				if exporter == nil {
					t.Errorf("Expected exporter for %s, got nil", tt.exporterName)
				}
			}
		})
	}
}

func createTestBuildResult() *types.BuildResult {
	return &types.BuildResult{
		Success:    true,
		Operations: 5,
		CacheHits:  2,
		Duration:   "30s",
		Metadata: map[string]string{
			"workdir":    "/app",
			"user":       "appuser",
			"cmd":        "echo hello",
			"entrypoint": "/bin/sh",
			"expose":     "8080",
			"volume":     "/data",
			"label.version": "1.0.0",
		},
	}
}

func createTestBuildConfig() *types.BuildConfig {
	return &types.BuildConfig{
		Context:    "/tmp/build",
		Dockerfile: "Dockerfile",
		Tags:       []string{"test:latest"},
		Output:     "image",
		Platforms: []types.Platform{
			{OS: "linux", Architecture: "amd64"},
		},
	}
}

func createTestMultiArchBuildResult() *types.BuildResult {
	return &types.BuildResult{
		Success:    true,
		Operations: 10,
		CacheHits:  4,
		Duration:   "60s",
		MultiArch:  true,
		PlatformResults: map[string]*types.PlatformResult{
			"linux/amd64": {
				Platform:   types.Platform{OS: "linux", Architecture: "amd64"},
				Success:    true,
				ImageID:    "sha256:abc123",
				ManifestID: "sha256:def456",
				Size:       1024000,
				CacheHits:  2,
			},
			"linux/arm64": {
				Platform:   types.Platform{OS: "linux", Architecture: "arm64"},
				Success:    true,
				ImageID:    "sha256:ghi789",
				ManifestID: "sha256:jkl012",
				Size:       1048576,
				CacheHits:  2,
			},
		},
		Metadata: map[string]string{
			"workdir": "/app",
			"cmd":     "echo hello",
		},
	}
}

func createTestMultiArchBuildConfig() *types.BuildConfig {
	return &types.BuildConfig{
		Context:    "/tmp/build",
		Dockerfile: "Dockerfile",
		Tags:       []string{"test:multiarch"},
		Output:     "multiarch",
		Platforms: []types.Platform{
			{OS: "linux", Architecture: "amd64"},
			{OS: "linux", Architecture: "arm64"},
		},
	}
}

func setupTestWorkDir(t *testing.T) string {
	workDir, err := os.MkdirTemp("", "ossb-exporter-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp work dir: %v", err)
	}
	
	// Create layers directory structure
	layersDir := filepath.Join(workDir, "layers")
	if err := os.MkdirAll(layersDir, 0755); err != nil {
		t.Fatalf("Failed to create layers dir: %v", err)
	}
	
	// Create test layer
	layer1Dir := filepath.Join(layersDir, "layer1")
	if err := os.MkdirAll(layer1Dir, 0755); err != nil {
		t.Fatalf("Failed to create layer1 dir: %v", err)
	}
	
	// Add test files to layer
	testFile := filepath.Join(layer1Dir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	
	testDir := filepath.Join(layer1Dir, "testdir")
	if err := os.MkdirAll(testDir, 0755); err != nil {
		t.Fatalf("Failed to create test dir: %v", err)
	}
	
	nestedFile := filepath.Join(testDir, "nested.txt")
	if err := os.WriteFile(nestedFile, []byte("nested content"), 0644); err != nil {
		t.Fatalf("Failed to create nested file: %v", err)
	}
	
	return workDir
}

func setupTestMultiArchWorkDir(t *testing.T) string {
	workDir, err := os.MkdirTemp("", "ossb-multiarch-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp work dir: %v", err)
	}
	
	// Create layers directory structure for multiple platforms
	platforms := []string{"linux/amd64", "linux/arm64"}
	
	for _, platform := range platforms {
		platformDir := filepath.Join(workDir, "layers", platform)
		if err := os.MkdirAll(platformDir, 0755); err != nil {
			t.Fatalf("Failed to create platform dir %s: %v", platform, err)
		}
		
		// Create test layer for this platform
		layer1Dir := filepath.Join(platformDir, "layer1")
		if err := os.MkdirAll(layer1Dir, 0755); err != nil {
			t.Fatalf("Failed to create layer1 dir for %s: %v", platform, err)
		}
		
		// Add platform-specific test files
		testFile := filepath.Join(layer1Dir, "platform.txt")
		content := "platform: " + platform
		if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create platform file for %s: %v", platform, err)
		}
	}
	
	return workDir
}

func verifyOCILayout(t *testing.T, outputPath string) {
	// Check for OCI layout file
	layoutPath := filepath.Join(outputPath, "oci-layout")
	if _, err := os.Stat(layoutPath); os.IsNotExist(err) {
		t.Errorf("OCI layout file not found at %s", layoutPath)
		return
	}
	
	// Verify layout content
	layoutData, err := os.ReadFile(layoutPath)
	if err != nil {
		t.Errorf("Failed to read OCI layout: %v", err)
		return
	}
	
	var layout map[string]interface{}
	if err := json.Unmarshal(layoutData, &layout); err != nil {
		t.Errorf("Failed to parse OCI layout: %v", err)
		return
	}
	
	if version, ok := layout["imageLayoutVersion"]; !ok || version != "1.0.0" {
		t.Errorf("Invalid OCI layout version: %v", version)
	}
	
	// Check for index.json
	indexPath := filepath.Join(outputPath, "index.json")
	if _, err := os.Stat(indexPath); os.IsNotExist(err) {
		t.Errorf("Index file not found at %s", indexPath)
		return
	}
	
	// Check for blobs directory
	blobsPath := filepath.Join(outputPath, "blobs", "sha256")
	if _, err := os.Stat(blobsPath); os.IsNotExist(err) {
		t.Errorf("Blobs directory not found at %s", blobsPath)
		return
	}
	
	// Verify blobs directory has content
	entries, err := os.ReadDir(blobsPath)
	if err != nil {
		t.Errorf("Failed to read blobs directory: %v", err)
		return
	}
	
	if len(entries) == 0 {
		t.Errorf("Blobs directory is empty")
	}
}

func verifyTarContent(t *testing.T, tarPath string) {
	// Check that tar file exists and has reasonable size
	info, err := os.Stat(tarPath)
	if err != nil {
		t.Errorf("Tar file not found: %v", err)
		return
	}
	
	if info.Size() == 0 {
		t.Errorf("Tar file is empty")
	}
	
	// TODO: Add more detailed tar content verification
	// This would require implementing tar reading logic
}

func verifyLocalExtraction(t *testing.T, outputPath string) {
	// Check for rootfs directory
	rootfsPath := filepath.Join(outputPath, "rootfs")
	if _, err := os.Stat(rootfsPath); os.IsNotExist(err) {
		t.Errorf("Rootfs directory not found at %s", rootfsPath)
		return
	}
	
	// Check for metadata files
	metadataFiles := []string{"config.json", "manifest.json", "ossb-metadata.json", "extraction-manifest.json"}
	for _, file := range metadataFiles {
		filePath := filepath.Join(outputPath, file)
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			t.Errorf("Metadata file %s not found at %s", file, filePath)
		}
	}
	
	// Verify extraction manifest content
	manifestPath := filepath.Join(outputPath, "extraction-manifest.json")
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Errorf("Failed to read extraction manifest: %v", err)
		return
	}
	
	var manifest map[string]interface{}
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Errorf("Failed to parse extraction manifest: %v", err)
		return
	}
	
	if version, ok := manifest["version"]; !ok || version != "1.0" {
		t.Errorf("Invalid extraction manifest version: %v", version)
	}
	
	if format, ok := manifest["format"]; !ok || format != "local-filesystem" {
		t.Errorf("Invalid extraction manifest format: %v", format)
	}
}

func TestExporterCompatibility(t *testing.T) {
	// Test that all exporters can handle the same build result
	workDir := setupTestWorkDir(t)
	defer os.RemoveAll(workDir)
	
	result := createTestBuildResult()
	config := createTestBuildConfig()
	
	exporters := []string{"image", "tar", "local"}
	
	for _, exporterName := range exporters {
		t.Run(exporterName, func(t *testing.T) {
			exporter, err := GetExporter(exporterName)
			if err != nil {
				t.Fatalf("Failed to get exporter %s: %v", exporterName, err)
			}
			
			// Create separate work directory for each exporter
			exporterWorkDir := filepath.Join(workDir, exporterName)
			if err := os.MkdirAll(exporterWorkDir, 0755); err != nil {
				t.Fatalf("Failed to create exporter work dir: %v", err)
			}
			
			// Copy layers to exporter work dir
			srcLayersDir := filepath.Join(workDir, "layers")
			destLayersDir := filepath.Join(exporterWorkDir, "layers")
			if err := copyDir(srcLayersDir, destLayersDir); err != nil {
				t.Fatalf("Failed to copy layers: %v", err)
			}
			
			// Export
			if err := exporter.Export(result, config, exporterWorkDir); err != nil {
				t.Errorf("Export failed for %s: %v", exporterName, err)
			}
			
			// Verify output path is set
			if result.OutputPath == "" {
				t.Errorf("Output path not set for %s", exporterName)
			}
			
			// Verify output exists
			if _, err := os.Stat(result.OutputPath); os.IsNotExist(err) {
				t.Errorf("Output path does not exist for %s: %s", exporterName, result.OutputPath)
			}
		})
	}
}

// Helper function to copy directories
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		
		dstPath := filepath.Join(dst, relPath)
		
		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}
		
		return copyFile(path, dstPath, info.Mode())
	})
}

// Helper function to copy files
func copyFile(src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()
	
	dstFile, err := os.OpenFile(dst, os.O_RDWR|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer dstFile.Close()
	
	_, err = srcFile.WriteTo(dstFile)
	return err
}