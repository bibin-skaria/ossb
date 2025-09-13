package security

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/bibin-skaria/ossb/internal/types"
)

// SecurityManager provides comprehensive security management for OSSB
type SecurityManager struct {
	inputValidator      *InputValidator
	secretManager       *SecretManager
	contextValidator    *SecurityContextValidator
	contextEnforcer     *SecurityContextEnforcer
	resourceValidator   *ResourceLimitValidator
	vulnerabilityScanner *VulnerabilityScanner
	scanResultAnalyzer  *ScanResultAnalyzer
	config              *SecurityConfig
}

// SecurityConfig holds security configuration
type SecurityConfig struct {
	// Input validation settings
	MaxDockerfileSize int64 `json:"max_dockerfile_size"`
	MaxBuildArgSize   int   `json:"max_build_arg_size"`
	MaxBuildArgCount  int   `json:"max_build_arg_count"`

	// Secret management settings
	SecretRotationInterval time.Duration `json:"secret_rotation_interval"`
	SecretOutputDir        string        `json:"secret_output_dir"`

	// Security context settings
	AllowPrivileged bool  `json:"allow_privileged"`
	MaxUID          int64 `json:"max_uid"`
	MaxGID          int64 `json:"max_gid"`

	// Resource limit settings
	MaxMemoryBytes int64   `json:"max_memory_bytes"`
	MaxCPUCores    float64 `json:"max_cpu_cores"`
	MaxDiskBytes   int64   `json:"max_disk_bytes"`

	// Vulnerability scanning settings
	EnableVulnerabilityScanning bool          `json:"enable_vulnerability_scanning"`
	ScanTimeout                 time.Duration `json:"scan_timeout"`
	FailOnCritical              bool          `json:"fail_on_critical"`
	FailOnHigh                  bool          `json:"fail_on_high"`
	ScanOutputDir               string        `json:"scan_output_dir"`

	// General security settings
	StrictMode bool `json:"strict_mode"`
}

// NewSecurityManager creates a new security manager with default configuration
func NewSecurityManager() *SecurityManager {
	config := &SecurityConfig{
		MaxDockerfileSize:           10 * 1024 * 1024, // 10MB
		MaxBuildArgSize:             1024,              // 1KB
		MaxBuildArgCount:            100,               // 100 args
		SecretRotationInterval:      24 * time.Hour,   // 24 hours
		SecretOutputDir:             "/tmp/ossb-secrets",
		AllowPrivileged:             false,
		MaxUID:                      65535,
		MaxGID:                      65535,
		MaxMemoryBytes:              8 * 1024 * 1024 * 1024, // 8GB
		MaxCPUCores:                 4.0,                     // 4 cores
		MaxDiskBytes:                50 * 1024 * 1024 * 1024, // 50GB
		EnableVulnerabilityScanning: true,
		ScanTimeout:                 10 * time.Minute,
		FailOnCritical:              true,
		FailOnHigh:                  false,
		ScanOutputDir:               "/tmp/ossb-scans",
		StrictMode:                  true,
	}

	return NewSecurityManagerWithConfig(config)
}

// NewSecurityManagerWithConfig creates a new security manager with custom configuration
func NewSecurityManagerWithConfig(config *SecurityConfig) *SecurityManager {
	sm := &SecurityManager{
		inputValidator:       NewInputValidator(),
		secretManager:        NewSecretManager(),
		contextValidator:     NewSecurityContextValidator(),
		contextEnforcer:      NewSecurityContextEnforcer(),
		resourceValidator:    NewResourceLimitValidator(),
		vulnerabilityScanner: NewVulnerabilityScanner(config.ScanOutputDir),
		scanResultAnalyzer:   NewScanResultAnalyzer(),
		config:               config,
	}

	// Configure components based on config
	sm.configureComponents()

	return sm
}

