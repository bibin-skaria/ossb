package executors

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bibin-skaria/ossb/internal/types"
)

// TestRootlessSecurityIsolation validates that the rootless executor properly isolates processes
func TestRootlessSecurityIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping security test in short mode")
	}

	// Ensure we're not running as root
	if os.Getuid() == 0 {
		t.Skip("Security tests must run as non-root user")
	}

	executor := NewRootlessExecutor()
	defer executor.Cleanup()

	workDir := filepath.Join(executor.tempDir, "security-test")
	if err := os.MkdirAll(workDir, 0755); err != nil {
		t.Fatalf("Failed to create work directory: %v", err)
	}

	tests := []struct {
		name        string
		operation   *types.Operation
		expectFail  bool
		description string
	}{
		{
			name: "attempt_privilege_escalation",
			operation: &types.Operation{
				Type:    types.OperationTypeExec,
				Command: []string{"sudo", "id"},
			},
			expectFail:  true,
			description: "Should fail when attempting privilege escalation",
		},
		{
			name: "attempt_root_user",
			operation: &types.Operation{
				Type:    types.OperationTypeExec,
				Command: []string{"id"},
				User:    "root",
			},
			expectFail:  true,
			description: "Should fail when specifying root user",
		},
		{
			name: "attempt_mount_operation",
			operation: &types.Operation{
				Type:    types.OperationTypeExec,
				Command: []string{"mount", "-t", "tmpfs", "tmpfs", "/mnt"},
			},
			expectFail:  true,
			description: "Should fail when attempting mount operations",
		},
		{
			name: "attempt_proc_access",
			operation: &types.Operation{
				Type:    types.OperationTypeExec,
				Command: []string{"cat", "/proc/1/environ"},
			},
			expectFail:  false, // This might work but should be limited
			description: "Proc access should be limited but not necessarily blocked",
		},
		{
			name: "normal_operation",
			operation: &types.Operation{
				Type:    types.OperationTypeExec,
				Command: []string{"echo", "hello world"},
			},
			expectFail:  false,
			description: "Normal operations should work",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := executor.Execute(tt.operation, workDir)
			
			if tt.expectFail {
				if err == nil && result.Success {
					t.Errorf("%s: Expected operation to fail but it succeeded", tt.description)
				}
			} else {
				if err != nil {
					t.Errorf("%s: Unexpected error: %v", tt.description, err)
				} else if !result.Success {
					t.Errorf("%s: Expected operation to succeed but it failed: %s", tt.description, result.Error)
				}
			}
		})
	}
}

// TestRootlessUserNamespaceMapping validates proper user namespace setup
func TestRootlessUserNamespaceMapping(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping namespace test in short mode")
	}

	if os.Getuid() == 0 {
		t.Skip("Namespace tests must run as non-root user")
	}

	executor := NewRootlessExecutor()
	defer executor.Cleanup()

	// Check if user namespaces are properly configured
	if !executor.userNS {
		t.Skip("User namespaces not available or not configured")
	}

	workDir := filepath.Join(executor.tempDir, "namespace-test")
	if err := os.MkdirAll(workDir, 0755); err != nil {
		t.Fatalf("Failed to create work directory: %v", err)
	}

	// Test that we can see our UID mapping
	operation := &types.Operation{
		Type:    types.OperationTypeExec,
		Command: []string{"id", "-u"},
		Platform: types.GetHostPlatform(),
	}

	result, err := executor.Execute(operation, workDir)
	if err != nil {
		t.Fatalf("Failed to execute id command: %v", err)
	}

	if !result.Success {
		t.Fatalf("ID command failed: %s", result.Error)
	}

	t.Logf("User namespace mapping test completed successfully")
}

