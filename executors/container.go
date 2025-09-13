package executors

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/bibin-skaria/ossb/internal/types"
)

type ContainerExecutor struct {
	runtime         string
	supportedQEMU   map[string]string
	registryAuth    string
	rootless        bool
	networkMode     string
	securityContext *types.SecurityContext
	resourceLimits  *types.ResourceLimits
	tempDir         string
	volumes         map[string]string
}

func NewContainerExecutor(runtime string) *ContainerExecutor {
	if runtime == "" {
		// Prefer podman for rootless operation
		if _, err := exec.LookPath("podman"); err == nil {
			runtime = "podman"
		} else if _, err := exec.LookPath("docker"); err == nil {
			runtime = "docker"
		} else {
			runtime = "podman" // Default fallback
		}
	}

	supportedQEMU := map[string]string{
		"linux/amd64":   "",
		"linux/arm64":   "qemu-aarch64-static",
		"linux/arm/v7":  "qemu-arm-static", 
		"linux/arm/v6":  "qemu-arm-static",
		"linux/386":     "qemu-i386-static",
		"linux/ppc64le": "qemu-ppc64le-static",
		"linux/s390x":   "qemu-s390x-static",
		"linux/riscv64": "qemu-riscv64-static",
		"linux/mips64":  "qemu-mips64-static",
		"linux/mips64le": "qemu-mips64el-static",
	}

	// Create temporary directory for this executor instance
	tempDir, _ := os.MkdirTemp("", "ossb-container-*")

	return &ContainerExecutor{
		runtime:       runtime,
		supportedQEMU: supportedQEMU,
		rootless:      runtime == "podman" || os.Getenv("ROOTLESS") == "true",
		networkMode:   "none", // Default to isolated network
		tempDir:       tempDir,
		volumes:       make(map[string]string),
	}
}

func init() {
	RegisterExecutor("container", NewContainerExecutor(""))
}

func (e *ContainerExecutor) Execute(operation *types.Operation, workDir string) (*types.OperationResult, error) {
	result := &types.OperationResult{
		Operation: operation,
		Success:   false,
	}

	// Initialize Podman API if needed
	if err := e.setupPodmanAPI(); err != nil {
		result.Error = fmt.Sprintf("failed to setup podman API: %v", err)
		return result, nil
	}

	// Test connection
	if err := e.testPodmanConnection(); err != nil {
		result.Error = fmt.Sprintf("failed to connect to container runtime: %v", err)
		return result, nil
	}

	// Setup network isolation
	if err := e.setupNetworkIsolation(); err != nil {
		result.Error = fmt.Sprintf("failed to setup network isolation: %v", err)
		return result, nil
	}

	// Validate platform support
	if err := e.validatePlatformSupport(operation.Platform); err != nil {
		result.Error = fmt.Sprintf("platform validation failed: %v", err)
		return result, nil
	}

	// Setup QEMU emulation if needed
	if err := e.setupQEMUEmulation(operation.Platform); err != nil {
		result.Error = fmt.Sprintf("failed to setup QEMU emulation: %v", err)
		return result, nil
	}

	switch operation.Type {
	case types.OperationTypeSource:
		return e.executeSource(operation, workDir, result)
	case types.OperationTypeExec:
		return e.executeExec(operation, workDir, result)
	case types.OperationTypeFile:
		return e.executeFile(operation, workDir, result)
	case types.OperationTypeMeta:
		return e.executeMeta(operation, workDir, result)
	default:
		result.Error = fmt.Sprintf("unsupported operation type: %s", operation.Type)
		return result, nil
	}
}

