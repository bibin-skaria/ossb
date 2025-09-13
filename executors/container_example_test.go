package executors

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/bibin-skaria/ossb/internal/types"
)

// ExampleContainerExecutor demonstrates setting up a rootless container executor
func ExampleContainerExecutor() {
	// Create a new container executor with Podman for rootless operation
	executor := NewContainerExecutor("podman")
	defer executor.Cleanup()

	// Configure security context for rootless operation
	uid := int64(1000)
	nonRoot := true
	executor.SetSecurityContext(&types.SecurityContext{
		RunAsUser:    &uid,
		RunAsNonRoot: &nonRoot,
		Capabilities: []string{}, // No additional capabilities
	})

	// Set resource limits
	executor.SetResourceLimits(&types.ResourceLimits{
		Memory: "512Mi",
		CPU:    "1000m",
	})

	// Configure network isolation
	executor.SetNetworkMode("none")

	fmt.Printf("Executor configured for rootless operation with runtime: %s\n", executor.runtime)
	fmt.Printf("Network mode: %s\n", executor.networkMode)
	fmt.Printf("Rootless: %v\n", executor.rootless)

	// Output:
	// Executor configured for rootless operation with runtime: podman
	// Network mode: none
	// Rootless: true
}

// ExampleContainerExecutor_multiArch demonstrates multi-architecture support
func ExampleContainerExecutor_multiArch() {
	executor := NewContainerExecutor("podman")
	defer executor.Cleanup()

	// Test platform validation for different architectures
	platforms := []types.Platform{
		{OS: "linux", Architecture: "amd64"},
		{OS: "linux", Architecture: "arm64"},
		{OS: "linux", Architecture: "arm", Variant: "v7"},
		{OS: "linux", Architecture: "s390x"},
	}

	fmt.Println("Supported platforms:")
	for _, platform := range platforms {
		err := executor.validatePlatformSupport(platform)
		if err == nil {
			fmt.Printf("✓ %s\n", platform.String())
		} else {
			fmt.Printf("✗ %s: %v\n", platform.String(), err)
		}
	}

	// Output:
	// Supported platforms:
	// ✓ linux/amd64
	// ✓ linux/arm64
	// ✓ linux/arm/v7
	// ✓ linux/s390x
}

// ExampleContainerExecutor_volumes demonstrates volume mounting
func ExampleContainerExecutor_volumes() {
	executor := NewContainerExecutor("podman")
	defer executor.Cleanup()

	// Create a temporary volume
	volumePath, err := executor.createTempVolume("example-data")
	if err != nil {
		fmt.Printf("Failed to create volume: %v\n", err)
		return
	}

	// Mount the volume
	err = executor.mountVolume(volumePath, "/data")
	if err != nil {
		fmt.Printf("Failed to mount volume: %v\n", err)
		return
	}

	fmt.Printf("Volume created at: %s\n", filepath.Base(volumePath))
	fmt.Printf("Mounted to: /data\n")
	fmt.Printf("Total volumes: %d\n", len(executor.volumes))

	// Output:
	// Volume created at: example-data
	// Mounted to: /data
	// Total volumes: 1
}

// ExampleContainerExecutor_security demonstrates security context configuration
func ExampleContainerExecutor_security() {
	executor := NewContainerExecutor("podman")
	defer executor.Cleanup()

	// Configure comprehensive security context
	uid := int64(1001)
	gid := int64(1001)
	nonRoot := true
	
	executor.SetSecurityContext(&types.SecurityContext{
		RunAsUser:    &uid,
		RunAsGroup:   &gid,
		RunAsNonRoot: &nonRoot,
		Capabilities: []string{"CAP_NET_BIND_SERVICE", "CAP_SYS_TIME"},
	})

	// Set strict resource limits
	executor.SetResourceLimits(&types.ResourceLimits{
		Memory: "256Mi",
		CPU:    "500m",
		Disk:   "1Gi",
	})

	// Configure isolated network
	executor.SetNetworkMode("none")

	fmt.Println("Security configuration:")
	if executor.securityContext != nil {
		fmt.Printf("  Run as user: %d\n", *executor.securityContext.RunAsUser)
		fmt.Printf("  Run as group: %d\n", *executor.securityContext.RunAsGroup)
		fmt.Printf("  Non-root: %v\n", *executor.securityContext.RunAsNonRoot)
		fmt.Printf("  Capabilities: %v\n", executor.securityContext.Capabilities)
	}

	if executor.resourceLimits != nil {
		fmt.Printf("Resource limits:\n")
		fmt.Printf("  Memory: %s\n", executor.resourceLimits.Memory)
		fmt.Printf("  CPU: %s\n", executor.resourceLimits.CPU)
		fmt.Printf("  Disk: %s\n", executor.resourceLimits.Disk)
	}

	fmt.Printf("Network mode: %s\n", executor.networkMode)

	// Output:
	// Security configuration:
	//   Run as user: 1001
	//   Run as group: 1001
	//   Non-root: true
	//   Capabilities: [CAP_NET_BIND_SERVICE CAP_SYS_TIME]
	// Resource limits:
	//   Memory: 256Mi
	//   CPU: 500m
	//   Disk: 1Gi
	// Network mode: none
}

