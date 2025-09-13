package k8s

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bibin-skaria/ossb/internal/types"
)

// TestKubernetesFullIntegration tests the complete Kubernetes integration workflow
func TestKubernetesFullIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping full integration test in short mode")
	}

	// Set up test environment
	tmpDir, err := ioutil.TempDir("", "k8s-full-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Set up Kubernetes environment variables
	os.Setenv("JOB_NAME", "test-ossb-job")
	os.Setenv("POD_NAME", "test-ossb-pod")
	os.Setenv("POD_NAMESPACE", "test-namespace")
	os.Setenv("NODE_NAME", "test-node")
	defer func() {
		os.Unsetenv("JOB_NAME")
		os.Unsetenv("POD_NAME")
		os.Unsetenv("POD_NAMESPACE")
		os.Unsetenv("NODE_NAME")
	}()

	// Create test build ID
	buildID := "test-build-full-integration"

	// Initialize job lifecycle manager
	manager := NewJobLifecycleManager(buildID)

	// Set up test paths
	manager.integration.secretsPath = filepath.Join(tmpDir, "secrets")
	manager.integration.configMapPath = filepath.Join(tmpDir, "configmaps")
	manager.integration.workspacePath = filepath.Join(tmpDir, "workspace")

	// Create test secrets and config maps
	setupTestKubernetesEnvironment(t, tmpDir)

	ctx := context.Background()

	// Test 1: Start job lifecycle
	t.Run("start_lifecycle", func(t *testing.T) {
		newCtx, err := manager.Start(ctx)
		if err != nil {
			t.Fatalf("Failed to start job lifecycle: %v", err)
		}
		ctx = newCtx

		if manager.GetStatus() != JobStatusRunning {
			t.Errorf("Expected status Running, got %s", manager.GetStatus())
		}
	})

	// Test 2: Load registry credentials
	t.Run("load_registry_credentials", func(t *testing.T) {
		registryConfig, err := manager.integration.LoadRegistryCredentials()
		if err != nil {
			t.Fatalf("Failed to load registry credentials: %v", err)
		}

		// Verify Docker Hub credentials
		if auth, exists := registryConfig.Registries["docker.io"]; !exists {
			t.Error("Docker Hub credentials not loaded")
		} else {
			if auth.Username != "testuser" || auth.Password != "testpass" {
				t.Errorf("Incorrect Docker Hub credentials: %+v", auth)
			}
		}

		// Verify GCR credentials
		if auth, exists := registryConfig.Registries["gcr.io"]; !exists {
			t.Error("GCR credentials not loaded")
		} else {
			if auth.Username != "_json_key" {
				t.Errorf("Incorrect GCR username: %s", auth.Username)
			}
		}
	})

	// Test 3: Load build secrets
	t.Run("load_build_secrets", func(t *testing.T) {
		secrets, err := manager.integration.LoadBuildSecrets()
		if err != nil {
			t.Fatalf("Failed to load build secrets: %v", err)
		}

		expectedSecrets := map[string]string{
			"api-key":     "secret-api-key",
			"db-password": "secret-db-pass",
		}

		for key, expectedValue := range expectedSecrets {
			if value, exists := secrets[key]; !exists {
				t.Errorf("Expected secret %s not found", key)
			} else if value != expectedValue {
				t.Errorf("Secret %s = %s, expected %s", key, value, expectedValue)
			}
		}
	})

	// Test 4: Setup workspace
	t.Run("setup_workspace", func(t *testing.T) {
		err := manager.integration.SetupWorkspace("10Gi")
		if err != nil {
			t.Fatalf("Failed to setup workspace: %v", err)
		}

		// Verify workspace directories
		expectedDirs := []string{"tmp", "cache", "layers", "manifests"}
		for _, dir := range expectedDirs {
			dirPath := filepath.Join(manager.integration.workspacePath, dir)
			if _, err := os.Stat(dirPath); os.IsNotExist(err) {
				t.Errorf("Expected workspace directory %s not created", dir)
			}
		}
	})

	// Test 5: Mount build context
	t.Run("mount_build_context", func(t *testing.T) {
		contextPath := filepath.Join(tmpDir, "build-context")
		err := manager.integration.MountBuildContext(contextPath)
		if err != nil {
			t.Fatalf("Failed to mount build context: %v", err)
		}

		// Verify symlink was created
		if _, err := os.Lstat(contextPath); err != nil {
			t.Errorf("Build context symlink not created: %v", err)
		}
	})

	// Test 6: Report progress during build simulation
	t.Run("report_progress", func(t *testing.T) {
		stages := []struct {
			stage    string
			progress float64
			message  string
			platform string
			operation string
			cacheHit bool
		}{
			{"pull", 10.0, "Pulling base image", "linux/amd64", "PULL", false},
			{"build", 30.0, "Building layer 1", "linux/amd64", "RUN", false},
			{"build", 50.0, "Building layer 2", "linux/amd64", "COPY", true},
			{"build", 70.0, "Building layer 3", "linux/arm64", "RUN", false},
			{"push", 90.0, "Pushing to registry", "", "PUSH", false},
		}

		for _, stage := range stages {
			manager.ReportProgress(ctx, stage.stage, stage.progress, stage.message, stage.platform, stage.operation, stage.cacheHit)
			time.Sleep(10 * time.Millisecond) // Simulate work
		}

		// Verify progress file was updated
		progressFile := "/tmp/ossb-progress.json"
		if data, err := ioutil.ReadFile(progressFile); err == nil {
			var progress ProgressReport
			if err := json.Unmarshal(data, &progress); err == nil {
				if progress.Stage != "push" || progress.Progress != 90.0 {
					t.Errorf("Final progress = %+v, expected stage=push, progress=90.0", progress)
				}
			}
		}
	})

	// Test 7: Add cleanup functions
	t.Run("add_cleanup", func(t *testing.T) {
		cleanupCalled := false
		manager.AddCleanupFunc(func() error {
			cleanupCalled = true
			return nil
		})

		// Cleanup will be tested when job completes
		if cleanupCalled {
			t.Error("Cleanup should not be called yet")
		}
	})

	// Test 8: Complete successful build
	t.Run("complete_successful_build", func(t *testing.T) {
		buildResult := &types.BuildResult{
			Success:    true,
			Operations: 15,
			CacheHits:  8,
			Duration:   "45s",
			ImageID:    "sha256:abc123",
			PlatformResults: map[string]*types.PlatformResult{
				"linux/amd64": {
					Platform: types.Platform{OS: "linux", Architecture: "amd64"},
					Success:  true,
					ImageID:  "sha256:amd64123",
				},
				"linux/arm64": {
					Platform: types.Platform{OS: "linux", Architecture: "arm64"},
					Success:  true,
					ImageID:  "sha256:arm64123",
				},
			},
			MultiArch: true,
		}

		exitCode := manager.Complete(ctx, buildResult)

		if exitCode != ExitCodeSuccess {
			t.Errorf("Expected exit code %d, got %d", ExitCodeSuccess, exitCode)
		}

		if manager.GetStatus() != JobStatusSucceeded {
			t.Errorf("Expected status Succeeded, got %s", manager.GetStatus())
		}

		// Verify status file was created
		statusFile := "/tmp/ossb-status.json"
		if data, err := ioutil.ReadFile(statusFile); err == nil {
			var status JobStatusReport
			if err := json.Unmarshal(data, &status); err == nil {
				if status.Status != JobStatusSucceeded || status.Progress != 100.0 {
					t.Errorf("Final status = %+v, expected status=Succeeded, progress=100.0", status)
				}
			}
		}
	})

	// Clean up test files
	os.Remove("/tmp/ossb-progress.json")
	os.Remove("/tmp/ossb-status.json")
}

