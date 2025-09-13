package executors

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/bibin-skaria/ossb/internal/types"
	"github.com/bibin-skaria/ossb/security"
)

type RootlessExecutor struct {
	runtime         string
	userNS          bool
	currentUID      int
	currentGID      int
	subUIDs         []UIDRange
	subGIDs         []GIDRange
	resourceLimits  *types.ResourceLimits
	securityContext *types.SecurityContext
	workspaceDir    string
	tempDir         string
	securityManager *security.SecurityManager
}

type UIDRange struct {
	Start int
	Count int
}

type GIDRange struct {
	Start int
	Count int
}

func NewRootlessExecutor() *RootlessExecutor {
	return NewRootlessExecutorWithConfig(nil, nil)
}

func NewRootlessExecutorWithConfig(resourceLimits *types.ResourceLimits, securityContext *types.SecurityContext) *RootlessExecutor {
	runtime := ""
	
	// Try to find a suitable container runtime
	if _, err := exec.LookPath("podman"); err == nil {
		runtime = "podman" // Prefer podman for rootless
	} else if _, err := exec.LookPath("docker"); err == nil {
		runtime = "docker"
	}

	currentUser, _ := user.Current()
	uid, _ := strconv.Atoi(currentUser.Uid)
	gid, _ := strconv.Atoi(currentUser.Gid)

	// Create temporary workspace directory with proper permissions
	tempDir, _ := os.MkdirTemp("", "ossb-rootless-*")
	workspaceDir := filepath.Join(tempDir, "workspace")
	os.MkdirAll(workspaceDir, 0755)

	executor := &RootlessExecutor{
		runtime:         runtime,
		currentUID:      uid,
		currentGID:      gid,
		resourceLimits:  resourceLimits,
		securityContext: securityContext,
		workspaceDir:    workspaceDir,
		tempDir:         tempDir,
		securityManager: security.NewSecurityManager(),
	}

	if err := executor.setupUserNamespaces(); err != nil {
		fmt.Printf("Warning: Failed to setup user namespaces: %v\n", err)
	}

	// Enhanced security validation and enforcement
	if err := executor.securityManager.EnforceSecurityContext(securityContext); err != nil {
		fmt.Printf("Warning: Security context enforcement failed: %v\n", err)
	}

	return executor
}

func init() {
	RegisterExecutor("rootless", NewRootlessExecutor())
}

func (e *RootlessExecutor) Execute(operation *types.Operation, workDir string) (*types.OperationResult, error) {
	result := &types.OperationResult{
		Operation: operation,
		Success:   false,
	}

	// Validate operation in rootless context using enhanced security validation
	if err := e.securityManager.ValidateOperation(operation); err != nil {
		result.Error = fmt.Sprintf("security validation failed: %v", err)
		return result, nil
	}

	// Enforce resource limits before execution
	if err := e.enforceResourceLimits(); err != nil {
		result.Error = fmt.Sprintf("failed to enforce resource limits: %v", err)
		return result, nil
	}

	// Set up isolated workspace for this operation
	operationWorkspace, err := e.setupOperationWorkspace(workDir, operation)
	if err != nil {
		result.Error = fmt.Sprintf("failed to setup operation workspace: %v", err)
		return result, nil
	}
	defer e.cleanupOperationWorkspace(operationWorkspace)

	switch operation.Type {
	case types.OperationTypeSource:
		return e.executeSource(operation, operationWorkspace, result)
	case types.OperationTypeExec:
		return e.executeExec(operation, operationWorkspace, result)
	case types.OperationTypeFile:
		return e.executeFile(operation, operationWorkspace, result)
	case types.OperationTypeMeta:
		return e.executeMeta(operation, operationWorkspace, result)
	default:
		result.Error = fmt.Sprintf("unsupported operation type: %s", operation.Type)
		return result, nil
	}
}

func (e *RootlessExecutor) validateOperation(operation *types.Operation) error {
	// Ensure no privileged operations are attempted
	if operation.User == "root" || operation.User == "0" {
		return fmt.Errorf("cannot run operation as root user in rootless mode")
	}

	// Validate environment variables don't contain sensitive paths
	for key, value := range operation.Environment {
		if strings.Contains(value, "/proc") || strings.Contains(value, "/sys") {
			return fmt.Errorf("environment variable %s contains potentially unsafe path: %s", key, value)
		}
	}

	// Validate commands don't attempt privileged operations
	for _, cmd := range operation.Command {
		if e.isPrivilegedCommand(cmd) {
			return fmt.Errorf("command contains privileged operation: %s", cmd)
		}
	}

	return nil
}

func (e *RootlessExecutor) isPrivilegedCommand(command string) bool {
	privilegedCommands := []string{
		"sudo", "su", "mount", "umount", "chroot", "setuid", "setgid",
		"iptables", "ip6tables", "modprobe", "insmod", "rmmod",
	}

	cmdParts := strings.Fields(command)
	if len(cmdParts) == 0 {
		return false
	}

	baseName := filepath.Base(cmdParts[0])
	for _, privCmd := range privilegedCommands {
		if baseName == privCmd {
			return true
		}
	}

	return false
}

