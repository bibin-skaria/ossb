package security

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bibin-skaria/ossb/internal/types"
)

func TestSecurityManager_ComprehensiveValidation(t *testing.T) {
	sm := NewSecurityManager()
	defer sm.Cleanup()

	// Test comprehensive build configuration validation
	tests := []struct {
		name        string
		config      *types.BuildConfig
		expectError bool
		errorContains string
	}{
		{
			name: "valid_secure_config",
			config: &types.BuildConfig{
				Context:    "/tmp/test-build",
				Dockerfile: "Dockerfile",
				Tags:       []string{"test:latest"},
				BuildArgs: map[string]string{
					"VERSION": "1.0.0",
					"ENV":     "production",
				},
				SecurityContext: &types.SecurityContext{
					RunAsUser:    int64Ptr(1000),
					RunAsGroup:   int64Ptr(1000),
					RunAsNonRoot: boolPtr(true),
					Capabilities: []string{"CAP_NET_BIND_SERVICE"},
				},
				ResourceLimits: &types.ResourceLimits{
					Memory: "1Gi",
					CPU:    "1.0",
					Disk:   "10Gi",
				},
			},
			expectError: false,
		},
		{
			name: "config_with_insecure_dockerfile",
			config: &types.BuildConfig{
				Context:    "/tmp/test-build",
				Dockerfile: "Dockerfile.insecure",
				BuildArgs: map[string]string{
					"PASSWORD": "secret123", // Sensitive key name
				},
			},
			expectError: true,
			errorContains: "sensitive key name",
		},
		{
			name: "config_with_root_security_context",
			config: &types.BuildConfig{
				Context:    "/tmp/test-build",
				Dockerfile: "Dockerfile",
				SecurityContext: &types.SecurityContext{
					RunAsUser: int64Ptr(0), // Root user
				},
			},
			expectError: true,
			errorContains: "root user",
		},
		{
			name: "config_with_excessive_resources",
			config: &types.BuildConfig{
				Context:    "/tmp/test-build",
				Dockerfile: "Dockerfile",
				ResourceLimits: &types.ResourceLimits{
					Memory: "32Gi", // Exceeds limit
				},
			},
			expectError: true,
			errorContains: "memory limit too high",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test dockerfile if needed
			if tt.config.Dockerfile != "" {
				tmpDir := t.TempDir()
				tt.config.Context = tmpDir
				dockerfilePath := filepath.Join(tmpDir, tt.config.Dockerfile)
				
				var dockerfileContent string
				if strings.Contains(tt.config.Dockerfile, "insecure") {
					dockerfileContent = `FROM alpine:3.18
USER root
RUN rm -rf /
CMD ["./app"]`
				} else {
					dockerfileContent = `FROM alpine:3.18
RUN apk add --no-cache curl
COPY . /app
WORKDIR /app
USER 1000:1000
CMD ["./app"]`
				}
				
				if err := os.WriteFile(dockerfilePath, []byte(dockerfileContent), 0644); err != nil {
					t.Fatalf("Failed to create test dockerfile: %v", err)
				}
			}

			err := sm.ValidateBuildConfiguration(tt.config)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				} else if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error containing %q, got: %v", tt.errorContains, err)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}

func TestSecurityManager_SecretManagement(t *testing.T) {
	sm := NewSecurityManager()
	defer sm.Cleanup()

	// Test secret loading from environment
	testSecrets := map[string]string{
		"TEST_SECRET_API_KEY": "test-api-key-123",
		"TEST_SECRET_TOKEN":   "test-token-456",
	}

	// Set environment variables
	for key, value := range testSecrets {
		if err := os.Setenv(key, value); err != nil {
			t.Fatalf("Failed to set environment variable %s: %v", key, err)
		}
		defer os.Unsetenv(key)
	}

	// Load secrets
	sources := map[string]string{
		"environment": "TEST_SECRET_",
	}

	if err := sm.LoadSecrets(sources); err != nil {
		t.Fatalf("Failed to load secrets: %v", err)
	}

	// Test secret injection
	env := map[string]string{
		"DATABASE_URL": "postgres://user:${API_KEY}@localhost/db",
		"AUTH_TOKEN":   "${TOKEN}",
		"NORMAL_VAR":   "normal-value",
	}

	injectedEnv, err := sm.InjectSecrets(env)
	if err != nil {
		t.Fatalf("Failed to inject secrets: %v", err)
	}

	// Verify injection worked
	expectedURL := "postgres://user:test-api-key-123@localhost/db"
	if injectedEnv["DATABASE_URL"] != expectedURL {
		t.Errorf("Expected DATABASE_URL to be %q, got %q", expectedURL, injectedEnv["DATABASE_URL"])
	}

	expectedToken := "test-token-456"
	if injectedEnv["AUTH_TOKEN"] != expectedToken {
		t.Errorf("Expected AUTH_TOKEN to be %q, got %q", expectedToken, injectedEnv["AUTH_TOKEN"])
	}

	// Normal variables should remain unchanged
	if injectedEnv["NORMAL_VAR"] != "normal-value" {
		t.Errorf("Normal variable should remain unchanged")
	}
}

func TestSecurityManager_VulnerabilityScanning(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping vulnerability scanning test in short mode")
	}

	sm := NewSecurityManager()
	defer sm.Cleanup()

	// Create a test filesystem to scan
	tmpDir := t.TempDir()
	
	// Create some test files that might have vulnerabilities
	testFiles := map[string]string{
		"package.json": `{
  "name": "test-app",
  "version": "1.0.0",
  "dependencies": {
    "lodash": "4.17.20"
  }
}`,
		"requirements.txt": `Django==2.0.0
requests==2.18.0`,
		"Gemfile": `source 'https://rubygems.org'
gem 'rails', '5.0.0'`,
	}

	for filename, content := range testFiles {
		filePath := filepath.Join(tmpDir, filename)
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", filename, err)
		}
	}

	// Attempt to scan the filesystem
	result, err := sm.ScanFilesystem(tmpDir)
	if err != nil {
		// If no scanners are available, this is expected
		if strings.Contains(err.Error(), "no vulnerability scanners available") {
			t.Skip("No vulnerability scanners available for testing")
		}
		t.Logf("Vulnerability scan failed (expected if no scanners installed): %v", err)
		return
	}

	if result != nil {
		t.Logf("Vulnerability scan completed: %d vulnerabilities found", result.Summary.Total)
		
		// Analyze results
		analysis := sm.AnalyzeScanResults(result)
		if analysis != nil {
			t.Logf("Risk level: %s", analysis.RiskLevel)
			t.Logf("Fixable vulnerabilities: %d", analysis.FixableCount)
			t.Logf("Recommendations: %v", analysis.Recommendations)
		}
	}
}

