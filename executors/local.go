package executors

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/bibin-skaria/ossb/internal/types"
	"github.com/bibin-skaria/ossb/registry"
)

type LocalExecutor struct {
	registryClient *registry.Client
	workspaceDir   string
	layerCounter   int
}

func init() {
	RegisterExecutor("local", NewLocalExecutor())
}

// NewLocalExecutor creates a new LocalExecutor with default registry client
func NewLocalExecutor() *LocalExecutor {
	return &LocalExecutor{
		registryClient: registry.NewClient(registry.DefaultClientOptions()),
		layerCounter:   0,
	}
}

// NewLocalExecutorWithClient creates a new LocalExecutor with a custom registry client
func NewLocalExecutorWithClient(client *registry.Client) *LocalExecutor {
	return &LocalExecutor{
		registryClient: client,
		layerCounter:   0,
	}
}

// SetWorkspaceDir sets the workspace directory for the executor
func (e *LocalExecutor) SetWorkspaceDir(dir string) {
	e.workspaceDir = dir
}

func (e *LocalExecutor) Execute(operation *types.Operation, workDir string) (*types.OperationResult, error) {
	result := &types.OperationResult{
		Operation: operation,
		Success:   false,
	}

	// Set workspace directory if not already set
	if e.workspaceDir == "" {
		e.workspaceDir = workDir
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
	case types.OperationTypePull:
		return e.executePull(operation, workDir, result)
	case types.OperationTypeExtract:
		return e.executeExtract(operation, workDir, result)
	case types.OperationTypeLayer:
		return e.executeLayer(operation, workDir, result)
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

	// Check if this is a stage reference
	isStageReference := operation.Metadata["stage_reference"] == "true"
	currentStage := operation.Metadata["current_stage"]
	
	var baseDir string
	var env map[string]string
	
	if isStageReference {
		// Copy from another stage
		sourceStage := operation.Metadata["source_stage"]
		if sourceStage == "" {
			sourceStage = image
		}
		
		// Get the source stage filesystem
		sourceStageDir, err := e.getStageFilesystem(sourceStage, workDir)
		if err != nil {
			result.Error = fmt.Sprintf("failed to get source stage filesystem: %v", err)
			return result, nil
		}
		
		// Create filesystem for current stage
		if currentStage != "" {
			stageDir, err := e.createStageFilesystem(currentStage, workDir)
			if err != nil {
				result.Error = fmt.Sprintf("failed to create stage filesystem: %v", err)
				return result, nil
			}
			baseDir = stageDir
		} else {
			baseDir = filepath.Join(workDir, "base")
		}
		
		// Copy the source stage filesystem to the current stage
		if err := e.copyDirectory(sourceStageDir, baseDir); err != nil {
			result.Error = fmt.Sprintf("failed to copy stage filesystem: %v", err)
			return result, nil
		}
		
		// Use default environment for stage references
		env = map[string]string{
			"PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		}
	} else {
		// Handle scratch image - create empty filesystem
		if image == "scratch" {
			if currentStage != "" {
				stageDir, err := e.createStageFilesystem(currentStage, workDir)
				if err != nil {
					result.Error = fmt.Sprintf("failed to create stage filesystem: %v", err)
					return result, nil
				}
				baseDir = stageDir
			} else {
				baseDir = filepath.Join(workDir, "base")
			}
			
			if err := os.MkdirAll(baseDir, 0755); err != nil {
				result.Error = fmt.Sprintf("failed to create base directory: %v", err)
				return result, nil
			}

			env = map[string]string{
				"PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
			}
		} else {
			// Parse image reference
			imageRef, err := registry.ParseImageReference(image)
			if err != nil {
				result.Error = fmt.Sprintf("failed to parse image reference: %v", err)
				return result, nil
			}

			// Determine base directory
			if currentStage != "" {
				stageDir, err := e.createStageFilesystem(currentStage, workDir)
				if err != nil {
					result.Error = fmt.Sprintf("failed to create stage filesystem: %v", err)
					return result, nil
				}
				baseDir = stageDir
			} else {
				baseDir = filepath.Join(workDir, "base")
			}

			// Pull and extract base image
			env, err = e.pullAndExtractBaseImage(imageRef, baseDir, operation.Platform)
			if err != nil {
				result.Error = fmt.Sprintf("failed to pull and extract base image: %v", err)
				return result, nil
			}
		}
	}

	result.Success = true
	result.Outputs = operation.Outputs
	result.Environment = env
	
	return result, nil
}

func (e *LocalExecutor) executeExec(operation *types.Operation, workDir string, result *types.OperationResult) (*types.OperationResult, error) {
	if len(operation.Command) == 0 {
		result.Error = "exec operation missing command"
		return result, nil
	}

	// Create layer directory for this execution
	e.layerCounter++
	layerDir := filepath.Join(workDir, "layers", fmt.Sprintf("layer-%d", e.layerCounter))
	
	// Setup filesystem for execution with overlay
	rootfsDir, err := e.setupExecutionFilesystem(workDir, layerDir)
	if err != nil {
		result.Error = fmt.Sprintf("failed to setup execution filesystem: %v", err)
		return result, nil
	}
	defer e.cleanupExecutionFilesystem(rootfsDir)

	// Execute command in chroot environment
	if err := e.executeInChroot(operation, rootfsDir, layerDir); err != nil {
		result.Error = fmt.Sprintf("command execution failed: %v", err)
		return result, nil
	}

	// Create layer from filesystem changes
	layerPath, err := e.createLayerFromChanges(layerDir)
	if err != nil {
		result.Error = fmt.Sprintf("failed to create layer: %v", err)
		return result, nil
	}

	result.Success = true
	result.Outputs = []string{layerPath}
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

	// Check if this is a multi-stage copy operation
	fromStage := operation.Metadata["from_stage"]
	var sources []string
	
	if fromStage != "" {
		// Copy from another stage - resolve stage filesystem paths
		stageSources, err := e.resolveStageFilePaths(operation.Inputs[1:], fromStage, workDir)
		if err != nil {
			result.Error = fmt.Sprintf("failed to resolve stage file paths: %v", err)
			return result, nil
		}
		sources = stageSources
	} else {
		// Regular copy from build context
		sources = operation.Inputs[1:]
	}
	
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

// pullAndExtractBaseImage pulls a base image and extracts it to the target directory
func (e *LocalExecutor) pullAndExtractBaseImage(imageRef registry.ImageReference, targetDir string, platform types.Platform) (map[string]string, error) {
	ctx := context.Background()
	
	// Pull the image manifest
	manifest, err := e.registryClient.PullImage(ctx, imageRef, platform)
	if err != nil {
		return nil, fmt.Errorf("failed to pull image: %v", err)
	}

	// Extract image to directory
	if err := e.registryClient.ExtractImageToDirectory(ctx, manifest, targetDir); err != nil {
		return nil, fmt.Errorf("failed to extract image: %v", err)
	}

	// Load image configuration to get environment variables
	configPath := filepath.Join(targetDir, "config.json")
	env, err := e.loadImageEnvironment(configPath)
	if err != nil {
		// Use default environment if config loading fails
		env = map[string]string{
			"PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		}
	}

	return env, nil
}

// loadImageEnvironment loads environment variables from image config
func (e *LocalExecutor) loadImageEnvironment(configPath string) (map[string]string, error) {
	configFile, err := os.Open(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open config file: %v", err)
	}
	defer configFile.Close()

	var config struct {
		Config struct {
			Env []string `json:"Env"`
		} `json:"config"`
	}

	if err := json.NewDecoder(configFile).Decode(&config); err != nil {
		return nil, fmt.Errorf("failed to decode config: %v", err)
	}

	env := make(map[string]string)
	
	// Set default environment
	env["PATH"] = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
	env["HOME"] = "/root"
	env["USER"] = "root"

	// Parse environment variables from config
	for _, envVar := range config.Config.Env {
		parts := strings.SplitN(envVar, "=", 2)
		if len(parts) == 2 {
			env[parts[0]] = parts[1]
		}
	}

	return env, nil
}

// setupExecutionFilesystem creates an overlay filesystem for command execution
func (e *LocalExecutor) setupExecutionFilesystem(workDir, layerDir string) (string, error) {
	baseDir := filepath.Join(workDir, "base")
	rootfsDir := filepath.Join(workDir, "rootfs")
	upperDir := layerDir
	workDirOverlay := filepath.Join(workDir, "work")

	// Create necessary directories
	for _, dir := range []string{rootfsDir, upperDir, workDirOverlay} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return "", fmt.Errorf("failed to create directory %s: %v", dir, err)
		}
	}

	// For simplicity, we'll use a copy-on-write approach instead of overlay
	// This is more portable and doesn't require root privileges
	
	// The base directory contains extracted layers, we need to merge them
	if err := e.mergeBaseLayers(baseDir, rootfsDir); err != nil {
		return "", fmt.Errorf("failed to merge base filesystem: %v", err)
	}

	return rootfsDir, nil
}

// mergeBaseLayers merges all layers from the base directory into a single rootfs
func (e *LocalExecutor) mergeBaseLayers(baseDir, rootfsDir string) error {
	// Check if base directory exists
	if _, err := os.Stat(baseDir); os.IsNotExist(err) {
		// No base directory, create empty rootfs
		return os.MkdirAll(rootfsDir, 0755)
	}

	// Read all layer directories
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return fmt.Errorf("failed to read base directory: %v", err)
	}

	// Sort layer directories by name to ensure correct order
	layerDirs := []string{}
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "layer-") {
			layerDirs = append(layerDirs, entry.Name())
		}
	}

	// If no layers found, check if there are files directly in base
	if len(layerDirs) == 0 {
		if err := e.copyDirectory(baseDir, rootfsDir); err != nil {
			return err
		}
		return e.makeDirectoryWritable(rootfsDir)
	}

	// Merge layers in order
	for _, layerName := range layerDirs {
		layerPath := filepath.Join(baseDir, layerName)
		if err := e.copyDirectory(layerPath, rootfsDir); err != nil {
			return fmt.Errorf("failed to copy layer %s: %v", layerName, err)
		}
	}

	// Make the rootfs writable
	return e.makeDirectoryWritable(rootfsDir)
}

