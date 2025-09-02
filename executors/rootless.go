package executors

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/bibin-skaria/ossb/internal/types"
)

type RootlessExecutor struct {
	runtime    string
	userNS     bool
	currentUID int
	currentGID int
	subUIDs    []string
	subGIDs    []string
}

func NewRootlessExecutor() *RootlessExecutor {
	runtime := "podman" // Prefer podman for rootless
	if _, err := exec.LookPath("docker"); err == nil && os.Getenv("RUNTIME") == "docker" {
		runtime = "docker"
	}

	currentUser, _ := user.Current()
	uid, _ := strconv.Atoi(currentUser.Uid)
	gid, _ := strconv.Atoi(currentUser.Gid)

	executor := &RootlessExecutor{
		runtime:    runtime,
		currentUID: uid,
		currentGID: gid,
	}

	executor.setupUserNamespaces()
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

	// Build rootless container run command
	runArgs := []string{
		"run", "--rm", "--platform", platform.String(),
		"--user", fmt.Sprintf("%d:%d", e.currentUID, e.currentGID),
		"-v", fmt.Sprintf("%s:/workspace:Z", baseDir),
		"-w", operation.WorkDir,
	}

	// Add environment variables
	for key, value := range operation.Environment {
		runArgs = append(runArgs, "-e", fmt.Sprintf("%s=%s", key, value))
	}

	// Add the base image and command
	runArgs = append(runArgs, "alpine:latest")
	if len(operation.Command) == 1 {
		runArgs = append(runArgs, "sh", "-c", operation.Command[0])
	} else {
		runArgs = append(runArgs, operation.Command...)
	}

	cmd := e.buildRootlessCommand(runArgs)
	output, err := cmd.CombinedOutput()
	if err != nil {
		result.Error = fmt.Sprintf("rootless command failed: %v, output: %s", err, string(output))
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

func (e *RootlessExecutor) setupUserNamespaces() error {
	// Check if user namespaces are available
	currentUser, err := user.Current()
	if err != nil {
		return err
	}

	// Read subuid and subgid mappings
	e.subUIDs, _ = e.readSubIDFile("/etc/subuid", currentUser.Username)
	e.subGIDs, _ = e.readSubIDFile("/etc/subgid", currentUser.Username)

	e.userNS = len(e.subUIDs) > 0 && len(e.subGIDs) > 0
	return nil
}

func (e *RootlessExecutor) readSubIDFile(filename, username string) ([]string, error) {
	content, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var ranges []string
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		parts := strings.Split(line, ":")
		if len(parts) >= 3 && parts[0] == username {
			ranges = append(ranges, fmt.Sprintf("%s:%s", parts[1], parts[2]))
		}
	}

	return ranges, nil
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
		cmd := exec.Command("cp", "-a", source, dest)
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to copy %s: %v, output: %s", source, err, string(output))
		}
	}
	return nil
}

func (e *RootlessExecutor) addFilesRootless(sources []string, dest string) error {
	return e.copyFilesRootless(sources, dest)
}