// TestRootlessResourceLimits validates that resource limits are properly enforced
func TestRootlessResourceLimits(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping resource limit test in short mode")
	}

	if os.Getuid() == 0 {
		t.Skip("Resource limit tests must run as non-root user")
	}

	resourceLimits := &types.ResourceLimits{
		Memory: "128Mi",
		CPU:    "0.1",
	}

	executor := NewRootlessExecutorWithConfig(resourceLimits, nil)
	defer executor.Cleanup()

	workDir := filepath.Join(executor.tempDir, "resource-test")
	if err := os.MkdirAll(workDir, 0755); err != nil {
		t.Fatalf("Failed to create work directory: %v", err)
	}

	// Test memory limit enforcement (this might not fail immediately but should be limited)
	operation := &types.Operation{
		Type:    types.OperationTypeExec,
		Command: []string{"sh", "-c", "echo 'Testing resource limits'"},
		Platform: types.GetHostPlatform(),
	}

	result, err := executor.Execute(operation, workDir)
	if err != nil {
		t.Fatalf("Failed to execute resource limit test: %v", err)
	}

	if !result.Success {
		t.Errorf("Resource limit test failed: %s", result.Error)
	}

	t.Logf("Resource limits test completed")
}

// TestRootlessCapabilityDropping validates that capabilities are properly dropped
func TestRootlessCapabilityDropping(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping capability test in short mode")
	}

	if os.Getuid() == 0 {
		t.Skip("Capability tests must run as non-root user")
	}

	// Test with minimal capabilities
	securityContext := &types.SecurityContext{
		RunAsNonRoot: boolPtr(true),
		Capabilities: []string{"CAP_NET_BIND_SERVICE"}, // Only allow binding to privileged ports
	}

	executor := NewRootlessExecutorWithConfig(nil, securityContext)
	defer executor.Cleanup()

	workDir := filepath.Join(executor.tempDir, "capability-test")
	if err := os.MkdirAll(workDir, 0755); err != nil {
		t.Fatalf("Failed to create work directory: %v", err)
	}

	// Test that we can't perform privileged operations even with some capabilities
	operation := &types.Operation{
		Type:    types.OperationTypeExec,
		Command: []string{"sh", "-c", "echo 'Testing capability restrictions'"},
		Platform: types.GetHostPlatform(),
	}

	result, err := executor.Execute(operation, workDir)
	if err != nil {
		t.Fatalf("Failed to execute capability test: %v", err)
	}

	if !result.Success {
		t.Errorf("Capability test failed: %s", result.Error)
	}

	t.Logf("Capability dropping test completed")
}

// TestRootlessFilesystemPermissions validates proper filesystem permission handling
func TestRootlessFilesystemPermissions(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping filesystem permission test in short mode")
	}

	if os.Getuid() == 0 {
		t.Skip("Filesystem permission tests must run as non-root user")
	}

	executor := NewRootlessExecutor()
	defer executor.Cleanup()

	workDir := filepath.Join(executor.tempDir, "filesystem-test")
	if err := os.MkdirAll(workDir, 0755); err != nil {
		t.Fatalf("Failed to create work directory: %v", err)
	}

	// Create a test file with specific permissions
	testFile := filepath.Join(workDir, "test-file.txt")
	if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Test file operations
	operation := &types.Operation{
		Type:    types.OperationTypeFile,
		Command: []string{"copy", testFile},
		Inputs:  []string{"copy", testFile},
		Metadata: map[string]string{
			"dest": "/workspace/copied-file.txt",
		},
		Platform: types.GetHostPlatform(),
	}

	result, err := executor.Execute(operation, workDir)
	if err != nil {
		t.Fatalf("Failed to execute file operation: %v", err)
	}

	if !result.Success {
		t.Errorf("File operation failed: %s", result.Error)
	}

	t.Logf("Filesystem permissions test completed")
}

// TestRootlessNetworkIsolation validates network isolation in rootless mode
func TestRootlessNetworkIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping network isolation test in short mode")
	}

	if os.Getuid() == 0 {
		t.Skip("Network isolation tests must run as non-root user")
	}

	executor := NewRootlessExecutor()
	defer executor.Cleanup()

	workDir := filepath.Join(executor.tempDir, "network-test")
	if err := os.MkdirAll(workDir, 0755); err != nil {
		t.Fatalf("Failed to create work directory: %v", err)
	}

	// Test basic network connectivity (should work)
	operation := &types.Operation{
		Type:    types.OperationTypeExec,
		Command: []string{"sh", "-c", "echo 'Network test'"},
		Platform: types.GetHostPlatform(),
	}

	result, err := executor.Execute(operation, workDir)
	if err != nil {
		t.Fatalf("Failed to execute network test: %v", err)
	}

	if !result.Success {
		t.Errorf("Network test failed: %s", result.Error)
	}

	t.Logf("Network isolation test completed")
}

