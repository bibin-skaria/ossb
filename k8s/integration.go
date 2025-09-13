package k8s

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"

	"github.com/bibin-skaria/ossb/internal/types"
)

// KubernetesIntegration provides Kubernetes-specific functionality for OSSB
type KubernetesIntegration struct {
	logger          *logrus.Logger
	secretsPath     string
	configMapPath   string
	workspacePath   string
	jobName         string
	podName         string
	namespace       string
}

// JobStatus represents the status of a Kubernetes job
type JobStatus string

const (
	JobStatusRunning   JobStatus = "Running"
	JobStatusSucceeded JobStatus = "Succeeded"
	JobStatusFailed    JobStatus = "Failed"
)

// Credentials holds registry authentication information
type Credentials struct {
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	Token    string `json:"token,omitempty"`
	AuthFile string `json:"auth_file,omitempty"`
}

// BuildSecret represents a build-time secret
type BuildSecret struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	File  string `json:"file,omitempty"`
}

// ProgressReport represents build progress information
type ProgressReport struct {
	Stage       string    `json:"stage"`
	Progress    float64   `json:"progress"`
	Message     string    `json:"message,omitempty"`
	Timestamp   time.Time `json:"timestamp"`
	Platform    string    `json:"platform,omitempty"`
	Operation   string    `json:"operation,omitempty"`
	CacheHit    bool      `json:"cache_hit,omitempty"`
}

// JobStatusReport represents job status information
type JobStatusReport struct {
	Status      JobStatus `json:"status"`
	Message     string    `json:"message,omitempty"`
	Timestamp   time.Time `json:"timestamp"`
	Progress    float64   `json:"progress,omitempty"`
	Error       string    `json:"error,omitempty"`
	BuildResult *types.BuildResult `json:"build_result,omitempty"`
}

// NewKubernetesIntegration creates a new Kubernetes integration instance
func NewKubernetesIntegration() *KubernetesIntegration {
	logger := logrus.New()
	logger.SetFormatter(&logrus.JSONFormatter{
		TimestampFormat: time.RFC3339,
		FieldMap: logrus.FieldMap{
			logrus.FieldKeyTime:  "timestamp",
			logrus.FieldKeyLevel: "level",
			logrus.FieldKeyMsg:   "message",
		},
	})

	return &KubernetesIntegration{
		logger:        logger,
		secretsPath:   "/var/run/secrets",
		configMapPath: "/var/run/configmaps",
		workspacePath: "/workspace",
		jobName:       os.Getenv("JOB_NAME"),
		podName:       os.Getenv("POD_NAME"),
		namespace:     os.Getenv("POD_NAMESPACE"),
	}
}

// IsRunningInKubernetes checks if OSSB is running inside a Kubernetes pod
func (k *KubernetesIntegration) IsRunningInKubernetes() bool {
	// Check for Kubernetes service account token
	if _, err := os.Stat("/var/run/secrets/kubernetes.io/serviceaccount/token"); err == nil {
		return true
	}
	
	// Check for Kubernetes environment variables
	if k.podName != "" || k.namespace != "" {
		return true
	}
	
	return false
}

// LoadRegistryCredentials loads registry credentials from Kubernetes secrets
func (k *KubernetesIntegration) LoadRegistryCredentials() (*types.RegistryConfig, error) {
	k.logger.Info("Loading registry credentials from Kubernetes secrets")
	
	registryConfig := &types.RegistryConfig{
		Registries: make(map[string]types.RegistryAuth),
		Insecure:   []string{},
		Mirrors:    make(map[string][]string),
	}

	// Load Docker registry secret (standard format)
	dockerConfigPath := filepath.Join(k.secretsPath, "registry-secret", ".dockerconfigjson")
	if data, err := ioutil.ReadFile(dockerConfigPath); err == nil {
		if err := k.parseDockerConfig(data, registryConfig); err != nil {
			k.logger.WithError(err).Warn("Failed to parse Docker config from secret")
		} else {
			k.logger.Info("Successfully loaded Docker registry credentials")
		}
	}

	// Load individual registry secrets
	secretDirs := []string{"registry-auth", "docker-registry", "registry-credentials"}
	for _, secretDir := range secretDirs {
		secretPath := filepath.Join(k.secretsPath, secretDir)
		if err := k.loadRegistrySecretDir(secretPath, registryConfig); err != nil {
			k.logger.WithError(err).WithField("secret_dir", secretDir).Debug("Failed to load registry secret directory")
		}
	}

	// Load registry configuration from ConfigMap
	configPath := filepath.Join(k.configMapPath, "registry-config", "config.yaml")
	if data, err := ioutil.ReadFile(configPath); err == nil {
		if err := k.parseRegistryConfig(data, registryConfig); err != nil {
			k.logger.WithError(err).Warn("Failed to parse registry config from ConfigMap")
		} else {
			k.logger.Info("Successfully loaded registry configuration")
		}
	}

	return registryConfig, nil
}

