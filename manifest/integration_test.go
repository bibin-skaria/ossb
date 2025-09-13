package manifest

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/bibin-skaria/ossb/internal/types"
	"github.com/bibin-skaria/ossb/layers"
)

// TestCompleteWorkflow tests the complete manifest generation workflow
func TestCompleteWorkflow(t *testing.T) {
	generator := NewGenerator(nil)

	// Define a realistic Dockerfile
	instructions := []types.DockerfileInstruction{
		{Command: "FROM", Value: "alpine:3.18", Line: 1},
		{Command: "RUN", Value: "apk add --no-cache curl ca-certificates", Line: 2},
		{Command: "WORKDIR", Value: "/app", Line: 3},
		{Command: "COPY", Value: "app.jar /app/", Line: 4},
		{Command: "EXPOSE", Value: "8080", Line: 5},
		{Command: "ENV", Value: "JAVA_OPTS=-Xmx512m", Line: 6},
		{Command: "USER", Value: "1000", Line: 7},
		{Command: "CMD", Value: `["java", "-jar", "app.jar"]`, Line: 8},
	}

	// Define target platforms
	platforms := []types.Platform{
		{OS: "linux", Architecture: "amd64"},
		{OS: "linux", Architecture: "arm64"},
	}

	var platformManifests []PlatformManifest

	// Generate manifests for each platform
	for _, platform := range platforms {
		t.Run("platform_"+platform.String(), func(t *testing.T) {
			// Generate image configuration
			config, err := generator.GenerateImageConfig(instructions, platform)
			if err != nil {
				t.Fatalf("GenerateImageConfig failed: %v", err)
			}

			// Verify config properties
			if config.Architecture != platform.Architecture {
				t.Errorf("Config architecture = %v, want %v", config.Architecture, platform.Architecture)
			}
			if config.OS != platform.OS {
				t.Errorf("Config OS = %v, want %v", config.OS, platform.OS)
			}
			if config.Config.WorkingDir != "/app" {
				t.Errorf("WorkingDir = %v, want /app", config.Config.WorkingDir)
			}
			if config.Config.User != "1000" {
				t.Errorf("User = %v, want 1000", config.Config.User)
			}

			// Check exposed ports
			if _, exists := config.Config.ExposedPorts["8080/tcp"]; !exists {
				t.Error("Port 8080/tcp not found in ExposedPorts")
			}

			// Check environment variables
			found := false
			for _, env := range config.Config.Env {
				if env == "JAVA_OPTS=-Xmx512m" {
					found = true
					break
				}
			}
			if !found {
				t.Error("JAVA_OPTS environment variable not found")
			}

			// Check CMD
			expectedCmd := []string{"java", "-jar", "app.jar"}
			if len(config.Config.Cmd) != len(expectedCmd) {
				t.Errorf("Cmd length = %v, want %v", len(config.Config.Cmd), len(expectedCmd))
			} else {
				for i, cmd := range expectedCmd {
					if config.Config.Cmd[i] != cmd {
						t.Errorf("Cmd[%d] = %v, want %v", i, config.Config.Cmd[i], cmd)
					}
				}
			}

			// Create sample layers that would be generated during build
			testLayers := []*layers.Layer{
				{
					Digest:    "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
					Size:      5242880, // 5MB
					MediaType: layers.MediaTypeImageLayerGzip,
					CreatedBy: "FROM alpine:3.18",
					Comment:   "base layer",
				},
				{
					Digest:    "sha256:2345678901bcdef12345678901bcdef12345678901bcdef12345678901bcdef1",
					Size:      10485760, // 10MB
					MediaType: layers.MediaTypeImageLayerGzip,
					CreatedBy: "RUN apk add --no-cache curl ca-certificates",
					Comment:   "package installation",
				},
				{
					Digest:    "sha256:3456789012cdef123456789012cdef123456789012cdef123456789012cdef12",
					Size:      20971520, // 20MB
					MediaType: layers.MediaTypeImageLayerGzip,
					CreatedBy: "COPY app.jar /app/",
					Comment:   "application files",
				},
			}

			// Add layers to config
			for _, layer := range testLayers {
				err := generator.AddLayerToConfig(config, layer)
				if err != nil {
					t.Fatalf("AddLayerToConfig failed: %v", err)
				}
			}

			// Verify layers were added to config
			if len(config.RootFS.DiffIDs) != len(testLayers) {
				t.Errorf("DiffIDs length = %v, want %v", len(config.RootFS.DiffIDs), len(testLayers))
			}

			// Generate image manifest
			manifest, err := generator.GenerateImageManifest(config, testLayers)
			if err != nil {
				t.Fatalf("GenerateImageManifest failed: %v", err)
			}

			// Verify manifest properties
			if manifest.SchemaVersion != 2 {
				t.Errorf("SchemaVersion = %v, want 2", manifest.SchemaVersion)
			}
			if manifest.MediaType != MediaTypeOCIManifest {
				t.Errorf("MediaType = %v, want %v", manifest.MediaType, MediaTypeOCIManifest)
			}
			if len(manifest.Layers) != len(testLayers) {
				t.Errorf("Manifest layers length = %v, want %v", len(manifest.Layers), len(testLayers))
			}

			// Validate manifest
			err = generator.ValidateImageManifest(manifest)
			if err != nil {
				t.Errorf("Manifest validation failed: %v", err)
			}

			// Calculate and verify digest
			digest, err := generator.CalculateManifestDigest(manifest)
			if err != nil {
				t.Fatalf("CalculateManifestDigest failed: %v", err)
			}
			if !strings.HasPrefix(digest, "sha256:") {
				t.Errorf("Digest format invalid: %v", digest)
			}

			// Serialize manifest
			manifestBytes, err := generator.SerializeManifest(manifest)
			if err != nil {
				t.Fatalf("SerializeManifest failed: %v", err)
			}

			// Verify serialized manifest is valid JSON
			var parsed ImageManifest
			err = json.Unmarshal(manifestBytes, &parsed)
			if err != nil {
				t.Errorf("Serialized manifest is not valid JSON: %v", err)
			}

			// Create platform manifest for manifest list
			platformManifest, err := generator.CreatePlatformManifest(manifest, platform)
			if err != nil {
				t.Fatalf("CreatePlatformManifest failed: %v", err)
			}

			// Verify platform manifest
			if platformManifest.Platform.OS != platform.OS {
				t.Errorf("Platform OS = %v, want %v", platformManifest.Platform.OS, platform.OS)
			}
			if platformManifest.Platform.Architecture != platform.Architecture {
				t.Errorf("Platform Architecture = %v, want %v", platformManifest.Platform.Architecture, platform.Architecture)
			}

			platformManifests = append(platformManifests, *platformManifest)
		})
	}

	// Generate multi-architecture manifest list
	t.Run("manifest_list", func(t *testing.T) {
		manifestList, err := generator.GenerateManifestList(platformManifests)
		if err != nil {
			t.Fatalf("GenerateManifestList failed: %v", err)
		}

		// Verify manifest list properties
		if manifestList.SchemaVersion != 2 {
			t.Errorf("ManifestList SchemaVersion = %v, want 2", manifestList.SchemaVersion)
		}
		if manifestList.MediaType != MediaTypeOCIIndex {
			t.Errorf("ManifestList MediaType = %v, want %v", manifestList.MediaType, MediaTypeOCIIndex)
		}
		if len(manifestList.Manifests) != len(platforms) {
			t.Errorf("ManifestList manifests length = %v, want %v", len(manifestList.Manifests), len(platforms))
		}

		// Verify platforms are sorted correctly
		if manifestList.Manifests[0].Platform.Architecture != "amd64" {
			t.Errorf("First platform architecture = %v, want amd64", manifestList.Manifests[0].Platform.Architecture)
		}
		if manifestList.Manifests[1].Platform.Architecture != "arm64" {
			t.Errorf("Second platform architecture = %v, want arm64", manifestList.Manifests[1].Platform.Architecture)
		}

		// Validate manifest list
		err = generator.ValidateManifestList(manifestList)
		if err != nil {
			t.Errorf("ManifestList validation failed: %v", err)
		}

		// Calculate manifest list digest
		listDigest, err := generator.CalculateManifestListDigest(manifestList)
		if err != nil {
			t.Fatalf("CalculateManifestListDigest failed: %v", err)
		}
		if !strings.HasPrefix(listDigest, "sha256:") {
			t.Errorf("ManifestList digest format invalid: %v", listDigest)
		}

		// Serialize manifest list
		manifestListBytes, err := generator.SerializeManifestList(manifestList)
		if err != nil {
			t.Fatalf("SerializeManifestList failed: %v", err)
		}

		// Verify serialized manifest list is valid JSON
		var parsedList ManifestList
		err = json.Unmarshal(manifestListBytes, &parsedList)
		if err != nil {
			t.Errorf("Serialized manifest list is not valid JSON: %v", err)
		}

		t.Logf("Successfully generated multi-arch manifest list with digest: %s", listDigest)
	})
}

