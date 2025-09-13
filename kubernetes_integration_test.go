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
)

// TestKubernetesIntegrationJobExecution tests complete job execution in Kubernetes
func TestKubernetesIntegrationJobExecution(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping Kubernetes job integration test in short mode")
	}

	if !isRunningInKubernetes() {
		t.Skip("Not running in Kubernetes environment")
	}

	// Set up test environment
	tempDir, err := ioutil.TempDir("", "ossb-k8s-job-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create test Dockerfile
	dockerfile := `FROM alpine:latest
RUN apk add --no-cache curl
COPY test-script.sh /test-script.sh
RUN chmod +x /test-script.sh
CMD ["/test-script.sh"]`

	dockerfilePath := filepath.Join(tempDir, "Dockerfile")
	if err := ioutil.WriteFile(dockerfilePath, []byte(dockerfile), 0644); err != nil {
		t.Fatalf("Failed to write Dockerfile: %v", err)
	}

	// Create test script
	testScript := `#!/bin/sh
echo "Running in Kubernetes pod: $HOSTNAME"
echo "Namespace: $POD_NAMESPACE"
echo "Job: $JOB_NAME"
curl --version
echo "Test completed successfully"
`
	testScriptPath := filepath.Join(tempDir, "test-script.sh")
	if err := ioutil.WriteFile(testScriptPath, []byte(testScript), 0755); err != nil {
		t.Fatalf("Failed to write test script: %v", err)
	}

	buildID := fmt.Sprintf("k8s-integration-test-%d", time.Now().Unix())
	jobManager := k8s.NewJobLifecycleManager(buildID)

	ctx := context.Background()
	ctx, err = jobManager.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start job lifecycle: %v", err)
	}

	// Test mounting build context
	if err := jobManager.GetIntegration().MountBuildContext(tempDir); err != nil {
		t.Logf("Warning: Failed to mount build context: %v", err)
	}

	// Load registry credentials
	registryConfig, err := jobManager.GetIntegration().LoadRegistryCredentials()
	if err != nil {
		t.Logf("Warning: Failed to load registry credentials: %v", err)
	}

	// Load build secrets
	secrets, err := jobManager.GetIntegration().LoadBuildSecrets()
	if err != nil {
		t.Logf("Warning: Failed to load build secrets: %v", err)
	}

	config := &types.BuildConfig{
		Context:        tempDir,
		Dockerfile:     "Dockerfile",
		Tags:           []string{fmt.Sprintf("k8s-test:%s", buildID)},
		Output:         "image",
		Frontend:       "dockerfile",
		NoCache:        false,
		Progress:       true,
		BuildArgs:      map[string]string{},
		Platforms:      []types.Platform{{OS: "linux", Architecture: "amd64"}},
		Push:           false,
		RegistryConfig: registryConfig,
		Secrets:        secrets,
	}

	builder, err := engine.NewBuilder(config)
	if err != nil {
		jobManager.Fail(ctx, err, "builder_creation")
		t.Fatalf("Failed to create builder: %v", err)
	}

	// Add cleanup to job manager
	jobManager.AddCleanupFunc(func() error {
		builder.Cleanup()
		return nil
	})

	// Report progress during build
	jobManager.ReportProgress(ctx, "build", 10.0, "Starting Kubernetes build", "linux/amd64", "BUILD", false)

	result, err := builder.Build()
	if err != nil {
		jobManager.Fail(ctx, err, "build")
		t.Fatalf("Build failed: %v", err)
	}

	if !result.Success {
		buildErr := fmt.Errorf("build failed: %s", result.Error)
		jobManager.Fail(ctx, buildErr, "build")
		t.Fatalf("Build failed: %s", result.Error)
	}

	// Complete job
	exitCode := jobManager.Complete(ctx, result)
	if exitCode != k8s.ExitCodeSuccess {
		t.Errorf("Expected exit code %d, got %d", k8s.ExitCodeSuccess, exitCode)
	}

	t.Logf("Kubernetes job integration test completed successfully")
	t.Logf("  Build ID: %s", buildID)
	t.Logf("  Image ID: %s", result.ImageID)
	t.Logf("  Operations: %d", result.Operations)
	t.Logf("  Cache hits: %d", result.CacheHits)
}