// configureComponents configures individual security components based on the security config
func (sm *SecurityManager) configureComponents() {
	// Configure input validator
	sm.inputValidator.maxDockerfileSize = sm.config.MaxDockerfileSize
	sm.inputValidator.maxBuildArgSize = sm.config.MaxBuildArgSize
	sm.inputValidator.maxBuildArgCount = sm.config.MaxBuildArgCount

	// Configure context validator
	sm.contextValidator.allowPrivileged = sm.config.AllowPrivileged
	sm.contextValidator.maxUID = sm.config.MaxUID
	sm.contextValidator.maxGID = sm.config.MaxGID

	// Configure resource validator
	sm.resourceValidator.maxMemoryBytes = sm.config.MaxMemoryBytes
	sm.resourceValidator.maxCPUCores = sm.config.MaxCPUCores
	sm.resourceValidator.maxDiskBytes = sm.config.MaxDiskBytes

	// Configure vulnerability scanner
	sm.vulnerabilityScanner.SetTimeout(sm.config.ScanTimeout)
	sm.vulnerabilityScanner.SetFailurePolicy(sm.config.FailOnCritical, sm.config.FailOnHigh)
}

// ValidateBuildConfiguration performs comprehensive validation of build configuration
func (sm *SecurityManager) ValidateBuildConfiguration(config *types.BuildConfig) error {
	if config == nil {
		return fmt.Errorf("build configuration is nil")
	}

	// Validate Dockerfile if present
	if config.Dockerfile != "" {
		dockerfilePath := filepath.Join(config.Context, config.Dockerfile)
		if err := sm.inputValidator.ValidateDockerfile(dockerfilePath); err != nil {
			return fmt.Errorf("dockerfile validation failed: %v", err)
		}
	}

	// Validate build arguments
	if err := sm.inputValidator.ValidateBuildArgs(config.BuildArgs); err != nil {
		return fmt.Errorf("build args validation failed: %v", err)
	}

	// Validate security context
	if err := sm.contextValidator.ValidateSecurityContext(config.SecurityContext); err != nil {
		return fmt.Errorf("security context validation failed: %v", err)
	}

	// Validate resource limits
	if err := sm.resourceValidator.ValidateResourceLimits(config.ResourceLimits); err != nil {
		return fmt.Errorf("resource limits validation failed: %v", err)
	}

	// Validate registry configuration
	if config.RegistryConfig != nil {
		if err := sm.validateRegistryConfig(config.RegistryConfig); err != nil {
			return fmt.Errorf("registry config validation failed: %v", err)
		}
	}

	return nil
}

// ValidateOperation validates an operation for security compliance
func (sm *SecurityManager) ValidateOperation(operation *types.Operation) error {
	return sm.inputValidator.ValidateOperation(operation)
}

// EnforceSecurityContext enforces security context settings
func (sm *SecurityManager) EnforceSecurityContext(ctx *types.SecurityContext) error {
	return sm.contextEnforcer.ValidateAndEnforce(ctx)
}

// LoadSecrets loads secrets from various sources
func (sm *SecurityManager) LoadSecrets(sources map[string]string) error {
	loader := NewSecretLoader(sm.secretManager)

	for sourceType, sourcePath := range sources {
		switch sourceType {
		case "environment":
			if err := loader.LoadFromEnvironment(sourcePath); err != nil {
				return fmt.Errorf("failed to load secrets from environment: %v", err)
			}
		case "file":
			// sourcePath should be in format "key:path"
			parts := strings.SplitN(sourcePath, ":", 2)
			if len(parts) != 2 {
				return fmt.Errorf("invalid file source format: %s", sourcePath)
			}
			if err := loader.LoadFromFile(parts[0], parts[1]); err != nil {
				return fmt.Errorf("failed to load secret from file: %v", err)
			}
		case "kubernetes":
			if err := loader.LoadFromKubernetesSecret(sourcePath); err != nil {
				return fmt.Errorf("failed to load secrets from kubernetes: %v", err)
			}
		default:
			return fmt.Errorf("unknown secret source type: %s", sourceType)
		}
	}

	return nil
}

// InjectSecrets injects secrets into environment variables
func (sm *SecurityManager) InjectSecrets(env map[string]string) (map[string]string, error) {
	injector := NewSecretInjector(sm.secretManager)
	return injector.InjectSecretsIntoEnvironment(env)
}

// ScanImage performs vulnerability scanning on a container image
func (sm *SecurityManager) ScanImage(imageRef string) (*ScanResult, error) {
	if !sm.config.EnableVulnerabilityScanning {
		return nil, nil // Scanning disabled
	}

	result, err := sm.vulnerabilityScanner.ScanImage(imageRef)
	if err != nil {
		return nil, fmt.Errorf("vulnerability scan failed: %v", err)
	}

	return result, nil
}