// cleanupExecutionFilesystem cleans up the execution filesystem
func (e *LocalExecutor) cleanupExecutionFilesystem(rootfsDir string) {
	// For now, we keep the rootfs for debugging
	// In production, we might want to clean this up
	_ = rootfsDir
}

// executeInChroot executes a command in a chroot environment
func (e *LocalExecutor) executeInChroot(operation *types.Operation, rootfsDir, layerDir string) error {
	cmdWorkDir := operation.WorkDir
	if cmdWorkDir == "" {
		cmdWorkDir = "/"
	}

	// Ensure working directory exists in rootfs
	fullWorkDir := filepath.Join(rootfsDir, strings.TrimPrefix(cmdWorkDir, "/"))
	if err := os.MkdirAll(fullWorkDir, 0755); err != nil {
		return fmt.Errorf("failed to create working directory: %v", err)
	}

	// For non-root execution, we'll simulate the command execution in the rootfs
	var cmd *exec.Cmd
	if len(operation.Command) == 1 {
		// Use sh -c for single commands to handle shell features
		if e.canUseChroot() {
			cmd = exec.Command("chroot", rootfsDir, "sh", "-c", operation.Command[0])
		} else {
			// Fallback: simulate the command by creating the directory structure
			// and executing the command with modified paths
			cmd = exec.Command("sh", "-c", e.translateCommand(operation.Command[0], rootfsDir))
			cmd.Dir = fullWorkDir
		}
	} else {
		// Direct command execution
		if e.canUseChroot() {
			args := append([]string{rootfsDir}, operation.Command...)
			cmd = exec.Command("chroot", args...)
		} else {
			// Fallback: translate the command for non-chroot execution
			translatedCmd := e.translateCommandArgs(operation.Command, rootfsDir)
			if len(translatedCmd) > 0 {
				cmd = exec.Command(translatedCmd[0], translatedCmd[1:]...)
				cmd.Dir = fullWorkDir
			} else {
				return fmt.Errorf("failed to translate command: %v", operation.Command)
			}
		}
	}

	cmd.Env = e.buildEnvironment(operation.Environment)

	// Set user if specified and we have privileges
	if operation.User != "" && operation.User != "root" && e.canUseChroot() {
		uid, gid, err := e.parseUser(operation.User)
		if err != nil {
			return fmt.Errorf("failed to parse user: %v", err)
		}
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Credential: &syscall.Credential{
				Uid: uid,
				Gid: gid,
			},
		}
	}

	// Execute command
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("command failed: %v, output: %s", err, string(output))
	}

	// Copy changes from rootfs to layer directory
	return e.captureFilesystemChanges(rootfsDir, layerDir)
}

