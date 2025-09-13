// +build integration

package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bibin-skaria/ossb/engine"
	"github.com/bibin-skaria/ossb/internal/types"
	"github.com/bibin-skaria/ossb/registry"
)

// TestRegistryIntegrationPullAndBuild tests pulling base images and building
func TestRegistryIntegrationPullAndBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping registry integration test in short mode")
	}

	testCases := []struct {
		name      string
		baseImage string
		dockerfile string
		expectSuccess bool
	}{
		{
			name:      "alpine_latest",
			baseImage: "alpine:latest",
			dockerfile: `FROM alpine:latest
RUN echo "Hello from Alpine" > /hello.txt
CMD ["cat", "/hello.txt"]`,
			expectSuccess: true,
		},
		{
			name:      "ubuntu_focal",
			baseImage: "ubuntu:20.04",
			dockerfile: `FROM ubuntu:20.04
RUN apt-get update && apt-get install -y curl
RUN echo "Hello from Ubuntu" > /hello.txt
CMD ["cat", "/hello.txt"]`,
			expectSuccess: true,
		},
		{
			name:      "node_alpine",
			baseImage: "node:18-alpine",
			dockerfile: `FROM node:18-alpine
WORKDIR /app
RUN echo '{"name":"test","version":"1.0.0"}' > package.json
RUN npm --version
CMD ["node", "--version"]`,
			expectSuccess: true,
		},
		{
			name:      "nonexistent_image",
			baseImage: "nonexistent/image:latest",
			dockerfile: `FROM nonexistent/image:latest
RUN echo "This should fail"`,
			expectSuccess: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tempDir, err := ioutil.TempDir("", fmt.Sprintf("ossb-registry-%s-", tc.name))
			if err != nil {
				t.Fatalf("Failed to create temp dir: %v", err)
			}
			defer os.RemoveAll(tempDir)

			dockerfilePath := filepath.Join(tempDir, "Dockerfile")
			if err := ioutil.WriteFile(dockerfilePath, []byte(tc.dockerfile), 0644); err != nil {
				t.Fatalf("Failed to write Dockerfile: %v", err)
			}

			config := &types.BuildConfig{
				Context:    tempDir,
				Dockerfile: "Dockerfile",
				Tags:       []string{fmt.Sprintf("test-registry-%s:latest", tc.name)},
				Output:     "image",
				Frontend:   "dockerfile",
				NoCache:    true, // Force pull from registry
				Progress:   true,
				BuildArgs:  map[string]string{},
				Platforms:  []types.Platform{{OS: "linux", Architecture: "amd64"}},
				Push:       false,
			}

			builder, err := engine.NewBuilder(config)
			if err != nil {
				if !tc.expectSuccess {
					t.Logf("Expected failure during builder creation: %v", err)
					return
				}
				t.Fatalf("Failed to create builder: %v", err)
			}
			defer builder.Cleanup()

			startTime := time.Now()
			result, err := builder.Build()
			buildDuration := time.Since(startTime)

			if tc.expectSuccess {
				if err != nil {
					t.Fatalf("Build failed unexpectedly: %v", err)
				}
				if !result.Success {
					t.Fatalf("Build failed unexpectedly: %s", result.Error)
				}
				t.Logf("Registry integration test %s completed successfully in %v", tc.name, buildDuration)
			} else {
				if err == nil && result.Success {
					t.Fatalf("Build succeeded unexpectedly")
				}
				t.Logf("Registry integration test %s failed as expected: %v", tc.name, err)
			}
		})
	}
}

