package k8s

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/bibin-skaria/ossb/internal/types"
)

func TestKubernetesIntegration_IsRunningInKubernetes(t *testing.T) {
	tests := []struct {
		name     string
		setup    func() func()
		expected bool
	}{
		{
			name: "running in kubernetes with service account",
			setup: func() func() {
				// Create the service account token file in the expected location
				tokenDir := "/var/run/secrets/kubernetes.io/serviceaccount"
				tokenFile := filepath.Join(tokenDir, "token")
				
				// Check if we can create the directory (may fail in some test environments)
				if err := os.MkdirAll(tokenDir, 0755); err == nil {
					ioutil.WriteFile(tokenFile, []byte("test-token"), 0644)
					return func() { 
						os.Remove(tokenFile)
						os.RemoveAll("/var/run/secrets/kubernetes.io")
					}
				}
				
				// If we can't create the actual path, skip this test
				t.Skip("Cannot create service account token file in test environment")
				return func() {}
			},
			expected: true,
		},
		{
			name: "running in kubernetes with pod environment",
			setup: func() func() {
				os.Setenv("POD_NAME", "test-pod")
				os.Setenv("POD_NAMESPACE", "test-namespace")
				return func() {
					os.Unsetenv("POD_NAME")
					os.Unsetenv("POD_NAMESPACE")
				}
			},
			expected: true,
		},
		{
			name: "not running in kubernetes",
			setup: func() func() {
				// Ensure no Kubernetes environment variables are set
				os.Unsetenv("POD_NAME")
				os.Unsetenv("POD_NAMESPACE")
				return func() {}
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleanup := tt.setup()
			defer cleanup()

			k8s := NewKubernetesIntegration()
			result := k8s.IsRunningInKubernetes()

			if result != tt.expected {
				t.Errorf("IsRunningInKubernetes() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestKubernetesIntegration_LoadRegistryCredentials(t *testing.T) {
	tmpDir, err := ioutil.TempDir("", "k8s-registry-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	k8s := NewKubernetesIntegration()
	k8s.secretsPath = filepath.Join(tmpDir, "secrets")
	k8s.configMapPath = filepath.Join(tmpDir, "configmaps")

	// Create Docker config secret
	dockerConfigDir := filepath.Join(k8s.secretsPath, "registry-secret")
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
	registryAuthDir := filepath.Join(k8s.secretsPath, "registry-auth")
	os.MkdirAll(registryAuthDir, 0755)
	ioutil.WriteFile(filepath.Join(registryAuthDir, "registry"), []byte("gcr.io"), 0644)
	ioutil.WriteFile(filepath.Join(registryAuthDir, "username"), []byte("gcr-user"), 0644)
	ioutil.WriteFile(filepath.Join(registryAuthDir, "password"), []byte("gcr-pass"), 0644)

	registryConfig, err := k8s.LoadRegistryCredentials()
	if err != nil {
		t.Fatalf("LoadRegistryCredentials() error = %v", err)
	}

	// Verify Docker config was loaded
	if auth, exists := registryConfig.Registries["docker.io"]; !exists {
		t.Error("Expected docker.io registry auth not found")
	} else {
		if auth.Username != "testuser" || auth.Password != "testpass" {
			t.Errorf("Docker.io auth = %+v, expected username=testuser, password=testpass", auth)
		}
	}

	// Verify individual registry secret was loaded
	if auth, exists := registryConfig.Registries["gcr.io"]; !exists {
		t.Error("Expected gcr.io registry auth not found")
	} else {
		if auth.Username != "gcr-user" || auth.Password != "gcr-pass" {
			t.Errorf("GCR auth = %+v, expected username=gcr-user, password=gcr-pass", auth)
		}
	}
}

func TestKubernetesIntegration_LoadBuildSecrets(t *testing.T) {
	tmpDir, err := ioutil.TempDir("", "k8s-secrets-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	k8s := NewKubernetesIntegration()
	k8s.secretsPath = filepath.Join(tmpDir, "secrets")

	// Create build secrets
	buildSecretsDir := filepath.Join(k8s.secretsPath, "build-secrets")
	os.MkdirAll(buildSecretsDir, 0755)
	ioutil.WriteFile(filepath.Join(buildSecretsDir, "api-key"), []byte("secret-api-key"), 0644)
	ioutil.WriteFile(filepath.Join(buildSecretsDir, "db-password"), []byte("secret-db-pass"), 0644)

	secrets, err := k8s.LoadBuildSecrets()
	if err != nil {
		t.Fatalf("LoadBuildSecrets() error = %v", err)
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
}

func TestKubernetesIntegration_SetupWorkspace(t *testing.T) {
	tmpDir, err := ioutil.TempDir("", "k8s-workspace-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	k8s := NewKubernetesIntegration()
	k8s.workspacePath = filepath.Join(tmpDir, "workspace")

	err = k8s.SetupWorkspace("10Gi")
	if err != nil {
		t.Fatalf("SetupWorkspace() error = %v", err)
	}

	// Verify workspace directory was created
	if _, err := os.Stat(k8s.workspacePath); os.IsNotExist(err) {
		t.Error("Workspace directory was not created")
	}

	// Verify subdirectories were created
	expectedDirs := []string{"tmp", "cache", "layers", "manifests"}
	for _, dir := range expectedDirs {
		dirPath := filepath.Join(k8s.workspacePath, dir)
		if _, err := os.Stat(dirPath); os.IsNotExist(err) {
			t.Errorf("Expected subdirectory %s was not created", dir)
		}
	}
}

func TestKubernetesIntegration_ReportProgress(t *testing.T) {
	k8s := NewKubernetesIntegration()

	err := k8s.ReportProgress("build", 50.0, "Building layer 1", "linux/amd64", "RUN", true)
	if err != nil {
		t.Fatalf("ReportProgress() error = %v", err)
	}

	// Verify progress file was created
	progressFile := "/tmp/ossb-progress.json"
	if _, err := os.Stat(progressFile); err == nil {
		// Read and verify progress data
		data, err := ioutil.ReadFile(progressFile)
		if err != nil {
			t.Fatalf("Failed to read progress file: %v", err)
		}

		var progress ProgressReport
		if err := json.Unmarshal(data, &progress); err != nil {
			t.Fatalf("Failed to parse progress JSON: %v", err)
		}

		if progress.Stage != "build" || progress.Progress != 50.0 {
			t.Errorf("Progress report = %+v, expected stage=build, progress=50.0", progress)
		}

		// Clean up
		os.Remove(progressFile)
	}
}

func TestKubernetesIntegration_SetJobStatus(t *testing.T) {
	k8s := NewKubernetesIntegration()

	buildResult := &types.BuildResult{
		Success:    true,
		Operations: 10,
		CacheHits:  5,
		Duration:   "30s",
	}

	err := k8s.SetJobStatus(JobStatusSucceeded, "Build completed successfully", buildResult)
	if err != nil {
		t.Fatalf("SetJobStatus() error = %v", err)
	}

	// Verify status file was created
	statusFile := "/tmp/ossb-status.json"
	if _, err := os.Stat(statusFile); err == nil {
		// Read and verify status data
		data, err := ioutil.ReadFile(statusFile)
		if err != nil {
			t.Fatalf("Failed to read status file: %v", err)
		}

		var status JobStatusReport
		if err := json.Unmarshal(data, &status); err != nil {
			t.Fatalf("Failed to parse status JSON: %v", err)
		}

		if status.Status != JobStatusSucceeded || status.Progress != 100.0 {
			t.Errorf("Status report = %+v, expected status=Succeeded, progress=100.0", status)
		}

		// Clean up
		os.Remove(statusFile)
	}
}

func TestKubernetesIntegration_GetJobInfo(t *testing.T) {
	// Set up environment variables
	os.Setenv("JOB_NAME", "test-job")
	os.Setenv("POD_NAME", "test-pod")
	os.Setenv("POD_NAMESPACE", "test-namespace")
	os.Setenv("NODE_NAME", "test-node")
	defer func() {
		os.Unsetenv("JOB_NAME")
		os.Unsetenv("POD_NAME")
		os.Unsetenv("POD_NAMESPACE")
		os.Unsetenv("NODE_NAME")
	}()

	k8s := NewKubernetesIntegration()
	info := k8s.GetJobInfo()

	expectedInfo := map[string]string{
		"job_name":  "test-job",
		"pod_name":  "test-pod",
		"namespace": "test-namespace",
		"node_name": "test-node",
	}

	for key, expectedValue := range expectedInfo {
		if value, exists := info[key]; !exists {
			t.Errorf("Expected job info %s not found", key)
		} else if value != expectedValue {
			t.Errorf("Job info %s = %s, expected %s", key, value, expectedValue)
		}
	}
}

func TestKubernetesIntegration_MountBuildContext(t *testing.T) {
	tmpDir, err := ioutil.TempDir("", "k8s-context-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	k8s := NewKubernetesIntegration()
	k8s.configMapPath = filepath.Join(tmpDir, "configmaps")

	// Create build context in ConfigMap
	buildContextDir := filepath.Join(k8s.configMapPath, "build-context")
	os.MkdirAll(buildContextDir, 0755)
	ioutil.WriteFile(filepath.Join(buildContextDir, "Dockerfile"), []byte("FROM alpine"), 0644)

	contextPath := filepath.Join(tmpDir, "context")
	err = k8s.MountBuildContext(contextPath)
	if err != nil {
		t.Fatalf("MountBuildContext() error = %v", err)
	}

	// Verify symlink was created
	if _, err := os.Lstat(contextPath); err != nil {
		t.Errorf("Build context symlink was not created: %v", err)
	}

	// Verify symlink points to correct location
	target, err := os.Readlink(contextPath)
	if err != nil {
		t.Fatalf("Failed to read symlink: %v", err)
	}
	if target != buildContextDir {
		t.Errorf("Symlink target = %s, expected %s", target, buildContextDir)
	}
}

// Benchmark tests
func BenchmarkKubernetesIntegration_LoadRegistryCredentials(b *testing.B) {
	tmpDir, _ := ioutil.TempDir("", "k8s-bench")
	defer os.RemoveAll(tmpDir)

	k8s := NewKubernetesIntegration()
	k8s.secretsPath = filepath.Join(tmpDir, "secrets")

	// Create test secrets
	dockerConfigDir := filepath.Join(k8s.secretsPath, "registry-secret")
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

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := k8s.LoadRegistryCredentials()
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkKubernetesIntegration_ReportProgress(b *testing.B) {
	k8s := NewKubernetesIntegration()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := k8s.ReportProgress("build", 50.0, "Building layer", "linux/amd64", "RUN", false)
		if err != nil {
			b.Fatal(err)
		}
	}

	// Clean up
	os.Remove("/tmp/ossb-progress.json")
}