// LoadBuildSecrets loads build secrets from Kubernetes secrets
func (k *KubernetesIntegration) LoadBuildSecrets() (map[string]string, error) {
	k.logger.Info("Loading build secrets from Kubernetes secrets")
	
	secrets := make(map[string]string)

	// Load build secrets from mounted secret volumes
	secretDirs := []string{"build-secrets", "ossb-secrets"}
	for _, secretDir := range secretDirs {
		secretPath := filepath.Join(k.secretsPath, secretDir)
		if err := k.loadBuildSecretDir(secretPath, secrets); err != nil {
			k.logger.WithError(err).WithField("secret_dir", secretDir).Debug("Failed to load build secret directory")
		}
	}

	k.logger.WithField("secret_count", len(secrets)).Info("Loaded build secrets")
	return secrets, nil
}

// MountBuildContext sets up the build context from ConfigMap or volume
func (k *KubernetesIntegration) MountBuildContext(contextPath string) error {
	k.logger.WithField("context_path", contextPath).Info("Setting up build context")

	// Check if context is already mounted
	if _, err := os.Stat(contextPath); err == nil {
		k.logger.Info("Build context already available")
		return nil
	}

	// Try to find build context in ConfigMap
	configMapContextPath := filepath.Join(k.configMapPath, "build-context")
	if _, err := os.Stat(configMapContextPath); err == nil {
		k.logger.Info("Found build context in ConfigMap, creating symlink")
		return os.Symlink(configMapContextPath, contextPath)
	}

	// Try to find build context in mounted volume
	volumeContextPath := "/var/run/build-context"
	if _, err := os.Stat(volumeContextPath); err == nil {
		k.logger.Info("Found build context in volume, creating symlink")
		return os.Symlink(volumeContextPath, contextPath)
	}

	return fmt.Errorf("build context not found in ConfigMap or volume")
}

// SetupWorkspace creates and configures the workspace directory
func (k *KubernetesIntegration) SetupWorkspace(workspaceSize string) error {
	k.logger.WithFields(logrus.Fields{
		"workspace_path": k.workspacePath,
		"workspace_size": workspaceSize,
	}).Info("Setting up workspace")

	// Create workspace directory if it doesn't exist
	if err := os.MkdirAll(k.workspacePath, 0755); err != nil {
		return fmt.Errorf("failed to create workspace directory: %v", err)
	}

	// Set up temporary directories
	tempDirs := []string{"tmp", "cache", "layers", "manifests"}
	for _, dir := range tempDirs {
		dirPath := filepath.Join(k.workspacePath, dir)
		if err := os.MkdirAll(dirPath, 0755); err != nil {
			return fmt.Errorf("failed to create workspace subdirectory %s: %v", dir, err)
		}
	}

	k.logger.Info("Workspace setup completed")
	return nil
}

// ReportProgress reports build progress for Kubernetes monitoring
func (k *KubernetesIntegration) ReportProgress(stage string, progress float64, message string, platform string, operation string, cacheHit bool) error {
	report := ProgressReport{
		Stage:     stage,
		Progress:  progress,
		Message:   message,
		Timestamp: time.Now(),
		Platform:  platform,
		Operation: operation,
		CacheHit:  cacheHit,
	}

	k.logger.WithFields(logrus.Fields{
		"component": "progress",
		"stage":     stage,
		"progress":  progress,
		"platform":  platform,
		"operation": operation,
		"cache_hit": cacheHit,
	}).Info(message)

	// Write progress to file for external monitoring
	progressFile := "/tmp/ossb-progress.json"
	if data, err := json.Marshal(report); err == nil {
		ioutil.WriteFile(progressFile, data, 0644)
	}

	return nil
}

