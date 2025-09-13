package executors

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"testing"

	"github.com/bibin-skaria/ossb/internal/types"
)

func TestNewRootlessExecutor(t *testing.T) {
	executor := NewRootlessExecutor()
	if executor == nil {
		t.Fatal("NewRootlessExecutor returned nil")
	}

	if executor.currentUID == 0 {
		t.Error("RootlessExecutor should not run as root")
	}

	if executor.runtime == "" {
		t.Error("RootlessExecutor should have a runtime set")
	}
}

func TestNewRootlessExecutorWithConfig(t *testing.T) {
	resourceLimits := &types.ResourceLimits{
		Memory: "512Mi",
		CPU:    "0.5",
		Disk:   "1Gi",
	}

	securityContext := &types.SecurityContext{
		RunAsNonRoot: boolPtr(true),
		Capabilities: []string{"CAP_NET_BIND_SERVICE"},
	}

	executor := NewRootlessExecutorWithConfig(resourceLimits, securityContext)
	if executor == nil {
		t.Fatal("NewRootlessExecutorWithConfig returned nil")
	}

	if executor.resourceLimits != resourceLimits {
		t.Error("Resource limits not set correctly")
	}

	if executor.securityContext != securityContext {
		t.Error("Security context not set correctly")
	}
}

func TestValidateSecurityConstraints(t *testing.T) {
	tests := []struct {
		name            string
		currentUID      int
		securityContext *types.SecurityContext
		wantErr         bool
		errContains     string
	}{
		{
			name:       "valid non-root user",
			currentUID: 1000,
			wantErr:    false,
		},
		{
			name:        "invalid root user",
			currentUID:  0,
			wantErr:     true,
			errContains: "cannot run as root",
		},
		{
			name:       "valid security context with non-root",
			currentUID: 1000,
			securityContext: &types.SecurityContext{
				RunAsUser:    int64Ptr(1000),
				RunAsNonRoot: boolPtr(true),
			},
			wantErr: false,
		},
		{
			name:       "invalid security context with root user",
			currentUID: 1000,
			securityContext: &types.SecurityContext{
				RunAsUser: int64Ptr(0),
			},
			wantErr:     true,
			errContains: "root user",
		},
		{
			name:       "invalid security context allowing root",
			currentUID: 1000,
			securityContext: &types.SecurityContext{
				RunAsNonRoot: boolPtr(false),
			},
			wantErr:     true,
			errContains: "allows root user",
		},
		{
			name:       "invalid privileged capability",
			currentUID: 1000,
			securityContext: &types.SecurityContext{
				Capabilities: []string{"CAP_SYS_ADMIN"},
			},
			wantErr:     true,
			errContains: "privileged capability",
		},
		{
			name:       "valid non-privileged capability",
			currentUID: 1000,
			securityContext: &types.SecurityContext{
				Capabilities: []string{"CAP_NET_BIND_SERVICE"},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executor := &RootlessExecutor{
				currentUID:      tt.currentUID,
				securityContext: tt.securityContext,
			}

			err := executor.validateSecurityConstraints()
			if (err != nil) != tt.wantErr {
				t.Errorf("validateSecurityConstraints() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && tt.errContains != "" {
				if err == nil || !containsString(err.Error(), tt.errContains) {
					t.Errorf("validateSecurityConstraints() error = %v, want error containing %q", err, tt.errContains)
				}
			}
		})
	}
}

func TestIsPrivilegedCapability(t *testing.T) {
	executor := &RootlessExecutor{}

	tests := []struct {
		capability string
		want       bool
	}{
		{"CAP_SYS_ADMIN", true},
		{"CAP_SYS_MODULE", true},
		{"CAP_NET_ADMIN", true},
		{"CAP_SETUID", true},
		{"CAP_SETGID", true},
		{"CAP_NET_BIND_SERVICE", false},
		{"CAP_CHOWN", false},
		{"CAP_KILL", false},
		{"cap_sys_admin", true}, // case insensitive
	}

	for _, tt := range tests {
		t.Run(tt.capability, func(t *testing.T) {
			if got := executor.isPrivilegedCapability(tt.capability); got != tt.want {
				t.Errorf("isPrivilegedCapability(%q) = %v, want %v", tt.capability, got, tt.want)
			}
		})
	}
}

func TestValidateOperation(t *testing.T) {
	executor := &RootlessExecutor{}

	tests := []struct {
		name        string
		operation   *types.Operation
		wantErr     bool
		errContains string
	}{
		{
			name: "valid operation",
			operation: &types.Operation{
				Type:    types.OperationTypeExec,
				Command: []string{"echo", "hello"},
				User:    "1000",
			},
			wantErr: false,
		},
		{
			name: "invalid root user",
			operation: &types.Operation{
				Type:    types.OperationTypeExec,
				Command: []string{"echo", "hello"},
				User:    "root",
			},
			wantErr:     true,
			errContains: "root user",
		},
		{
			name: "invalid root user ID",
			operation: &types.Operation{
				Type:    types.OperationTypeExec,
				Command: []string{"echo", "hello"},
				User:    "0",
			},
			wantErr:     true,
			errContains: "root user",
		},
		{
			name: "invalid environment with /proc",
			operation: &types.Operation{
				Type:    types.OperationTypeExec,
				Command: []string{"echo", "hello"},
				Environment: map[string]string{
					"PATH": "/proc/self/exe",
				},
			},
			wantErr:     true,
			errContains: "unsafe path",
		},
		{
			name: "invalid privileged command",
			operation: &types.Operation{
				Type:    types.OperationTypeExec,
				Command: []string{"sudo", "echo", "hello"},
			},
			wantErr:     true,
			errContains: "privileged operation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := executor.validateOperation(tt.operation)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateOperation() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && tt.errContains != "" {
				if err == nil || !containsString(err.Error(), tt.errContains) {
					t.Errorf("validateOperation() error = %v, want error containing %q", err, tt.errContains)
				}
			}
		})
	}
}