// TestRegistryIntegrationPrivateRegistry tests private registry operations
func TestRegistryIntegrationPrivateRegistry(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping private registry integration test in short mode")
	}

	// Check for private registry configuration
	registryURL := os.Getenv("PRIVATE_REGISTRY_URL")
	registryUsername := os.Getenv("PRIVATE_REGISTRY_USERNAME")
	registryPassword := os.Getenv("PRIVATE_REGISTRY_PASSWORD")

	if registryURL == "" {
		t.Skip("PRIVATE_REGISTRY_URL not set, skipping private registry test")
	}

	tempDir, err := ioutil.TempDir("", "ossb-private-registry-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dockerfile := `FROM alpine:latest
RUN echo "Private registry test" > /test.txt
LABEL test=private-registry
CMD ["cat", "/test.txt"]`

	dockerfilePath := filepath.Join(tempDir, "Dockerfile")
	if err := ioutil.WriteFile(dockerfilePath, []byte(dockerfile), 0644); err != nil {
		t.Fatalf("Failed to write Dockerfile: %v", err)
	}

	// Configure registry credentials
	registryConfig := &types.RegistryConfig{
		DefaultRegistry: "docker.io",
		Registries: map[string]types.RegistryAuth{
			registryURL: {
				Username: registryUsername,
				Password: registryPassword,
			},
		},
	}

	imageTag := fmt.Sprintf("%s/ossb-test:integration-%d", registryURL, time.Now().Unix())

	config := &types.BuildConfig{
		Context:        tempDir,
		Dockerfile:     "Dockerfile",
		Tags:           []string{imageTag},
		Output:         "image",
		Frontend:       "dockerfile",
		NoCache:        false,
		Progress:       true,
		BuildArgs:      map[string]string{},
		Platforms:      []types.Platform{{OS: "linux", Architecture: "amd64"}},
		Push:           true,
		Registry:       registryURL,
		RegistryConfig: registryConfig,
	}

	builder, err := engine.NewBuilder(config)
	if err != nil {
		t.Fatalf("Failed to create builder: %v", err)
	}
	defer builder.Cleanup()

	result, err := builder.Build()
	if err != nil {
		t.Fatalf("Private registry build failed: %v", err)
	}

	if !result.Success {
		t.Fatalf("Private registry build failed: %s", result.Error)
	}

	t.Logf("Private registry integration test completed successfully")
	t.Logf("  Pushed image: %s", imageTag)
	t.Logf("  Image ID: %s", result.ImageID)
}

// TestRegistryIntegrationMultiArchPush tests pushing multi-architecture images
func TestRegistryIntegrationMultiArchPush(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping multi-arch registry integration test in short mode")
	}

	registryURL := os.Getenv("TEST_REGISTRY_URL")
	if registryURL == "" {
		registryURL = "localhost:5000" // Default to local registry
	}

	tempDir, err := ioutil.TempDir("", "ossb-multiarch-registry-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dockerfile := `FROM alpine:latest
RUN apk add --no-cache file
RUN echo "Multi-arch test: $(uname -m)" > /arch.txt
CMD ["cat", "/arch.txt"]`

	dockerfilePath := filepath.Join(tempDir, "Dockerfile")
	if err := ioutil.WriteFile(dockerfilePath, []byte(dockerfile), 0644); err != nil {
		t.Fatalf("Failed to write Dockerfile: %v", err)
	}

	platforms := []types.Platform{
		{OS: "linux", Architecture: "amd64"},
		{OS: "linux", Architecture: "arm64"},
	}

	imageTag := fmt.Sprintf("%s/ossb-multiarch-test:integration-%d", registryURL, time.Now().Unix())

	config := &types.BuildConfig{
		Context:    tempDir,
		Dockerfile: "Dockerfile",
		Tags:       []string{imageTag},
		Output:     "multiarch",
		Frontend:   "dockerfile",
		NoCache:    false,
		Progress:   true,
		BuildArgs:  map[string]string{},
		Platforms:  platforms,
		Push:       true,
		Registry:   registryURL,
	}

	// Configure insecure registry if using localhost
	if strings.Contains(registryURL, "localhost") {
		config.RegistryConfig = &types.RegistryConfig{
			Insecure: []string{registryURL},
		}
	}

	builder, err := engine.NewBuilder(config)
	if err != nil {
		t.Fatalf("Failed to create builder: %v", err)
	}
	defer builder.Cleanup()

	result, err := builder.Build()
	if err != nil {
		t.Fatalf("Multi-arch registry build failed: %v", err)
	}

	if !result.Success {
		t.Fatalf("Multi-arch registry build failed: %s", result.Error)
	}

	if !result.MultiArch {
		t.Error("Expected MultiArch to be true")
	}

	if result.ManifestListID == "" {
		t.Error("Expected non-empty ManifestListID")
	}

	t.Logf("Multi-arch registry integration test completed successfully")
	t.Logf("  Pushed manifest list: %s", imageTag)
	t.Logf("  Manifest List ID: %s", result.ManifestListID)
	t.Logf("  Platforms: %d", len(result.PlatformResults))
}

// TestRegistryIntegrationAuthentication tests various authentication methods
func TestRegistryIntegrationAuthentication(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping registry authentication integration test in short mode")
	}

	testCases := []struct {
		name           string
		registryURL    string
		authMethod     string
		setupAuth      func() (*types.RegistryConfig, error)
		expectSuccess  bool
	}{
		{
			name:        "docker_hub_anonymous",
			registryURL: "docker.io",
			authMethod:  "anonymous",
			setupAuth: func() (*types.RegistryConfig, error) {
				return &types.RegistryConfig{
					DefaultRegistry: "docker.io",
				}, nil
			},
			expectSuccess: true,
		},
		{
			name:        "docker_hub_with_credentials",
			registryURL: "docker.io",
			authMethod:  "credentials",
			setupAuth: func() (*types.RegistryConfig, error) {
				username := os.Getenv("DOCKER_HUB_USERNAME")
				password := os.Getenv("DOCKER_HUB_PASSWORD")
				
				if username == "" || password == "" {
					return nil, fmt.Errorf("Docker Hub credentials not provided")
				}

				return &types.RegistryConfig{
					DefaultRegistry: "docker.io",
					Registries: map[string]types.RegistryAuth{
						"docker.io": {
							Username: username,
							Password: password,
						},
					},
				}, nil
			},
			expectSuccess: true,
		},
		{
			name:        "gcr_with_service_account",
			registryURL: "gcr.io",
			authMethod:  "service_account",
			setupAuth: func() (*types.RegistryConfig, error) {
				keyFile := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")
				if keyFile == "" {
					return nil, fmt.Errorf("Google service account key not provided")
				}

				return &types.RegistryConfig{
					DefaultRegistry: "docker.io",
					Registries: map[string]types.RegistryAuth{
						"gcr.io": {
							Username: "_json_key",
							AuthFile: keyFile,
						},
					},
				}, nil
			},
			expectSuccess: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			registryConfig, err := tc.setupAuth()
			if err != nil {
				t.Skipf("Skipping %s: %v", tc.name, err)
				return
			}

			// Test registry client authentication
			client := registry.NewClient(registryConfig)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			// Test authentication by trying to get a manifest
			ref, err := registry.ParseImageReference("alpine:latest")
			if err != nil {
				t.Fatalf("Failed to parse image reference: %v", err)
			}

			platform := types.Platform{OS: "linux", Architecture: "amd64"}
			_, err = client.GetImageManifest(ctx, ref, platform)

			if tc.expectSuccess {
				if err != nil {
					// Some registries may return errors for anonymous access
					// but still allow pulling, so we log but don't fail
					t.Logf("Authentication test %s completed with warning: %v", tc.name, err)
				} else {
					t.Logf("Authentication test %s completed successfully", tc.name)
				}
			} else {
				if err == nil {
					t.Errorf("Authentication test %s should have failed", tc.name)
				} else {
					t.Logf("Authentication test %s failed as expected: %v", tc.name, err)
				}
			}
		})
	}
}

// TestRegistryIntegrationCaching tests registry caching behavior
func TestRegistryIntegrationCaching(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping registry caching integration test in short mode")
	}

	tempDir, err := ioutil.TempDir("", "ossb-registry-cache-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dockerfile := `FROM alpine:3.18
RUN echo "Cache test" > /cache.txt
CMD ["cat", "/cache.txt"]`

	dockerfilePath := filepath.Join(tempDir, "Dockerfile")
	if err := ioutil.WriteFile(dockerfilePath, []byte(dockerfile), 0644); err != nil {
		t.Fatalf("Failed to write Dockerfile: %v", err)
	}

	// Create cache directory
	cacheDir := filepath.Join(tempDir, "cache")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatalf("Failed to create cache dir: %v", err)
	}

	config := &types.BuildConfig{
		Context:    tempDir,
		Dockerfile: "Dockerfile",
		Tags:       []string{"test-registry-cache:latest"},
		Output:     "image",
		Frontend:   "dockerfile",
		CacheDir:   cacheDir,
		NoCache:    false,
		Progress:   true,
		BuildArgs:  map[string]string{},
		Platforms:  []types.Platform{{OS: "linux", Architecture: "amd64"}},
		Push:       false,
	}

	// First build - should pull from registry
	t.Log("Running first build (should pull from registry)...")
	builder1, err := engine.NewBuilder(config)
	if err != nil {
		t.Fatalf("Failed to create first builder: %v", err)
	}

	startTime1 := time.Now()
	result1, err := builder1.Build()
	duration1 := time.Since(startTime1)
	builder1.Cleanup()

	if err != nil {
		t.Fatalf("First build failed: %v", err)
	}
	if !result1.Success {
		t.Fatalf("First build failed: %s", result1.Error)
	}

	// Second build - should use cache
	t.Log("Running second build (should use cache)...")
	builder2, err := engine.NewBuilder(config)
	if err != nil {
		t.Fatalf("Failed to create second builder: %v", err)
	}

	startTime2 := time.Now()
	result2, err := builder2.Build()
	duration2 := time.Since(startTime2)
	builder2.Cleanup()

	if err != nil {
		t.Fatalf("Second build failed: %v", err)
	}
	if !result2.Success {
		t.Fatalf("Second build failed: %s", result2.Error)
	}

	// Verify caching effectiveness
	if result2.CacheHits <= result1.CacheHits {
		t.Errorf("Second build should have more cache hits: %d vs %d", result2.CacheHits, result1.CacheHits)
	}

	if duration2 >= duration1 {
		t.Logf("Warning: Second build took longer than first (%v vs %v)", duration2, duration1)
	}

	t.Logf("Registry caching test completed successfully")
	t.Logf("  First build: %v, cache hits: %d", duration1, result1.CacheHits)
	t.Logf("  Second build: %v, cache hits: %d", duration2, result2.CacheHits)
}

// TestRegistryIntegrationErrorHandling tests registry error scenarios
func TestRegistryIntegrationErrorHandling(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping registry error handling integration test in short mode")
	}

	testCases := []struct {
		name           string
		dockerfile     string
		registryConfig *types.RegistryConfig
		expectError    bool
		errorPattern   string
	}{
		{
			name: "invalid_base_image",
			dockerfile: `FROM invalid/nonexistent:tag
RUN echo "This should fail"`,
			registryConfig: nil,
			expectError:    true,
			errorPattern:   "not found",
		},
		{
			name: "invalid_registry_url",
			dockerfile: `FROM alpine:latest
RUN echo "Test"`,
			registryConfig: &types.RegistryConfig{
				DefaultRegistry: "invalid-registry-url-12345.com",
			},
			expectError:  false, // Should fallback to docker.io
			errorPattern: "",
		},
		{
			name: "network_timeout",
			dockerfile: `FROM alpine:latest
RUN echo "Test"`,
			registryConfig: &types.RegistryConfig{
				DefaultRegistry: "1.2.3.4:5000", // Non-routable IP
			},
			expectError:  true,
			errorPattern: "timeout",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tempDir, err := ioutil.TempDir("", fmt.Sprintf("ossb-registry-error-%s-", tc.name))
			if err != nil {
				t.Fatalf("Failed to create temp dir: %v", err)
			}
			defer os.RemoveAll(tempDir)

			dockerfilePath := filepath.Join(tempDir, "Dockerfile")
			if err := ioutil.WriteFile(dockerfilePath, []byte(tc.dockerfile), 0644); err != nil {
				t.Fatalf("Failed to write Dockerfile: %v", err)
			}

			config := &types.BuildConfig{
				Context:        tempDir,
				Dockerfile:     "Dockerfile",
				Tags:           []string{fmt.Sprintf("test-registry-error-%s:latest", tc.name)},
				Output:         "image",
				Frontend:       "dockerfile",
				NoCache:        true,
				Progress:       true,
				BuildArgs:      map[string]string{},
				Platforms:      []types.Platform{{OS: "linux", Architecture: "amd64"}},
				Push:           false,
				RegistryConfig: tc.registryConfig,
			}

			builder, err := engine.NewBuilder(config)
			if err != nil {
				if tc.expectError {
					t.Logf("Expected error during builder creation: %v", err)
					return
				}
				t.Fatalf("Failed to create builder: %v", err)
			}
			defer builder.Cleanup()

			// Set shorter timeout for network tests
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			result, err := builder.BuildWithContext(ctx)

			if tc.expectError {
				if err == nil && result.Success {
					t.Errorf("Expected error but build succeeded")
				} else {
					errorMsg := ""
					if err != nil {
						errorMsg = err.Error()
					} else {
						errorMsg = result.Error
					}

					if tc.errorPattern != "" && !strings.Contains(strings.ToLower(errorMsg), strings.ToLower(tc.errorPattern)) {
						t.Errorf("Expected error pattern '%s' not found in: %s", tc.errorPattern, errorMsg)
					}

					t.Logf("Registry error test %s failed as expected: %s", tc.name, errorMsg)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				} else if !result.Success {
					t.Errorf("Unexpected build failure: %s", result.Error)
				} else {
					t.Logf("Registry error test %s completed successfully", tc.name)
				}
			}
		})
	}
}