func (e *RootlessExecutor) setupOperationWorkspace(baseWorkDir string, operation *types.Operation) (string, error) {
	// Create isolated workspace for this operation
	operationID := fmt.Sprintf("op-%d", time.Now().UnixNano())
	workspace := filepath.Join(e.workspaceDir, operationID)

	if err := os.MkdirAll(workspace, 0755); err != nil {
		return "", fmt.Errorf("failed to create operation workspace: %v", err)
	}

	// Set up proper ownership and permissions
	if err := e.setupWorkspacePermissions(workspace); err != nil {
		return "", fmt.Errorf("failed to setup workspace permissions: %v", err)
	}

	return workspace, nil
}

func (e *RootlessExecutor) setupWorkspacePermissions(workspace string) error {
	// Ensure the workspace is owned by the current user
	if err := os.Chown(workspace, e.currentUID, e.currentGID); err != nil {
		return fmt.Errorf("failed to set workspace ownership: %v", err)
	}

	// Set restrictive permissions (only owner can access)
	if err := os.Chmod(workspace, 0700); err != nil {
		return fmt.Errorf("failed to set workspace permissions: %v", err)
	}

	return nil
}

func (e *RootlessExecutor) cleanupOperationWorkspace(workspace string) {
	if workspace != "" && strings.HasPrefix(workspace, e.workspaceDir) {
		os.RemoveAll(workspace)
	}
}