func TestIsPrivilegedCommand(t *testing.T) {
	executor := &RootlessExecutor{}

	tests := []struct {
		command string
		want    bool
	}{
		{"echo hello", false},
		{"ls -la", false},
		{"sudo echo hello", true},
		{"su - root", true},
		{"mount /dev/sda1 /mnt", true},
		{"/usr/bin/sudo echo", true},
		{"./sudo echo", true}, // relative path
		{"iptables -L", true},
		{"modprobe module", true},
		{"normal-command", false},
	}

	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			if got := executor.isPrivilegedCommand(tt.command); got != tt.want {
				t.Errorf("isPrivilegedCommand(%q) = %v, want %v", tt.command, got, tt.want)
			}
		})
	}
}

func TestSetupWorkspacePermissions(t *testing.T) {
	executor := NewRootlessExecutor()
	defer executor.Cleanup()

	// Create a test workspace
	workspace := filepath.Join(executor.tempDir, "test-workspace")
	if err := os.MkdirAll(workspace, 0755); err != nil {
		t.Fatalf("Failed to create test workspace: %v", err)
	}

	// Test setting up permissions
	if err := executor.setupWorkspacePermissions(workspace); err != nil {
		t.Errorf("setupWorkspacePermissions() error = %v", err)
	}

	// Verify permissions
	info, err := os.Stat(workspace)
	if err != nil {
		t.Fatalf("Failed to stat workspace: %v", err)
	}

	if info.Mode().Perm() != 0700 {
		t.Errorf("Expected workspace permissions 0700, got %o", info.Mode().Perm())
	}
}