// TestKubernetesFailedBuildIntegration tests the failed build workflow
func TestKubernetesFailedBuildIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping failed build integration test in short mode")
	}

	buildID := "test-build-failed"
	manager := NewJobLifecycleManager(buildID)

	ctx := context.Background()
	ctx, err := manager.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start job lifecycle: %v", err)
	}

	// Simulate build failure
	buildResult := &types.BuildResult{
		Success:    false,
		Error:      "failed to push image to registry: authentication failed",
		Operations: 10,
		CacheHits:  5,
		Duration:   "30s",
	}

	exitCode := manager.Complete(ctx, buildResult)

	if exitCode != ExitCodeRegistryError {
		t.Errorf("Expected exit code %d, got %d", ExitCodeRegistryError, exitCode)
	}

	if manager.GetStatus() != JobStatusFailed {
		t.Errorf("Expected status Failed, got %s", manager.GetStatus())
	}

	// Clean up
	os.Remove("/tmp/ossb-status.json")
}

// TestKubernetesCancelledBuildIntegration tests the cancelled build workflow
func TestKubernetesCancelledBuildIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping cancelled build integration test in short mode")
	}

	buildID := "test-build-cancelled"
	manager := NewJobLifecycleManager(buildID)

	ctx := context.Background()
	ctx, err := manager.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start job lifecycle: %v", err)
	}

	// Simulate build cancellation
	exitCode := manager.Cancel(ctx, "user requested cancellation")

	if exitCode != ExitCodeCancelledError {
		t.Errorf("Expected exit code %d, got %d", ExitCodeCancelledError, exitCode)
	}

	if manager.GetStatus() != JobStatusFailed {
		t.Errorf("Expected status Failed, got %s", manager.GetStatus())
	}

	if !manager.IsCancelled() {
		t.Error("Manager should be marked as cancelled")
	}

	// Verify context was cancelled
	select {
	case <-ctx.Done():
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Error("Context should be cancelled")
	}

	// Clean up
	os.Remove("/tmp/ossb-status.json")
}