// TestKubernetesIntegrationSecretHandling tests secret mounting and usage
func TestKubernetesIntegrationSecretHandling(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping Kubernetes secret integration test in short mode")
	}

	if !isRunningInKubernetes() {
		t.Skip("Not running in Kubernetes environment")
	}

	// Create test environment with secrets
	tempDir, err := ioutil.TempDir("", "ossb-k8s-secrets-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Set up mock Kubernetes secrets directory
	setupMockKubernetesSecrets(t, tempDir)

	k8sIntegration := k8s.NewKubernetesIntegration()
	k8sIntegration.SetSecretsPath(filepath.Join(tempDir, "secrets"))
	k8sIntegration.SetConfigMapPath(filepath.Join(tempDir, "configmaps"))

	// Test loading registry credentials
	t.Run("load_registry_credentials", func(t *testing.T) {
		registryConfig, err := k8sIntegration.LoadRegistryCredentials()
		if err != nil {
			t.Fatalf("Failed to load registry credentials: %v", err)
		}

		if registryConfig == nil {
			t.Fatal("Registry config is nil")
		}

		// Verify Docker Hub credentials
		if auth, exists := registryConfig.Registries["docker.io"]; !exists {
			t.Error("Docker Hub credentials not loaded")
		} else {
			if auth.Username == "" || auth.Password == "" {
				t.Errorf("Invalid Docker Hub credentials: %+v", auth)
			}
		}

		t.Log("Registry credentials loaded successfully")
	})

	// Test loading build secrets
	t.Run("load_build_secrets", func(t *testing.T) {
		secrets, err := k8sIntegration.LoadBuildSecrets()
		if err != nil {
			t.Fatalf("Failed to load build secrets: %v", err)
		}

		expectedSecrets := []string{"api-key", "db-password", "ssl-cert"}
		for _, secretName := range expectedSecrets {
			if _, exists := secrets[secretName]; !exists {
				t.Errorf("Expected secret %s not found", secretName)
			}
		}

		t.Logf("Build secrets loaded successfully: %d secrets", len(secrets))
	})

	// Test workspace setup
	t.Run("setup_workspace", func(t *testing.T) {
		workspaceDir := filepath.Join(tempDir, "workspace")
		k8sIntegration.SetWorkspacePath(workspaceDir)

		err := k8sIntegration.SetupWorkspace("5Gi")
		if err != nil {
			t.Fatalf("Failed to setup workspace: %v", err)
		}

		// Verify workspace directories
		expectedDirs := []string{"tmp", "cache", "layers", "manifests"}
		for _, dir := range expectedDirs {
			dirPath := filepath.Join(workspaceDir, dir)
			if _, err := os.Stat(dirPath); os.IsNotExist(err) {
				t.Errorf("Expected workspace directory %s not created", dir)
			}
		}

		t.Log("Workspace setup completed successfully")
	})
}