// TestReproducibleBuilds tests that builds with the same timestamp produce identical results
func TestReproducibleBuilds(t *testing.T) {
	fixedTime := time.Date(2023, 12, 1, 12, 0, 0, 0, time.UTC)
	
	options := &GeneratorOptions{
		DefaultUser:       "",
		DefaultWorkingDir: "/",
		DefaultShell:      []string{"/bin/sh", "-c"},
		IncludeHistory:    true,
		Timestamp:         &fixedTime,
	}

	generator1 := NewGenerator(options)
	generator2 := NewGenerator(options)

	instructions := []types.DockerfileInstruction{
		{Command: "FROM", Value: "alpine:latest", Line: 1},
		{Command: "RUN", Value: "echo hello", Line: 2},
	}

	platform := types.Platform{OS: "linux", Architecture: "amd64"}

	// Generate configs with both generators
	config1, err := generator1.GenerateImageConfig(instructions, platform)
	if err != nil {
		t.Fatalf("Generator1 failed: %v", err)
	}

	config2, err := generator2.GenerateImageConfig(instructions, platform)
	if err != nil {
		t.Fatalf("Generator2 failed: %v", err)
	}

	// Serialize both configs
	bytes1, err := generator1.SerializeConfig(config1)
	if err != nil {
		t.Fatalf("Serialize config1 failed: %v", err)
	}

	bytes2, err := generator2.SerializeConfig(config2)
	if err != nil {
		t.Fatalf("Serialize config2 failed: %v", err)
	}

	// Compare serialized configs - they should be identical
	if string(bytes1) != string(bytes2) {
		t.Error("Reproducible builds failed - configs are different")
		t.Logf("Config1: %s", string(bytes1))
		t.Logf("Config2: %s", string(bytes2))
	}

	// Calculate digests - they should be identical
	digest1, err := generator1.CalculateConfigDigest(config1)
	if err != nil {
		t.Fatalf("Calculate digest1 failed: %v", err)
	}

	digest2, err := generator2.CalculateConfigDigest(config2)
	if err != nil {
		t.Fatalf("Calculate digest2 failed: %v", err)
	}

	if digest1 != digest2 {
		t.Errorf("Config digests differ: %s != %s", digest1, digest2)
	}

	t.Logf("Reproducible build test passed - digest: %s", digest1)
}