func (e *RootlessExecutor) executeSource(operation *types.Operation, workDir string, result *types.OperationResult) (*types.OperationResult, error) {
	image := operation.Metadata["image"]
	if image == "" {
		result.Error = "source operation missing image metadata"
		return result, nil
	}

	platform := operation.Platform
	if platform.OS == "" {
		platform = types.GetHostPlatform()
	}

	// Handle scratch image without container runtime
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

	// Check if container runtime is available for non-scratch images
	if e.runtime == "" {
		result.Error = "no container runtime available (podman or docker required)"
		return result, nil
	}

	// Use rootless container runtime
	cmd := e.buildRootlessCommand([]string{
		"pull", "--platform", platform.String(), image,
	})

	output, err := cmd.CombinedOutput()
	if err != nil {
		result.Error = fmt.Sprintf("failed to pull image %s for %s: %v, output: %s", 
			image, platform.String(), err, string(output))
		return result, nil
	}

	if err := e.setupRootlessQEMU(platform); err != nil {
		result.Error = fmt.Sprintf("failed to setup rootless QEMU for %s: %v", platform.String(), err)
		return result, nil
	}

	baseDir := filepath.Join(workDir, "base", platform.String())
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		result.Error = fmt.Sprintf("failed to create base directory: %v", err)
		return result, nil
	}

	// Extract image using rootless container
	containerName := fmt.Sprintf("ossb-rootless-extract-%d", time.Now().UnixNano())
	createCmd := e.buildRootlessCommand([]string{
		"create", "--platform", platform.String(), "--name", containerName, image,
	})
	
	if output, err := createCmd.CombinedOutput(); err != nil {
		result.Error = fmt.Sprintf("failed to create rootless container: %v, output: %s", err, string(output))
		return result, nil
	}

	defer func() {
		rmCmd := e.buildRootlessCommand([]string{"rm", containerName})
		rmCmd.Run()
	}()

	// Export and extract using user-owned processes
	exportCmd := e.buildRootlessCommand([]string{"export", containerName})
	tarCmd := exec.Command("tar", "-xf", "-", "-C", baseDir, "--no-same-owner")
	
	// Create pipe between export and tar commands
	pipeReader, pipeWriter := io.Pipe()
	exportCmd.Stdout = pipeWriter
	tarCmd.Stdin = pipeReader
	tarCmd.Stderr = os.Stderr

	if err := exportCmd.Start(); err != nil {
		result.Error = fmt.Sprintf("failed to start rootless export: %v", err)
		return result, nil
	}

	if err := tarCmd.Start(); err != nil {
		result.Error = fmt.Sprintf("failed to start tar extraction: %v", err)
		return result, nil
	}

	if err := exportCmd.Wait(); err != nil {
		result.Error = fmt.Sprintf("failed to export rootless container: %v", err)
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

func (e *RootlessExecutor) executeExec(operation *types.Operation, workDir string, result *types.OperationResult) (*types.OperationResult, error) {
	if len(operation.Command) == 0 {
		result.Error = "exec operation missing command"
		return result, nil
	}

	// Check if container runtime is available
	if e.runtime == "" {
		result.Error = "no container runtime available (podman or docker required)"
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

	// Create a snapshot of the base directory for change detection
	snapshotDir := filepath.Join(workDir, "snapshots", platform.String(), fmt.Sprintf("snapshot-%d", time.Now().UnixNano()))
	if err := e.createFilesystemSnapshot(baseDir, snapshotDir); err != nil {
		result.Error = fmt.Sprintf("failed to create filesystem snapshot: %v", err)
		return result, nil
	}
	defer os.RemoveAll(snapshotDir)

	// Build rootless container run command with proper user namespace mapping
	runArgs := e.buildRootlessRunArgs(platform, baseDir, operation)

	// Add resource limits if specified
	if e.resourceLimits != nil {
		runArgs = e.addResourceLimits(runArgs)
	}

	// Add security constraints
	runArgs = e.addSecurityConstraints(runArgs)

	// Add the base image and command
	runArgs = append(runArgs, "alpine:latest")
	if len(operation.Command) == 1 {
		runArgs = append(runArgs, "sh", "-c", operation.Command[0])
	} else {
		runArgs = append(runArgs, operation.Command...)
	}

	// Execute with timeout and proper context
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	cmd := e.buildRootlessCommandWithContext(ctx, runArgs)
	
	// Set up proper process group for cleanup
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		result.Error = fmt.Sprintf("rootless command failed: %v, output: %s", err, string(output))
		return result, nil
	}

	// Capture filesystem changes with proper permission handling
	if err := e.captureRootlessChangesWithPermissions(baseDir, snapshotDir, layerDir); err != nil {
		result.Error = fmt.Sprintf("failed to capture rootless changes: %v", err)
		return result, nil
	}

	result.Success = true
	result.Outputs = operation.Outputs
	result.Environment = operation.Environment

	return result, nil
}

func (e *RootlessExecutor) buildRootlessRunArgs(platform types.Platform, baseDir string, operation *types.Operation) []string {
	runArgs := []string{
		"run", "--rm", "--platform", platform.String(),
	}

	// Add user namespace mapping
	if e.userNS && len(e.subUIDs) > 0 && len(e.subGIDs) > 0 {
		// Map current user to root inside container, and use subuid/subgid ranges
		runArgs = append(runArgs, 
			"--uidmap", fmt.Sprintf("0:%d:1", e.currentUID),
			"--gidmap", fmt.Sprintf("0:%d:1", e.currentGID),
		)
		
		// Add additional mappings from subuid/subgid ranges
		for i, uidRange := range e.subUIDs {
			if i == 0 { // Use first range for container users 1-65536
				runArgs = append(runArgs, "--uidmap", fmt.Sprintf("1:%d:%d", uidRange.Start, min(uidRange.Count, 65535)))
			}
		}
		for i, gidRange := range e.subGIDs {
			if i == 0 { // Use first range for container groups 1-65536
				runArgs = append(runArgs, "--gidmap", fmt.Sprintf("1:%d:%d", gidRange.Start, min(gidRange.Count, 65535)))
			}
		}
	} else {
		// Fallback to current user mapping
		runArgs = append(runArgs, "--user", fmt.Sprintf("%d:%d", e.currentUID, e.currentGID))
	}

	// Mount workspace with proper SELinux context
	runArgs = append(runArgs, "-v", fmt.Sprintf("%s:/workspace:Z", baseDir))
	
	// Set working directory
	if operation.WorkDir != "" {
		runArgs = append(runArgs, "-w", operation.WorkDir)
	} else {
		runArgs = append(runArgs, "-w", "/workspace")
	}

	// Add environment variables
	for key, value := range operation.Environment {
		runArgs = append(runArgs, "-e", fmt.Sprintf("%s=%s", key, value))
	}

	return runArgs
}

func (e *RootlessExecutor) addResourceLimits(runArgs []string) []string {
	if e.resourceLimits.Memory != "" {
		runArgs = append(runArgs, "--memory", e.resourceLimits.Memory)
	}
	if e.resourceLimits.CPU != "" {
		runArgs = append(runArgs, "--cpus", e.resourceLimits.CPU)
	}
	// Note: Disk limits are handled at the filesystem level
	return runArgs
}

func (e *RootlessExecutor) addSecurityConstraints(runArgs []string) []string {
	// Always run without privileged access
	runArgs = append(runArgs, "--privileged=false")
	
	// Drop all capabilities by default
	runArgs = append(runArgs, "--cap-drop", "ALL")
	
	// Add only explicitly allowed capabilities
	if e.securityContext != nil && len(e.securityContext.Capabilities) > 0 {
		for _, cap := range e.securityContext.Capabilities {
			if !e.isPrivilegedCapability(cap) {
				runArgs = append(runArgs, "--cap-add", cap)
			}
		}
	}
	
	// Ensure no new privileges can be gained
	runArgs = append(runArgs, "--security-opt", "no-new-privileges")
	
	// Use read-only root filesystem where possible
	runArgs = append(runArgs, "--read-only")
	runArgs = append(runArgs, "--tmpfs", "/tmp:rw,noexec,nosuid,size=100m")
	
	return runArgs
}

func (e *RootlessExecutor) buildRootlessCommandWithContext(ctx context.Context, args []string) *exec.Cmd {
	if e.runtime == "podman" {
		return exec.CommandContext(ctx, "podman", args...)
	} else {
		dockerArgs := []string{"--context", "rootless"}
		dockerArgs = append(dockerArgs, args...)
		return exec.CommandContext(ctx, "docker", dockerArgs...)
	}
}

func (e *RootlessExecutor) createFilesystemSnapshot(sourceDir, snapshotDir string) error {
	if err := os.MkdirAll(snapshotDir, 0755); err != nil {
		return err
	}

	// Use cp with archive mode to preserve all attributes
	cmd := exec.Command("cp", "-a", sourceDir+"/.", snapshotDir+"/")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create snapshot: %v, output: %s", err, string(output))
	}

	return nil
}