func (e *ContainerExecutor) executeSource(operation *types.Operation, workDir string, result *types.OperationResult) (*types.OperationResult, error) {
	image := operation.Metadata["image"]
	if image == "" {
		result.Error = "source operation missing image metadata"
		return result, nil
	}

	platform := operation.Platform
	if platform.OS == "" {
		platform = types.GetHostPlatform()
	}

	if image == "scratch" {
		baseDir := filepath.Join(workDir, "base", platform.String())
		if err := os.MkdirAll(baseDir, 0755); err != nil {
			result.Error = fmt.Sprintf("failed to create base directory: %v", err)
			return result, nil
		}
		result.Success = true
		result.Outputs = operation.Outputs
		return result, nil
	}

	platformFlag := fmt.Sprintf("--platform=%s", platform.String())
	
	cmd := exec.Command(e.runtime, "pull", platformFlag, image)
	output, err := cmd.CombinedOutput()
	if err != nil {
		result.Error = fmt.Sprintf("failed to pull image %s for %s: %v, output: %s", 
			image, platform.String(), err, string(output))
		return result, nil
	}

	if err := e.setupQEMU(platform); err != nil {
		result.Error = fmt.Sprintf("failed to setup QEMU for %s: %v", platform.String(), err)
		return result, nil
	}

	baseDir := filepath.Join(workDir, "base", platform.String())
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		result.Error = fmt.Sprintf("failed to create base directory: %v", err)
		return result, nil
	}

	containerName := fmt.Sprintf("ossb-extract-%d", time.Now().UnixNano())
	createCmd := exec.Command(e.runtime, "create", platformFlag, "--name", containerName, image)
	if output, err := createCmd.CombinedOutput(); err != nil {
		result.Error = fmt.Sprintf("failed to create container: %v, output: %s", err, string(output))
		return result, nil
	}

	defer func() {
		exec.Command(e.runtime, "rm", containerName).Run()
	}()

	exportCmd := exec.Command(e.runtime, "export", containerName)
	tarCmd := exec.Command("tar", "-xf", "-", "-C", baseDir)
	
	// Create pipe between export and tar commands
	pipeReader, pipeWriter := io.Pipe()
	exportCmd.Stdout = pipeWriter
	tarCmd.Stdin = pipeReader
	tarCmd.Stderr = os.Stderr

	if err := exportCmd.Start(); err != nil {
		result.Error = fmt.Sprintf("failed to start export: %v", err)
		return result, nil
	}

	if err := tarCmd.Start(); err != nil {
		result.Error = fmt.Sprintf("failed to start tar extraction: %v", err)
		return result, nil
	}

	if err := exportCmd.Wait(); err != nil {
		result.Error = fmt.Sprintf("failed to export container: %v", err)
		return result, nil
	}

	if err := tarCmd.Wait(); err != nil {
		result.Error = fmt.Sprintf("failed to extract tar: %v", err)
		return result, nil
	}

	result.Success = true
	result.Outputs = operation.Outputs
	result.Environment = map[string]string{
		"PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
	}

	return result, nil
}