// setupTestKubernetesEnvironment creates test secrets and config maps
func setupTestKubernetesEnvironment(t *testing.T, tmpDir string) {
	secretsPath := filepath.Join(tmpDir, "secrets")
	configMapsPath := filepath.Join(tmpDir, "configmaps")

	// Create Docker registry secret
	dockerConfigDir := filepath.Join(secretsPath, "registry-secret")
	os.MkdirAll(dockerConfigDir, 0755)
	dockerConfig := map[string]interface{}{
		"auths": map[string]interface{}{
			"docker.io": map[string]interface{}{
				"username": "testuser",
				"password": "testpass",
			},
		},
	}
	dockerConfigData, _ := json.Marshal(dockerConfig)
	ioutil.WriteFile(filepath.Join(dockerConfigDir, ".dockerconfigjson"), dockerConfigData, 0644)

	// Create individual registry secret
	registryAuthDir := filepath.Join(secretsPath, "registry-auth")
	os.MkdirAll(registryAuthDir, 0755)
	ioutil.WriteFile(filepath.Join(registryAuthDir, "registry"), []byte("gcr.io"), 0644)
	ioutil.WriteFile(filepath.Join(registryAuthDir, "username"), []byte("gcr-user"), 0644)
	ioutil.WriteFile(filepath.Join(registryAuthDir, "password"), []byte("gcr-pass"), 0644)

	// Create build secrets
	buildSecretsDir := filepath.Join(secretsPath, "build-secrets")
	os.MkdirAll(buildSecretsDir, 0755)
	ioutil.WriteFile(filepath.Join(buildSecretsDir, "api-key"), []byte("secret-api-key"), 0644)
	ioutil.WriteFile(filepath.Join(buildSecretsDir, "db-password"), []byte("secret-db-pass"), 0644)

	// Create registry config
	registryConfigDir := filepath.Join(configMapsPath, "registry-config")
	os.MkdirAll(registryConfigDir, 0755)
	registryConfigYAML := `
default_registry: docker.io
registries:
  gcr.io:
    username: _json_key
    auth_file: /var/run/secrets/gcr-key/key.json
insecure:
  - localhost:5000
mirrors:
  docker.io:
    - mirror.gcr.io
`
	ioutil.WriteFile(filepath.Join(registryConfigDir, "config.yaml"), []byte(registryConfigYAML), 0644)

	// Create build context
	buildContextDir := filepath.Join(configMapsPath, "build-context")
	os.MkdirAll(buildContextDir, 0755)
	dockerfile := `FROM alpine:latest
RUN apk add --no-cache curl
COPY app.sh /app.sh
RUN chmod +x /app.sh
EXPOSE 8080
CMD ["/app.sh"]`
	ioutil.WriteFile(filepath.Join(buildContextDir, "Dockerfile"), []byte(dockerfile), 0644)
	
	appScript := `#!/bin/sh
echo "Hello from OSSB!"
exec "$@"`
	ioutil.WriteFile(filepath.Join(buildContextDir, "app.sh"), []byte(appScript), 0644)
}