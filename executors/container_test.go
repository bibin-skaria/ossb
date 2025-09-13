package executors

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bibin-skaria/ossb/internal/types"
)

func TestNewContainerExecutor(t *testing.T) {
	tests := []struct {
		name    string
		runtime string
	}{
		{
			name:    "default runtime",
			runtime: "",
		},
		{
			name:    "explicit docker",
			runtime: "docker",
		},
		{
			name:    "explicit podman",
			runtime: "podman",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executor := NewContainerExecutor(tt.runtime)
			
			// Check that a runtime was selected
			if executor.runtime == "" {
				t.Error("expected runtime to be set")
			}
			
			// Check that rootless is set correctly for podman
			if executor.runtime == "podman" && !executor.rootless {
				t.Error("expected rootless to be true for podman")
			}
			
			if executor.networkMode != "none" {
				t.Errorf("expected network mode 'none', got %s", executor.networkMode)
			}
			
			if executor.tempDir == "" {
				t.Error("expected temp directory to be created")
			}
			
			if executor.volumes == nil {
				t.Error("expected volumes map to be initialized")
			}
			
			// Check supported QEMU platforms
			expectedPlatforms := []string{
				"linux/amd64", "linux/arm64", "linux/arm/v7", "linux/arm/v6",
				"linux/386", "linux/ppc64le", "linux/s390x", "linux/riscv64",
				"linux/mips64", "linux/mips64le",
			}
			
			for _, platform := range expectedPlatforms {
				if _, exists := executor.supportedQEMU[platform]; !exists {
					t.Errorf("expected platform %s to be supported", platform)
				}
			}
			
			// Cleanup
			executor.Cleanup()
		})
	}
}

func TestContainerExecutor_SetSecurityContext(t *testing.T) {
	executor := NewContainerExecutor("podman")
	defer executor.Cleanup()
	
	uid := int64(1000)
	gid := int64(1000)
	nonRoot := true
	
	secCtx := &types.SecurityContext{
		RunAsUser:    &uid,
		RunAsGroup:   &gid,
		RunAsNonRoot: &nonRoot,
		Capabilities: []string{"CAP_NET_BIND_SERVICE"},
	}
	
	executor.SetSecurityContext(secCtx)
	
	if executor.securityContext == nil {
		t.Error("security context not set")
	}
	
	if *executor.securityContext.RunAsUser != uid {
		t.Errorf("expected user %d, got %d", uid, *executor.securityContext.RunAsUser)
	}
}

func TestContainerExecutor_SetResourceLimits(t *testing.T) {
	executor := NewContainerExecutor("podman")
	defer executor.Cleanup()
	
	limits := &types.ResourceLimits{
		Memory: "1Gi",
		CPU:    "1000m",
		Disk:   "10Gi",
	}
	
	executor.SetResourceLimits(limits)
	
	if executor.resourceLimits == nil {
		t.Error("resource limits not set")
	}
	
	if executor.resourceLimits.Memory != "1Gi" {
		t.Errorf("expected memory limit 1Gi, got %s", executor.resourceLimits.Memory)
	}
}

func TestContainerExecutor_SetNetworkMode(t *testing.T) {
	executor := NewContainerExecutor("podman")
	defer executor.Cleanup()
	
	executor.SetNetworkMode("bridge")
	
	if executor.networkMode != "bridge" {
		t.Errorf("expected network mode 'bridge', got %s", executor.networkMode)
	}
}

