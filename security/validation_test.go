package security

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bibin-skaria/ossb/internal/types"
)

func TestInputValidator_ValidateDockerfile(t *testing.T) {
	validator := NewInputValidator()

	tests := []struct {
		name        string
		dockerfile  string
		expectError bool
		errorContains string
	}{
		{
			name: "valid_dockerfile",
			dockerfile: `FROM alpine:3.18
RUN apk add --no-cache curl
COPY . /app
WORKDIR /app
USER 1000:1000
CMD ["./app"]`,
			expectError: false,
		},
		{
			name: "dockerfile_with_root_user",
			dockerfile: `FROM alpine:3.18
USER root
CMD ["./app"]`,
			expectError: true,
			errorContains: "root user",
		},
		{
			name: "dockerfile_with_dangerous_command",
			dockerfile: `FROM alpine:3.18
RUN rm -rf /
CMD ["./app"]`,
			expectError: true,
			errorContains: "dangerous command pattern",
		},
		{
			name: "dockerfile_with_curl_pipe",
			dockerfile: `FROM alpine:3.18
RUN curl -sSL https://example.com/script.sh | sh
CMD ["./app"]`,
			expectError: true,
			errorContains: "dangerous command pattern",
		},
		{
			name: "dockerfile_with_sensitive_copy",
			dockerfile: `FROM alpine:3.18
COPY id_rsa /root/.ssh/
CMD ["./app"]`,
			expectError: true,
			errorContains: "sensitive file",
		},
		{
			name: "dockerfile_with_sensitive_env",
			dockerfile: `FROM alpine:3.18
ENV PASSWORD=secret123
CMD ["./app"]`,
			expectError: true,
			errorContains: "sensitive environment variable",
		},
		{
			name: "dockerfile_with_path_traversal",
			dockerfile: `FROM alpine:3.18
COPY ../../../etc/passwd /app/
CMD ["./app"]`,
			expectError: true,
			errorContains: "path traversal",
		},
		{
			name: "dockerfile_with_unknown_command",
			dockerfile: `FROM alpine:3.18
BADCOMMAND something
CMD ["./app"]`,
			expectError: true,
			errorContains: "unknown or disallowed command",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary dockerfile
			tmpDir := t.TempDir()
			dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
			
			if err := os.WriteFile(dockerfilePath, []byte(tt.dockerfile), 0644); err != nil {
				t.Fatalf("Failed to create test dockerfile: %v", err)
			}

			err := validator.ValidateDockerfile(dockerfilePath)

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

func TestInputValidator_ValidateBuildArgs(t *testing.T) {
	validator := NewInputValidator()

	tests := []struct {
		name        string
		buildArgs   map[string]string
		expectError bool
		errorContains string
	}{
		{
			name: "valid_build_args",
			buildArgs: map[string]string{
				"VERSION": "1.0.0",
				"ENV":     "production",
			},
			expectError: false,
		},
		{
			name: "build_arg_with_sensitive_key",
			buildArgs: map[string]string{
				"PASSWORD": "secret123",
			},
			expectError: true,
			errorContains: "sensitive key name",
		},
		{
			name: "build_arg_with_command_injection",
			buildArgs: map[string]string{
				"VERSION": "1.0.0; rm -rf /",
			},
			expectError: true,
			errorContains: "dangerous pattern",
		},
		{
			name: "build_arg_with_path_traversal",
			buildArgs: map[string]string{
				"CONFIG_PATH": "../../../etc/passwd",
			},
			expectError: true,
			errorContains: "path traversal",
		},
		{
			name: "build_arg_with_sensitive_data",
			buildArgs: map[string]string{
				"TOKEN": "sk-1234567890abcdef1234567890abcdef12345678",
			},
			expectError: true,
			errorContains: "sensitive data",
		},
		{
			name: "too_many_build_args",
			buildArgs: func() map[string]string {
				args := make(map[string]string)
				for i := 0; i < 101; i++ {
					args[fmt.Sprintf("ARG_%d", i)] = "value"
				}
				return args
			}(),
			expectError: true,
			errorContains: "too many build arguments",
		},
		{
			name: "build_arg_value_too_long",
			buildArgs: map[string]string{
				"LONG_VALUE": strings.Repeat("a", 1025),
			},
			expectError: true,
			errorContains: "value too long",
		},
		{
			name: "build_arg_key_too_long",
			buildArgs: map[string]string{
				strings.Repeat("a", 129): "value",
			},
			expectError: true,
			errorContains: "key too long",
		},
		{
			name: "build_arg_invalid_key_chars",
			buildArgs: map[string]string{
				"INVALID@KEY": "value",
			},
			expectError: true,
			errorContains: "invalid character",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateBuildArgs(tt.buildArgs)

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

func TestInputValidator_ValidateOperation(t *testing.T) {
	validator := NewInputValidator()

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
			name: "operation_with_dangerous_command",
			operation: &types.Operation{
				Type:    types.OperationTypeExec,
				Command: []string{"rm -rf /"},
			},
			expectError: true,
			errorContains: "blocked command pattern",
		},
		{
			name: "operation_with_sensitive_env",
			operation: &types.Operation{
				Type:    types.OperationTypeExec,
				Command: []string{"echo", "hello"},
				Environment: map[string]string{
					"SECRET_KEY": "sk-1234567890abcdef1234567890abcdef12345678",
				},
			},
			expectError: true,
			errorContains: "sensitive data",
		},
		{
			name: "operation_with_invalid_workdir",
			operation: &types.Operation{
				Type:    types.OperationTypeExec,
				Command: []string{"echo", "hello"},
				WorkDir: "../../../etc",
			},
			expectError: true,
			errorContains: "path traversal",
		},
		{
			name: "operation_with_too_many_env_vars",
			operation: &types.Operation{
				Type:    types.OperationTypeExec,
				Command: []string{"echo", "hello"},
				Environment: func() map[string]string {
					env := make(map[string]string)
					for i := 0; i < 101; i++ {
						env[fmt.Sprintf("VAR_%d", i)] = "value"
					}
					return env
				}(),
			},
			expectError: true,
			errorContains: "too many environment variables",
		},
		{
			name: "nil_operation",
			operation: nil,
			expectError: true,
			errorContains: "nil operation",
		},
		{
			name: "operation_with_invalid_type",
			operation: &types.Operation{
				Type:    types.OperationType("invalid"),
				Command: []string{"echo", "hello"},
			},
			expectError: true,
			errorContains: "invalid operation type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateOperation(tt.operation)

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

func TestInputValidator_ValidateImageReference(t *testing.T) {
	validator := NewInputValidator()

	tests := []struct {
		name        string
		imageRef    string
		expectError bool
		errorContains string
	}{
		{
			name:        "valid_image_with_tag",
			imageRef:    "alpine:3.18",
			expectError: false,
		},
		{
			name:        "valid_image_with_registry",
			imageRef:    "docker.io/library/alpine:3.18",
			expectError: false,
		},
		{
			name:        "valid_image_with_digest",
			imageRef:    "alpine@sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			expectError: false,
		},
		{
			name:        "empty_image_reference",
			imageRef:    "",
			expectError: true,
			errorContains: "empty image reference",
		},
		{
			name:        "image_reference_too_long",
			imageRef:    strings.Repeat("a", 513),
			expectError: true,
			errorContains: "too long",
		},
		{
			name:        "invalid_image_format",
			imageRef:    "invalid@image@reference",
			expectError: true,
			errorContains: "invalid image reference format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.validateImageReference(tt.imageRef)

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

func TestInputValidator_ContainsSensitiveData(t *testing.T) {
	validator := NewInputValidator()

	tests := []struct {
		name      string
		value     string
		expected  bool
	}{
		{
			name:     "openai_api_key",
			value:    "sk-1234567890abcdef1234567890abcdef12345678",
			expected: true,
		},
		{
			name:     "github_token",
			value:    "ghp_1234567890abcdef1234567890abcdef123456",
			expected: true,
		},
		{
			name:     "aws_access_key",
			value:    "AKIA1234567890ABCDEF",
			expected: true,
		},
		{
			name:     "base64_secret",
			value:    "dGhpc2lzYXNlY3JldGtleXRoYXRzaG91bGRub3RiZWV4cG9zZWQ=",
			expected: true,
		},
		{
			name:     "hex_secret",
			value:    "1234567890abcdef1234567890abcdef12345678",
			expected: true,
		},
		{
			name:     "jwt_token",
			value:    "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c",
			expected: true,
		},
		{
			name:     "normal_string",
			value:    "hello world",
			expected: false,
		},
		{
			name:     "version_string",
			value:    "1.0.0",
			expected: false,
		},
		{
			name:     "short_hex",
			value:    "abc123",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validator.containsSensitiveData(tt.value)
			if result != tt.expected {
				t.Errorf("containsSensitiveData(%q) = %v, expected %v", tt.value, result, tt.expected)
			}
		})
	}
}

func TestInputValidator_ValidateDockerfileSize(t *testing.T) {
	validator := NewInputValidator()

	// Create a large dockerfile
	tmpDir := t.TempDir()
	largeDockerfilePath := filepath.Join(tmpDir, "large-dockerfile")
	
	// Create content larger than 10MB
	largeContent := strings.Repeat("# This is a comment\n", 500000) // ~10MB
	if err := os.WriteFile(largeDockerfilePath, []byte(largeContent), 0644); err != nil {
		t.Fatalf("Failed to create large dockerfile: %v", err)
	}

	err := validator.ValidateDockerfile(largeDockerfilePath)
	if err == nil {
		t.Error("Expected error for large dockerfile but got none")
	} else if !strings.Contains(err.Error(), "too large") {
		t.Errorf("Expected error about file size, got: %v", err)
	}
}

func TestInputValidator_ValidateDockerfileLongLine(t *testing.T) {
	validator := NewInputValidator()

	tmpDir := t.TempDir()
	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
	
	// Create dockerfile with very long line
	longLine := "RUN " + strings.Repeat("echo hello && ", 1000) + "echo done"
	dockerfile := "FROM alpine:3.18\n" + longLine + "\nCMD [\"./app\"]"
	
	if err := os.WriteFile(dockerfilePath, []byte(dockerfile), 0644); err != nil {
		t.Fatalf("Failed to create test dockerfile: %v", err)
	}

	err := validator.ValidateDockerfile(dockerfilePath)
	if err == nil {
		t.Error("Expected error for long line but got none")
	} else if !strings.Contains(err.Error(), "line too long") {
		t.Errorf("Expected error about line length, got: %v", err)
	}
}

func TestInputValidator_ValidatePackageManagerSecurity(t *testing.T) {
	validator := NewInputValidator()

	tmpDir := t.TempDir()
	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
	
	// Create dockerfile with package installation without update
	dockerfile := `FROM ubuntu:20.04
RUN apt-get install -y curl
CMD ["./app"]`
	
	if err := os.WriteFile(dockerfilePath, []byte(dockerfile), 0644); err != nil {
		t.Fatalf("Failed to create test dockerfile: %v", err)
	}

	// This should pass validation but generate a warning
	err := validator.ValidateDockerfile(dockerfilePath)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	// Note: Warning messages are printed to stdout, not returned as errors
}

func BenchmarkInputValidator_ValidateDockerfile(b *testing.B) {
	validator := NewInputValidator()
	
	tmpDir := b.TempDir()
	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
	
	dockerfile := `FROM alpine:3.18
RUN apk add --no-cache curl git
COPY . /app
WORKDIR /app
USER 1000:1000
EXPOSE 8080
ENV NODE_ENV=production
CMD ["./app"]`
	
	if err := os.WriteFile(dockerfilePath, []byte(dockerfile), 0644); err != nil {
		b.Fatalf("Failed to create test dockerfile: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		validator.ValidateDockerfile(dockerfilePath)
	}
}

func BenchmarkInputValidator_ValidateBuildArgs(b *testing.B) {
	validator := NewInputValidator()
	
	buildArgs := map[string]string{
		"VERSION":     "1.0.0",
		"ENVIRONMENT": "production",
		"BUILD_DATE":  "2023-01-01",
		"GIT_COMMIT":  "abc123def456",
		"NODE_ENV":    "production",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		validator.ValidateBuildArgs(buildArgs)
	}
}

func BenchmarkInputValidator_ValidateOperation(b *testing.B) {
	validator := NewInputValidator()
	
	operation := &types.Operation{
		Type:    types.OperationTypeExec,
		Command: []string{"echo", "hello world"},
		User:    "1000",
		Environment: map[string]string{
			"PATH":    "/usr/bin:/bin",
			"HOME":    "/home/user",
			"USER":    "user",
			"SHELL":   "/bin/sh",
			"TERM":    "xterm",
		},
		WorkDir: "/app",
		Metadata: map[string]string{
			"stage": "build",
			"layer": "1",
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		validator.ValidateOperation(operation)
	}
}