func (e *RootlessExecutor) captureRootlessChangesWithPermissions(baseDir, snapshotDir, layerDir string) error {
	if err := os.MkdirAll(layerDir, 0755); err != nil {
		return err
	}

	// Use rsync to capture only changes and preserve permissions
	cmd := exec.Command("rsync", "-a", "--numeric-ids", "--delete", 
		"--compare-dest="+snapshotDir+"/", baseDir+"/", layerDir+"/")
	
	if _, err := cmd.CombinedOutput(); err != nil {
		// Fallback to diff-based approach if rsync fails
		return e.captureChangesWithDiff(baseDir, snapshotDir, layerDir)
	}

	// Fix ownership to current user for files in layer
	return e.fixLayerOwnership(layerDir)
}

func (e *RootlessExecutor) captureChangesWithDiff(baseDir, snapshotDir, layerDir string) error {
	// Find all files that have changed
	cmd := exec.Command("find", baseDir, "-newer", snapshotDir, "-type", "f")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to find changed files: %v", err)
	}

	changedFiles := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, file := range changedFiles {
		if file == "" {
			continue
		}

		relPath, err := filepath.Rel(baseDir, file)
		if err != nil {
			continue
		}

		destPath := filepath.Join(layerDir, relPath)
		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			continue
		}

		// Copy file preserving permissions
		if err := e.copyFileWithPermissions(file, destPath); err != nil {
			return fmt.Errorf("failed to copy changed file %s: %v", file, err)
		}
	}

	return nil
}

func (e *RootlessExecutor) copyFileWithPermissions(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	return e.copyFileWithPermissionMapping(src, dst, srcInfo)
}

func (e *RootlessExecutor) fixLayerOwnership(layerDir string) error {
	return filepath.Walk(layerDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		return os.Chown(path, e.currentUID, e.currentGID)
	})
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (e *RootlessExecutor) executeFile(operation *types.Operation, workDir string, result *types.OperationResult) (*types.OperationResult, error) {
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
		if err := e.copyFilesRootless(sources, destPath); err != nil {
			result.Error = fmt.Sprintf("rootless copy failed: %v", err)
			return result, nil
		}
	case "add":
		if err := e.addFilesRootless(sources, destPath); err != nil {
			result.Error = fmt.Sprintf("rootless add failed: %v", err)
			return result, nil
		}
	default:
		result.Error = fmt.Sprintf("unsupported file operation: %s", operationType)
		return result, nil
	}

	if err := e.captureRootlessChanges(baseDir, layerDir); err != nil {
		result.Error = fmt.Sprintf("failed to capture rootless changes: %v", err)
		return result, nil
	}

	result.Success = true
	result.Outputs = operation.Outputs
	result.Environment = operation.Environment

	return result, nil
}

func (e *RootlessExecutor) executeMeta(operation *types.Operation, workDir string, result *types.OperationResult) (*types.OperationResult, error) {
	result.Success = true
	result.Outputs = operation.Outputs
	result.Environment = operation.Environment

	return result, nil
}

func (e *RootlessExecutor) buildRootlessCommand(args []string) *exec.Cmd {
	if e.runtime == "podman" {
		// Podman is rootless by default
		return exec.Command("podman", args...)
	} else {
		// Docker rootless mode
		dockerArgs := []string{"--context", "rootless"}
		dockerArgs = append(dockerArgs, args...)
		return exec.Command("docker", dockerArgs...)
	}
}

// Cleanup releases resources used by the rootless executor
func (e *RootlessExecutor) Cleanup() error {
	if e.tempDir != "" {
		return os.RemoveAll(e.tempDir)
	}
	return nil
}

