// +build integration

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bibin-skaria/ossb/engine"
	"github.com/bibin-skaria/ossb/internal/types"
	"github.com/bibin-skaria/ossb/k8s"
	"github.com/bibin-skaria/ossb/registry"
)

// TestEndToEndSingleArchBuild tests complete single architecture build workflow
func TestEndToEndSingleArchBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping end-to-end integration test in short mode")
	}

	testCases := []struct {
		name       string
		dockerfile string
		platform   types.Platform
		tags       []string
		buildArgs  map[string]string
		expectPush bool
	}{
		{
			name: "simple_alpine_amd64",
			dockerfile: `FROM alpine:latest
RUN apk add --no-cache curl
COPY test.txt /test.txt
EXPOSE 8080
CMD ["cat", "/test.txt"]`,
			platform:   types.Platform{OS: "linux", Architecture: "amd64"},
			tags:       []string{"test-alpine:latest"},
			buildArgs:  map[string]string{},
			expectPush: false,
		},
		{
			name: "ubuntu_with_buildargs",
			dockerfile: `FROM ubuntu:20.04
ARG VERSION=1.0
ARG USER=testuser
RUN apt-get update && apt-get install -y curl
RUN useradd -m $USER
LABEL version=$VERSION
USER $USER
WORKDIR /home/$USER
CMD ["echo", "Version: $VERSION"]`,
			platform: types.Platform{OS: "linux", Architecture: "amd64"},
			tags:     []string{"test-ubuntu:v1.0"},
			buildArgs: map[string]string{
				"VERSION": "2.0",
				"USER":    "myuser",
			},
			expectPush: false,
		},
		{
			name: "node_app_build",
			dockerfile: `FROM node:18-alpine
WORKDIR /app
COPY package*.json ./
RUN npm ci --only=production
COPY . .
EXPOSE 3000
CMD ["npm", "start"]`,
			platform:   types.Platform{OS: "linux", Architecture: "amd64"},
			tags:       []string{"test-node:latest"},
			buildArgs:  map[string]string{},
			expectPush: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create temporary build context
			tempDir, err := ioutil.TempDir("", fmt.Sprintf("ossb-e2e-%s-", tc.name))
			if err != nil {
				t.Fatalf("Failed to create temp dir: %v", err)
			}
			defer os.RemoveAll(tempDir)

			// Create Dockerfile
			dockerfilePath := filepath.Join(tempDir, "Dockerfile")
			if err := ioutil.WriteFile(dockerfilePath, []byte(tc.dockerfile), 0644); err != nil {
				t.Fatalf("Failed to write Dockerfile: %v", err)
			}

			// Create test files
			testFilePath := filepath.Join(tempDir, "test.txt")
			if err := ioutil.WriteFile(testFilePath, []byte("Hello from OSSB integration test!"), 0644); err != nil {
				t.Fatalf("Failed to write test file: %v", err)
			}

			// Create package.json for Node.js test
			if strings.Contains(tc.dockerfile, "npm") {
				packageJSON := `{
  "name": "test-app",
  "version": "1.0.0",
  "scripts": {
    "start": "node index.js"
  },
  "dependencies": {
    "express": "^4.18.0"
  }
}`
				if err := ioutil.WriteFile(filepath.Join(tempDir, "package.json"), []byte(packageJSON), 0644); err != nil {
					t.Fatalf("Failed to write package.json: %v", err)
				}

				indexJS := `const express = require('express');
const app = express();
app.get('/', (req, res) => res.send('Hello from OSSB!'));
app.listen(3000, () => console.log('Server running on port 3000'));`
				if err := ioutil.WriteFile(filepath.Join(tempDir, "index.js"), []byte(indexJS), 0644); err != nil {
					t.Fatalf("Failed to write index.js: %v", err)
				}
			}

			// Configure build
			config := &types.BuildConfig{
				Context:    tempDir,
				Dockerfile: "Dockerfile",
				Tags:       tc.tags,
				Output:     "image",
				Frontend:   "dockerfile",
				NoCache:    false,
				Progress:   true,
				BuildArgs:  tc.buildArgs,
				Platforms:  []types.Platform{tc.platform},
				Push:       tc.expectPush,
			}

			// Create builder
			builder, err := engine.NewBuilder(config)
			if err != nil {
				t.Fatalf("Failed to create builder: %v", err)
			}
			defer builder.Cleanup()

			// Execute build
			startTime := time.Now()
			result, err := builder.Build()
			buildDuration := time.Since(startTime)

			if err != nil {
				t.Fatalf("Build failed: %v", err)
			}

			if !result.Success {
				t.Fatalf("Build failed: %s", result.Error)
			}

			// Validate results
			if result.ImageID == "" {
				t.Error("Expected non-empty ImageID")
			}

			if result.Operations == 0 {
				t.Error("Expected non-zero operation count")
			}

			if buildDuration > 5*time.Minute {
				t.Errorf("Build took too long: %v", buildDuration)
			}

			t.Logf("Build completed successfully:")
			t.Logf("  Image ID: %s", result.ImageID)
			t.Logf("  Operations: %d", result.Operations)
			t.Logf("  Cache hits: %d", result.CacheHits)
			t.Logf("  Duration: %s", buildDuration)
		})
	}
}

