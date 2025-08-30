package executors

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/bibin-skaria/ossb/internal/types"
)

type LocalExecutor struct{}

func init() {
	RegisterExecutor("local", &LocalExecutor{})
}

func (e *LocalExecutor) Execute(operation *types.Operation, workDir string) (*types.OperationResult, error) {
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

func (e *LocalExecutor) executeSource(operation *types.Operation, workDir string, result *types.OperationResult) (*types.OperationResult, error) {
	image := operation.Metadata["image"]
	if image == "" {
		result.Error = "source operation missing image metadata"
		return result, nil
	}

	if image == "scratch" {
		result.Success = true
		result.Outputs = operation.Outputs
		return result, nil
	}

	baseDir := filepath.Join(workDir, "base")
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		result.Error = fmt.Sprintf("failed to create base directory: %v", err)
		return result, nil
	}

	result.Success = true
	result.Outputs = operation.Outputs
	result.Environment = map[string]string{
		"PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
	}
	
	return result, nil
}

func (e *LocalExecutor) executeExec(operation *types.Operation, workDir string, result *types.OperationResult) (*types.OperationResult, error) {
	if len(operation.Command) == 0 {
		result.Error = "exec operation missing command"
		return result, nil
	}

	layerDir := filepath.Join(workDir, "layers", fmt.Sprintf("layer-%d", len(operation.Outputs)))
	if err := os.MkdirAll(layerDir, 0755); err != nil {
		result.Error = fmt.Sprintf("failed to create layer directory: %v", err)
		return result, nil
	}

	cmdWorkDir := operation.WorkDir
	if cmdWorkDir == "" {
		cmdWorkDir = "/"
	}

	var cmd *exec.Cmd
	if len(operation.Command) == 1 {
		cmd = exec.Command("sh", "-c", operation.Command[0])
	} else {
		cmd = exec.Command(operation.Command[0], operation.Command[1:]...)
	}

	cmd.Dir = filepath.Join(layerDir, strings.TrimPrefix(cmdWorkDir, "/"))
	if err := os.MkdirAll(cmd.Dir, 0755); err != nil {
		result.Error = fmt.Sprintf("failed to create working directory: %v", err)
		return result, nil
	}

	cmd.Env = e.buildEnvironment(operation.Environment)

	if operation.User != "" && operation.User != "root" {
		uid, gid, err := e.parseUser(operation.User)
		if err != nil {
			result.Error = fmt.Sprintf("failed to parse user: %v", err)
			return result, nil
		}
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Credential: &syscall.Credential{
				Uid: uid,
				Gid: gid,
			},
		}
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		result.Error = fmt.Sprintf("command failed: %v, output: %s", err, string(output))
		return result, nil
	}

	result.Success = true
	result.Outputs = operation.Outputs
	result.Environment = operation.Environment
	
	return result, nil
}

func (e *LocalExecutor) executeFile(operation *types.Operation, workDir string, result *types.OperationResult) (*types.OperationResult, error) {
	if len(operation.Command) == 0 {
		result.Error = "file operation missing command"
		return result, nil
	}

	operationType := operation.Command[0]
	dest := operation.Metadata["dest"]
	if dest == "" {
		result.Error = "file operation missing destination"
		return result, nil
	}

	layerDir := filepath.Join(workDir, "layers", fmt.Sprintf("layer-%d", len(operation.Outputs)))
	if err := os.MkdirAll(layerDir, 0755); err != nil {
		result.Error = fmt.Sprintf("failed to create layer directory: %v", err)
		return result, nil
	}

	destPath := filepath.Join(layerDir, strings.TrimPrefix(dest, "/"))
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

	result.Success = true
	result.Outputs = operation.Outputs
	result.Environment = operation.Environment
	
	return result, nil
}

func (e *LocalExecutor) executeMeta(operation *types.Operation, workDir string, result *types.OperationResult) (*types.OperationResult, error) {
	result.Success = true
	result.Outputs = operation.Outputs
	result.Environment = operation.Environment
	
	return result, nil
}

func (e *LocalExecutor) copyFiles(sources []string, dest string) error {
	for _, source := range sources {
		if err := e.copyPath(source, dest); err != nil {
			return err
		}
	}
	return nil
}

func (e *LocalExecutor) addFiles(sources []string, dest string) error {
	return e.copyFiles(sources, dest)
}

func (e *LocalExecutor) copyPath(source, dest string) error {
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

func (e *LocalExecutor) copyFile(source, dest string) error {
	srcFile, err := os.Open(source)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	srcInfo, err := srcFile.Stat()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}

	destFile, err := os.OpenFile(dest, os.O_RDWR|os.O_CREATE|os.O_TRUNC, srcInfo.Mode())
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, srcFile)
	return err
}

func (e *LocalExecutor) copyDir(source, dest string) error {
	srcInfo, err := os.Stat(source)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dest, srcInfo.Mode()); err != nil {
		return err
	}

	entries, err := os.ReadDir(source)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(source, entry.Name())
		destPath := filepath.Join(dest, entry.Name())

		if entry.IsDir() {
			if err := e.copyDir(srcPath, destPath); err != nil {
				return err
			}
		} else {
			if err := e.copyFile(srcPath, destPath); err != nil {
				return err
			}
		}
	}

	return nil
}

func (e *LocalExecutor) buildEnvironment(env map[string]string) []string {
	var result []string
	
	baseEnv := map[string]string{
		"PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME": "/root",
		"USER": "root",
	}

	for key, value := range baseEnv {
		if _, exists := env[key]; !exists {
			result = append(result, key+"="+value)
		}
	}

	for key, value := range env {
		result = append(result, key+"="+value)
	}

	return result
}

func (e *LocalExecutor) parseUser(user string) (uint32, uint32, error) {
	parts := strings.Split(user, ":")
	
	uid, err := strconv.ParseUint(parts[0], 10, 32)
	if err != nil {
		return 1000, 1000, nil
	}
	
	gid := uid
	if len(parts) > 1 {
		if parsed, err := strconv.ParseUint(parts[1], 10, 32); err == nil {
			gid = parsed
		}
	}
	
	return uint32(uid), uint32(gid), nil
}