func TestContainerExecutor_ValidatePlatformSupport(t *testing.T) {
	executor := NewContainerExecutor("podman")
	defer executor.Cleanup()
	
	tests := []struct {
		name     string
		platform types.Platform
		wantErr  bool
	}{
		{
			name:     "supported amd64",
			platform: types.Platform{OS: "linux", Architecture: "amd64"},
			wantErr:  false,
		},
		{
			name:     "supported arm64",
			platform: types.Platform{OS: "linux", Architecture: "arm64"},
			wantErr:  false,
		},
		{
			name:     "unsupported platform",
			platform: types.Platform{OS: "windows", Architecture: "amd64"},
			wantErr:  true,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := executor.validatePlatformSupport(tt.platform)
			if (err != nil) != tt.wantErr {
				t.Errorf("validatePlatformSupport() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestContainerExecutor_MountVolume(t *testing.T) {
	executor := NewContainerExecutor("podman")
	defer executor.Cleanup()
	
	tempDir, err := os.MkdirTemp("", "test-mount-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)
	
	hostPath := filepath.Join(tempDir, "host")
	containerPath := "/container/path"
	
	err = executor.mountVolume(hostPath, containerPath)
	if err != nil {
		t.Errorf("mountVolume() error = %v", err)
	}
	
	if _, exists := executor.volumes[hostPath]; !exists {
		t.Error("volume not added to volumes map")
	}
	
	if executor.volumes[hostPath] != containerPath {
		t.Errorf("expected container path %s, got %s", containerPath, executor.volumes[hostPath])
	}
	
	// Check that host path was created
	if _, err := os.Stat(hostPath); os.IsNotExist(err) {
		t.Error("host path was not created")
	}
}

func TestContainerExecutor_CreateTempVolume(t *testing.T) {
	executor := NewContainerExecutor("podman")
	defer executor.Cleanup()
	
	volumePath, err := executor.createTempVolume("test-volume")
	if err != nil {
		t.Errorf("createTempVolume() error = %v", err)
	}
	
	if volumePath == "" {
		t.Error("volume path is empty")
	}
	
	// Check that volume path was created
	if _, err := os.Stat(volumePath); os.IsNotExist(err) {
		t.Error("volume path was not created")
	}
	
	// Check that it's under temp directory
	if !filepath.HasPrefix(volumePath, executor.tempDir) {
		t.Error("volume path is not under temp directory")
	}
}

func TestContainerExecutor_Cleanup(t *testing.T) {
	executor := NewContainerExecutor("podman")
	
	tempDir := executor.tempDir
	
	// Create a test file in temp directory
	testFile := filepath.Join(tempDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}
	
	// Verify file exists
	if _, err := os.Stat(testFile); os.IsNotExist(err) {
		t.Fatal("test file was not created")
	}
	
	// Cleanup
	err := executor.Cleanup()
	if err != nil {
		t.Errorf("Cleanup() error = %v", err)
	}
	
	// Verify temp directory is removed
	if _, err := os.Stat(tempDir); !os.IsNotExist(err) {
		t.Error("temp directory was not removed")
	}
}

// Mock tests for operations that require actual container runtime
func TestContainerExecutor_ExecuteSource_Mock(t *testing.T) {
	executor := NewContainerExecutor("podman")
	defer executor.Cleanup()
	
	tempDir, err := os.MkdirTemp("", "test-execute-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)
	
	operation := &types.Operation{
		Type: types.OperationTypeSource,
		Metadata: map[string]string{
			"image": "scratch",
		},
		Platform: types.Platform{OS: "linux", Architecture: "amd64"},
		Outputs:  []string{"base"},
	}
	
	result, err := executor.executeSource(operation, tempDir, &types.OperationResult{
		Operation: operation,
		Success:   false,
	})
	
	if err != nil {
		t.Errorf("executeSource() error = %v", err)
	}
	
	if !result.Success {
		t.Errorf("executeSource() failed: %s", result.Error)
	}
	
	// Check that base directory was created
	baseDir := filepath.Join(tempDir, "base", "linux/amd64")
	if _, err := os.Stat(baseDir); os.IsNotExist(err) {
		t.Error("base directory was not created")
	}
}

func TestContainerExecutor_ExecuteMeta(t *testing.T) {
	executor := NewContainerExecutor("podman")
	defer executor.Cleanup()
	
	operation := &types.Operation{
		Type:        types.OperationTypeMeta,
		Environment: map[string]string{"TEST": "value"},
		Outputs:     []string{"meta"},
	}
	
	result, err := executor.executeMeta(operation, "", &types.OperationResult{
		Operation: operation,
		Success:   false,
	})
	
	if err != nil {
		t.Errorf("executeMeta() error = %v", err)
	}
	
	if !result.Success {
		t.Errorf("executeMeta() failed: %s", result.Error)
	}
	
	if result.Environment["TEST"] != "value" {
		t.Error("environment not preserved")
	}
}

// Integration test markers - these would require actual container runtime
func TestContainerExecutor_Integration_MultiArch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	
	// This test would require actual podman/docker and QEMU setup
	t.Skip("integration test requires container runtime setup")
}

func TestContainerExecutor_Integration_RootlessMode(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	
	// This test would require actual rootless podman setup
	t.Skip("integration test requires rootless container runtime setup")
}

func TestContainerExecutor_Integration_NetworkIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	
	// This test would require actual container runtime and network setup
	t.Skip("integration test requires container runtime and network setup")
}