// TestEndToEndMultiArchBuild tests multi-architecture build workflow
func TestEndToEndMultiArchBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping multi-arch integration test in short mode")
	}

	// Create temporary build context
	tempDir, err := ioutil.TempDir("", "ossb-multiarch-e2e-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create multi-arch compatible Dockerfile
	dockerfile := `FROM alpine:latest
RUN apk add --no-cache curl file
COPY test.sh /test.sh
RUN chmod +x /test.sh
CMD ["/test.sh"]`

	dockerfilePath := filepath.Join(tempDir, "Dockerfile")
	if err := ioutil.WriteFile(dockerfilePath, []byte(dockerfile), 0644); err != nil {
		t.Fatalf("Failed to write Dockerfile: %v", err)
	}

	// Create test script that shows architecture
	testScript := `#!/bin/sh
echo "Architecture: $(uname -m)"
echo "Platform: $(uname -s)"
file /bin/sh
`
	testScriptPath := filepath.Join(tempDir, "test.sh")
	if err := ioutil.WriteFile(testScriptPath, []byte(testScript), 0755); err != nil {
		t.Fatalf("Failed to write test script: %v", err)
	}

	platforms := []types.Platform{
		{OS: "linux", Architecture: "amd64"},
		{OS: "linux", Architecture: "arm64"},
		{OS: "linux", Architecture: "arm", Variant: "v7"},
	}

	config := &types.BuildConfig{
		Context:    tempDir,
		Dockerfile: "Dockerfile",
		Tags:       []string{"test-multiarch:latest"},
		Output:     "multiarch",
		Frontend:   "dockerfile",
		NoCache:    false,
		Progress:   true,
		BuildArgs:  map[string]string{},
		Platforms:  platforms,
		Push:       false,
	}

	builder, err := engine.NewBuilder(config)
	if err != nil {
		t.Fatalf("Failed to create builder: %v", err)
	}
	defer builder.Cleanup()

	startTime := time.Now()
	result, err := builder.Build()
	buildDuration := time.Since(startTime)

	if err != nil {
		t.Fatalf("Multi-arch build failed: %v", err)
	}

	if !result.Success {
		t.Fatalf("Multi-arch build failed: %s", result.Error)
	}

	// Validate multi-arch results
	if !result.MultiArch {
		t.Error("Expected MultiArch to be true")
	}

	if len(result.PlatformResults) != len(platforms) {
		t.Errorf("Expected %d platform results, got %d", len(platforms), len(result.PlatformResults))
	}

	for _, platform := range platforms {
		platformStr := platform.String()
		platformResult, exists := result.PlatformResults[platformStr]
		if !exists {
			t.Errorf("Missing platform result for %s", platformStr)
			continue
		}

		if !platformResult.Success {
			t.Errorf("Platform %s build failed: %s", platformStr, platformResult.Error)
		}

		if platformResult.ImageID == "" {
			t.Errorf("Platform %s has empty ImageID", platformStr)
		}
	}

	if result.ManifestListID == "" {
		t.Error("Expected non-empty ManifestListID for multi-arch build")
	}

	t.Logf("Multi-arch build completed successfully:")
	t.Logf("  Manifest List ID: %s", result.ManifestListID)
	t.Logf("  Platforms: %d", len(result.PlatformResults))
	t.Logf("  Operations: %d", result.Operations)
	t.Logf("  Cache hits: %d", result.CacheHits)
	t.Logf("  Duration: %s", buildDuration)
}