// TestKubernetesIntegrationMultiArchJob tests multi-architecture builds in Kubernetes
func TestKubernetesIntegrationMultiArchJob(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping Kubernetes multi-arch integration test in short mode")
	}

	if !isRunningInKubernetes() {
		t.Skip("Not running in Kubernetes environment")
	}

	tempDir, err := ioutil.TempDir("", "ossb-k8s-multiarch-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dockerfile := `FROM alpine:latest
RUN apk add --no-cache file
RUN echo "Architecture: $(uname -m)" > /arch-info.txt
RUN file /bin/sh >> /arch-info.txt
CMD ["cat", "/arch-info.txt"]`

	dockerfilePath := filepath.Join(tempDir, "Dockerfile")
	if err := ioutil.WriteFile(dockerfilePath, []byte(dockerfile), 0644); err != nil {
		t.Fatalf("Failed to write Dockerfile: %v", err)
	}

	buildID := fmt.Sprintf("k8s-multiarch-test-%d", time.Now().Unix())
	jobManager := k8s.NewJobLifecycleManager(buildID)

	ctx := context.Background()
	ctx, err = jobManager.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start job lifecycle: %v", err)
	}

	platforms := []types.Platform{
		{OS: "linux", Architecture: "amd64"},
		{OS: "linux", Architecture: "arm64"},
	}

	config := &types.BuildConfig{
		Context:    tempDir,
		Dockerfile: "Dockerfile",
		Tags:       []string{fmt.Sprintf("k8s-multiarch-test:%s", buildID)},
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
		jobManager.Fail(ctx, err, "builder_creation")
		t.Fatalf("Failed to create builder: %v", err)
	}

	jobManager.AddCleanupFunc(func() error {
		builder.Cleanup()
		return nil
	})

	// Report progress for each platform
	for i, platform := range platforms {
		progress := float64(10 + i*40)
		jobManager.ReportProgress(ctx, "build", progress, 
			fmt.Sprintf("Building for %s", platform.String()), 
			platform.String(), "BUILD", false)
	}

	result, err := builder.Build()
	if err != nil {
		jobManager.Fail(ctx, err, "build")
		t.Fatalf("Multi-arch build failed: %v", err)
	}

	if !result.Success {
		buildErr := fmt.Errorf("multi-arch build failed: %s", result.Error)
		jobManager.Fail(ctx, buildErr, "build")
		t.Fatalf("Multi-arch build failed: %s", result.Error)
	}

	if !result.MultiArch {
		t.Error("Expected MultiArch to be true")
	}

	if len(result.PlatformResults) != len(platforms) {
		t.Errorf("Expected %d platform results, got %d", len(platforms), len(result.PlatformResults))
	}

	exitCode := jobManager.Complete(ctx, result)
	if exitCode != k8s.ExitCodeSuccess {
		t.Errorf("Expected exit code %d, got %d", k8s.ExitCodeSuccess, exitCode)
	}

	t.Logf("Kubernetes multi-arch integration test completed successfully")
	t.Logf("  Manifest List ID: %s", result.ManifestListID)
	t.Logf("  Platforms built: %d", len(result.PlatformResults))
}

// TestKubernetesIntegrationJobFailure tests job failure handling
func TestKubernetesIntegrationJobFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping Kubernetes job failure integration test in short mode")
	}

	if !isRunningInKubernetes() {
		t.Skip("Not running in Kubernetes environment")
	}

	tempDir, err := ioutil.TempDir("", "ossb-k8s-failure-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create Dockerfile that will fail
	dockerfile := `FROM alpine:latest
RUN exit 1  # This will cause the build to fail
CMD ["echo", "This should not run"]`

	dockerfilePath := filepath.Join(tempDir, "Dockerfile")
	if err := ioutil.WriteFile(dockerfilePath, []byte(dockerfile), 0644); err != nil {
		t.Fatalf("Failed to write Dockerfile: %v", err)
	}

	buildID := fmt.Sprintf("k8s-failure-test-%d", time.Now().Unix())
	jobManager := k8s.NewJobLifecycleManager(buildID)

	ctx := context.Background()
	ctx, err = jobManager.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start job lifecycle: %v", err)
	}

	config := &types.BuildConfig{
		Context:    tempDir,
		Dockerfile: "Dockerfile",
		Tags:       []string{fmt.Sprintf("k8s-failure-test:%s", buildID)},
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
		jobManager.Fail(ctx, err, "builder_creation")
		t.Fatalf("Failed to create builder: %v", err)
	}

	jobManager.AddCleanupFunc(func() error {
		builder.Cleanup()
		return nil
	})

	result, err := builder.Build()

	// Expect the build to fail
	if err == nil && result.Success {
		t.Fatal("Expected build to fail but it succeeded")
	}

	// Test job failure handling
	var exitCode k8s.ExitCode
	if err != nil {
		exitCode = jobManager.Fail(ctx, err, "build")
	} else {
		buildErr := fmt.Errorf("build failed: %s", result.Error)
		exitCode = jobManager.Fail(ctx, buildErr, "build")
	}

	if exitCode == k8s.ExitCodeSuccess {
		t.Error("Expected non-success exit code for failed build")
	}

	if jobManager.GetStatus() != k8s.JobStatusFailed {
		t.Errorf("Expected job status Failed, got %s", jobManager.GetStatus())
	}

	t.Logf("Kubernetes job failure integration test completed successfully")
	t.Logf("  Exit code: %d", exitCode)
	t.Logf("  Job status: %s", jobManager.GetStatus())
}

// TestKubernetesIntegrationProgressReporting tests progress reporting
func TestKubernetesIntegrationProgressReporting(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping Kubernetes progress reporting integration test in short mode")
	}

	if !isRunningInKubernetes() {
		t.Skip("Not running in Kubernetes environment")
	}

	buildID := fmt.Sprintf("k8s-progress-test-%d", time.Now().Unix())
	jobManager := k8s.NewJobLifecycleManager(buildID)

	ctx := context.Background()
	ctx, err := jobManager.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start job lifecycle: %v", err)
	}

	// Test progress reporting
	progressSteps := []struct {
		stage     string
		progress  float64
		message   string
		platform  string
		operation string
		cacheHit  bool
	}{
		{"init", 5.0, "Initializing build", "", "INIT", false},
		{"pull", 15.0, "Pulling base image", "linux/amd64", "PULL", false},
		{"build", 30.0, "Building layer 1", "linux/amd64", "RUN", false},
		{"build", 50.0, "Building layer 2", "linux/amd64", "COPY", true},
		{"build", 70.0, "Building layer 3", "linux/arm64", "RUN", false},
		{"push", 90.0, "Pushing to registry", "", "PUSH", false},
		{"complete", 100.0, "Build completed", "", "COMPLETE", false},
	}

	for _, step := range progressSteps {
		jobManager.ReportProgress(ctx, step.stage, step.progress, step.message, 
			step.platform, step.operation, step.cacheHit)
		
		// Small delay to simulate real build progress
		time.Sleep(100 * time.Millisecond)
	}

	// Verify progress file was created and updated
	progressFile := "/tmp/ossb-progress.json"
	if data, err := ioutil.ReadFile(progressFile); err == nil {
		var progress k8s.ProgressReport
		if err := json.Unmarshal(data, &progress); err == nil {
			if progress.Stage != "complete" || progress.Progress != 100.0 {
				t.Errorf("Final progress = %+v, expected stage=complete, progress=100.0", progress)
			}
		} else {
			t.Errorf("Failed to parse progress JSON: %v", err)
		}
	} else {
		t.Errorf("Progress file not found: %v", err)
	}

	// Complete the job
	result := &types.BuildResult{
		Success:    true,
		Operations: 10,
		CacheHits:  5,
		Duration:   "2m30s",
		ImageID:    "sha256:test123",
	}

	exitCode := jobManager.Complete(ctx, result)
	if exitCode != k8s.ExitCodeSuccess {
		t.Errorf("Expected exit code %d, got %d", k8s.ExitCodeSuccess, exitCode)
	}

	t.Log("Kubernetes progress reporting integration test completed successfully")

	// Clean up test files
	os.Remove(progressFile)
	os.Remove("/tmp/ossb-status.json")
}

// TestKubernetesIntegrationResourceLimits tests resource limit enforcement
func TestKubernetesIntegrationResourceLimits(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping Kubernetes resource limits integration test in short mode")
	}

	if !isRunningInKubernetes() {
		t.Skip("Not running in Kubernetes environment")
	}

	// Test resource limit detection
	k8sIntegration := k8s.NewKubernetesIntegration()
	
	// Check if resource limits are available
	limits, err := k8sIntegration.GetResourceLimits()
	if err != nil {
		t.Logf("Warning: Failed to get resource limits: %v", err)
		return
	}

	if limits != nil {
		t.Logf("Resource limits detected:")
		if limits.Memory != "" {
			t.Logf("  Memory: %s", limits.Memory)
		}
		if limits.CPU != "" {
			t.Logf("  CPU: %s", limits.CPU)
		}
	} else {
		t.Log("No resource limits configured")
	}

	// Test workspace size calculation based on limits
	workspaceSize, err := k8sIntegration.CalculateWorkspaceSize()
	if err != nil {
		t.Logf("Warning: Failed to calculate workspace size: %v", err)
	} else {
		t.Logf("Calculated workspace size: %s", workspaceSize)
	}

	t.Log("Kubernetes resource limits integration test completed")
}

// Helper functions

func isRunningInKubernetes() bool {
	// Check for Kubernetes environment variables
	if os.Getenv("KUBERNETES_SERVICE_HOST") != "" {
		return true
	}

	// Check for service account token
	if _, err := os.Stat("/var/run/secrets/kubernetes.io/serviceaccount/token"); err == nil {
		return true
	}

	// Check for kubectl availability and cluster access
	cmd := exec.Command("kubectl", "cluster-info")
	return cmd.Run() == nil
}

func setupMockKubernetesSecrets(t *testing.T, baseDir string) {
	secretsDir := filepath.Join(baseDir, "secrets")
	configMapsDir := filepath.Join(baseDir, "configmaps")

	// Create Docker registry secret
	dockerSecretDir := filepath.Join(secretsDir, "docker-registry-secret")
	os.MkdirAll(dockerSecretDir, 0755)
	
	dockerConfig := map[string]interface{}{
		"auths": map[string]interface{}{
			"docker.io": map[string]interface{}{
				"username": "testuser",
				"password": "testpass",
				"auth":     "dGVzdHVzZXI6dGVzdHBhc3M=", // base64 of testuser:testpass
			},
		},
	}
	dockerConfigData, _ := json.Marshal(dockerConfig)
	ioutil.WriteFile(filepath.Join(dockerSecretDir, ".dockerconfigjson"), dockerConfigData, 0644)

	// Create individual registry credentials
	registryAuthDir := filepath.Join(secretsDir, "registry-auth")
	os.MkdirAll(registryAuthDir, 0755)
	ioutil.WriteFile(filepath.Join(registryAuthDir, "registry"), []byte("gcr.io"), 0644)
	ioutil.WriteFile(filepath.Join(registryAuthDir, "username"), []byte("_json_key"), 0644)
	ioutil.WriteFile(filepath.Join(registryAuthDir, "password"), []byte("fake-service-account-key"), 0644)

	// Create build secrets
	buildSecretsDir := filepath.Join(secretsDir, "build-secrets")
	os.MkdirAll(buildSecretsDir, 0755)
	ioutil.WriteFile(filepath.Join(buildSecretsDir, "api-key"), []byte("secret-api-key-value"), 0644)
	ioutil.WriteFile(filepath.Join(buildSecretsDir, "db-password"), []byte("secret-db-password"), 0644)
	ioutil.WriteFile(filepath.Join(buildSecretsDir, "ssl-cert"), []byte("-----BEGIN CERTIFICATE-----\nfake-cert\n-----END CERTIFICATE-----"), 0644)

	// Create registry config map
	registryConfigDir := filepath.Join(configMapsDir, "registry-config")
	os.MkdirAll(registryConfigDir, 0755)
	registryConfigYAML := `
default_registry: docker.io
registries:
  docker.io:
    username: testuser
    password: testpass
  gcr.io:
    username: _json_key
    auth_file: /var/run/secrets/gcr-key/key.json
insecure:
  - localhost:5000
  - registry.local:5000
mirrors:
  docker.io:
    - mirror.gcr.io
    - registry-mirror.local
`
	ioutil.WriteFile(filepath.Join(registryConfigDir, "config.yaml"), []byte(registryConfigYAML), 0644)

	// Create build context config map
	buildContextDir := filepath.Join(configMapsDir, "build-context")
	os.MkdirAll(buildContextDir, 0755)
	
	sampleDockerfile := `FROM alpine:latest
RUN apk add --no-cache curl
COPY app.sh /app.sh
RUN chmod +x /app.sh
EXPOSE 8080
CMD ["/app.sh"]`
	ioutil.WriteFile(filepath.Join(buildContextDir, "Dockerfile"), []byte(sampleDockerfile), 0644)
	
	sampleApp := `#!/bin/sh
echo "Hello from Kubernetes build context!"
echo "Pod: $HOSTNAME"
echo "Namespace: $POD_NAMESPACE"
exec "$@"`
	ioutil.WriteFile(filepath.Join(buildContextDir, "app.sh"), []byte(sampleApp), 0644)
}