func (e *ContainerExecutor) executeExec(operation *types.Operation, workDir string, result *types.OperationResult) (*types.OperationResult, error) {
	if len(operation.Command) == 0 {
		result.Error = "exec operation missing command"
		return result, nil
	}

	platform := operation.Platform
	if platform.OS == "" {
		platform = types.GetHostPlatform()
	}

	layerDir := filepath.Join(workDir, "layers", platform.String(), fmt.Sprintf("layer-%d", len(operation.Outputs)))
	if err := os.MkdirAll(layerDir, 0755); err != nil {
		result.Error = fmt.Sprintf("failed to create layer directory: %v", err)
		return result, nil
	}

	baseDir := filepath.Join(workDir, "base", platform.String())
	if _, err := os.Stat(baseDir); os.IsNotExist(err) {
		if err := os.MkdirAll(baseDir, 0755); err != nil {
			result.Error = fmt.Sprintf("failed to create base directory: %v", err)
			return result, nil
		}
	}

	// Create temporary workspace volume
	workspaceVol, err := e.createTempVolume("workspace")
	if err != nil {
		result.Error = fmt.Sprintf("failed to create workspace volume: %v", err)
		return result, nil
	}

	// Copy base filesystem to workspace
	if err := e.copyDir(baseDir, workspaceVol); err != nil {
		result.Error = fmt.Sprintf("failed to copy base to workspace: %v", err)
		return result, nil
	}

	// Mount the workspace volume
	if err := e.mountVolume(workspaceVol, "/workspace"); err != nil {
		result.Error = fmt.Sprintf("failed to mount workspace: %v", err)
		return result, nil
	}

	// Determine the base image for execution
	baseImage := "busybox:latest"
	if operation.Metadata != nil {
		if img, exists := operation.Metadata["base_image"]; exists && img != "" {
			baseImage = img
		}
	}

	// Prepare command for execution
	var execCmd []string
	if len(operation.Command) == 1 {
		execCmd = []string{"sh", "-c", operation.Command[0]}
	} else {
		execCmd = operation.Command
	}

	// Create secure container
	containerName, err := e.createSecureContainer(
		baseImage,
		platform,
		execCmd,
		operation.Environment,
		operation.WorkDir,
	)
	if err != nil {
		result.Error = fmt.Sprintf("failed to create container: %v", err)
		return result, nil
	}

	// Ensure cleanup
	defer func() {
		e.removeContainer(containerName)
		// Clear volumes for next operation
		e.volumes = make(map[string]string)
	}()

	// Execute the container
	output, err := e.runContainer(containerName)
	if err != nil {
		result.Error = fmt.Sprintf("command failed: %v, output: %s", err, string(output))
		return result, nil
	}

	// Capture filesystem changes
	if err := e.captureLayerChanges(workspaceVol, layerDir); err != nil {
		result.Error = fmt.Sprintf("failed to capture layer changes: %v", err)
		return result, nil
	}

	// Copy changes back to base directory
	if err := e.copyDir(workspaceVol, baseDir); err != nil {
		result.Error = fmt.Sprintf("failed to copy changes back: %v", err)
		return result, nil
	}

	result.Success = true
	result.Outputs = operation.Outputs
	result.Environment = operation.Environment

	return result, nil
}

func (e *ContainerExecutor) executeFile(operation *types.Operation, workDir string, result *types.OperationResult) (*types.OperationResult, error) {
	if len(operation.Command) == 0 {
		result.Error = "file operation missing command"
		return result, nil
	}

	platform := operation.Platform
	if platform.OS == "" {
		platform = types.GetHostPlatform()
	}

	operationType := operation.Command[0]
	dest := operation.Metadata["dest"]
	if dest == "" {
		result.Error = "file operation missing destination"
		return result, nil
	}

	layerDir := filepath.Join(workDir, "layers", platform.String(), fmt.Sprintf("layer-%d", len(operation.Outputs)))
	if err := os.MkdirAll(layerDir, 0755); err != nil {
		result.Error = fmt.Sprintf("failed to create layer directory: %v", err)
		return result, nil
	}

	baseDir := filepath.Join(workDir, "base", platform.String())
	destPath := filepath.Join(baseDir, strings.TrimPrefix(dest, "/"))
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		result.Error = fmt.Sprintf("failed to create destination directory: %v", err)
		return result, nil
	}

	sources := operation.Inputs[1:] 

	switch operationType {
	case "copy":
		if err := e.copyFiles(sources, destPath); err != nil {
			result.Error = fmt.Sprintf("copy failed: %v", err)
			return result, nil
		}
	case "add":
		if err := e.addFiles(sources, destPath); err != nil {
			result.Error = fmt.Sprintf("add failed: %v", err)
			return result, nil
		}
	default:
		result.Error = fmt.Sprintf("unsupported file operation: %s", operationType)
		return result, nil
	}

	if err := e.captureLayerChanges(baseDir, layerDir); err != nil {
		result.Error = fmt.Sprintf("failed to capture layer changes: %v", err)
		return result, nil
	}

	result.Success = true
	result.Outputs = operation.Outputs
	result.Environment = operation.Environment

	return result, nil
}

func (e *ContainerExecutor) executeMeta(operation *types.Operation, workDir string, result *types.OperationResult) (*types.OperationResult, error) {
	result.Success = true
	result.Outputs = operation.Outputs
	result.Environment = operation.Environment

	return result, nil
}

