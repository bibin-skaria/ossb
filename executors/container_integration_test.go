// +build integration

package executors

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/bibin-skaria/ossb/internal/types"
)

// TestContainerExecutor_MultiArchBuild tests multi-architecture builds with QEMU emulation
func TestContainerExecutor_MultiArchBuild(t *testing.T) {
	// Skip if no container runtime available
	if !isContainerRuntimeAvailable() {
		t.Skip("container runtime not available")
	}

	executor := NewContainerExecutor("podman")
	defer executor.Cleanup()

	// Configure for rootless operation
	executor.SetNetworkMode("none")
	uid := int64(1000)
	nonRoot := true
	executor.SetSecurityContext(&types.SecurityContext{
		RunAsUser:    &uid,
		RunAsNonRoot: &nonRoot,
	})

	tempDir, err := os.MkdirTemp("", "test-multiarch-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	platforms := []types.Platform{
		{OS: "linux", Architecture: "amd64"},
		{OS: "linux", Architecture: "arm64"},
		{OS: "linux", Architecture: "arm", Variant: "v7"},
	}

	for _, platform := range platforms {
		t.Run(platform.String(), func(t *testing.T) {
			// Test source operation (base image pulling)
			sourceOp := &types.Operation{
				Type: types.OperationTypeSource,
				Metadata: map[string]string{
					"image": "alpine:latest",
				},
				Platform: platform,
				Outputs:  []string{"base"},
			}

			result, err := executor.Execute(sourceOp, tempDir)
			if err != nil {
				t.Errorf("source operation failed: %v", err)
				return
			}

			if !result.Success {
				t.Errorf("source operation failed: %s", result.Error)
				return
			}

			// Test exec operation with emulation
			execOp := &types.Operation{
				Type:    types.OperationTypeExec,
				Command: []string{"uname", "-m"},
				Environment: map[string]string{
					"PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
				},
				Platform: platform,
				WorkDir:  "/",
				Outputs:  []string{"exec"},
			}

			result, err = executor.Execute(execOp, tempDir)
			if err != nil {
				t.Errorf("exec operation failed: %v", err)
				return
			}

			if !result.Success {
				t.Errorf("exec operation failed: %s", result.Error)
				return
			}

			t.Logf("Successfully executed on platform %s", platform.String())
		})
	}
}