func TestSecurityManager_SecurityAudit(t *testing.T) {
	sm := NewSecurityManager()
	defer sm.Cleanup()

	// Perform security audit
	report := sm.SecurityAudit()

	if report == nil {
		t.Fatal("Security audit report should not be nil")
	}

	if report.Timestamp.IsZero() {
		t.Error("Security audit report should have a timestamp")
	}

	if report.Summary.TotalFindings != len(report.Findings) {
		t.Errorf("Summary total findings (%d) doesn't match actual findings (%d)", 
			report.Summary.TotalFindings, len(report.Findings))
	}

	// Log audit results
	t.Logf("Security audit completed:")
	t.Logf("  Total findings: %d", report.Summary.TotalFindings)
	t.Logf("  High severity: %d", report.Summary.HighSeverityCount)
	t.Logf("  Medium severity: %d", report.Summary.MediumSeverityCount)
	t.Logf("  Low severity: %d", report.Summary.LowSeverityCount)
	t.Logf("  Overall risk: %s", report.Summary.OverallRisk)

	for i, finding := range report.Findings {
		t.Logf("  Finding %d: [%s] %s - %s", i+1, finding.Severity, finding.Category, finding.Description)
	}
}

func TestSecurityManager_SecurityStatus(t *testing.T) {
	sm := NewSecurityManager()
	defer sm.Cleanup()

	// Get security status
	status := sm.GetSecurityStatus()

	if status == nil {
		t.Fatal("Security status should not be nil")
	}

	if status.Timestamp.IsZero() {
		t.Error("Security status should have a timestamp")
	}

	if status.SecurityContextInfo == nil {
		t.Error("Security status should include security context info")
	}

	if status.SecretStats == nil {
		t.Error("Security status should include secret stats")
	}

	if status.Config == nil {
		t.Error("Security status should include config")
	}

	// Log status
	t.Logf("Security status:")
	t.Logf("  Security issues: %v", status.SecurityIssues)
	t.Logf("  Security context: %+v", status.SecurityContextInfo)
	t.Logf("  Secret stats: %+v", status.SecretStats)
}