// SetJobStatus sets the job status for Kubernetes monitoring
func (k *KubernetesIntegration) SetJobStatus(status JobStatus, message string, buildResult *types.BuildResult) error {
	report := JobStatusReport{
		Status:      status,
		Message:     message,
		Timestamp:   time.Now(),
		BuildResult: buildResult,
	}

	if buildResult != nil {
		// Calculate overall progress based on build result
		if buildResult.Success {
			report.Progress = 100.0
		} else {
			report.Progress = 0.0
			report.Error = buildResult.Error
		}
	}

	k.logger.WithFields(logrus.Fields{
		"component":    "job_status",
		"status":       status,
		"job_name":     k.jobName,
		"pod_name":     k.podName,
		"namespace":    k.namespace,
		"progress":     report.Progress,
	}).Info(message)

	// Write status to file for external monitoring
	statusFile := "/tmp/ossb-status.json"
	if data, err := json.Marshal(report); err == nil {
		ioutil.WriteFile(statusFile, data, 0644)
	}

	// Set appropriate exit code for Kubernetes job
	if status == JobStatusFailed {
		k.logger.Error("Job failed, will exit with code 1")
	} else if status == JobStatusSucceeded {
		k.logger.Info("Job succeeded, will exit with code 0")
	}

	return nil
}

// GetJobInfo returns information about the current Kubernetes job
func (k *KubernetesIntegration) GetJobInfo() map[string]string {
	info := make(map[string]string)
	
	if k.jobName != "" {
		info["job_name"] = k.jobName
	}
	if k.podName != "" {
		info["pod_name"] = k.podName
	}
	if k.namespace != "" {
		info["namespace"] = k.namespace
	}
	
	// Add node information if available
	if nodeName := os.Getenv("NODE_NAME"); nodeName != "" {
		info["node_name"] = nodeName
	}
	
	return info
}

// parseDockerConfig parses Docker config JSON format
func (k *KubernetesIntegration) parseDockerConfig(data []byte, registryConfig *types.RegistryConfig) error {
	var dockerConfig struct {
		Auths map[string]struct {
			Username string `json:"username"`
			Password string `json:"password"`
			Auth     string `json:"auth"`
		} `json:"auths"`
	}

	if err := json.Unmarshal(data, &dockerConfig); err != nil {
		return fmt.Errorf("failed to parse Docker config: %v", err)
	}

	for registry, auth := range dockerConfig.Auths {
		registryConfig.Registries[registry] = types.RegistryAuth{
			Username: auth.Username,
			Password: auth.Password,
		}
	}

	return nil
}

// loadRegistrySecretDir loads registry secrets from a directory
func (k *KubernetesIntegration) loadRegistrySecretDir(secretPath string, registryConfig *types.RegistryConfig) error {
	if _, err := os.Stat(secretPath); os.IsNotExist(err) {
		return nil
	}

	files, err := ioutil.ReadDir(secretPath)
	if err != nil {
		return err
	}

	auth := types.RegistryAuth{}
	registry := ""

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		filePath := filepath.Join(secretPath, file.Name())
		data, err := ioutil.ReadFile(filePath)
		if err != nil {
			continue
		}

		value := strings.TrimSpace(string(data))
		switch file.Name() {
		case "registry":
			registry = value
		case "username":
			auth.Username = value
		case "password":
			auth.Password = value
		case "token":
			auth.Token = value
		case "auth_file":
			auth.AuthFile = value
		}
	}

	if registry != "" && (auth.Username != "" || auth.Token != "") {
		registryConfig.Registries[registry] = auth
	}

	return nil
}

// parseRegistryConfig parses registry configuration YAML
func (k *KubernetesIntegration) parseRegistryConfig(data []byte, registryConfig *types.RegistryConfig) error {
	var config struct {
		DefaultRegistry string                    `yaml:"default_registry"`
		Registries      map[string]types.RegistryAuth `yaml:"registries"`
		Insecure        []string                  `yaml:"insecure"`
		Mirrors         map[string][]string       `yaml:"mirrors"`
	}

	if err := yaml.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse registry config: %v", err)
	}

	if config.DefaultRegistry != "" {
		registryConfig.DefaultRegistry = config.DefaultRegistry
	}

	for registry, auth := range config.Registries {
		registryConfig.Registries[registry] = auth
	}

	if len(config.Insecure) > 0 {
		registryConfig.Insecure = append(registryConfig.Insecure, config.Insecure...)
	}

	for registry, mirrors := range config.Mirrors {
		registryConfig.Mirrors[registry] = mirrors
	}

	return nil
}

// loadBuildSecretDir loads build secrets from a directory
func (k *KubernetesIntegration) loadBuildSecretDir(secretPath string, secrets map[string]string) error {
	if _, err := os.Stat(secretPath); os.IsNotExist(err) {
		return nil
	}

	files, err := ioutil.ReadDir(secretPath)
	if err != nil {
		return err
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		filePath := filepath.Join(secretPath, file.Name())
		data, err := ioutil.ReadFile(filePath)
		if err != nil {
			k.logger.WithError(err).WithField("file", filePath).Warn("Failed to read secret file")
			continue
		}

		secrets[file.Name()] = strings.TrimSpace(string(data))
	}

	return nil
}