// ScanFilesystem performs vulnerability scanning on a filesystem
func (sm *SecurityManager) ScanFilesystem(path string) (*ScanResult, error) {
	if !sm.config.EnableVulnerabilityScanning {
		return nil, nil // Scanning disabled
	}

	result, err := sm.vulnerabilityScanner.ScanFilesystem(path)
	if err != nil {
		return nil, fmt.Errorf("vulnerability scan failed: %v", err)
	}

	return result, nil
}

// AnalyzeScanResults analyzes vulnerability scan results
func (sm *SecurityManager) AnalyzeScanResults(result *ScanResult) *ScanAnalysis {
	return sm.scanResultAnalyzer.AnalyzeResults(result)
}

// RotateSecrets rotates old secrets
func (sm *SecurityManager) RotateSecrets() {
	sm.secretManager.RotateSecrets(sm.config.SecretRotationInterval)
}

// GetSecurityStatus returns the current security status
func (sm *SecurityManager) GetSecurityStatus() *SecurityStatus {
	status := &SecurityStatus{
		Timestamp:           time.Now(),
		SecurityContextInfo: sm.contextEnforcer.GetProcessSecurityInfo(),
		SecretStats:         sm.secretManager.GetSecretStats(),
		Config:              sm.config,
	}

	// Check current security context
	if err := sm.contextEnforcer.CheckCurrentSecurityContext(); err != nil {
		status.SecurityIssues = append(status.SecurityIssues, fmt.Sprintf("Security context issue: %v", err))
	}

	// Check for available vulnerability scanners
	if sm.config.EnableVulnerabilityScanning {
		availableScanners := 0
		for _, scanner := range sm.vulnerabilityScanner.scanners {
			if scanner.IsAvailable() {
				availableScanners++
			}
		}
		if availableScanners == 0 {
			status.SecurityIssues = append(status.SecurityIssues, "No vulnerability scanners available")
		}
	}

	return status
}

// Cleanup performs security cleanup operations
func (sm *SecurityManager) Cleanup() {
	sm.secretManager.Cleanup()
}

// SecurityStatus represents the current security status
type SecurityStatus struct {
	Timestamp           time.Time              `json:"timestamp"`
	SecurityContextInfo map[string]interface{} `json:"security_context_info"`
	SecretStats         map[string]interface{} `json:"secret_stats"`
	SecurityIssues      []string               `json:"security_issues"`
	Config              *SecurityConfig        `json:"config"`
}

// validateRegistryConfig validates registry configuration for security
func (sm *SecurityManager) validateRegistryConfig(config *types.RegistryConfig) error {
	// Validate default registry
	if config.DefaultRegistry != "" {
		if err := sm.inputValidator.validateImageReference(config.DefaultRegistry + "/test:latest"); err != nil {
			return fmt.Errorf("invalid default registry: %v", err)
		}
	}

	// Validate registry auth configurations
	for registry, auth := range config.Registries {
		if err := sm.validateRegistryAuth(registry, &auth); err != nil {
			return fmt.Errorf("invalid registry auth for %s: %v", registry, err)
		}
	}

	// Validate insecure registries (warn about security implications)
	for _, registry := range config.Insecure {
		if sm.config.StrictMode {
			return fmt.Errorf("insecure registry %s not allowed in strict mode", registry)
		}
		fmt.Printf("Warning: Using insecure registry %s\n", registry)
	}

	return nil
}

// validateRegistryAuth validates registry authentication configuration
func (sm *SecurityManager) validateRegistryAuth(registry string, auth *types.RegistryAuth) error {
	// Validate registry name
	if err := sm.inputValidator.validateImageReference(registry + "/test:latest"); err != nil {
		return fmt.Errorf("invalid registry name: %v", err)
	}

	// Check for sensitive data in auth fields
	if auth.Username != "" && sm.inputValidator.containsSensitiveData(auth.Username) {
		return fmt.Errorf("username appears to contain sensitive data")
	}

	if auth.Password != "" && sm.inputValidator.containsSensitiveData(auth.Password) {
		return fmt.Errorf("password appears to contain sensitive data")
	}

	if auth.Token != "" && sm.inputValidator.containsSensitiveData(auth.Token) {
		return fmt.Errorf("token appears to contain sensitive data")
	}

	// Validate auth file path if specified
	if auth.AuthFile != "" {
		if err := sm.inputValidator.validatePath(auth.AuthFile); err != nil {
			return fmt.Errorf("invalid auth file path: %v", err)
		}
	}

	return nil
}