func (e *RootlessExecutor) setupUserNamespaces() error {
	// Check if user namespaces are available
	if !e.isUserNamespaceSupported() {
		return fmt.Errorf("user namespaces not supported on this system")
	}

	currentUser, err := user.Current()
	if err != nil {
		return fmt.Errorf("failed to get current user: %v", err)
	}

	// Read subuid and subgid mappings
	subUIDs, err := e.readSubIDFile("/etc/subuid", currentUser.Username)
	if err != nil {
		return fmt.Errorf("failed to read subuid mappings: %v", err)
	}
	e.subUIDs = subUIDs

	subGIDs, err := e.readSubGIDFile("/etc/subgid", currentUser.Username)
	if err != nil {
		return fmt.Errorf("failed to read subgid mappings: %v", err)
	}
	e.subGIDs = subGIDs

	e.userNS = len(e.subUIDs) > 0 && len(e.subGIDs) > 0

	if !e.userNS {
		return fmt.Errorf("no subuid/subgid mappings found for user %s", currentUser.Username)
	}

	// Validate that we have sufficient ID ranges
	if err := e.validateIDRanges(); err != nil {
		return fmt.Errorf("invalid ID ranges: %v", err)
	}

	// Test user namespace creation to ensure it works
	if err := e.testUserNamespaceCreation(); err != nil {
		return fmt.Errorf("user namespace creation test failed: %v", err)
	}

	return nil
}

func (e *RootlessExecutor) isUserNamespaceSupported() bool {
	// Check if user namespaces are enabled in the kernel
	if _, err := os.Stat("/proc/self/ns/user"); err != nil {
		return false
	}

	// Check if unprivileged user namespaces are allowed
	if content, err := os.ReadFile("/proc/sys/kernel/unprivileged_userns_clone"); err == nil {
		return strings.TrimSpace(string(content)) == "1"
	}

	// Check if user_namespaces are available via unshare
	cmd := exec.Command("unshare", "--user", "--map-root-user", "true")
	return cmd.Run() == nil
}

func (e *RootlessExecutor) readSubIDFile(filename, username string) ([]UIDRange, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var ranges []UIDRange
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.Split(line, ":")
		if len(parts) >= 3 && parts[0] == username {
			start, err := strconv.Atoi(parts[1])
			if err != nil {
				continue
			}
			count, err := strconv.Atoi(parts[2])
			if err != nil {
				continue
			}
			ranges = append(ranges, UIDRange{Start: start, Count: count})
		}
	}

	return ranges, scanner.Err()
}

func (e *RootlessExecutor) readSubGIDFile(filename, username string) ([]GIDRange, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var ranges []GIDRange
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.Split(line, ":")
		if len(parts) >= 3 && parts[0] == username {
			start, err := strconv.Atoi(parts[1])
			if err != nil {
				continue
			}
			count, err := strconv.Atoi(parts[2])
			if err != nil {
				continue
			}
			ranges = append(ranges, GIDRange{Start: start, Count: count})
		}
	}

	return ranges, scanner.Err()
}

func (e *RootlessExecutor) validateIDRanges() error {
	if len(e.subUIDs) == 0 {
		return fmt.Errorf("no subuid ranges available")
	}
	if len(e.subGIDs) == 0 {
		return fmt.Errorf("no subgid ranges available")
	}

	// Ensure we have at least 65536 UIDs and GIDs for proper container operation
	totalUIDs := 0
	for _, r := range e.subUIDs {
		totalUIDs += r.Count
	}
	if totalUIDs < 65536 {
		return fmt.Errorf("insufficient subuid range: need at least 65536, got %d", totalUIDs)
	}

	totalGIDs := 0
	for _, r := range e.subGIDs {
		totalGIDs += r.Count
	}
	if totalGIDs < 65536 {
		return fmt.Errorf("insufficient subgid range: need at least 65536, got %d", totalGIDs)
	}

	return nil
}

func (e *RootlessExecutor) validateSecurityConstraints() error {
	// Ensure we're not running as root
	if e.currentUID == 0 {
		return fmt.Errorf("rootless executor cannot run as root user")
	}

	// Validate security context if provided
	if e.securityContext != nil {
		if e.securityContext.RunAsUser != nil && *e.securityContext.RunAsUser == 0 {
			return fmt.Errorf("security context specifies root user, which is not allowed in rootless mode")
		}

		if e.securityContext.RunAsNonRoot != nil && !*e.securityContext.RunAsNonRoot {
			return fmt.Errorf("security context allows root user, which is not allowed in rootless mode")
		}

		// Validate capabilities - rootless mode should not have privileged capabilities
		if len(e.securityContext.Capabilities) > 0 {
			for _, cap := range e.securityContext.Capabilities {
				if e.isPrivilegedCapability(cap) {
					return fmt.Errorf("privileged capability %s not allowed in rootless mode", cap)
				}
			}
		}
	}

	// Validate resource limits are within acceptable bounds
	if err := e.validateResourceLimits(); err != nil {
		return fmt.Errorf("resource limit validation failed: %v", err)
	}

	// Ensure no privileged access to system resources
	if err := e.validateSystemAccess(); err != nil {
		return fmt.Errorf("system access validation failed: %v", err)
	}

	return nil
}