func (e *ContainerExecutor) setupQEMU(platform types.Platform) error {
	// This method is now replaced by setupQEMUEmulation
	return e.setupQEMUEmulation(platform)
}

func (e *ContainerExecutor) captureLayerChanges(baseDir, layerDir string) error {
	cmd := exec.Command("cp", "-a", baseDir+"/.", layerDir+"/")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to capture changes: %v, output: %s", err, string(output))
	}
	return nil
}

func (e *ContainerExecutor) copyFiles(sources []string, dest string) error {
	for _, source := range sources {
		if err := e.copyPath(source, dest); err != nil {
			return err
		}
	}
	return nil
}

func (e *ContainerExecutor) addFiles(sources []string, dest string) error {
	return e.copyFiles(sources, dest)
}

func (e *ContainerExecutor) copyPath(source, dest string) error {
	srcInfo, err := os.Stat(source)
	if err != nil {
		return fmt.Errorf("source does not exist: %s", source)
	}

	if srcInfo.IsDir() {
		return e.copyDir(source, dest)
	} else {
		return e.copyFile(source, dest)
	}
}

func (e *ContainerExecutor) copyFile(source, dest string) error {
	cmd := exec.Command("cp", "-a", source, dest)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to copy file: %v, output: %s", err, string(output))
	}
	return nil
}

func (e *ContainerExecutor) copyDir(source, dest string) error {
	cmd := exec.Command("cp", "-a", source, dest)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to copy directory: %v, output: %s", err, string(output))
	}
	return nil
}

// SetSecurityContext configures security context for rootless operation
func (e *ContainerExecutor) SetSecurityContext(ctx *types.SecurityContext) {
	e.securityContext = ctx
}

// SetResourceLimits configures resource limits for containers
func (e *ContainerExecutor) SetResourceLimits(limits *types.ResourceLimits) {
	e.resourceLimits = limits
}

// SetNetworkMode configures network isolation mode
func (e *ContainerExecutor) SetNetworkMode(mode string) {
	e.networkMode = mode
}

// Cleanup removes temporary directories and volumes
func (e *ContainerExecutor) Cleanup() error {
	if e.tempDir != "" {
		return os.RemoveAll(e.tempDir)
	}
	return nil
}

// setupPodmanAPI configures Podman for rootless operation
func (e *ContainerExecutor) setupPodmanAPI() error {
	if e.runtime != "podman" {
		return nil
	}

	// Check if Podman is running in rootless mode
	cmd := exec.Command("podman", "info", "--format", "json")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to get podman info: %v", err)
	}

	var info map[string]interface{}
	if err := json.Unmarshal(output, &info); err != nil {
		return fmt.Errorf("failed to parse podman info: %v", err)
	}

	// Verify rootless mode if required
	if e.rootless {
		if host, ok := info["host"].(map[string]interface{}); ok {
			if security, ok := host["security"].(map[string]interface{}); ok {
				if rootless, ok := security["rootless"].(bool); ok && !rootless {
					return fmt.Errorf("podman is not running in rootless mode")
				}
			}
		}
	}

	return nil
}

// setupQEMUEmulation sets up QEMU emulation for cross-platform builds
func (e *ContainerExecutor) setupQEMUEmulation(platform types.Platform) error {
	hostPlatform := types.GetHostPlatform()
	if platform.String() == hostPlatform.String() {
		return nil // No emulation needed
	}

	qemuBinary, exists := e.supportedQEMU[platform.String()]
	if !exists {
		return fmt.Errorf("unsupported platform for emulation: %s", platform.String())
	}

	if qemuBinary == "" {
		return nil // Native platform
	}

	// Check if QEMU is already available
	if _, err := exec.LookPath(qemuBinary); err == nil {
		return nil
	}

	// Setup QEMU using binfmt_misc
	if e.rootless {
		// For rootless mode, use user-mode emulation
		return e.setupUserModeQEMU(platform)
	} else {
		// For privileged mode, use system-wide binfmt
		return e.setupSystemQEMU(platform)
	}
}