// TestComplexDockerfile tests manifest generation with a complex Dockerfile
func TestComplexDockerfile(t *testing.T) {
	generator := NewGenerator(nil)

	instructions := []types.DockerfileInstruction{
		{Command: "FROM", Value: "node:18-alpine", Line: 1},
		{Command: "LABEL", Value: `maintainer="test@example.com" version="1.0.0"`, Line: 2},
		{Command: "RUN", Value: "apk add --no-cache git python3 make g++", Line: 3},
		{Command: "WORKDIR", Value: "/usr/src/app", Line: 4},
		{Command: "COPY", Value: "package*.json ./", Line: 5},
		{Command: "RUN", Value: "npm ci --only=production", Line: 6},
		{Command: "COPY", Value: ". .", Line: 7},
		{Command: "RUN", Value: "npm run build", Line: 8},
		{Command: "EXPOSE", Value: "3000 8080", Line: 9},
		{Command: "VOLUME", Value: "/usr/src/app/data", Line: 10},
		{Command: "ENV", Value: "NODE_ENV=production", Line: 11},
		{Command: "ENV", Value: "PORT=3000", Line: 12},
		{Command: "USER", Value: "node", Line: 13},
		{Command: "HEALTHCHECK", Value: "CMD curl -f http://localhost:3000/health || exit 1", Line: 14},
		{Command: "CMD", Value: `["npm", "start"]`, Line: 15},
	}

	platform := types.Platform{OS: "linux", Architecture: "amd64"}

	config, err := generator.GenerateImageConfig(instructions, platform)
	if err != nil {
		t.Fatalf("GenerateImageConfig failed: %v", err)
	}

	// Verify complex configuration
	if config.Config.WorkingDir != "/usr/src/app" {
		t.Errorf("WorkingDir = %v, want /usr/src/app", config.Config.WorkingDir)
	}

	if config.Config.User != "node" {
		t.Errorf("User = %v, want node", config.Config.User)
	}

	// Check labels
	if config.Config.Labels["maintainer"] != "test@example.com" {
		t.Errorf("Label maintainer = %v, want test@example.com", config.Config.Labels["maintainer"])
	}
	if config.Config.Labels["version"] != "1.0.0" {
		t.Errorf("Label version = %v, want 1.0.0", config.Config.Labels["version"])
	}

	// Check environment variables
	envVars := map[string]bool{
		"NODE_ENV=production": false,
		"PORT=3000":          false,
	}
	for _, env := range config.Config.Env {
		if _, exists := envVars[env]; exists {
			envVars[env] = true
		}
	}
	for env, found := range envVars {
		if !found {
			t.Errorf("Environment variable %s not found", env)
		}
	}

	// Check exposed ports
	expectedPorts := []string{"3000/tcp", "8080/tcp"}
	for _, port := range expectedPorts {
		if _, exists := config.Config.ExposedPorts[port]; !exists {
			t.Errorf("Port %s not found in ExposedPorts", port)
		}
	}

	// Check volumes
	if _, exists := config.Config.Volumes["/usr/src/app/data"]; !exists {
		t.Error("Volume /usr/src/app/data not found")
	}

	// Check healthcheck
	if config.Config.Healthcheck == nil {
		t.Error("Healthcheck not configured")
	} else {
		if len(config.Config.Healthcheck.Test) == 0 {
			t.Error("Healthcheck test not configured")
		}
	}

	// Check CMD
	expectedCmd := []string{"npm", "start"}
	if len(config.Config.Cmd) != len(expectedCmd) {
		t.Errorf("Cmd length = %v, want %v", len(config.Config.Cmd), len(expectedCmd))
	} else {
		for i, cmd := range expectedCmd {
			if config.Config.Cmd[i] != cmd {
				t.Errorf("Cmd[%d] = %v, want %v", i, config.Config.Cmd[i], cmd)
			}
		}
	}

	// Validate the complex configuration
	err = generator.ValidateImageConfig(config)
	if err != nil {
		t.Errorf("Complex config validation failed: %v", err)
	}

	t.Log("Complex Dockerfile test passed")
}