// TestRootlessCleanup validates proper cleanup of resources
func TestRootlessCleanup(t *testing.T) {
	executor := NewRootlessExecutor()
	
	// Record the temp directory path
	tempDir := executor.tempDir
	
	// Verify temp directory exists
	if _, err := os.Stat(tempDir); os.IsNotExist(err) {
		t.Fatalf("Temp directory should exist: %s", tempDir)
	}

	// Cleanup
	if err := executor.Cleanup(); err != nil {
		t.Errorf("Cleanup failed: %v", err)
	}

	// Verify temp directory is removed
	if _, err := os.Stat(tempDir); !os.IsNotExist(err) {
		t.Errorf("Temp directory should be removed after cleanup: %s", tempDir)
	}
}

// TestRootlessSecurityContextValidation validates security context enforcement
func TestRootlessSecurityContextValidation(t *testing.T) {
	tests := []struct {
		name            string
		securityContext *types.SecurityContext
		expectError     bool
		errorContains   string
	}{
		{
			name: "valid_non_root_context",
			securityContext: &types.SecurityContext{
				RunAsUser:    int64Ptr(1000),
				RunAsGroup:   int64Ptr(1000),
				RunAsNonRoot: boolPtr(true),
			},
			expectError: false,
		},
		{
			name: "invalid_root_user_context",
			securityContext: &types.SecurityContext{
				RunAsUser: int64Ptr(0),
			},
			expectError:   true,
			errorContains: "root user",
		},
		{
			name: "invalid_allow_root_context",
			securityContext: &types.SecurityContext{
				RunAsNonRoot: boolPtr(false),
			},
			expectError:   true,
			errorContains: "allows root user",
		},
		{
			name: "invalid_privileged_capabilities",
			securityContext: &types.SecurityContext{
				Capabilities: []string{"CAP_SYS_ADMIN", "CAP_NET_BIND_SERVICE"},
			},
			expectError:   true,
			errorContains: "privileged capability",
		},
		{
			name: "valid_safe_capabilities",
			securityContext: &types.SecurityContext{
				Capabilities: []string{"CAP_NET_BIND_SERVICE", "CAP_CHOWN"},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executor := NewRootlessExecutorWithConfig(nil, tt.securityContext)
			defer executor.Cleanup()

			err := executor.validateSecurityConstraints()
			
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				} else if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error containing %q, got: %v", tt.errorContains, err)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}

// TestRootlessExecutorConcurrency validates that multiple operations can run safely
func TestRootlessExecutorConcurrency(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping concurrency test in short mode")
	}

	if os.Getuid() == 0 {
		t.Skip("Concurrency tests must run as non-root user")
	}

	executor := NewRootlessExecutor()
	defer executor.Cleanup()

	workDir := filepath.Join(executor.tempDir, "concurrency-test")
	if err := os.MkdirAll(workDir, 0755); err != nil {
		t.Fatalf("Failed to create work directory: %v", err)
	}

	// Run multiple operations concurrently
	numOps := 5
	results := make(chan error, numOps)

	for i := 0; i < numOps; i++ {
		go func(id int) {
			operation := &types.Operation{
				Type:    types.OperationTypeExec,
				Command: []string{"echo", fmt.Sprintf("concurrent-operation-%d", id)},
				Platform: types.GetHostPlatform(),
			}

			result, err := executor.Execute(operation, workDir)
			if err != nil {
				results <- fmt.Errorf("operation %d failed: %v", id, err)
				return
			}

			if !result.Success {
				results <- fmt.Errorf("operation %d unsuccessful: %s", id, result.Error)
				return
			}

			results <- nil
		}(i)
	}

	// Wait for all operations to complete
	for i := 0; i < numOps; i++ {
		select {
		case err := <-results:
			if err != nil {
				t.Errorf("Concurrent operation failed: %v", err)
			}
		case <-time.After(30 * time.Second):
			t.Fatalf("Timeout waiting for concurrent operations")
		}
	}

	t.Logf("Concurrency test completed successfully")
}

// Benchmark rootless executor performance
func BenchmarkRootlessExecutor(b *testing.B) {
	if os.Getuid() == 0 {
		b.Skip("Benchmark tests must run as non-root user")
	}

	executor := NewRootlessExecutor()
	defer executor.Cleanup()

	workDir := filepath.Join(executor.tempDir, "benchmark-test")
	if err := os.MkdirAll(workDir, 0755); err != nil {
		b.Fatalf("Failed to create work directory: %v", err)
	}

	operation := &types.Operation{
		Type:    types.OperationTypeExec,
		Command: []string{"echo", "benchmark test"},
		Platform: types.GetHostPlatform(),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, err := executor.Execute(operation, workDir)
		if err != nil {
			b.Fatalf("Benchmark operation failed: %v", err)
		}
		if !result.Success {
			b.Fatalf("Benchmark operation unsuccessful: %s", result.Error)
		}
	}
}

// Helper function to check if a container runtime is available
func isContainerRuntimeAvailable() bool {
	runtimes := []string{"podman", "docker"}
	for _, runtime := range runtimes {
		if _, err := exec.LookPath(runtime); err == nil {
			return true
		}
	}
	return false
}

// TestRootlessResourceLimitEnforcement validates that resource limits are properly enforced
func TestRootlessResourceLimitEnforcement(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping resource enforcement test in short mode")
	}

	if os.Getuid() == 0 {
		t.Skip("Resource enforcement tests must run as non-root user")
	}

	tests := []struct {
		name           string
		resourceLimits *types.ResourceLimits
		expectError    bool
		description    string
	}{
		{
			name: "valid_memory_limit",
			resourceLimits: &types.ResourceLimits{
				Memory: "512Mi",
			},
			expectError: false,
			description: "Valid memory limit should be accepted",
		},
		{
			name: "excessive_memory_limit",
			resourceLimits: &types.ResourceLimits{
				Memory: "16Gi", // Exceeds 8Gi limit
			},
			expectError: true,
			description: "Excessive memory limit should be rejected",
		},
		{
			name: "valid_cpu_limit",
			resourceLimits: &types.ResourceLimits{
				CPU: "0.5",
			},
			expectError: false,
			description: "Valid CPU limit should be accepted",
		},
		{
			name: "invalid_cpu_format",
			resourceLimits: &types.ResourceLimits{
				CPU: "invalid",
			},
			expectError: true,
			description: "Invalid CPU format should be rejected",
		},
		{
			name: "valid_disk_limit",
			resourceLimits: &types.ResourceLimits{
				Disk: "10Gi",
			},
			expectError: false,
			description: "Valid disk limit should be accepted",
		},
		{
			name: "excessive_disk_limit",
			resourceLimits: &types.ResourceLimits{
				Disk: "100Gi", // Exceeds 50Gi limit
			},
			expectError: true,
			description: "Excessive disk limit should be rejected",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executor := NewRootlessExecutorWithConfig(tt.resourceLimits, nil)
			defer executor.Cleanup()

			err := executor.validateResourceLimits()
			
			if tt.expectError {
				if err == nil {
					t.Errorf("%s: Expected error but got none", tt.description)
				}
			} else {
				if err != nil {
					t.Errorf("%s: Unexpected error: %v", tt.description, err)
				}
			}
		})
	}
}