func (e *RootlessExecutor) isPrivilegedCapability(capability string) bool {
	privilegedCaps := []string{
		"CAP_SYS_ADMIN",
		"CAP_SYS_MODULE",
		"CAP_SYS_RAWIO",
		"CAP_SYS_PTRACE",
		"CAP_DAC_OVERRIDE",
		"CAP_DAC_READ_SEARCH",
		"CAP_FOWNER",
		"CAP_SETUID",
		"CAP_SETGID",
		"CAP_NET_ADMIN",
		"CAP_NET_RAW",
	}

	for _, privCap := range privilegedCaps {
		if strings.EqualFold(capability, privCap) {
			return true
		}
	}
	return false
}

func (e *RootlessExecutor) setupRootlessQEMU(platform types.Platform) error {
	hostPlatform := types.GetHostPlatform()
	if platform.String() == hostPlatform.String() {
		return nil
	}

	// For rootless mode, we use user-mode QEMU emulation
	qemuArch := ""
	switch platform.Architecture {
	case "arm64":
		qemuArch = "aarch64"
	case "arm":
		qemuArch = "arm"
	case "386":
		qemuArch = "i386"
	case "ppc64le":
		qemuArch = "ppc64le"
	case "s390x":
		qemuArch = "s390x"
	default:
		return nil
	}

	qemuBinary := fmt.Sprintf("qemu-%s-static", qemuArch)
	if _, err := exec.LookPath(qemuBinary); err != nil {
		// Try to install binfmt support using rootless container
		cmd := e.buildRootlessCommand([]string{
			"run", "--rm", "--privileged=false",
			"tonistiigi/binfmt:qemu-v8",
			"--install", platform.Architecture,
		})
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to setup rootless QEMU: %v, output: %s", err, string(output))
		}
	}

	return nil
}

func (e *RootlessExecutor) captureRootlessChanges(baseDir, layerDir string) error {
	// Use rsync to preserve ownership and permissions correctly
	cmd := exec.Command("rsync", "-a", "--numeric-ids", baseDir+"/", layerDir+"/")
	if _, err := cmd.CombinedOutput(); err != nil {
		// Fallback to cp if rsync is not available
		cmd = exec.Command("cp", "-a", baseDir+"/.", layerDir+"/")
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to capture changes: %v, output: %s", err, string(output))
		}
	}
	return nil
}

func (e *RootlessExecutor) copyFilesRootless(sources []string, dest string) error {
	for _, source := range sources {
		// Use enhanced permission handling for better rootless support
		if err := e.enhanceFilesystemPermissionHandling(source, dest); err != nil {
			// Fallback to cp command if enhanced handling fails
			cmd := exec.Command("cp", "-a", source, dest)
			if output, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("failed to copy %s: %v, output: %s", source, err, string(output))
			}
			
			// Fix ownership after cp
			if err := e.fixLayerOwnership(dest); err != nil {
				fmt.Printf("Warning: Could not fix ownership after copy: %v\n", err)
			}
		}
	}
	return nil
}

func (e *RootlessExecutor) addFilesRootless(sources []string, dest string) error {
	return e.copyFilesRootless(sources, dest)
}

// testUserNamespaceCreation tests that we can actually create user namespaces
func (e *RootlessExecutor) testUserNamespaceCreation() error {
	// Test creating a simple user namespace with ID mapping
	cmd := exec.Command("unshare", "--user", "--map-root-user", "true")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create test user namespace: %v", err)
	}

	// Test that we can map UIDs and GIDs correctly
	if len(e.subUIDs) > 0 && len(e.subGIDs) > 0 {
		testCmd := exec.Command("unshare", "--user", 
			"--map-user", fmt.Sprintf("0:%d:1", e.currentUID),
			"--map-group", fmt.Sprintf("0:%d:1", e.currentGID),
			"id")
		if err := testCmd.Run(); err != nil {
			return fmt.Errorf("failed to test UID/GID mapping: %v", err)
		}
	}

	return nil
}

// validateResourceLimits ensures resource limits are within acceptable bounds for rootless operation
func (e *RootlessExecutor) validateResourceLimits() error {
	if e.resourceLimits == nil {
		return nil
	}

	// Validate memory limits
	if e.resourceLimits.Memory != "" {
		if err := e.validateMemoryLimit(e.resourceLimits.Memory); err != nil {
			return fmt.Errorf("invalid memory limit: %v", err)
		}
	}

	// Validate CPU limits
	if e.resourceLimits.CPU != "" {
		if err := e.validateCPULimit(e.resourceLimits.CPU); err != nil {
			return fmt.Errorf("invalid CPU limit: %v", err)
		}
	}

	// Validate disk limits
	if e.resourceLimits.Disk != "" {
		if err := e.validateDiskLimit(e.resourceLimits.Disk); err != nil {
			return fmt.Errorf("invalid disk limit: %v", err)
		}
	}

	return nil
}