// canUseChroot checks if we can use chroot (requires root privileges)
func (e *LocalExecutor) canUseChroot() bool {
	// Simple check: try to run chroot --help
	cmd := exec.Command("chroot", "--help")
	err := cmd.Run()
	return err == nil && os.Geteuid() == 0
}

// captureFilesystemChanges captures changes made to the filesystem
func (e *LocalExecutor) captureFilesystemChanges(rootfsDir, layerDir string) error {
	// For now, we'll use a simple approach: copy everything from rootfs to layer
	// In a more sophisticated implementation, we would track only the changes
	
	// Ensure layer directory exists
	if err := os.MkdirAll(layerDir, 0755); err != nil {
		return fmt.Errorf("failed to create layer directory: %v", err)
	}
	
	return e.copyDirectory(rootfsDir, layerDir)
}

// createLayerFromChanges creates a tar layer from filesystem changes
func (e *LocalExecutor) createLayerFromChanges(layerDir string) (string, error) {
	layerPath := layerDir + ".tar.gz"
	
	// Create tar.gz file
	tarFile, err := os.Create(layerPath)
	if err != nil {
		return "", fmt.Errorf("failed to create tar file: %v", err)
	}
	defer tarFile.Close()

	gzipWriter := gzip.NewWriter(tarFile)
	defer gzipWriter.Close()

	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	// Walk through layer directory and add files to tar
	err = filepath.Walk(layerDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip the layer directory itself
		if path == layerDir {
			return nil
		}

		// Get relative path
		relPath, err := filepath.Rel(layerDir, path)
		if err != nil {
			return err
		}

		// Create tar header
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = relPath

		// Write header
		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}

		// Write file content if it's a regular file
		if info.Mode().IsRegular() {
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			defer file.Close()

			if _, err := io.Copy(tarWriter, file); err != nil {
				return err
			}
		}

		return nil
	})

	if err != nil {
		return "", fmt.Errorf("failed to create tar: %v", err)
	}

	return layerPath, nil
}

