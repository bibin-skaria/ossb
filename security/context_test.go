package security

import (
	"strings"
	"testing"

	"github.com/bibin-skaria/ossb/internal/types"
)

func TestSecurityContextValidator_ValidateSecurityContext(t *testing.T) {
	validator := NewSecurityContextValidator()

	tests := []struct {
		name        string
		context     *types.SecurityContext
		expectError bool
		errorContains string
	}{
		{
			name:        "nil_context",
			context:     nil,
			expectError: false,
		},
		{
			name: "valid_context",
			context: &types.SecurityContext{
				RunAsUser:    int64Ptr(1000),
				RunAsGroup:   int64Ptr(1000),
				RunAsNonRoot: boolPtr(true),
				Capabilities: []string{"CAP_NET_BIND_SERVICE"},
			},
			expectError: false,
		},
		{
			name: "root_user_context",
			context: &types.SecurityContext{
				RunAsUser: int64Ptr(0),
			},
			expectError: true,
			errorContains: "root user",
		},
		{
			name: "negative_uid",
			context: &types.SecurityContext{
				RunAsUser: int64Ptr(-1),
			},
			expectError: true,
			errorContains: "negative UID",
		},
		{
			name: "uid_too_high",
			context: &types.SecurityContext{
				RunAsUser: int64Ptr(70000),
			},
			expectError: true,
			errorContains: "UID too high",
		},
		{
			name: "root_group_context",
			context: &types.SecurityContext{
				RunAsGroup: int64Ptr(0),
			},
			expectError: true,
			errorContains: "root group",
		},
		{
			name: "negative_gid",
			context: &types.SecurityContext{
				RunAsGroup: int64Ptr(-1),
			},
			expectError: true,
			errorContains: "negative GID",
		},
		{
			name: "gid_too_high",
			context: &types.SecurityContext{
				RunAsGroup: int64Ptr(70000),
			},
			expectError: true,
			errorContains: "GID too high",
		},
		{
			name: "run_as_root_allowed",
			context: &types.SecurityContext{
				RunAsNonRoot: boolPtr(false),
			},
			expectError: true,
			errorContains: "RunAsNonRoot must be true",
		},
		{
			name: "dangerous_capability",
			context: &types.SecurityContext{
				Capabilities: []string{"CAP_SYS_ADMIN"},
			},
			expectError: true,
			errorContains: "blocked in rootless mode",
		},
		{
			name: "unknown_capability",
			context: &types.SecurityContext{
				Capabilities: []string{"CAP_UNKNOWN"},
			},
			expectError: true,
			errorContains: "unknown capability",
		},
		{
			name: "too_many_capabilities",
			context: &types.SecurityContext{
				Capabilities: func() []string {
					caps := make([]string, 21)
					for i := range caps {
						caps[i] = "CAP_CHOWN"
					}
					return caps
				}(),
			},
			expectError: true,
			errorContains: "too many capabilities",
		},
		{
			name: "inconsistent_context",
			context: &types.SecurityContext{
				RunAsUser:    int64Ptr(0),
				RunAsNonRoot: boolPtr(true),
			},
			expectError: true,
			errorContains: "conflicts with",
		},
		{
			name: "valid_safe_capabilities",
			context: &types.SecurityContext{
				RunAsUser:    int64Ptr(1000),
				RunAsNonRoot: boolPtr(true),
				Capabilities: []string{"CAP_NET_BIND_SERVICE", "CAP_CHOWN"},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateSecurityContext(tt.context)

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

func TestSecurityContextValidator_ValidateCapability(t *testing.T) {
	validator := NewSecurityContextValidator()

	tests := []struct {
		name        string
		capability  string
		expectError bool
		errorContains string
	}{
		{
			name:        "allowed_capability",
			capability:  "CAP_NET_BIND_SERVICE",
			expectError: false,
		},
		{
			name:        "allowed_capability_without_prefix",
			capability:  "NET_BIND_SERVICE",
			expectError: false,
		},
		{
			name:        "blocked_capability",
			capability:  "CAP_SYS_ADMIN",
			expectError: true,
			errorContains: "blocked in rootless mode",
		},
		{
			name:        "disallowed_capability",
			capability:  "CAP_DAC_OVERRIDE",
			expectError: true,
			errorContains: "not allowed in rootless mode",
		},
		{
			name:        "unknown_capability",
			capability:  "CAP_NONEXISTENT",
			expectError: true,
			errorContains: "unknown capability",
		},
		{
			name:        "case_insensitive_capability",
			capability:  "cap_net_bind_service",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.validateCapability(tt.capability)

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

func TestSecurityContextValidator_GetDefaultSecurityContext(t *testing.T) {
	validator := NewSecurityContextValidator()

	defaultCtx := validator.GetDefaultSecurityContext()

	if defaultCtx == nil {
		t.Fatal("Default security context should not be nil")
	}

	if defaultCtx.RunAsUser == nil || *defaultCtx.RunAsUser == 0 {
		t.Error("Default context should not run as root")
	}

	if defaultCtx.RunAsGroup == nil || *defaultCtx.RunAsGroup == 0 {
		t.Error("Default context should not use root group")
	}

	if defaultCtx.RunAsNonRoot == nil || !*defaultCtx.RunAsNonRoot {
		t.Error("Default context should enforce RunAsNonRoot")
	}

	if len(defaultCtx.Capabilities) != 0 {
		t.Error("Default context should have no capabilities")
	}
}

func TestSecurityContextEnforcer_CheckCurrentSecurityContext(t *testing.T) {
	enforcer := NewSecurityContextEnforcer()

	// This test will pass if not running as root
	err := enforcer.CheckCurrentSecurityContext()
	
	// We can't guarantee the test environment, so we just check that the function runs
	// In a real rootless environment, this should pass
	// In a root environment, this should fail
	if err != nil {
		t.Logf("Current security context check failed (expected if running as root): %v", err)
	} else {
		t.Logf("Current security context check passed")
	}
}

func TestSecurityContextEnforcer_GetProcessSecurityInfo(t *testing.T) {
	enforcer := NewSecurityContextEnforcer()

	info := enforcer.GetProcessSecurityInfo()

	// Check that basic fields are present
	if _, ok := info["uid"]; !ok {
		t.Error("Process security info should include uid")
	}

	if _, ok := info["gid"]; !ok {
		t.Error("Process security info should include gid")
	}

	if _, ok := info["euid"]; !ok {
		t.Error("Process security info should include euid")
	}

	if _, ok := info["egid"]; !ok {
		t.Error("Process security info should include egid")
	}

	if _, ok := info["user_namespace"]; !ok {
		t.Error("Process security info should include user_namespace")
	}

	t.Logf("Process security info: %+v", info)
}

func TestResourceLimitValidator_ValidateResourceLimits(t *testing.T) {
	validator := NewResourceLimitValidator()

	tests := []struct {
		name        string
		limits      *types.ResourceLimits
		expectError bool
		errorContains string
	}{
		{
			name:        "nil_limits",
			limits:      nil,
			expectError: false,
		},
		{
			name: "valid_limits",
			limits: &types.ResourceLimits{
				Memory: "1Gi",
				CPU:    "1.0",
				Disk:   "10Gi",
			},
			expectError: false,
		},
		{
			name: "memory_too_high",
			limits: &types.ResourceLimits{
				Memory: "16Gi",
			},
			expectError: true,
			errorContains: "memory limit too high",
		},
		{
			name: "memory_too_low",
			limits: &types.ResourceLimits{
				Memory: "32Mi",
			},
			expectError: true,
			errorContains: "memory limit too low",
		},
		{
			name: "invalid_memory_format",
			limits: &types.ResourceLimits{
				Memory: "invalid",
			},
			expectError: true,
			errorContains: "invalid memory limit format",
		},
		{
			name: "cpu_too_high",
			limits: &types.ResourceLimits{
				CPU: "8.0",
			},
			expectError: true,
			errorContains: "CPU limit too high",
		},
		{
			name: "cpu_too_low",
			limits: &types.ResourceLimits{
				CPU: "0.05",
			},
			expectError: true,
			errorContains: "CPU limit too low",
		},
		{
			name: "invalid_cpu_format",
			limits: &types.ResourceLimits{
				CPU: "invalid",
			},
			expectError: true,
			errorContains: "invalid CPU limit format",
		},
		{
			name: "disk_too_high",
			limits: &types.ResourceLimits{
				Disk: "100Gi",
			},
			expectError: true,
			errorContains: "disk limit too high",
		},
		{
			name: "disk_too_low",
			limits: &types.ResourceLimits{
				Disk: "512Mi",
			},
			expectError: true,
			errorContains: "disk limit too low",
		},
		{
			name: "millicores_cpu",
			limits: &types.ResourceLimits{
				CPU: "500m",
			},
			expectError: false,
		},
		{
			name: "memory_with_different_units",
			limits: &types.ResourceLimits{
				Memory: "1024Mi",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateResourceLimits(tt.limits)

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

func TestParseMemoryToBytes(t *testing.T) {
	tests := []struct {
		name        string
		memory      string
		expected    int64
		expectError bool
	}{
		{
			name:     "bytes",
			memory:   "1024",
			expected: 1024,
		},
		{
			name:     "kilobytes",
			memory:   "1K",
			expected: 1000,
		},
		{
			name:     "kibibytes",
			memory:   "1Ki",
			expected: 1024,
		},
		{
			name:     "megabytes",
			memory:   "1M",
			expected: 1000000,
		},
		{
			name:     "mebibytes",
			memory:   "1Mi",
			expected: 1048576,
		},
		{
			name:     "gigabytes",
			memory:   "1G",
			expected: 1000000000,
		},
		{
			name:     "gibibytes",
			memory:   "1Gi",
			expected: 1073741824,
		},
		{
			name:     "terabytes",
			memory:   "1T",
			expected: 1000000000000,
		},
		{
			name:     "tebibytes",
			memory:   "1Ti",
			expected: 1099511627776,
		},
		{
			name:     "decimal_value",
			memory:   "1.5Gi",
			expected: 1610612736, // 1.5 * 1024^3
		},
		{
			name:        "empty_string",
			memory:      "",
			expectError: true,
		},
		{
			name:        "invalid_format",
			memory:      "invalid",
			expectError: true,
		},
		{
			name:        "invalid_number",
			memory:      "abcGi",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseMemoryToBytes(tt.memory)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				} else if result != tt.expected {
					t.Errorf("Expected %d, got %d", tt.expected, result)
				}
			}
		})
	}
}

func TestParseCPUToCores(t *testing.T) {
	tests := []struct {
		name        string
		cpu         string
		expected    float64
		expectError bool
	}{
		{
			name:     "whole_cores",
			cpu:      "2",
			expected: 2.0,
		},
		{
			name:     "decimal_cores",
			cpu:      "1.5",
			expected: 1.5,
		},
		{
			name:     "millicores",
			cpu:      "1000m",
			expected: 1.0,
		},
		{
			name:     "partial_millicores",
			cpu:      "500m",
			expected: 0.5,
		},
		{
			name:     "small_millicores",
			cpu:      "100m",
			expected: 0.1,
		},
		{
			name:        "empty_string",
			cpu:         "",
			expectError: true,
		},
		{
			name:        "invalid_format",
			cpu:         "invalid",
			expectError: true,
		},
		{
			name:        "invalid_millicores",
			cpu:         "abcm",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseCPUToCores(tt.cpu)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				} else if result != tt.expected {
					t.Errorf("Expected %f, got %f", tt.expected, result)
				}
			}
		})
	}
}

func BenchmarkSecurityContextValidator_ValidateSecurityContext(b *testing.B) {
	validator := NewSecurityContextValidator()
	
	ctx := &types.SecurityContext{
		RunAsUser:    int64Ptr(1000),
		RunAsGroup:   int64Ptr(1000),
		RunAsNonRoot: boolPtr(true),
		Capabilities: []string{"CAP_NET_BIND_SERVICE", "CAP_CHOWN"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		validator.ValidateSecurityContext(ctx)
	}
}

func BenchmarkResourceLimitValidator_ValidateResourceLimits(b *testing.B) {
	validator := NewResourceLimitValidator()
	
	limits := &types.ResourceLimits{
		Memory: "1Gi",
		CPU:    "1.0",
		Disk:   "10Gi",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		validator.ValidateResourceLimits(limits)
	}
}

func BenchmarkParseMemoryToBytes(b *testing.B) {
	memory := "1Gi"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		parseMemoryToBytes(memory)
	}
}

func BenchmarkParseCPUToCores(b *testing.B) {
	cpu := "1000m"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		parseCPUToCores(cpu)
	}
}