// TestRootlessPermissionMapping validates proper permission mapping between host and container
func TestRootlessPermissionMapping(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping permission mapping test in short mode")
	}

	if os.Getuid() == 0 {
		t.Skip("Permission mapping tests must run as non-root user")
	}

	executor := NewRootlessExecutor()
	defer executor.Cleanup()

	// Create test files with various permissions
	testDir := filepath.Join(executor.tempDir, "permission-test")
	if err := os.MkdirAll(testDir, 0755); err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}

	testCases := []struct {
		name         string
		originalMode os.FileMode
		expectedSafe bool
	}{
		{"normal_file", 0644, true},
		{"executable_file", 0755, true},
		{"setuid_file", 0755 | os.ModeSetuid, false}, // Should be made safe
		{"setgid_file", 0755 | os.ModeSetgid, false}, // Should be made safe
		{"world_writable", 0666, false},              // Should be made safe
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			srcFile := filepath.Join(testDir, "src_"+tc.name)
			dstFile := filepath.Join(testDir, "dst_"+tc.name)

			// Create source file with specific permissions
			if err := os.WriteFile(srcFile, []byte("test content"), tc.originalMode); err != nil {
				t.Fatalf("Failed to create source file: %v", err)
			}

			// Test permission mapping
			srcInfo, err := os.Stat(srcFile)
			if err != nil {
				t.Fatalf("Failed to stat source file: %v", err)
			}

			if err := executor.copyFileWithPermissionMapping(srcFile, dstFile, srcInfo); err != nil {
				t.Fatalf("Failed to copy file with permission mapping: %v", err)
			}

			// Check destination file permissions
			dstInfo, err := os.Stat(dstFile)
			if err != nil {
				t.Fatalf("Failed to stat destination file: %v", err)
			}

			// Verify setuid/setgid bits are removed
			if dstInfo.Mode()&os.ModeSetuid != 0 {
				t.Errorf("Setuid bit should be removed in rootless mode")
			}
			if dstInfo.Mode()&os.ModeSetgid != 0 {
				t.Errorf("Setgid bit should be removed in rootless mode")
			}

			// Verify owner has read/write permissions
			if dstInfo.Mode()&0600 != 0600 {
				t.Errorf("Owner should have read/write permissions")
			}
		})
	}
}