// copyDirectory recursively copies a directory
func (e *LocalExecutor) copyDirectory(src, dst string) error {
	// Check if source exists
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return nil // Skip if source doesn't exist
	}

	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// Skip files that can't be accessed
			return nil
		}

		// Get relative path
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		dstPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}

		// Handle different file types
		switch info.Mode() & os.ModeType {
		case os.ModeSymlink:
			// Handle symbolic links
			linkTarget, err := os.Readlink(path)
			if err != nil {
				return nil // Skip broken symlinks
			}
			// Create parent directory
			if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
				return err
			}
			// Remove existing file/link if it exists
			os.Remove(dstPath)
			return os.Symlink(linkTarget, dstPath)
		case 0: // Regular file
			return e.copyFile(path, dstPath)
		default:
			// Skip special files (devices, pipes, etc.)
			return nil
		}
	})
}

// executePull handles pull operations for base images
func (e *LocalExecutor) executePull(operation *types.Operation, workDir string, result *types.OperationResult) (*types.OperationResult, error) {
	image := operation.Metadata["image"]
	if image == "" {
		result.Error = "pull operation missing image metadata"
		return result, nil
	}

	imageRef, err := registry.ParseImageReference(image)
	if err != nil {
		result.Error = fmt.Sprintf("failed to parse image reference: %v", err)
		return result, nil
	}

	ctx := context.Background()
	manifest, err := e.registryClient.PullImage(ctx, imageRef, operation.Platform)
	if err != nil {
		result.Error = fmt.Sprintf("failed to pull image: %v", err)
		return result, nil
	}

	// Store manifest information in metadata
	manifestPath := filepath.Join(workDir, "manifest.json")
	if err := e.saveManifest(manifest, manifestPath); err != nil {
		result.Error = fmt.Sprintf("failed to save manifest: %v", err)
		return result, nil
	}

	result.Success = true
	result.Outputs = []string{manifestPath}
	return result, nil
}