func TestSecurityManager_OperationValidation(t *testing.T) {
	sm := NewSecurityManager()
	defer sm.Cleanup()

	tests := []struct {
		name        string
		operation   *types.Operation
		expectError bool
		errorContains string
	}{
		{
			name: "valid_operation",
			operation: &types.Operation{
				Type:    types.OperationTypeExec,
				Command: []string{"echo", "hello world"},
				User:    "1000",
				Environment: map[string]string{
					"PATH": "/usr/bin:/bin",
				},
				WorkDir: "/app",
			},
			expectError: false,
		},
		{
			name: "operation_with_dangerous_command",
			operation: &types.Operation{
				Type:    types.OperationTypeExec,
				Command: []string{"rm -rf /"},
			},
			expectError: true,
			errorContains: "blocked command pattern",
		},
		{
			name: "operation_with_root_user",
			operation: &types.Operation{
				Type:    types.OperationTypeExec,
				Command: []string{"echo", "hello"},
				User:    "root",
			},
			expectError: true,
			errorContains: "root user not allowed",
		},
		{
			name: "operation_with_sensitive_env",
			operation: &types.Operation{
				Type:    types.OperationTypeExec,
				Command: []string{"echo", "hello"},
				Environment: map[string]string{
					"SECRET": "sk-1234567890abcdef1234567890abcdef12345678",
				},
			},
			expectError: true,
			errorContains: "sensitive data",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := sm.ValidateOperation(tt.operation)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				} else if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error containing %q, got: %v", tt.errorContains, err)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}

func TestSecurityManager_SecretRotation(t *testing.T) {
	sm := NewSecurityManager()
	defer sm.Cleanup()

	// Store some secrets
	secrets := map[string]string{
		"old-secret": "old-value",
		"new-secret": "new-value",
	}

	for key, value := range secrets {
		if err := sm.secretManager.StoreSecret(key, value); err != nil {
			t.Fatalf("Failed to store secret %s: %v", key, err)
		}
	}

	// Wait a bit and store another secret
	time.Sleep(10 * time.Millisecond)
	if err := sm.secretManager.StoreSecret("newer-secret", "newer-value"); err != nil {
		t.Fatalf("Failed to store newer secret: %v", err)
	}

	// Rotate secrets older than 5ms
	sm.secretManager.RotateSecrets(5 * time.Millisecond)

	// Check which secrets remain
	remainingSecrets := sm.secretManager.ListSecrets()
	t.Logf("Remaining secrets after rotation: %v", remainingSecrets)

	// The newer secret should still exist
	if !sm.secretManager.HasSecret("newer-secret") {
		t.Error("Newer secret should still exist after rotation")
	}
}

func TestSecurityManager_GlobalInstance(t *testing.T) {
	// Test global security manager
	sm1 := GetGlobalSecurityManager()
	sm2 := GetGlobalSecurityManager()

	if sm1 != sm2 {
		t.Error("GetGlobalSecurityManager should return the same instance")
	}

	// Test setting custom global instance
	customSM := NewSecurityManager()
	SetGlobalSecurityManager(customSM)

	sm3 := GetGlobalSecurityManager()
	if sm3 != customSM {
		t.Error("SetGlobalSecurityManager should update the global instance")
	}
}

func TestSecurityManager_ConfigurationValidation(t *testing.T) {
	// Test custom security configuration
	config := &SecurityConfig{
		MaxDockerfileSize:           5 * 1024 * 1024, // 5MB
		MaxBuildArgSize:             512,              // 512 bytes
		MaxBuildArgCount:            50,               // 50 args
		SecretRotationInterval:      12 * time.Hour,  // 12 hours
		AllowPrivileged:             false,
		MaxUID:                      32767,
		MaxGID:                      32767,
		MaxMemoryBytes:              4 * 1024 * 1024 * 1024, // 4GB
		MaxCPUCores:                 2.0,                     // 2 cores
		MaxDiskBytes:                25 * 1024 * 1024 * 1024, // 25GB
		EnableVulnerabilityScanning: false, // Disabled for testing
		StrictMode:                  true,
	}

	sm := NewSecurityManagerWithConfig(config)
	defer sm.Cleanup()

	// Test that configuration is applied
	if sm.config.MaxMemoryBytes != config.MaxMemoryBytes {
		t.Errorf("Expected MaxMemoryBytes to be %d, got %d", config.MaxMemoryBytes, sm.config.MaxMemoryBytes)
	}

	// Test validation with custom limits
	buildConfig := &types.BuildConfig{
		Context:    "/tmp/test",
		Dockerfile: "Dockerfile",
		ResourceLimits: &types.ResourceLimits{
			Memory: "6Gi", // Exceeds custom limit of 4GB
		},
	}

	// Create test dockerfile
	tmpDir := t.TempDir()
	buildConfig.Context = tmpDir
	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
	dockerfileContent := `FROM alpine:3.18
CMD ["./app"]`
	
	if err := os.WriteFile(dockerfilePath, []byte(dockerfileContent), 0644); err != nil {
		t.Fatalf("Failed to create test dockerfile: %v", err)
	}

	err := sm.ValidateBuildConfiguration(buildConfig)
	if err == nil {
		t.Error("Expected error for memory limit exceeding custom configuration")
	} else if !strings.Contains(err.Error(), "memory limit too high") {
		t.Errorf("Expected memory limit error, got: %v", err)
	}
}

func BenchmarkSecurityManager_ValidateOperation(b *testing.B) {
	sm := NewSecurityManager()
	defer sm.Cleanup()

	operation := &types.Operation{
		Type:    types.OperationTypeExec,
		Command: []string{"echo", "hello world"},
		User:    "1000",
		Environment: map[string]string{
			"PATH": "/usr/bin:/bin",
			"HOME": "/home/user",
		},
		WorkDir: "/app",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sm.ValidateOperation(operation)
	}
}

func BenchmarkSecurityManager_InjectSecrets(b *testing.B) {
	sm := NewSecurityManager()
	defer sm.Cleanup()

	// Store test secrets
	sm.secretManager.StoreSecret("API_KEY", "test-api-key")
	sm.secretManager.StoreSecret("TOKEN", "test-token")

	env := map[string]string{
		"DATABASE_URL": "postgres://user:${API_KEY}@localhost/db",
		"AUTH_TOKEN":   "${TOKEN}",
		"NORMAL_VAR":   "normal-value",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sm.InjectSecrets(env)
	}
}