// setupUserModeQEMU configures user-mode QEMU emulation
func (e *ContainerExecutor) setupUserModeQEMU(platform types.Platform) error {
	// Check if binfmt_misc is available
	if _, err := os.Stat("/proc/sys/fs/binfmt_misc"); os.IsNotExist(err) {
		return fmt.Errorf("binfmt_misc not available for QEMU emulation")
	}

	// Use podman's built-in QEMU support
	cmd := exec.Command(e.runtime, "run", "--rm", "--privileged",
		"--platform", platform.String(),
		"tonistiigi/binfmt:qemu-v8.1.5", "--install", platform.Architecture)
	
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to setup QEMU emulation: %v, output: %s", err, string(output))
	}

	return nil
}

// setupSystemQEMU configures system-wide QEMU emulation
func (e *ContainerExecutor) setupSystemQEMU(platform types.Platform) error {
	cmd := exec.Command(e.runtime, "run", "--privileged", "--rm",
		"tonistiigi/binfmt:qemu-v8.1.5", "--install", platform.Architecture)
	
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to setup system QEMU: %v, output: %s", err, string(output))
	}

	return nil
}

// createSecureContainer creates a container with proper security context
func (e *ContainerExecutor) createSecureContainer(image string, platform types.Platform, cmd []string, env map[string]string, workDir string) (string, error) {
	containerName := fmt.Sprintf("ossb-build-%d", time.Now().UnixNano())
	
	args := []string{"create"}
	
	// Platform specification
	args = append(args, "--platform", platform.String())
	
	// Security context
	if e.securityContext != nil {
		if e.securityContext.RunAsUser != nil {
			args = append(args, "--user", strconv.FormatInt(*e.securityContext.RunAsUser, 10))
		}
		if e.securityContext.RunAsNonRoot != nil && *e.securityContext.RunAsNonRoot {
			args = append(args, "--user", "1000:1000")
		}
		for _, cap := range e.securityContext.Capabilities {
			args = append(args, "--cap-add", cap)
		}
	}
	
	// Resource limits
	if e.resourceLimits != nil {
		if e.resourceLimits.Memory != "" {
			args = append(args, "--memory", e.resourceLimits.Memory)
		}
		if e.resourceLimits.CPU != "" {
			args = append(args, "--cpus", e.resourceLimits.CPU)
		}
	}
	
	// Network isolation
	args = append(args, "--network", e.networkMode)
	
	// Environment variables
	for key, value := range env {
		args = append(args, "-e", fmt.Sprintf("%s=%s", key, value))
	}
	
	// Working directory
	if workDir != "" {
		args = append(args, "-w", workDir)
	}
	
	// Volume mounts
	for hostPath, containerPath := range e.volumes {
		args = append(args, "-v", fmt.Sprintf("%s:%s", hostPath, containerPath))
	}
	
	// Container name and image
	args = append(args, "--name", containerName, image)
	
	// Command to execute
	args = append(args, cmd...)
	
	createCmd := exec.Command(e.runtime, args...)
	output, err := createCmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to create container: %v, output: %s", err, string(output))
	}
	
	return containerName, nil
}

// runContainer executes a container and returns the output
func (e *ContainerExecutor) runContainer(containerName string) ([]byte, error) {
	// Start the container
	startCmd := exec.Command(e.runtime, "start", containerName)
	if err := startCmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to start container: %v", err)
	}
	
	// Wait for completion and get logs
	waitCmd := exec.Command(e.runtime, "wait", containerName)
	exitCodeBytes, err := waitCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to wait for container: %v", err)
	}
	
	exitCode := strings.TrimSpace(string(exitCodeBytes))
	
	// Get container logs
	logsCmd := exec.Command(e.runtime, "logs", containerName)
	output, err := logsCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get container logs: %v", err)
	}
	
	// Check exit code
	if exitCode != "0" {
		return output, fmt.Errorf("container exited with code %s", exitCode)
	}
	
	return output, nil
}