// executeExtract handles extraction of pulled images
func (e *LocalExecutor) executeExtract(operation *types.Operation, workDir string, result *types.OperationResult) (*types.OperationResult, error) {
	manifestPath := operation.Metadata["manifest"]
	if manifestPath == "" {
		result.Error = "extract operation missing manifest metadata"
		return result, nil
	}

	targetDir := operation.Metadata["target"]
	if targetDir == "" {
		targetDir = filepath.Join(workDir, "extracted")
	}

	// Load manifest
	manifest, err := e.loadManifest(manifestPath)
	if err != nil {
		result.Error = fmt.Sprintf("failed to load manifest: %v", err)
		return result, nil
	}

	// Extract image
	ctx := context.Background()
	if err := e.registryClient.ExtractImageToDirectory(ctx, manifest, targetDir); err != nil {
		result.Error = fmt.Sprintf("failed to extract image: %v", err)
		return result, nil
	}

	result.Success = true
	result.Outputs = []string{targetDir}
	return result, nil
}

// executeLayer handles layer creation operations
func (e *LocalExecutor) executeLayer(operation *types.Operation, workDir string, result *types.OperationResult) (*types.OperationResult, error) {
	sourceDir := operation.Metadata["source"]
	if sourceDir == "" {
		result.Error = "layer operation missing source metadata"
		return result, nil
	}

	layerPath, err := e.createLayerFromChanges(sourceDir)
	if err != nil {
		result.Error = fmt.Sprintf("failed to create layer: %v", err)
		return result, nil
	}

	// Calculate layer digest
	digest, err := e.calculateLayerDigest(layerPath)
	if err != nil {
		result.Error = fmt.Sprintf("failed to calculate layer digest: %v", err)
		return result, nil
	}

	result.Success = true
	result.Outputs = []string{layerPath}
	result.Environment = map[string]string{
		"layer_digest": digest,
	}
	return result, nil
}

// manifestCache stores pulled manifests with their image data
var manifestCache = make(map[string]*registry.ImageManifest)

// saveManifest saves an image manifest to a file and caches it
func (e *LocalExecutor) saveManifest(manifest *registry.ImageManifest, path string) error {
	// Cache the manifest with image data
	manifestCache[path] = manifest
	
	// Save just the manifest structure to file (without image data)
	manifestData := &registry.ImageManifest{
		SchemaVersion: manifest.SchemaVersion,
		MediaType:     manifest.MediaType,
		Config:        manifest.Config,
		Layers:        manifest.Layers,
		Annotations:   manifest.Annotations,
	}
	
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(manifestData)
}

// loadManifest loads an image manifest from cache or file
func (e *LocalExecutor) loadManifest(path string) (*registry.ImageManifest, error) {
	// Try to get from cache first
	if cached, exists := manifestCache[path]; exists {
		return cached, nil
	}
	
	// Fallback to loading from file (without image data)
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var manifest registry.ImageManifest
	if err := json.NewDecoder(file).Decode(&manifest); err != nil {
		return nil, err
	}

	return &manifest, nil
}

// calculateLayerDigest calculates the SHA256 digest of a layer file
func (e *LocalExecutor) calculateLayerDigest(layerPath string) (string, error) {
	file, err := os.Open(layerPath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}

	return fmt.Sprintf("sha256:%x", hasher.Sum(nil)), nil
}

// makeDirectoryWritable recursively makes a directory and its contents writable
func (e *LocalExecutor) makeDirectoryWritable(dir string) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip files that can't be accessed
		}

		// Make directories writable
		if info.IsDir() {
			return os.Chmod(path, info.Mode()|0755)
		}

		// Make files writable if they're regular files
		if info.Mode().IsRegular() {
			return os.Chmod(path, info.Mode()|0644)
		}

		return nil
	})
}

// translateCommand translates a shell command to work without chroot
func (e *LocalExecutor) translateCommand(command, rootfsDir string) string {
	// Replace absolute paths with rootfs-relative paths
	result := command
	
	// Handle common patterns
	if strings.Contains(command, " > /") {
		// Handle output redirection to absolute paths
		parts := strings.Split(command, " > /")
		if len(parts) == 2 {
			outputPath := "/" + parts[1]
			rootfsPath := filepath.Join(rootfsDir, strings.TrimPrefix(outputPath, "/"))
			// Ensure parent directory exists
			parentDir := filepath.Dir(rootfsPath)
			os.MkdirAll(parentDir, 0755)
			result = parts[0] + " > " + rootfsPath
		}
	} else if strings.HasPrefix(command, "mkdir") {
		// Extract the directory path and make it relative to rootfs
		parts := strings.Fields(command)
		if len(parts) >= 2 {
			for i := 1; i < len(parts); i++ {
				if strings.HasPrefix(parts[i], "/") {
					// Convert absolute path to rootfs path
					parts[i] = filepath.Join(rootfsDir, strings.TrimPrefix(parts[i], "/"))
				}
			}
			result = strings.Join(parts, " ")
		}
	}
	
	return result
}