// validateMemoryLimit validates memory limit format and ensures it's reasonable for rootless operation
func (e *RootlessExecutor) validateMemoryLimit(memLimit string) error {
	// Parse memory limit (supports formats like "512Mi", "1Gi", "2048m")
	if !strings.HasSuffix(memLimit, "i") && !strings.HasSuffix(memLimit, "m") && !strings.HasSuffix(memLimit, "g") {
		return fmt.Errorf("memory limit must end with 'Mi', 'Gi', or 'm'")
	}

	// Ensure memory limit is not too high for rootless operation (max 8Gi)
	if strings.HasSuffix(memLimit, "Gi") {
		limitStr := strings.TrimSuffix(memLimit, "Gi")
		if limit, err := strconv.ParseFloat(limitStr, 64); err == nil && limit > 8.0 {
			return fmt.Errorf("memory limit %s exceeds maximum allowed for rootless operation (8Gi)", memLimit)
		}
	}

	return nil
}

// validateCPULimit validates CPU limit format and ensures it's reasonable for rootless operation
func (e *RootlessExecutor) validateCPULimit(cpuLimit string) error {
	// Parse CPU limit (supports formats like "0.5", "1000m", "2")
	if strings.HasSuffix(cpuLimit, "m") {
		limitStr := strings.TrimSuffix(cpuLimit, "m")
		if limit, err := strconv.ParseInt(limitStr, 10, 64); err != nil || limit <= 0 {
			return fmt.Errorf("invalid CPU limit format: %s", cpuLimit)
		}
	} else {
		if limit, err := strconv.ParseFloat(cpuLimit, 64); err != nil || limit <= 0 {
			return fmt.Errorf("invalid CPU limit format: %s", cpuLimit)
		}
	}

	return nil
}

// validateDiskLimit validates disk limit format and ensures it's reasonable for rootless operation
func (e *RootlessExecutor) validateDiskLimit(diskLimit string) error {
	// Parse disk limit (supports formats like "1Gi", "512Mi", "2048m")
	if !strings.HasSuffix(diskLimit, "i") && !strings.HasSuffix(diskLimit, "m") && !strings.HasSuffix(diskLimit, "g") {
		return fmt.Errorf("disk limit must end with 'Mi', 'Gi', or 'm'")
	}

	// Ensure disk limit is not too high for rootless operation (max 50Gi)
	if strings.HasSuffix(diskLimit, "Gi") {
		limitStr := strings.TrimSuffix(diskLimit, "Gi")
		if limit, err := strconv.ParseFloat(limitStr, 64); err == nil && limit > 50.0 {
			return fmt.Errorf("disk limit %s exceeds maximum allowed for rootless operation (50Gi)", diskLimit)
		}
	}

	return nil
}

// validateSystemAccess ensures no privileged access to system resources
func (e *RootlessExecutor) validateSystemAccess() error {
	// Check that we don't have access to privileged system directories
	privilegedPaths := []string{
		"/proc/sys",
		"/sys/kernel",
		"/dev/mem",
		"/dev/kmem",
		"/proc/kcore",
	}

	for _, path := range privilegedPaths {
		if info, err := os.Stat(path); err == nil {
			// Check if we have write access (which we shouldn't in rootless mode)
			if info.Mode().Perm()&0200 != 0 {
				// Try to actually write to verify
				testFile := filepath.Join(path, "test-rootless-access")
				if file, err := os.Create(testFile); err == nil {
					file.Close()
					os.Remove(testFile)
					return fmt.Errorf("unexpected write access to privileged path: %s", path)
				}
			}
		}
	}

	// Verify we can't bind to privileged ports without proper capabilities
	if err := e.testPrivilegedPortBinding(); err != nil {
		return err
	}

	return nil
}

// testPrivilegedPortBinding tests that we can't bind to privileged ports without proper capabilities
func (e *RootlessExecutor) testPrivilegedPortBinding() error {
	// Skip this test on macOS as it has different port binding behavior
	if runtime.GOOS == "darwin" {
		return nil
	}

	// Only test if we don't have CAP_NET_BIND_SERVICE capability
	hasBindCapability := false
	if e.securityContext != nil {
		for _, cap := range e.securityContext.Capabilities {
			if strings.EqualFold(cap, "CAP_NET_BIND_SERVICE") {
				hasBindCapability = true
				break
			}
		}
	}

	if !hasBindCapability {
		// Try to bind to port 80 (should fail in rootless mode without capability)
		// Use a more portable approach with Go's net package
		listener, err := net.Listen("tcp", ":80")
		if err == nil {
			listener.Close()
			return fmt.Errorf("unexpected ability to bind to privileged port 80 without CAP_NET_BIND_SERVICE")
		}
		// Expected to fail, which is good
	}

	return nil
}

// enforceResourceLimits applies resource limits to the current process and its children
func (e *RootlessExecutor) enforceResourceLimits() error {
	if e.resourceLimits == nil {
		return nil
	}

	// Set memory limits using cgroups v2 if available
	if e.resourceLimits.Memory != "" {
		if err := e.setMemoryLimit(e.resourceLimits.Memory); err != nil {
			return fmt.Errorf("failed to set memory limit: %v", err)
		}
	}

	// Set CPU limits using cgroups v2 if available
	if e.resourceLimits.CPU != "" {
		if err := e.setCPULimit(e.resourceLimits.CPU); err != nil {
			return fmt.Errorf("failed to set CPU limit: %v", err)
		}
	}

	return nil
}