// TestEndToEndMultiStageBuild tests multi-stage Dockerfile builds
func TestEndToEndMultiStageBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping multi-stage integration test in short mode")
	}

	testCases := []struct {
		name       string
		dockerfile string
		target     string
	}{
		{
			name: "simple_multistage",
			dockerfile: `FROM alpine:latest AS builder
RUN echo "Building..." > /build.txt
RUN echo "Build complete" >> /build.txt

FROM alpine:latest AS runtime
COPY --from=builder /build.txt /app/build.txt
CMD ["cat", "/app/build.txt"]`,
			target: "",
		},
		{
			name: "complex_multistage",
			dockerfile: `FROM alpine:latest AS base
RUN apk add --no-cache ca-certificates

FROM base AS build-deps
RUN apk add --no-cache gcc musl-dev

FROM build-deps AS app-builder
COPY src.c /src.c
RUN gcc -o /app /src.c

FROM base AS final
COPY --from=app-builder /app /usr/local/bin/app
CMD ["/usr/local/bin/app"]`,
			target: "",
		},
		{
			name: "target_specific_stage",
			dockerfile: `FROM alpine:latest AS stage1
RUN echo "Stage 1" > /stage1.txt

FROM alpine:latest AS stage2
COPY --from=stage1 /stage1.txt /stage1.txt
RUN echo "Stage 2" > /stage2.txt

FROM alpine:latest AS stage3
COPY --from=stage2 /stage1.txt /stage1.txt
COPY --from=stage2 /stage2.txt /stage2.txt
RUN echo "Stage 3" > /stage3.txt
CMD ["cat", "/stage1.txt", "/stage2.txt", "/stage3.txt"]`,
			target: "stage2",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tempDir, err := ioutil.TempDir("", fmt.Sprintf("ossb-multistage-%s-", tc.name))
			if err != nil {
				t.Fatalf("Failed to create temp dir: %v", err)
			}
			defer os.RemoveAll(tempDir)

			dockerfilePath := filepath.Join(tempDir, "Dockerfile")
			if err := ioutil.WriteFile(dockerfilePath, []byte(tc.dockerfile), 0644); err != nil {
				t.Fatalf("Failed to write Dockerfile: %v", err)
			}

			// Create source file for complex example
			if strings.Contains(tc.dockerfile, "src.c") {
				srcContent := `#include <stdio.h>
int main() {
    printf("Hello from OSSB multi-stage build!\\n");
    return 0;
}`
				if err := ioutil.WriteFile(filepath.Join(tempDir, "src.c"), []byte(srcContent), 0644); err != nil {
					t.Fatalf("Failed to write src.c: %v", err)
				}
			}

			config := &types.BuildConfig{
				Context:    tempDir,
				Dockerfile: "Dockerfile",
				Tags:       []string{fmt.Sprintf("test-multistage-%s:latest", tc.name)},
				Output:     "image",
				Frontend:   "dockerfile",
				NoCache:    false,
				Progress:   true,
				BuildArgs:  map[string]string{},
				Platforms:  []types.Platform{{OS: "linux", Architecture: "amd64"}},
				Push:       false,
			}

			if tc.target != "" {
				config.Target = tc.target
			}

			builder, err := engine.NewBuilder(config)
			if err != nil {
				t.Fatalf("Failed to create builder: %v", err)
			}
			defer builder.Cleanup()

			result, err := builder.Build()
			if err != nil {
				t.Fatalf("Multi-stage build failed: %v", err)
			}

			if !result.Success {
				t.Fatalf("Multi-stage build failed: %s", result.Error)
			}

			t.Logf("Multi-stage build %s completed successfully", tc.name)
			t.Logf("  Image ID: %s", result.ImageID)
			t.Logf("  Operations: %d", result.Operations)
			t.Logf("  Cache hits: %d", result.CacheHits)
		})
	}
}