// removeContainer cleans up a container
func (e *ContainerExecutor) removeContainer(containerName string) error {
	cmd := exec.Command(e.runtime, "rm", "-f", containerName)
	return cmd.Run()
}

// mountVolume adds a volume mount for the container
func (e *ContainerExecutor) mountVolume(hostPath, containerPath string) error {
	// Ensure host path exists
	if err := os.MkdirAll(hostPath, 0755); err != nil {
		return fmt.Errorf("failed to create host path %s: %v", hostPath, err)
	}
	
	e.volumes[hostPath] = containerPath
	return nil
}

// createTempVolume creates a temporary volume for build operations
func (e *ContainerExecutor) createTempVolume(name string) (string, error) {
	volumePath := filepath.Join(e.tempDir, name)
	if err := os.MkdirAll(volumePath, 0755); err != nil {
		return "", fmt.Errorf("failed to create temp volume: %v", err)
	}
	return volumePath, nil
}

// setupNetworkIsolation configures network isolation for security
func (e *ContainerExecutor) setupNetworkIsolation() error {
	if e.networkMode == "none" {
		return nil // Already configured for no network
	}
	
	// For custom network modes, ensure they exist
	if e.networkMode != "host" && e.networkMode != "bridge" {
		// Check if custom network exists
		cmd := exec.Command(e.runtime, "network", "inspect", e.networkMode)
		if err := cmd.Run(); err != nil {
			// Create isolated network
			createCmd := exec.Command(e.runtime, "network", "create", 
				"--driver", "bridge",
				"--internal", // No external connectivity
				e.networkMode)
			if output, err := createCmd.CombinedOutput(); err != nil {
				return fmt.Errorf("failed to create network %s: %v, output: %s", 
					e.networkMode, err, string(output))
			}
		}
	}
	
	return nil
}

// validatePlatformSupport checks if the platform is supported for emulation
func (e *ContainerExecutor) validatePlatformSupport(platform types.Platform) error {
	if _, exists := e.supportedQEMU[platform.String()]; !exists {
		return fmt.Errorf("unsupported platform: %s", platform.String())
	}
	return nil
}

// getPodmanAPISocket returns the Podman API socket path
func (e *ContainerExecutor) getPodmanAPISocket() (string, error) {
	if e.runtime != "podman" {
		return "", fmt.Errorf("not using podman runtime")
	}
	
	// Check for user socket first (rootless)
	if e.rootless {
		runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
		if runtimeDir == "" {
			runtimeDir = fmt.Sprintf("/run/user/%d", os.Getuid())
		}
		userSocket := filepath.Join(runtimeDir, "podman", "podman.sock")
		if _, err := os.Stat(userSocket); err == nil {
			return userSocket, nil
		}
	}
	
	// Check system socket
	systemSocket := "/run/podman/podman.sock"
	if _, err := os.Stat(systemSocket); err == nil {
		return systemSocket, nil
	}
	
	return "", fmt.Errorf("podman API socket not found")
}

// testPodmanConnection tests connectivity to Podman API
func (e *ContainerExecutor) testPodmanConnection() error {
	if e.runtime != "podman" {
		return nil
	}
	
	socketPath, err := e.getPodmanAPISocket()
	if err != nil {
		// Fall back to CLI if API socket not available
		return nil
	}
	
	// Test connection
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return fmt.Errorf("failed to connect to podman API: %v", err)
	}
	conn.Close()
	
	return nil
}

func (e *ContainerExecutor) inspectImage(image string, platform types.Platform) (map[string]interface{}, error) {
	platformFlag := fmt.Sprintf("--platform=%s", platform.String())
	cmd := exec.Command(e.runtime, "inspect", platformFlag, image)
	
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to inspect image: %v", err)
	}
	
	var inspectData []map[string]interface{}
	if err := json.Unmarshal(stdout.Bytes(), &inspectData); err != nil {
		return nil, fmt.Errorf("failed to parse inspect output: %v", err)
	}
	
	if len(inspectData) == 0 {
		return nil, fmt.Errorf("no inspect data returned")
	}
	
	return inspectData[0], nil
}