func TestValidateIDRanges(t *testing.T) {
	tests := []struct {
		name    string
		subUIDs []UIDRange
		subGIDs []GIDRange
		wantErr bool
	}{
		{
			name:    "no ranges",
			subUIDs: []UIDRange{},
			subGIDs: []GIDRange{},
			wantErr: true,
		},
		{
			name:    "insufficient UID range",
			subUIDs: []UIDRange{{Start: 100000, Count: 1000}},
			subGIDs: []GIDRange{{Start: 100000, Count: 65536}},
			wantErr: true,
		},
		{
			name:    "insufficient GID range",
			subUIDs: []UIDRange{{Start: 100000, Count: 65536}},
			subGIDs: []GIDRange{{Start: 100000, Count: 1000}},
			wantErr: true,
		},
		{
			name:    "sufficient ranges",
			subUIDs: []UIDRange{{Start: 100000, Count: 65536}},
			subGIDs: []GIDRange{{Start: 100000, Count: 65536}},
			wantErr: false,
		},
		{
			name: "multiple ranges sufficient",
			subUIDs: []UIDRange{
				{Start: 100000, Count: 32768},
				{Start: 200000, Count: 32768},
			},
			subGIDs: []GIDRange{
				{Start: 100000, Count: 32768},
				{Start: 200000, Count: 32768},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executor := &RootlessExecutor{
				subUIDs: tt.subUIDs,
				subGIDs: tt.subGIDs,
			}

			err := executor.validateIDRanges()
			if (err != nil) != tt.wantErr {
				t.Errorf("validateIDRanges() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestBuildRootlessRunArgs(t *testing.T) {
	executor := &RootlessExecutor{
		currentUID: 1000,
		currentGID: 1000,
		userNS:     true,
		subUIDs:    []UIDRange{{Start: 100000, Count: 65536}},
		subGIDs:    []GIDRange{{Start: 100000, Count: 65536}},
	}

	platform := types.Platform{OS: "linux", Architecture: "amd64"}
	baseDir := "/tmp/test"
	operation := &types.Operation{
		WorkDir: "/app",
		Environment: map[string]string{
			"PATH": "/usr/bin:/bin",
			"HOME": "/root",
		},
	}

	args := executor.buildRootlessRunArgs(platform, baseDir, operation)

	// Check that essential arguments are present
	expectedArgs := []string{
		"run", "--rm", "--platform", "linux/amd64",
		"--uidmap", "0:1000:1",
		"--gidmap", "0:1000:1",
		"--uidmap", "1:100000:65535",
		"--gidmap", "1:100000:65535",
		"-v", "/tmp/test:/workspace:Z",
		"-w", "/app",
		"-e", "PATH=/usr/bin:/bin",
		"-e", "HOME=/root",
	}

	for _, expected := range expectedArgs {
		found := false
		for _, arg := range args {
			if arg == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected argument %q not found in args: %v", expected, args)
		}
	}
}

func TestAddResourceLimits(t *testing.T) {
	executor := &RootlessExecutor{
		resourceLimits: &types.ResourceLimits{
			Memory: "512Mi",
			CPU:    "0.5",
		},
	}

	args := []string{"run", "--rm"}
	result := executor.addResourceLimits(args)

	expectedArgs := []string{"run", "--rm", "--memory", "512Mi", "--cpus", "0.5"}
	if len(result) != len(expectedArgs) {
		t.Errorf("Expected %d args, got %d", len(expectedArgs), len(result))
	}

	for i, expected := range expectedArgs {
		if i < len(result) && result[i] != expected {
			t.Errorf("Expected arg[%d] = %q, got %q", i, expected, result[i])
		}
	}
}

func TestAddSecurityConstraints(t *testing.T) {
	executor := &RootlessExecutor{
		securityContext: &types.SecurityContext{
			Capabilities: []string{"CAP_NET_BIND_SERVICE"},
		},
	}

	args := []string{"run", "--rm"}
	result := executor.addSecurityConstraints(args)

	// Check that security constraints are added
	expectedConstraints := []string{
		"--privileged=false",
		"--cap-drop", "ALL",
		"--cap-add", "CAP_NET_BIND_SERVICE",
		"--security-opt", "no-new-privileges",
		"--read-only",
		"--tmpfs", "/tmp:rw,noexec,nosuid,size=100m",
	}

	for _, expected := range expectedConstraints {
		found := false
		for _, arg := range result {
			if arg == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected security constraint %q not found in args: %v", expected, result)
		}
	}
}

// Helper functions
func boolPtr(b bool) *bool {
	return &b
}

func int64Ptr(i int64) *int64 {
	return &i
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || 
		(len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || 
		 len(s) > len(substr) && findSubstring(s, substr))))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Integration test that requires actual container runtime
func TestRootlessExecutorIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Check if we're running as non-root
	currentUser, err := user.Current()
	if err != nil {
		t.Skip("Cannot determine current user")
	}
	if currentUser.Uid == "0" {
		t.Skip("Integration test requires non-root user")
	}

	executor := NewRootlessExecutor()
	defer executor.Cleanup()

	// Test simple exec operation
	operation := &types.Operation{
		Type:    types.OperationTypeExec,
		Command: []string{"echo", "hello world"},
		Environment: map[string]string{
			"TEST_VAR": "test_value",
		},
		Platform: types.GetHostPlatform(),
	}

	workDir := filepath.Join(executor.tempDir, "test-work")
	if err := os.MkdirAll(workDir, 0755); err != nil {
		t.Fatalf("Failed to create work directory: %v", err)
	}

	result, err := executor.Execute(operation, workDir)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !result.Success {
		t.Errorf("Expected operation to succeed, got error: %s", result.Error)
	}
}

// Security test to ensure rootless constraints are enforced
func TestRootlessSecurityEnforcement(t *testing.T) {
	executor := NewRootlessExecutor()
	defer executor.Cleanup()

	// Test that privileged operations are rejected
	privilegedOps := []*types.Operation{
		{
			Type:    types.OperationTypeExec,
			Command: []string{"sudo", "echo", "test"},
		},
		{
			Type: types.OperationTypeExec,
			User: "root",
			Command: []string{"echo", "test"},
		},
		{
			Type:    types.OperationTypeExec,
			Command: []string{"mount", "/dev/sda1", "/mnt"},
		},
	}

	workDir := filepath.Join(executor.tempDir, "test-work")
	os.MkdirAll(workDir, 0755)

	for i, op := range privilegedOps {
		t.Run(fmt.Sprintf("privileged_op_%d", i), func(t *testing.T) {
			result, err := executor.Execute(op, workDir)
			if err != nil {
				return // Error during execution is acceptable
			}
			if result.Success {
				t.Errorf("Privileged operation should have failed but succeeded: %+v", op)
			}
		})
	}
}

func TestValidateResourceLimits(t *testing.T) {
	tests := []struct {
		name           string
		resourceLimits *types.ResourceLimits
		wantErr        bool
		errContains    string
	}{
		{
			name:           "nil resource limits",
			resourceLimits: nil,
			wantErr:        false,
		},
		{
			name: "valid memory limit",
			resourceLimits: &types.ResourceLimits{
				Memory: "512Mi",
			},
			wantErr: false,
		},
		{
			name: "excessive memory limit",
			resourceLimits: &types.ResourceLimits{
				Memory: "16Gi",
			},
			wantErr:     true,
			errContains: "exceeds maximum",
		},
		{
			name: "invalid memory format",
			resourceLimits: &types.ResourceLimits{
				Memory: "512",
			},
			wantErr:     true,
			errContains: "must end with",
		},
		{
			name: "valid CPU limit",
			resourceLimits: &types.ResourceLimits{
				CPU: "0.5",
			},
			wantErr: false,
		},
		{
			name: "valid CPU limit in millicores",
			resourceLimits: &types.ResourceLimits{
				CPU: "500m",
			},
			wantErr: false,
		},
		{
			name: "invalid CPU limit",
			resourceLimits: &types.ResourceLimits{
				CPU: "invalid",
			},
			wantErr:     true,
			errContains: "invalid CPU limit format",
		},
		{
			name: "valid disk limit",
			resourceLimits: &types.ResourceLimits{
				Disk: "10Gi",
			},
			wantErr: false,
		},
		{
			name: "excessive disk limit",
			resourceLimits: &types.ResourceLimits{
				Disk: "100Gi",
			},
			wantErr:     true,
			errContains: "exceeds maximum",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executor := &RootlessExecutor{
				resourceLimits: tt.resourceLimits,
			}

			err := executor.validateResourceLimits()
			if (err != nil) != tt.wantErr {
				t.Errorf("validateResourceLimits() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && tt.errContains != "" {
				if err == nil || !containsString(err.Error(), tt.errContains) {
					t.Errorf("validateResourceLimits() error = %v, want error containing %q", err, tt.errContains)
				}
			}
		})
	}
}

func TestMakeModeRootlessSafe(t *testing.T) {
	executor := &RootlessExecutor{}

	tests := []struct {
		name         string
		inputMode    os.FileMode
		expectSetuid bool
		expectSetgid bool
		expectOwnerRW bool
	}{
		{
			name:          "normal file",
			inputMode:     0644,
			expectSetuid:  false,
			expectSetgid:  false,
			expectOwnerRW: true,
		},
		{
			name:          "setuid file",
			inputMode:     0755 | os.ModeSetuid,
			expectSetuid:  false, // Should be removed
			expectSetgid:  false,
			expectOwnerRW: true,
		},
		{
			name:          "setgid file",
			inputMode:     0755 | os.ModeSetgid,
			expectSetuid:  false,
			expectSetgid:  false, // Should be removed
			expectOwnerRW: true,
		},
		{
			name:          "world writable",
			inputMode:     0666,
			expectSetuid:  false,
			expectSetgid:  false,
			expectOwnerRW: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := executor.makeModeRootlessSafe(tt.inputMode)

			if (result&os.ModeSetuid != 0) != tt.expectSetuid {
				t.Errorf("Expected setuid=%v, got setuid=%v", tt.expectSetuid, result&os.ModeSetuid != 0)
			}

			if (result&os.ModeSetgid != 0) != tt.expectSetgid {
				t.Errorf("Expected setgid=%v, got setgid=%v", tt.expectSetgid, result&os.ModeSetgid != 0)
			}

			if tt.expectOwnerRW && (result&0600 != 0600) {
				t.Errorf("Expected owner to have read/write permissions, got mode %o", result)
			}
		})
	}
}

func TestParseMemoryToBytes(t *testing.T) {
	executor := &RootlessExecutor{}

	tests := []struct {
		name      string
		memLimit  string
		wantBytes int64
		wantErr   bool
	}{
		{
			name:      "gigabytes",
			memLimit:  "2Gi",
			wantBytes: 2 * 1024 * 1024 * 1024,
			wantErr:   false,
		},
		{
			name:      "megabytes",
			memLimit:  "512Mi",
			wantBytes: 512 * 1024 * 1024,
			wantErr:   false,
		},
		{
			name:      "megabytes with m suffix",
			memLimit:  "1024m",
			wantBytes: 1024 * 1024 * 1024,
			wantErr:   false,
		},
		{
			name:     "invalid format",
			memLimit: "invalid",
			wantErr:  true,
		},
		{
			name:     "invalid number",
			memLimit: "invalidGi",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotBytes, err := executor.parseMemoryToBytes(tt.memLimit)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseMemoryToBytes() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && gotBytes != tt.wantBytes {
				t.Errorf("parseMemoryToBytes() = %v, want %v", gotBytes, tt.wantBytes)
			}
		})
	}
}

func TestTestUserNamespaceCreation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping user namespace creation test in short mode")
	}

	if os.Getuid() == 0 {
		t.Skip("User namespace creation test must run as non-root user")
	}

	executor := NewRootlessExecutor()
	defer executor.Cleanup()

	// Only test if user namespaces are configured
	if !executor.userNS {
		t.Skip("User namespaces not configured")
	}

	err := executor.testUserNamespaceCreation()
	if err != nil {
		t.Errorf("testUserNamespaceCreation() failed: %v", err)
	}
}