// ExampleContainerExecutor_qemu demonstrates QEMU emulation setup
func ExampleContainerExecutor_qemu() {
	executor := NewContainerExecutor("podman")
	defer executor.Cleanup()

	// Show supported QEMU platforms (just a few examples)
	fmt.Println("QEMU emulation support:")
	examples := []string{"linux/amd64", "linux/arm64", "linux/arm/v7"}
	for _, platform := range examples {
		if qemuBinary, exists := executor.supportedQEMU[platform]; exists {
			if qemuBinary == "" {
				fmt.Printf("  %s: native (no emulation needed)\n", platform)
			} else {
				fmt.Printf("  %s: %s\n", platform, qemuBinary)
			}
		}
	}

	// Test emulation setup for ARM64
	armPlatform := types.Platform{OS: "linux", Architecture: "arm64"}
	fmt.Printf("\nTesting emulation setup for %s...\n", armPlatform.String())
	
	// Note: This would actually try to setup QEMU in a real environment
	// For the example, we just show the process
	fmt.Printf("Would setup QEMU emulation using: %s\n", executor.supportedQEMU[armPlatform.String()])

	// Output:
	// QEMU emulation support:
	//   linux/amd64: native (no emulation needed)
	//   linux/arm64: qemu-aarch64-static
	//   linux/arm/v7: qemu-arm-static
	//
	// Testing emulation setup for linux/arm64...
	// Would setup QEMU emulation using: qemu-aarch64-static
}

// ExampleContainerExecutor_operations demonstrates executing operations
func ExampleContainerExecutor_operations() {
	executor := NewContainerExecutor("podman")
	defer executor.Cleanup()

	// Create a temporary work directory
	tempDir, err := os.MkdirTemp("", "example-work-*")
	if err != nil {
		fmt.Printf("Failed to create temp dir: %v\n", err)
		return
	}
	defer os.RemoveAll(tempDir)

	// Example source operation (scratch image)
	sourceOp := &types.Operation{
		Type: types.OperationTypeSource,
		Metadata: map[string]string{
			"image": "scratch",
		},
		Platform: types.Platform{OS: "linux", Architecture: "amd64"},
		Outputs:  []string{"base"},
	}

	fmt.Println("Executing source operation...")
	result, err := executor.executeSource(sourceOp, tempDir, &types.OperationResult{
		Operation: sourceOp,
		Success:   false,
	})

	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	if result.Success {
		fmt.Println("✓ Source operation completed successfully")
	} else {
		fmt.Printf("✗ Source operation failed: %s\n", result.Error)
	}

	// Example meta operation
	metaOp := &types.Operation{
		Type: types.OperationTypeMeta,
		Environment: map[string]string{
			"EXAMPLE_VAR": "example_value",
		},
		Outputs: []string{"meta"},
	}

	fmt.Println("\nExecuting meta operation...")
	result, err = executor.executeMeta(metaOp, tempDir, &types.OperationResult{
		Operation: metaOp,
		Success:   false,
	})

	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	if result.Success {
		fmt.Println("✓ Meta operation completed successfully")
	} else {
		fmt.Printf("✗ Meta operation failed: %s\n", result.Error)
	}

	// Output:
	// Executing source operation...
	// ✓ Source operation completed successfully
	//
	// Executing meta operation...
	// ✓ Meta operation completed successfully
}