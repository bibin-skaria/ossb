package executors

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bibin-skaria/ossb/internal/types"
)

// TestRootlessExecutorComprehensive demonstrates all key features of the completed RootlessExecutor
func TestRootlessExecutorComprehensive(t *testing.T) {
	// Test comprehensive rootless executor functionality
	t.Run("comprehensive_security_validation", func(t *testing.T) {
		testComprehensiveSecurityValidation(t)
	})

	t.Run("comprehensive_resource_management", func(t *testing.T) {
		testComprehensiveResourceManagement(t)
	})

	t.Run("comprehensive_permission_handling", func(t *testing.T) {
		testComprehensivePermissionHandling(t)
	})

	t.Run("comprehensive_operation_execution", func(t *testing.T) {
		testComprehensiveOperationExecution(t)
	})
}

func testComprehensiveSecurityValidation(t *testing.T) {
	// Test 1: Secure configuration should work
	secureContext := &types.SecurityContext{
		RunAsNonRoot: boolPtr(true),
		Capabilities: []string{"CAP_NET_BIND_SERVICE"},
	}
	
	secureResources := &types.ResourceLimits{
		Memory: "512Mi",
		CPU:    "0.5",
		Disk:   "5Gi",
	}

	executor := NewRootlessExecutorWithConfig(secureResources, secureContext)
	defer executor.Cleanup()

	if executor == nil {
		t.Fatal("Failed to create secure rootless executor")
	}

	// Test 2: Insecure configurations should be rejected
	insecureConfigs := []struct {
		name            string
		securityContext *types.SecurityContext
		resourceLimits  *types.ResourceLimits
		shouldFail      bool
	}{
		{
			name: "root_user_context",
			securityContext: &types.SecurityContext{
				RunAsUser: int64Ptr(0),
			},
			shouldFail: true,
		},
		{
			name: "privileged_capabilities",
			securityContext: &types.SecurityContext{
				Capabilities: []string{"CAP_SYS_ADMIN"},
			},
			shouldFail: true,
		},
		{
			name: "excessive_memory",
			resourceLimits: &types.ResourceLimits{
				Memory: "16Gi",
			},
			shouldFail: true,
		},
		{
			name: "excessive_disk",
			resourceLimits: &types.ResourceLimits{
				Disk: "100Gi",
			},
			shouldFail: true,
		},
	}

	for _, config := range insecureConfigs {
		t.Run(config.name, func(t *testing.T) {
			testExecutor := NewRootlessExecutorWithConfig(config.resourceLimits, config.securityContext)
			defer testExecutor.Cleanup()

			err := testExecutor.validateSecurityConstraints()
			if config.shouldFail && err == nil {
				t.Errorf("Expected insecure configuration to be rejected: %s", config.name)
			}
		})
	}

	t.Logf("Comprehensive security validation completed")
}

func testComprehensiveResourceManagement(t *testing.T) {
	resourceLimits := &types.ResourceLimits{
		Memory: "256Mi",
		CPU:    "0.25",
		Disk:   "2Gi",
	}

	executor := NewRootlessExecutorWithConfig(resourceLimits, nil)
	defer executor.Cleanup()

	// Test resource limit validation
	if err := executor.validateResourceLimits(); err != nil {
		t.Errorf("Valid resource limits should be accepted: %v", err)
	}

	// Test resource limit enforcement
	if err := executor.enforceResourceLimits(); err != nil {
		t.Errorf("Resource limit enforcement should succeed: %v", err)
	}

	// Test memory parsing
	bytes, err := executor.parseMemoryToBytes("256Mi")
	if err != nil {
		t.Errorf("Memory parsing should succeed: %v", err)
	}
	expectedBytes := int64(256 * 1024 * 1024)
	if bytes != expectedBytes {
		t.Errorf("Expected %d bytes, got %d", expectedBytes, bytes)
	}

	t.Logf("Comprehensive resource management completed")
}

func testComprehensivePermissionHandling(t *testing.T) {
	executor := NewRootlessExecutor()
	defer executor.Cleanup()

	testDir := filepath.Join(executor.tempDir, "permission-test")
	if err := os.MkdirAll(testDir, 0755); err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}

	// Test permission mapping for various file types
	testFiles := []struct {
		name         string
		mode         os.FileMode
		content      string
		expectSafe   bool
	}{
		{"normal.txt", 0644, "normal file", true},
		{"executable.sh", 0755, "#!/bin/sh\necho test", true},
		{"setuid.bin", 0755 | os.ModeSetuid, "setuid binary", false},
		{"setgid.bin", 0755 | os.ModeSetgid, "setgid binary", false},
		{"world_writable.txt", 0666, "world writable", false},
	}

	for _, tf := range testFiles {
		t.Run(tf.name, func(t *testing.T) {
			srcPath := filepath.Join(testDir, "src_"+tf.name)
			dstPath := filepath.Join(testDir, "dst_"+tf.name)

			// Create source file
			if err := os.WriteFile(srcPath, []byte(tf.content), tf.mode); err != nil {
				t.Fatalf("Failed to create source file: %v", err)
			}

			// Test permission mapping
			srcInfo, err := os.Stat(srcPath)
			if err != nil {
				t.Fatalf("Failed to stat source file: %v", err)
			}

			if err := executor.copyFileWithPermissionMapping(srcPath, dstPath, srcInfo); err != nil {
				t.Fatalf("Failed to copy with permission mapping: %v", err)
			}

			// Verify destination file
			dstInfo, err := os.Stat(dstPath)
			if err != nil {
				t.Fatalf("Failed to stat destination file: %v", err)
			}

			// Check that dangerous bits are removed
			if dstInfo.Mode()&os.ModeSetuid != 0 {
				t.Errorf("Setuid bit should be removed")
			}
			if dstInfo.Mode()&os.ModeSetgid != 0 {
				t.Errorf("Setgid bit should be removed")
			}

			// Check that owner has read/write
			if dstInfo.Mode()&0600 != 0600 {
				t.Errorf("Owner should have read/write permissions")
			}
		})
	}

	t.Logf("Comprehensive permission handling completed")
}