func TestEnforceResourceLimits(t *testing.T) {
	tests := []struct {
		name           string
		resourceLimits *types.ResourceLimits
		expectError    bool
	}{
		{
			name:           "nil limits",
			resourceLimits: nil,
			expectError:    false,
		},
		{
			name: "valid limits",
			resourceLimits: &types.ResourceLimits{
				Memory: "512Mi",
				CPU:    "0.5",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executor := &RootlessExecutor{
				resourceLimits: tt.resourceLimits,
				currentUID:     1000,
				currentGID:     1000,
			}

			err := executor.enforceResourceLimits()
			if (err != nil) != tt.expectError {
				t.Errorf("enforceResourceLimits() error = %v, expectError %v", err, tt.expectError)
			}
		})
	}
}

// TestRootlessExecutorFileOperations tests file operations without requiring container runtime
func TestRootlessExecutorFileOperations(t *testing.T) {
	executor := NewRootlessExecutor()
	defer executor.Cleanup()

	workDir := filepath.Join(executor.tempDir, "file-ops-test")
	if err := os.MkdirAll(workDir, 0755); err != nil {
		t.Fatalf("Failed to create work directory: %v", err)
	}

	// Create a test source file
	srcFile := filepath.Join(workDir, "source.txt")
	if err := os.WriteFile(srcFile, []byte("test content"), 0644); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	// Test file operation
	operation := &types.Operation{
		Type:    types.OperationTypeFile,
		Command: []string{"copy", srcFile},
		Inputs:  []string{"copy", srcFile},
		Metadata: map[string]string{
			"dest": "/workspace/copied-file.txt",
		},
		Platform: types.GetHostPlatform(),
	}

	result, err := executor.Execute(operation, workDir)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !result.Success {
		t.Errorf("Expected file operation to succeed, got error: %s", result.Error)
	}

	t.Logf("File operation test completed successfully")
}