// translateCommandArgs translates command arguments to work without chroot
func (e *LocalExecutor) translateCommandArgs(args []string, rootfsDir string) []string {
	if len(args) == 0 {
		return args
	}

	result := make([]string, len(args))
	copy(result, args)

	// Handle mkdir command specially
	if args[0] == "mkdir" {
		for i := 1; i < len(result); i++ {
			if strings.HasPrefix(result[i], "/") {
				// Convert absolute path to rootfs path
				result[i] = filepath.Join(rootfsDir, strings.TrimPrefix(result[i], "/"))
			}
		}
	}

	return result
}

// resolveStageFilePaths resolves file paths from a previous build stage
func (e *LocalExecutor) resolveStageFilePaths(sources []string, fromStage, workDir string) ([]string, error) {
	var resolvedSources []string
	
	// Find the stage filesystem directory
	// In a multi-stage build, each stage would have its own filesystem
	// For now, we'll look for stage-specific directories in the work directory
	stageDir := filepath.Join(workDir, "stages", fromStage)
	
	// If stage directory doesn't exist, try to find it by looking for stage metadata
	if _, err := os.Stat(stageDir); os.IsNotExist(err) {
		// Look for any directory with stage metadata
		stagesDir := filepath.Join(workDir, "stages")
		if entries, err := os.ReadDir(stagesDir); err == nil {
			for _, entry := range entries {
				if entry.IsDir() {
					// Check if this directory contains the stage we're looking for
					metadataPath := filepath.Join(stagesDir, entry.Name(), ".stage_metadata")
					if data, err := os.ReadFile(metadataPath); err == nil {
						var metadata map[string]string
						if err := json.Unmarshal(data, &metadata); err == nil {
							if metadata["stage_name"] == fromStage {
								stageDir = filepath.Join(stagesDir, entry.Name())
								break
							}
						}
					}
				}
			}
		}
	}
	
	// If we still can't find the stage directory, return an error
	if _, err := os.Stat(stageDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("stage '%s' filesystem not found", fromStage)
	}
	
	// Resolve each source path relative to the stage filesystem
	for _, source := range sources {
		// Remove leading slash and join with stage directory
		cleanSource := strings.TrimPrefix(source, "/")
		stagePath := filepath.Join(stageDir, cleanSource)
		
		// Check if the path exists
		if _, err := os.Stat(stagePath); err != nil {
			return nil, fmt.Errorf("source path '%s' not found in stage '%s'", source, fromStage)
		}
		
		resolvedSources = append(resolvedSources, stagePath)
	}
	
	return resolvedSources, nil
}

// createStageFilesystem creates a filesystem for a specific stage
func (e *LocalExecutor) createStageFilesystem(stageName, workDir string) (string, error) {
	stageDir := filepath.Join(workDir, "stages", stageName)
	if err := os.MkdirAll(stageDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create stage directory: %v", err)
	}
	
	// Create stage metadata
	metadata := map[string]string{
		"stage_name": stageName,
		"created_at": fmt.Sprintf("%d", os.Getpid()), // Simple timestamp
	}
	
	metadataPath := filepath.Join(stageDir, ".stage_metadata")
	metadataData, err := json.Marshal(metadata)
	if err != nil {
		return "", fmt.Errorf("failed to marshal stage metadata: %v", err)
	}
	
	if err := os.WriteFile(metadataPath, metadataData, 0644); err != nil {
		return "", fmt.Errorf("failed to write stage metadata: %v", err)
	}
	
	return stageDir, nil
}

// getStageFilesystem returns the filesystem directory for a specific stage
func (e *LocalExecutor) getStageFilesystem(stageName, workDir string) (string, error) {
	stageDir := filepath.Join(workDir, "stages", stageName)
	
	// Check if stage directory exists
	if _, err := os.Stat(stageDir); os.IsNotExist(err) {
		return "", fmt.Errorf("stage '%s' filesystem not found", stageName)
	}
	
	return stageDir, nil
}