// setMemoryLimit sets memory limit using cgroups v2 for rootless operation
func (e *RootlessExecutor) setMemoryLimit(memLimit string) error {
	// In rootless mode, we can only set limits within user-owned cgroups
	cgroupPath := fmt.Sprintf("/sys/fs/cgroup/user.slice/user-%d.slice/user@%d.service", e.currentUID, e.currentUID)
	
	// Check if cgroups v2 is available and we have access
	if _, err := os.Stat(cgroupPath); os.IsNotExist(err) {
		// Fallback to container runtime limits (handled in buildRootlessRunArgs)
		return nil
	}

	// Convert memory limit to bytes
	bytes, err := e.parseMemoryToBytes(memLimit)
	if err != nil {
		return err
	}

	// Try to write to memory.max (this might fail in rootless mode, which is expected)
	memoryMaxPath := filepath.Join(cgroupPath, "memory.max")
	if err := os.WriteFile(memoryMaxPath, []byte(fmt.Sprintf("%d", bytes)), 0644); err != nil {
		// This is expected to fail in many rootless setups, so we don't return an error
		// The container runtime will handle the limits instead
	}

	return nil
}

// setCPULimit sets CPU limit using cgroups v2 for rootless operation
func (e *RootlessExecutor) setCPULimit(cpuLimit string) error {
	// Similar to memory limits, CPU limits in rootless mode are primarily handled by the container runtime
	// This is a placeholder for future cgroups v2 integration
	return nil
}

// parseMemoryToBytes converts memory limit string to bytes
func (e *RootlessExecutor) parseMemoryToBytes(memLimit string) (int64, error) {
	if strings.HasSuffix(memLimit, "Gi") {
		limitStr := strings.TrimSuffix(memLimit, "Gi")
		if limit, err := strconv.ParseFloat(limitStr, 64); err == nil {
			return int64(limit * 1024 * 1024 * 1024), nil
		}
	} else if strings.HasSuffix(memLimit, "Mi") {
		limitStr := strings.TrimSuffix(memLimit, "Mi")
		if limit, err := strconv.ParseFloat(limitStr, 64); err == nil {
			return int64(limit * 1024 * 1024), nil
		}
	} else if strings.HasSuffix(memLimit, "m") {
		limitStr := strings.TrimSuffix(memLimit, "m")
		if limit, err := strconv.ParseInt(limitStr, 10, 64); err == nil {
			return limit * 1024 * 1024, nil
		}
	}

	return 0, fmt.Errorf("invalid memory limit format: %s", memLimit)
}

// enhanceFilesystemPermissionHandling improves permission handling between host and container contexts
func (e *RootlessExecutor) enhanceFilesystemPermissionHandling(sourcePath, destPath string) error {
	// Get source file info
	sourceInfo, err := os.Stat(sourcePath)
	if err != nil {
		return fmt.Errorf("failed to stat source file: %v", err)
	}

	// Create destination directory if it doesn't exist
	destDir := filepath.Dir(destPath)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %v", err)
	}

	// Copy file with proper permission mapping
	if err := e.copyFileWithPermissionMapping(sourcePath, destPath, sourceInfo); err != nil {
		return fmt.Errorf("failed to copy file with permission mapping: %v", err)
	}

	return nil
}

// copyFileWithPermissionMapping copies a file while properly mapping permissions for rootless operation
func (e *RootlessExecutor) copyFileWithPermissionMapping(src, dst string, srcInfo os.FileInfo) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, srcInfo.Mode())
	if err != nil {
		return err
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return err
	}

	// Map ownership to current user (since we're in rootless mode)
	if err := os.Chown(dst, e.currentUID, e.currentGID); err != nil {
		// In rootless mode, we might not be able to change ownership, which is expected
		// Log the warning but don't fail
		fmt.Printf("Warning: Could not change ownership of %s: %v\n", dst, err)
	}

	// Preserve permissions but ensure they're safe for rootless operation
	safeMode := e.makeModeRootlessSafe(srcInfo.Mode())
	if err := os.Chmod(dst, safeMode); err != nil {
		return fmt.Errorf("failed to set permissions: %v", err)
	}

	return nil
}

// makeModeRootlessSafe ensures file permissions are safe for rootless operation
func (e *RootlessExecutor) makeModeRootlessSafe(mode os.FileMode) os.FileMode {
	// Remove setuid and setgid bits for security
	safeMode := mode &^ (os.ModeSetuid | os.ModeSetgid)
	
	// Ensure owner has read/write permissions
	safeMode |= 0600
	
	// Limit other permissions in rootless mode
	if safeMode&0077 != 0 {
		// If group or other have permissions, limit them
		safeMode = (safeMode &^ 0077) | (safeMode & 0077 & 0055) // Remove write for group/other
	}
	
	return safeMode
}