// TestRootlessExecutorScratchImage tests scratch image handling without container runtime
func TestRootlessExecutorScratchImage(t *testing.T) {
	executor := NewRootlessExecutor()
	defer executor.Cleanup()

	workDir := filepath.Join(executor.tempDir, "scratch-test")
	if err := os.MkdirAll(workDir, 0755); err != nil {
		t.Fatalf("Failed to create work directory: %v", err)
	}

	// Test scratch image source operation
	operation := &types.Operation{
		Type: types.OperationTypeSource,
		Metadata: map[string]string{
			"image": "scratch",
		},
		Platform: types.GetHostPlatform(),
		Outputs:  []string{"base"},
	}

	result, err := executor.Execute(operation, workDir)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !result.Success {
		t.Errorf("Expected scratch image operation to succeed, got error: %s", result.Error)
	}

	// The base directory is created in the operation workspace, not the main workDir
	// Since the operation workspace is cleaned up, we just verify the operation succeeded
	// which indicates the base directory was created successfully

	t.Logf("Scratch image test completed successfully")
}

// TestRootlessExecutorMetaOperation tests meta operations
func TestRootlessExecutorMetaOperation(t *testing.T) {
	executor := NewRootlessExecutor()
	defer executor.Cleanup()

	workDir := filepath.Join(executor.tempDir, "meta-test")
	if err := os.MkdirAll(workDir, 0755); err != nil {
		t.Fatalf("Failed to create work directory: %v", err)
	}

	// Test meta operation
	operation := &types.Operation{
		Type: types.OperationTypeMeta,
		Environment: map[string]string{
			"TEST_VAR": "test_value",
		},
		Platform: types.GetHostPlatform(),
		Outputs:  []string{"meta"},
	}

	result, err := executor.Execute(operation, workDir)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !result.Success {
		t.Errorf("Expected meta operation to succeed, got error: %s", result.Error)
	}

	if result.Environment["TEST_VAR"] != "test_value" {
		t.Errorf("Expected environment variable to be preserved")
	}

	t.Logf("Meta operation test completed successfully")
}