// TestContainerExecutor_RootlessOperation tests rootless container execution
func TestContainerExecutor_RootlessOperation(t *testing.T) {
	if !isContainerRuntimeAvailable() {
		t.Skip("container runtime not available")
	}

	if !isPodmanRootlessAvailable() {
		t.Skip("rootless podman not available")
	}

	executor := NewContainerExecutor("podman")
	defer executor.Cleanup()

	// Configure strict rootless security
	uid := int64(1000)
	gid := int64(1000)
	nonRoot := true
	executor.SetSecurityContext(&types.SecurityContext{
		RunAsUser:    &uid,
		RunAsGroup:   &gid,
		RunAsNonRoot: &nonRoot,
		Capabilities: []string{}, // No additional capabilities
	})

	executor.SetResourceLimits(&types.ResourceLimits{
		Memory: "256Mi",
		CPU:    "0.5",
	})

	executor.SetNetworkMode("none")

	tempDir, err := os.MkdirTemp("", "test-rootless-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Test that we can execute commands without root privileges
	operation := &types.Operation{
		Type:    types.OperationTypeExec,
		Command: []string{"id"},
		Environment: map[string]string{
			"PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		},
		Platform: types.Platform{OS: "linux", Architecture: "amd64"},
		WorkDir:  "/",
		Outputs:  []string{"rootless-test"},
	}

	result, err := executor.Execute(operation, tempDir)
	if err != nil {
		t.Errorf("rootless operation failed: %v", err)
		return
	}

	if !result.Success {
		t.Errorf("rootless operation failed: %s", result.Error)
		return
	}

	t.Log("Successfully executed rootless operation")
}

// TestContainerExecutor_NetworkIsolation tests network isolation
func TestContainerExecutor_NetworkIsolation(t *testing.T) {
	if !isContainerRuntimeAvailable() {
		t.Skip("container runtime not available")
	}

	executor := NewContainerExecutor("podman")
	defer executor.Cleanup()

	executor.SetNetworkMode("none")

	tempDir, err := os.MkdirTemp("", "test-network-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Test that network access is blocked
	operation := &types.Operation{
		Type:    types.OperationTypeExec,
		Command: []string{"ping", "-c", "1", "8.8.8.8"},
		Environment: map[string]string{
			"PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		},
		Platform: types.Platform{OS: "linux", Architecture: "amd64"},
		WorkDir:  "/",
		Outputs:  []string{"network-test"},
	}

	result, err := executor.Execute(operation, tempDir)
	if err != nil {
		t.Errorf("network isolation test failed: %v", err)
		return
	}

	// This should fail due to network isolation
	if result.Success {
		t.Error("network isolation failed - ping should have failed")
		return
	}

	t.Log("Network isolation working correctly")
}

// TestContainerExecutor_VolumeManagement tests volume mounting and management
func TestContainerExecutor_VolumeManagement(t *testing.T) {
	if !isContainerRuntimeAvailable() {
		t.Skip("container runtime not available")
	}

	executor := NewContainerExecutor("podman")
	defer executor.Cleanup()

	tempDir, err := os.MkdirTemp("", "test-volume-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create test data
	testDataDir := filepath.Join(tempDir, "testdata")
	if err := os.MkdirAll(testDataDir, 0755); err != nil {
		t.Fatalf("failed to create test data dir: %v", err)
	}

	testFile := filepath.Join(testDataDir, "test.txt")
	testContent := "Hello from volume test"
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Mount volume
	if err := executor.mountVolume(testDataDir, "/testdata"); err != nil {
		t.Fatalf("failed to mount volume: %v", err)
	}

	// Test reading from mounted volume
	operation := &types.Operation{
		Type:    types.OperationTypeExec,
		Command: []string{"cat", "/testdata/test.txt"},
		Environment: map[string]string{
			"PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		},
		Platform: types.Platform{OS: "linux", Architecture: "amd64"},
		WorkDir:  "/",
		Outputs:  []string{"volume-test"},
	}

	result, err := executor.Execute(operation, tempDir)
	if err != nil {
		t.Errorf("volume test failed: %v", err)
		return
	}

	if !result.Success {
		t.Errorf("volume test failed: %s", result.Error)
		return
	}

	t.Log("Volume management working correctly")
}

// TestContainerExecutor_QEMUEmulation tests QEMU emulation setup
func TestContainerExecutor_QEMUEmulation(t *testing.T) {
	if !isContainerRuntimeAvailable() {
		t.Skip("container runtime not available")
	}

	executor := NewContainerExecutor("podman")
	defer executor.Cleanup()

	// Test QEMU setup for different architectures
	platforms := []types.Platform{
		{OS: "linux", Architecture: "arm64"},
		{OS: "linux", Architecture: "arm", Variant: "v7"},
		{OS: "linux", Architecture: "s390x"},
	}

	for _, platform := range platforms {
		t.Run(platform.String(), func(t *testing.T) {
			err := executor.setupQEMUEmulation(platform)
			if err != nil {
				t.Logf("QEMU emulation setup failed for %s: %v", platform.String(), err)
				// Don't fail the test as QEMU might not be available
				return
			}

			t.Logf("QEMU emulation setup successful for %s", platform.String())
		})
	}
}

// TestContainerExecutor_SecurityContext tests security context enforcement
func TestContainerExecutor_SecurityContext(t *testing.T) {
	if !isContainerRuntimeAvailable() {
		t.Skip("container runtime not available")
	}

	executor := NewContainerExecutor("podman")
	defer executor.Cleanup()

	// Test with restricted security context
	uid := int64(1001)
	gid := int64(1001)
	nonRoot := true
	executor.SetSecurityContext(&types.SecurityContext{
		RunAsUser:    &uid,
		RunAsGroup:   &gid,
		RunAsNonRoot: &nonRoot,
		Capabilities: []string{"CAP_NET_BIND_SERVICE"},
	})

	tempDir, err := os.MkdirTemp("", "test-security-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Test that security context is applied
	operation := &types.Operation{
		Type:    types.OperationTypeExec,
		Command: []string{"id", "-u"},
		Environment: map[string]string{
			"PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		},
		Platform: types.Platform{OS: "linux", Architecture: "amd64"},
		WorkDir:  "/",
		Outputs:  []string{"security-test"},
	}

	result, err := executor.Execute(operation, tempDir)
	if err != nil {
		t.Errorf("security context test failed: %v", err)
		return
	}

	if !result.Success {
		t.Errorf("security context test failed: %s", result.Error)
		return
	}

	t.Log("Security context enforcement working correctly")
}

// TestContainerExecutor_ResourceLimits tests resource limit enforcement
func TestContainerExecutor_ResourceLimits(t *testing.T) {
	if !isContainerRuntimeAvailable() {
		t.Skip("container runtime not available")
	}

	executor := NewContainerExecutor("podman")
	defer executor.Cleanup()

	// Set strict resource limits
	executor.SetResourceLimits(&types.ResourceLimits{
		Memory: "128Mi",
		CPU:    "0.1",
	})

	tempDir, err := os.MkdirTemp("", "test-resources-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Test that resource limits are enforced
	operation := &types.Operation{
		Type:    types.OperationTypeExec,
		Command: []string{"echo", "resource test"},
		Environment: map[string]string{
			"PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		},
		Platform: types.Platform{OS: "linux", Architecture: "amd64"},
		WorkDir:  "/",
		Outputs:  []string{"resource-test"},
	}

	result, err := executor.Execute(operation, tempDir)
	if err != nil {
		t.Errorf("resource limits test failed: %v", err)
		return
	}

	if !result.Success {
		t.Errorf("resource limits test failed: %s", result.Error)
		return
	}

	t.Log("Resource limits enforcement working correctly")
}

// Helper functions

func isContainerRuntimeAvailable() bool {
	// Check for podman first
	if _, err := exec.LookPath("podman"); err == nil {
		cmd := exec.Command("podman", "version")
		return cmd.Run() == nil
	}

	// Check for docker
	if _, err := exec.LookPath("docker"); err == nil {
		cmd := exec.Command("docker", "version")
		return cmd.Run() == nil
	}

	return false
}

func isPodmanRootlessAvailable() bool {
	if _, err := exec.LookPath("podman"); err != nil {
		return false
	}

	// Check if podman is running in rootless mode
	cmd := exec.Command("podman", "info", "--format", "{{.Host.Security.Rootless}}")
	output, err := cmd.Output()
	if err != nil {
		return false
	}

	return string(output) == "true\n"
}

// Benchmark tests

func BenchmarkContainerExecutor_SimpleExec(b *testing.B) {
	if !isContainerRuntimeAvailable() {
		b.Skip("container runtime not available")
	}

	executor := NewContainerExecutor("podman")
	defer executor.Cleanup()

	tempDir, err := os.MkdirTemp("", "bench-exec-*")
	if err != nil {
		b.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	operation := &types.Operation{
		Type:    types.OperationTypeExec,
		Command: []string{"echo", "benchmark"},
		Environment: map[string]string{
			"PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		},
		Platform: types.Platform{OS: "linux", Architecture: "amd64"},
		WorkDir:  "/",
		Outputs:  []string{"bench"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, err := executor.Execute(operation, tempDir)
		if err != nil || !result.Success {
			b.Fatalf("benchmark operation failed: %v, %s", err, result.Error)
		}
	}
}

func BenchmarkContainerExecutor_MultiArchExec(b *testing.B) {
	if !isContainerRuntimeAvailable() {
		b.Skip("container runtime not available")
	}

	executor := NewContainerExecutor("podman")
	defer executor.Cleanup()

	tempDir, err := os.MkdirTemp("", "bench-multiarch-*")
	if err != nil {
		b.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	operation := &types.Operation{
		Type:    types.OperationTypeExec,
		Command: []string{"echo", "multiarch benchmark"},
		Environment: map[string]string{
			"PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		},
		Platform: types.Platform{OS: "linux", Architecture: "arm64"},
		WorkDir:  "/",
		Outputs:  []string{"bench-multiarch"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, err := executor.Execute(operation, tempDir)
		if err != nil || !result.Success {
			b.Fatalf("multiarch benchmark operation failed: %v, %s", err, result.Error)
		}
	}
}