// SecurityAudit performs a comprehensive security audit
func (sm *SecurityManager) SecurityAudit() *SecurityAuditReport {
	report := &SecurityAuditReport{
		Timestamp: time.Now(),
		Findings:  []SecurityFinding{},
		Summary:   SecurityAuditSummary{},
	}

	// Check current security context
	if err := sm.contextEnforcer.CheckCurrentSecurityContext(); err != nil {
		report.Findings = append(report.Findings, SecurityFinding{
			Severity:    "high",
			Category:    "security_context",
			Description: fmt.Sprintf("Security context issue: %v", err),
			Recommendation: "Ensure the process is running with appropriate user privileges",
		})
		report.Summary.HighSeverityCount++
	}

	// Check secret management
	secretStats := sm.secretManager.GetSecretStats()
	if totalSecrets, ok := secretStats["total_secrets"].(int); ok && totalSecrets > 50 {
		report.Findings = append(report.Findings, SecurityFinding{
			Severity:    "medium",
			Category:    "secret_management",
			Description: fmt.Sprintf("Large number of secrets stored: %d", totalSecrets),
			Recommendation: "Consider rotating or removing unused secrets",
		})
		report.Summary.MediumSeverityCount++
	}

	// Check vulnerability scanning availability
	if sm.config.EnableVulnerabilityScanning {
		availableScanners := 0
		for _, scanner := range sm.vulnerabilityScanner.scanners {
			if scanner.IsAvailable() {
				availableScanners++
			}
		}
		if availableScanners == 0 {
			report.Findings = append(report.Findings, SecurityFinding{
				Severity:    "medium",
				Category:    "vulnerability_scanning",
				Description: "No vulnerability scanners available",
				Recommendation: "Install trivy or grype for vulnerability scanning",
			})
			report.Summary.MediumSeverityCount++
		}
	}

	// Calculate overall risk level
	if report.Summary.HighSeverityCount > 0 {
		report.Summary.OverallRisk = "high"
	} else if report.Summary.MediumSeverityCount > 2 {
		report.Summary.OverallRisk = "medium"
	} else if report.Summary.MediumSeverityCount > 0 || report.Summary.LowSeverityCount > 5 {
		report.Summary.OverallRisk = "low"
	} else {
		report.Summary.OverallRisk = "minimal"
	}

	report.Summary.TotalFindings = len(report.Findings)

	return report
}

// SecurityAuditReport represents a security audit report
type SecurityAuditReport struct {
	Timestamp time.Time             `json:"timestamp"`
	Findings  []SecurityFinding     `json:"findings"`
	Summary   SecurityAuditSummary  `json:"summary"`
}

// SecurityFinding represents a security finding
type SecurityFinding struct {
	Severity       string `json:"severity"`
	Category       string `json:"category"`
	Description    string `json:"description"`
	Recommendation string `json:"recommendation"`
}

// SecurityAuditSummary provides a summary of the security audit
type SecurityAuditSummary struct {
	TotalFindings        int    `json:"total_findings"`
	HighSeverityCount    int    `json:"high_severity_count"`
	MediumSeverityCount  int    `json:"medium_severity_count"`
	LowSeverityCount     int    `json:"low_severity_count"`
	OverallRisk          string `json:"overall_risk"`
}

// Global security manager instance
var globalSecurityManager *SecurityManager

// GetGlobalSecurityManager returns the global security manager instance
func GetGlobalSecurityManager() *SecurityManager {
	if globalSecurityManager == nil {
		globalSecurityManager = NewSecurityManager()
	}
	return globalSecurityManager
}

// SetGlobalSecurityManager sets the global security manager instance
func SetGlobalSecurityManager(sm *SecurityManager) {
	globalSecurityManager = sm
}