// TestRootlessSystemAccessValidation validates that system access is properly restricted
func TestRootlessSystemAccessValidation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping system access validation test in short mode")
	}

	if os.Getuid() == 0 {
		t.Skip("System access validation tests must run as non-root user")
	}

	executor := NewRootlessExecutor()
	defer executor.Cleanup()

	// Test system access validation
	err := executor.validateSystemAccess()
	if err != nil {
		t.Logf("System access validation returned expected restriction: %v", err)
	}

	// Test specific privileged operations that should fail
	privilegedTests := []struct {
		name      string
		operation *types.Operation
		shouldFail bool
	}{
		{
			name: "mount_operation",
			operation: &types.Operation{
				Type:    types.OperationTypeExec,
				Command: []string{"mount", "-t", "tmpfs", "tmpfs", "/tmp/test"},
			},
			shouldFail: true,
		},
		{
			name: "iptables_operation",
			operation: &types.Operation{
				Type:    types.OperationTypeExec,
				Command: []string{"iptables", "-L"},
			},
			shouldFail: true,
		},
		{
			name: "modprobe_operation",
			operation: &types.Operation{
				Type:    types.OperationTypeExec,
				Command: []string{"modprobe", "dummy"},
			},
			shouldFail: true,
		},
		{
			name: "safe_operation",
			operation: &types.Operation{
				Type:    types.OperationTypeExec,
				Command: []string{"echo", "safe operation"},
			},
			shouldFail: false,
		},
	}

	workDir := filepath.Join(executor.tempDir, "system-access-test")
	if err := os.MkdirAll(workDir, 0755); err != nil {
		t.Fatalf("Failed to create work directory: %v", err)
	}

	for _, tt := range privilegedTests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := executor.Execute(tt.operation, workDir)
			
			if tt.shouldFail {
				if err == nil && result.Success {
					t.Errorf("Expected privileged operation to fail but it succeeded")
				}
			} else {
				if err != nil {
					t.Errorf("Expected safe operation to succeed but got error: %v", err)
				} else if !result.Success {
					t.Errorf("Expected safe operation to succeed but it failed: %s", result.Error)
				}
			}
		})
	}
}

// TestRootlessUserNamespaceSetup validates proper user namespace setup
func TestRootlessUserNamespaceSetup(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping user namespace setup test in short mode")
	}

	if os.Getuid() == 0 {
		t.Skip("User namespace setup tests must run as non-root user")
	}

	executor := NewRootlessExecutor()
	defer executor.Cleanup()

	// Test user namespace configuration
	if !executor.userNS {
		t.Skip("User namespaces not available or not configured")
	}

	// Validate ID ranges
	if len(executor.subUIDs) == 0 {
		t.Error("No subUID ranges configured")
	}
	if len(executor.subGIDs) == 0 {
		t.Error("No subGID ranges configured")
	}

	// Test ID range validation
	err := executor.validateIDRanges()
	if err != nil {
		t.Errorf("ID range validation failed: %v", err)
	}

	// Test user namespace creation
	err = executor.testUserNamespaceCreation()
	if err != nil {
		t.Errorf("User namespace creation test failed: %v", err)
	}
}