// TestEndToEndDockerfilePatterns tests various Dockerfile instruction patterns
func TestEndToEndDockerfilePatterns(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping Dockerfile patterns integration test in short mode")
	}

	testCases := []struct {
		name       string
		dockerfile string
		files      map[string]string
	}{
		{
			name: "copy_patterns",
			dockerfile: `FROM alpine:latest
COPY file1.txt /app/
COPY dir1/ /app/dir1/
COPY file*.txt /app/files/
ADD archive.tar.gz /app/extracted/
WORKDIR /app
CMD ["ls", "-la"]`,
			files: map[string]string{
				"file1.txt":         "Content of file1",
				"file2.txt":         "Content of file2",
				"dir1/nested.txt":   "Nested file content",
				"archive.tar.gz":    "", // Will be created as empty tar.gz
			},
		},
		{
			name: "env_and_labels",
			dockerfile: `FROM alpine:latest
ENV APP_ENV=production
ENV DEBUG=false
ENV PATH=/app/bin:$PATH
LABEL maintainer="test@example.com"
LABEL version="1.0"
LABEL description="Test image for OSSB"
RUN echo "Environment: $APP_ENV" > /app/env.txt
CMD ["cat", "/app/env.txt"]`,
			files: map[string]string{},
		},
		{
			name: "user_and_workdir",
			dockerfile: `FROM alpine:latest
RUN addgroup -g 1000 appgroup
RUN adduser -D -u 1000 -G appgroup appuser
USER appuser
WORKDIR /home/appuser
COPY --chown=appuser:appgroup app.sh .
RUN chmod +x app.sh
CMD ["./app.sh"]`,
			files: map[string]string{
				"app.sh": "#!/bin/sh\necho 'Hello from user context'\nwhoami\npwd",
			},
		},
		{
			name: "volume_and_expose",
			dockerfile: `FROM alpine:latest
RUN mkdir -p /data /logs
VOLUME ["/data", "/logs"]
EXPOSE 8080 8443
EXPOSE 9090/tcp
EXPOSE 9091/udp
HEALTHCHECK --interval=30s --timeout=3s --retries=3 CMD echo "healthy"
CMD ["sleep", "infinity"]`,
			files: map[string]string{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tempDir, err := ioutil.TempDir("", fmt.Sprintf("ossb-patterns-%s-", tc.name))
			if err != nil {
				t.Fatalf("Failed to create temp dir: %v", err)
			}
			defer os.RemoveAll(tempDir)

			// Create Dockerfile
			dockerfilePath := filepath.Join(tempDir, "Dockerfile")
			if err := ioutil.WriteFile(dockerfilePath, []byte(tc.dockerfile), 0644); err != nil {
				t.Fatalf("Failed to write Dockerfile: %v", err)
			}

			// Create test files
			for filename, content := range tc.files {
				filePath := filepath.Join(tempDir, filename)
				
				// Create directory if needed
				if dir := filepath.Dir(filePath); dir != tempDir {
					if err := os.MkdirAll(dir, 0755); err != nil {
						t.Fatalf("Failed to create directory %s: %v", dir, err)
					}
				}

				// Handle special case for tar.gz
				if strings.HasSuffix(filename, ".tar.gz") {
					// Create empty tar.gz file
					cmd := exec.Command("tar", "-czf", filePath, "-T", "/dev/null")
					if err := cmd.Run(); err != nil {
						t.Logf("Warning: Failed to create tar.gz file: %v", err)
						// Create empty file as fallback
						if err := ioutil.WriteFile(filePath, []byte{}, 0644); err != nil {
							t.Fatalf("Failed to create fallback file: %v", err)
						}
					}
				} else {
					if err := ioutil.WriteFile(filePath, []byte(content), 0644); err != nil {
						t.Fatalf("Failed to write file %s: %v", filename, err)
					}
				}
			}

			config := &types.BuildConfig{
				Context:    tempDir,
				Dockerfile: "Dockerfile",
				Tags:       []string{fmt.Sprintf("test-patterns-%s:latest", tc.name)},
				Output:     "image",
				Frontend:   "dockerfile",
				NoCache:    false,
				Progress:   true,
				BuildArgs:  map[string]string{},
				Platforms:  []types.Platform{{OS: "linux", Architecture: "amd64"}},
				Push:       false,
			}

			builder, err := engine.NewBuilder(config)
			if err != nil {
				t.Fatalf("Failed to create builder: %v", err)
			}
			defer builder.Cleanup()

			result, err := builder.Build()
			if err != nil {
				t.Fatalf("Dockerfile patterns build failed: %v", err)
			}

			if !result.Success {
				t.Fatalf("Dockerfile patterns build failed: %s", result.Error)
			}

			t.Logf("Dockerfile patterns build %s completed successfully", tc.name)
		})
	}
}