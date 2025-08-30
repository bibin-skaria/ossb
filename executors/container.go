package executors

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/bibin-skaria/ossb/internal/types"
)

type ContainerExecutor struct {
	runtime         string
	supportedQEMU   map[string]string
	registryAuth    string
}

func NewContainerExecutor(runtime string) *ContainerExecutor {
	if runtime == "" {
		runtime = "docker"
		if _, err := exec.LookPath("podman"); err == nil && os.Getenv("RUNTIME") == "podman" {
			runtime = "podman"
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
	}

	return &ContainerExecutor{
		runtime:       runtime,
		supportedQEMU: supportedQEMU,
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
	
	exportCmd.Stdout = tarCmd.Stdin
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

	containerName := fmt.Sprintf("ossb-build-%d", time.Now().UnixNano())
	platformFlag := fmt.Sprintf("--platform=%s", platform.String())

	dockerfileContent := fmt.Sprintf(`FROM scratch
COPY . /
WORKDIR %s
`, operation.WorkDir)

	dockerfilePath := filepath.Join(layerDir, "Dockerfile.tmp")
	if err := os.WriteFile(dockerfilePath, []byte(dockerfileContent), 0644); err != nil {
		result.Error = fmt.Sprintf("failed to create temp Dockerfile: %v", err)
		return result, nil
	}

	envFlags := []string{}
	for key, value := range operation.Environment {
		envFlags = append(envFlags, "-e", fmt.Sprintf("%s=%s", key, value))
	}

	var cmd *exec.Cmd
	if len(operation.Command) == 1 {
		cmd = exec.Command(e.runtime, append([]string{
			"run", "--rm", platformFlag,
			"-v", fmt.Sprintf("%s:/workspace", baseDir),
			"-w", operation.WorkDir,
		}, append(envFlags, "busybox:latest", "sh", "-c", operation.Command[0])...)...)
	} else {
		cmd = exec.Command(e.runtime, append([]string{
			"run", "--rm", platformFlag,
			"-v", fmt.Sprintf("%s:/workspace", baseDir),
			"-w", operation.WorkDir,
		}, append(envFlags, append([]string{"busybox:latest"}, operation.Command...)...)...)...)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		result.Error = fmt.Sprintf("command failed: %v, output: %s", err, string(output))
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
	hostPlatform := types.GetHostPlatform()
	if platform.String() == hostPlatform.String() {
		return nil
	}

	qemuBinary, exists := e.supportedQEMU[platform.String()]
	if !exists || qemuBinary == "" {
		return nil
	}

	if _, err := exec.LookPath(qemuBinary); err != nil {
		cmd := exec.Command(e.runtime, "run", "--privileged", "--rm",
			"tonistiigi/binfmt:qemu-v8", "--install", platform.Architecture)
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to setup QEMU: %v, output: %s", err, string(output))
		}
	}

	return nil
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