func testComprehensiveOperationExecution(t *testing.T) {
	executor := NewRootlessExecutor()
	defer executor.Cleanup()

	workDir := filepath.Join(executor.tempDir, "operation-test")
	if err := os.MkdirAll(workDir, 0755); err != nil {
		t.Fatalf("Failed to create work directory: %v", err)
	}

	// Test various operation types that don't require container runtime
	operations := []struct {
		name      string
		operation *types.Operation
		shouldSucceed bool
	}{
		{
			name: "scratch_source",
			operation: &types.Operation{
				Type: types.OperationTypeSource,
				Metadata: map[string]string{
					"image": "scratch",
				},
				Platform: types.GetHostPlatform(),
				Outputs:  []string{"base"},
			},
			shouldSucceed: true,
		},
		{
			name: "meta_operation",
			operation: &types.Operation{
				Type: types.OperationTypeMeta,
				Environment: map[string]string{
					"TEST_VAR": "test_value",
				},
				Platform: types.GetHostPlatform(),
				Outputs:  []string{"meta"},
			},
			shouldSucceed: true,
		},
		{
			name: "privileged_operation",
			operation: &types.Operation{
				Type:    types.OperationTypeExec,
				Command: []string{"sudo", "echo", "test"},
				Platform: types.GetHostPlatform(),
			},
			shouldSucceed: false, // Should be rejected
		},
		{
			name: "root_user_operation",
			operation: &types.Operation{
				Type:    types.OperationTypeExec,
				Command: []string{"echo", "test"},
				User:    "root",
				Platform: types.GetHostPlatform(),
			},
			shouldSucceed: false, // Should be rejected
		},
	}

	for _, op := range operations {
		t.Run(op.name, func(t *testing.T) {
			result, err := executor.Execute(op.operation, workDir)
			
			if op.shouldSucceed {
				if err != nil {
					t.Errorf("Expected operation to succeed but got error: %v", err)
				} else if !result.Success {
					t.Errorf("Expected operation to succeed but got failure: %s", result.Error)
				}
			} else {
				if err == nil && result.Success {
					t.Errorf("Expected operation to fail but it succeeded")
				}
			}
		})
	}

	t.Logf("Comprehensive operation execution completed")
}

// TestRootlessExecutorDocumentation provides examples of how to use the RootlessExecutor
func TestRootlessExecutorDocumentation(t *testing.T) {
	t.Log("=== RootlessExecutor Usage Examples ===")

	// Example 1: Basic usage
	t.Log("Example 1: Basic RootlessExecutor creation")
	executor := NewRootlessExecutor()
	defer executor.Cleanup()
	t.Logf("Created basic rootless executor with runtime: %s", executor.runtime)

	// Example 2: With resource limits and security context
	t.Log("Example 2: RootlessExecutor with resource limits and security context")
	resourceLimits := &types.ResourceLimits{
		Memory: "512Mi",
		CPU:    "0.5",
		Disk:   "5Gi",
	}
	securityContext := &types.SecurityContext{
		RunAsNonRoot: boolPtr(true),
		Capabilities: []string{"CAP_NET_BIND_SERVICE"},
	}
	secureExecutor := NewRootlessExecutorWithConfig(resourceLimits, securityContext)
	defer secureExecutor.Cleanup()
	t.Logf("Created secure rootless executor with memory limit: %s", resourceLimits.Memory)

	// Example 3: Executing a scratch image operation
	t.Log("Example 3: Executing scratch image operation")
	workDir := filepath.Join(executor.tempDir, "example-work")
	os.MkdirAll(workDir, 0755)

	scratchOp := &types.Operation{
		Type: types.OperationTypeSource,
		Metadata: map[string]string{
			"image": "scratch",
		},
		Platform: types.GetHostPlatform(),
		Outputs:  []string{"base"},
	}

	result, err := executor.Execute(scratchOp, workDir)
	if err != nil {
		t.Errorf("Scratch operation failed: %v", err)
	} else if result.Success {
		t.Log("Scratch image operation completed successfully")
	}

	// Example 4: Security validation
	t.Log("Example 4: Security constraint validation")
	if err := secureExecutor.validateSecurityConstraints(); err != nil {
		t.Errorf("Security validation failed: %v", err)
	} else {
		t.Log("Security constraints validated successfully")
	}

	t.Log("=== RootlessExecutor Documentation Examples Completed ===")
}