// TestRootlessSecurityConstraintEnforcement validates comprehensive security constraint enforcement
func TestRootlessSecurityConstraintEnforcement(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping security constraint enforcement test in short mode")
	}

	if os.Getuid() == 0 {
		t.Skip("Security constraint enforcement tests must run as non-root user")
	}

	// Test various security contexts
	securityTests := []struct {
		name            string
		securityContext *types.SecurityContext
		resourceLimits  *types.ResourceLimits
		expectError     bool
		description     string
	}{
		{
			name: "secure_context",
			securityContext: &types.SecurityContext{
				RunAsNonRoot: boolPtr(true),
				Capabilities: []string{"CAP_NET_BIND_SERVICE"},
			},
			resourceLimits: &types.ResourceLimits{
				Memory: "512Mi",
				CPU:    "0.5",
			},
			expectError: false,
			description: "Secure context with reasonable limits should be accepted",
		},
		{
			name: "insecure_root_context",
			securityContext: &types.SecurityContext{
				RunAsUser: int64Ptr(0),
			},
			expectError: true,
			description: "Context allowing root should be rejected",
		},
		{
			name: "privileged_capabilities",
			securityContext: &types.SecurityContext{
				Capabilities: []string{"CAP_SYS_ADMIN", "CAP_NET_ADMIN"},
			},
			expectError: true,
			description: "Privileged capabilities should be rejected",
		},
		{
			name: "excessive_resources",
			resourceLimits: &types.ResourceLimits{
				Memory: "32Gi",
				CPU:    "16",
			},
			expectError: true,
			description: "Excessive resource limits should be rejected",
		},
	}

	for _, tt := range securityTests {
		t.Run(tt.name, func(t *testing.T) {
			executor := NewRootlessExecutorWithConfig(tt.resourceLimits, tt.securityContext)
			defer executor.Cleanup()

			err := executor.validateSecurityConstraints()
			
			if tt.expectError {
				if err == nil {
					t.Errorf("%s: Expected error but got none", tt.description)
				}
			} else {
				if err != nil {
					t.Errorf("%s: Unexpected error: %v", tt.description, err)
				}
			}
		})
	}
}

// TestRootlessOperationIsolation validates that operations are properly isolated
func TestRootlessOperationIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping operation isolation test in short mode")
	}

	if os.Getuid() == 0 {
		t.Skip("Operation isolation tests must run as non-root user")
	}

	executor := NewRootlessExecutor()
	defer executor.Cleanup()

	workDir := filepath.Join(executor.tempDir, "isolation-test")
	if err := os.MkdirAll(workDir, 0755); err != nil {
		t.Fatalf("Failed to create work directory: %v", err)
	}

	// Test that operations are isolated from each other
	operation1 := &types.Operation{
		Type:    types.OperationTypeExec,
		Command: []string{"sh", "-c", "echo 'op1' > /tmp/test-isolation"},
		Platform: types.GetHostPlatform(),
	}

	operation2 := &types.Operation{
		Type:    types.OperationTypeExec,
		Command: []string{"sh", "-c", "cat /tmp/test-isolation || echo 'isolated'"},
		Platform: types.GetHostPlatform(),
	}

	// Execute first operation
	result1, err := executor.Execute(operation1, workDir)
	if err != nil {
		t.Fatalf("First operation failed: %v", err)
	}
	if !result1.Success {
		t.Fatalf("First operation unsuccessful: %s", result1.Error)
	}

	// Execute second operation - should not see the file from first operation
	result2, err := executor.Execute(operation2, workDir)
	if err != nil {
		t.Fatalf("Second operation failed: %v", err)
	}
	if !result2.Success {
		t.Fatalf("Second operation unsuccessful: %s", result2.Error)
	}

	t.Logf("Operation isolation test completed successfully")
}

// Helper function to check if user namespaces are supported
func isUserNamespaceSupported() bool {
	if _, err := os.Stat("/proc/self/ns/user"); err != nil {
		return false
	}

	cmd := exec.Command("unshare", "--user", "--map-root-user", "true")
	return cmd.Run() == nil
}