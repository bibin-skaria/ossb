package security

import (
	"fmt"
	"os"
	"os/user"
	"strconv"
	"strings"

	"github.com/bibin-skaria/ossb/internal/types"
)

// SecurityContextValidator validates and enforces security contexts
type SecurityContextValidator struct {
	allowPrivileged bool
	maxUID          int64
	maxGID          int64
	allowedCaps     map[string]bool
	blockedCaps     map[string]bool
}

// NewSecurityContextValidator creates a new security context validator
func NewSecurityContextValidator() *SecurityContextValidator {
	// Define allowed capabilities for rootless operation
	allowedCaps := map[string]bool{
		"CAP_CHOWN":            true,  // Change file ownership
		"CAP_DAC_OVERRIDE":     false, // Override file permissions (dangerous)
		"CAP_FOWNER":           false, // File owner operations (dangerous)
		"CAP_FSETID":           false, // Set file capabilities (dangerous)
		"CAP_KILL":             true,  // Send signals to processes
		"CAP_SETGID":           false, // Set group ID (dangerous)
		"CAP_SETUID":           false, // Set user ID (dangerous)
		"CAP_SETPCAP":          false, // Set process capabilities (dangerous)
		"CAP_LINUX_IMMUTABLE":  false, // Set immutable flag (dangerous)
		"CAP_NET_BIND_SERVICE": true,  // Bind to privileged ports
		"CAP_NET_BROADCAST":    true,  // Broadcast packets
		"CAP_NET_ADMIN":        false, // Network administration (dangerous)
		"CAP_NET_RAW":          false, // Raw sockets (dangerous)
		"CAP_IPC_LOCK":         false, // Lock memory (dangerous)
		"CAP_IPC_OWNER":        false, // IPC ownership (dangerous)
		"CAP_SYS_MODULE":       false, // Load kernel modules (dangerous)
		"CAP_SYS_RAWIO":        false, // Raw I/O access (dangerous)
		"CAP_SYS_CHROOT":       false, // Use chroot (dangerous)
		"CAP_SYS_PTRACE":       false, // Trace processes (dangerous)
		"CAP_SYS_PACCT":        false, // Process accounting (dangerous)
		"CAP_SYS_ADMIN":        false, // System administration (dangerous)
		"CAP_SYS_BOOT":         false, // Reboot system (dangerous)
		"CAP_SYS_NICE":         true,  // Set process priority
		"CAP_SYS_RESOURCE":     false, // Override resource limits (dangerous)
		"CAP_SYS_TIME":         false, // Set system time (dangerous)
		"CAP_SYS_TTY_CONFIG":   false, // Configure TTY (dangerous)
		"CAP_MKNOD":            false, // Create device nodes (dangerous)
		"CAP_LEASE":            true,  // File leases
		"CAP_AUDIT_WRITE":      false, // Write to audit log (dangerous)
		"CAP_AUDIT_CONTROL":    false, // Control audit system (dangerous)
		"CAP_SETFCAP":          false, // Set file capabilities (dangerous)
		"CAP_MAC_OVERRIDE":     false, // Override MAC policy (dangerous)
		"CAP_MAC_ADMIN":        false, // MAC administration (dangerous)
		"CAP_SYSLOG":           false, // Access syslog (dangerous)
		"CAP_WAKE_ALARM":       false, // Wake system (dangerous)
		"CAP_BLOCK_SUSPEND":    false, // Block system suspend (dangerous)
	}

	// Define explicitly blocked capabilities
	blockedCaps := map[string]bool{
		"CAP_SYS_ADMIN":    true, // Most dangerous
		"CAP_SYS_MODULE":   true, // Kernel module loading
		"CAP_SYS_RAWIO":    true, // Raw I/O access
		"CAP_SYS_PTRACE":   true, // Process tracing
		"CAP_DAC_OVERRIDE": true, // Override file permissions
		"CAP_SETUID":       true, // Set user ID
		"CAP_SETGID":       true, // Set group ID
		"CAP_NET_ADMIN":    true, // Network administration
		"CAP_NET_RAW":      true, // Raw sockets
		"CAP_SYS_CHROOT":   true, // Chroot operations
		"CAP_SYS_TIME":     true, // Set system time
		"CAP_SYS_BOOT":     true, // Reboot system
	}

	return &SecurityContextValidator{
		allowPrivileged: false,
		maxUID:          65535, // Standard user range
		maxGID:          65535, // Standard group range
		allowedCaps:     allowedCaps,
		blockedCaps:     blockedCaps,
	}
}

// ValidateSecurityContext validates a security context for rootless operation
func (scv *SecurityContextValidator) ValidateSecurityContext(ctx *types.SecurityContext) error {
	if ctx == nil {
		// Default security context is acceptable
		return nil
	}

	// Validate RunAsUser
	if ctx.RunAsUser != nil {
		if err := scv.validateRunAsUser(*ctx.RunAsUser); err != nil {
			return fmt.Errorf("invalid RunAsUser: %v", err)
		}
	}

	// Validate RunAsGroup
	if ctx.RunAsGroup != nil {
		if err := scv.validateRunAsGroup(*ctx.RunAsGroup); err != nil {
			return fmt.Errorf("invalid RunAsGroup: %v", err)
		}
	}

	// Validate RunAsNonRoot
	if ctx.RunAsNonRoot != nil {
		if err := scv.validateRunAsNonRoot(*ctx.RunAsNonRoot); err != nil {
			return fmt.Errorf("invalid RunAsNonRoot: %v", err)
		}
	}

	// Validate Capabilities
	if err := scv.validateCapabilities(ctx.Capabilities); err != nil {
		return fmt.Errorf("invalid capabilities: %v", err)
	}

	// Cross-validation: ensure consistency
	if err := scv.validateConsistency(ctx); err != nil {
		return fmt.Errorf("inconsistent security context: %v", err)
	}

	return nil
}

// validateRunAsUser validates the RunAsUser setting
func (scv *SecurityContextValidator) validateRunAsUser(uid int64) error {
	if uid < 0 {
		return fmt.Errorf("negative UID not allowed: %d", uid)
	}

	if uid == 0 {
		return fmt.Errorf("root user (UID 0) not allowed in rootless mode")
	}

	if uid > scv.maxUID {
		return fmt.Errorf("UID too high: %d (max %d)", uid, scv.maxUID)
	}

	return nil
}

// validateRunAsGroup validates the RunAsGroup setting
func (scv *SecurityContextValidator) validateRunAsGroup(gid int64) error {
	if gid < 0 {
		return fmt.Errorf("negative GID not allowed: %d", gid)
	}

	if gid == 0 {
		return fmt.Errorf("root group (GID 0) not allowed in rootless mode")
	}

	if gid > scv.maxGID {
		return fmt.Errorf("GID too high: %d (max %d)", gid, scv.maxGID)
	}

	return nil
}

// validateRunAsNonRoot validates the RunAsNonRoot setting
func (scv *SecurityContextValidator) validateRunAsNonRoot(runAsNonRoot bool) error {
	if !runAsNonRoot {
		return fmt.Errorf("RunAsNonRoot must be true in rootless mode")
	}

	return nil
}

// validateCapabilities validates the capabilities list
func (scv *SecurityContextValidator) validateCapabilities(capabilities []string) error {
	if len(capabilities) > 20 {
		return fmt.Errorf("too many capabilities: %d (max 20)", len(capabilities))
	}

	for _, cap := range capabilities {
		if err := scv.validateCapability(cap); err != nil {
			return fmt.Errorf("invalid capability %s: %v", cap, err)
		}
	}

	return nil
}

// validateCapability validates a single capability
func (scv *SecurityContextValidator) validateCapability(capability string) error {
	// Normalize capability name
	cap := strings.ToUpper(capability)
	if !strings.HasPrefix(cap, "CAP_") {
		cap = "CAP_" + cap
	}

	// Check if capability is explicitly blocked
	if scv.blockedCaps[cap] {
		return fmt.Errorf("capability %s is blocked in rootless mode", cap)
	}

	// Check if capability is in allowed list
	allowed, exists := scv.allowedCaps[cap]
	if !exists {
		return fmt.Errorf("unknown capability: %s", cap)
	}

	if !allowed {
		return fmt.Errorf("capability %s is not allowed in rootless mode", cap)
	}

	return nil
}

// validateConsistency validates consistency between different security context settings
func (scv *SecurityContextValidator) validateConsistency(ctx *types.SecurityContext) error {
	// If RunAsUser is 0, RunAsNonRoot must be false (but we don't allow root anyway)
	if ctx.RunAsUser != nil && *ctx.RunAsUser == 0 {
		if ctx.RunAsNonRoot != nil && *ctx.RunAsNonRoot {
			return fmt.Errorf("RunAsUser=0 conflicts with RunAsNonRoot=true")
		}
	}

	// If RunAsNonRoot is false, we should have a non-zero RunAsUser (but we don't allow this)
	if ctx.RunAsNonRoot != nil && !*ctx.RunAsNonRoot {
		return fmt.Errorf("RunAsNonRoot=false is not allowed in rootless mode")
	}

	return nil
}

// EnforceSecurityContext applies security context settings to the current process
func (scv *SecurityContextValidator) EnforceSecurityContext(ctx *types.SecurityContext) error {
	if ctx == nil {
		return nil
	}

	// Get current user info
	currentUser, err := user.Current()
	if err != nil {
		return fmt.Errorf("failed to get current user: %v", err)
	}

	currentUID, _ := strconv.Atoi(currentUser.Uid)
	currentGID, _ := strconv.Atoi(currentUser.Gid)

	// Validate that we're not running as root
	if currentUID == 0 {
		return fmt.Errorf("cannot enforce security context: running as root")
	}

	// Check if requested UID/GID matches current user (we can't change in rootless mode)
	if ctx.RunAsUser != nil {
		if int64(currentUID) != *ctx.RunAsUser {
			return fmt.Errorf("cannot change UID in rootless mode: current=%d, requested=%d", currentUID, *ctx.RunAsUser)
		}
	}

	if ctx.RunAsGroup != nil {
		if int64(currentGID) != *ctx.RunAsGroup {
			return fmt.Errorf("cannot change GID in rootless mode: current=%d, requested=%d", currentGID, *ctx.RunAsGroup)
		}
	}

	// Capabilities are handled by the container runtime in rootless mode
	// We can only validate that they're safe, not actually set them

	return nil
}

// GetDefaultSecurityContext returns a secure default security context
func (scv *SecurityContextValidator) GetDefaultSecurityContext() *types.SecurityContext {
	currentUser, err := user.Current()
	if err != nil {
		// Fallback to safe defaults
		return &types.SecurityContext{
			RunAsUser:    int64Ptr(1000),
			RunAsGroup:   int64Ptr(1000),
			RunAsNonRoot: boolPtr(true),
			Capabilities: []string{}, // No capabilities by default
		}
	}

	uid, _ := strconv.ParseInt(currentUser.Uid, 10, 64)
	gid, _ := strconv.ParseInt(currentUser.Gid, 10, 64)

	// Ensure we don't default to root
	if uid == 0 {
		uid = 1000
	}
	if gid == 0 {
		gid = 1000
	}

	return &types.SecurityContext{
		RunAsUser:    &uid,
		RunAsGroup:   &gid,
		RunAsNonRoot: boolPtr(true),
		Capabilities: []string{}, // No capabilities by default
	}
}

// SecurityContextEnforcer handles runtime enforcement of security contexts
type SecurityContextEnforcer struct {
	validator *SecurityContextValidator
}

// NewSecurityContextEnforcer creates a new security context enforcer
func NewSecurityContextEnforcer() *SecurityContextEnforcer {
	return &SecurityContextEnforcer{
		validator: NewSecurityContextValidator(),
	}
}

// ValidateAndEnforce validates and enforces a security context
func (sce *SecurityContextEnforcer) ValidateAndEnforce(ctx *types.SecurityContext) error {
	// First validate the context
	if err := sce.validator.ValidateSecurityContext(ctx); err != nil {
		return fmt.Errorf("security context validation failed: %v", err)
	}

	// Then enforce it
	if err := sce.validator.EnforceSecurityContext(ctx); err != nil {
		return fmt.Errorf("security context enforcement failed: %v", err)
	}

	return nil
}

// CheckCurrentSecurityContext checks if the current process meets security requirements
func (sce *SecurityContextEnforcer) CheckCurrentSecurityContext() error {
	// Check that we're not running as root
	if os.Getuid() == 0 {
		return fmt.Errorf("process is running as root, which is not allowed")
	}

	if os.Getgid() == 0 {
		return fmt.Errorf("process is running with root group, which is not allowed")
	}

	// Check for dangerous capabilities (if we can)
	// Note: In rootless mode, we typically don't have access to check capabilities
	// This is more of a documentation of what we would check

	return nil
}

// GetProcessSecurityInfo returns information about the current process security context
func (sce *SecurityContextEnforcer) GetProcessSecurityInfo() map[string]interface{} {
	info := map[string]interface{}{
		"uid":  os.Getuid(),
		"gid":  os.Getgid(),
		"euid": os.Geteuid(),
		"egid": os.Getegid(),
	}

	// Get user info
	if currentUser, err := user.Current(); err == nil {
		info["username"] = currentUser.Username
		info["home_dir"] = currentUser.HomeDir
	}

	// Get group info
	if groups, err := os.Getgroups(); err == nil {
		info["groups"] = groups
	}

	// Check if running in user namespace
	if _, err := os.Stat("/proc/self/ns/user"); err == nil {
		info["user_namespace"] = true
	} else {
		info["user_namespace"] = false
	}

	return info
}

// ResourceLimitValidator validates and enforces resource limits
type ResourceLimitValidator struct {
	maxMemoryBytes int64
	maxCPUCores    float64
	maxDiskBytes   int64
}

// NewResourceLimitValidator creates a new resource limit validator
func NewResourceLimitValidator() *ResourceLimitValidator {
	return &ResourceLimitValidator{
		maxMemoryBytes: 8 * 1024 * 1024 * 1024, // 8GB
		maxCPUCores:    4.0,                     // 4 CPU cores
		maxDiskBytes:   50 * 1024 * 1024 * 1024, // 50GB
	}
}

// ValidateResourceLimits validates resource limits for security
func (rlv *ResourceLimitValidator) ValidateResourceLimits(limits *types.ResourceLimits) error {
	if limits == nil {
		return nil
	}

	// Validate memory limit
	if limits.Memory != "" {
		memoryBytes, err := parseMemoryToBytes(limits.Memory)
		if err != nil {
			return fmt.Errorf("invalid memory limit format: %v", err)
		}

		if memoryBytes > rlv.maxMemoryBytes {
			return fmt.Errorf("memory limit too high: %d bytes (max %d bytes)", memoryBytes, rlv.maxMemoryBytes)
		}

		if memoryBytes < 64*1024*1024 { // 64MB minimum
			return fmt.Errorf("memory limit too low: %d bytes (min 64MB)", memoryBytes)
		}
	}

	// Validate CPU limit
	if limits.CPU != "" {
		cpuCores, err := parseCPUToCores(limits.CPU)
		if err != nil {
			return fmt.Errorf("invalid CPU limit format: %v", err)
		}

		if cpuCores > rlv.maxCPUCores {
			return fmt.Errorf("CPU limit too high: %.2f cores (max %.2f cores)", cpuCores, rlv.maxCPUCores)
		}

		if cpuCores < 0.1 {
			return fmt.Errorf("CPU limit too low: %.2f cores (min 0.1 cores)", cpuCores)
		}
	}

	// Validate disk limit
	if limits.Disk != "" {
		diskBytes, err := parseMemoryToBytes(limits.Disk) // Same format as memory
		if err != nil {
			return fmt.Errorf("invalid disk limit format: %v", err)
		}

		if diskBytes > rlv.maxDiskBytes {
			return fmt.Errorf("disk limit too high: %d bytes (max %d bytes)", diskBytes, rlv.maxDiskBytes)
		}

		if diskBytes < 1024*1024*1024 { // 1GB minimum
			return fmt.Errorf("disk limit too low: %d bytes (min 1GB)", diskBytes)
		}
	}

	return nil
}

// parseMemoryToBytes parses memory strings like "1Gi", "512Mi", "1024Ki" to bytes
func parseMemoryToBytes(memory string) (int64, error) {
	if memory == "" {
		return 0, fmt.Errorf("empty memory string")
	}

	// Handle different suffixes
	suffixes := map[string]int64{
		"Ki": 1024,
		"Mi": 1024 * 1024,
		"Gi": 1024 * 1024 * 1024,
		"Ti": 1024 * 1024 * 1024 * 1024,
		"K":  1000,
		"M":  1000 * 1000,
		"G":  1000 * 1000 * 1000,
		"T":  1000 * 1000 * 1000 * 1000,
	}

	for suffix, multiplier := range suffixes {
		if strings.HasSuffix(memory, suffix) {
			valueStr := strings.TrimSuffix(memory, suffix)
			value, err := strconv.ParseFloat(valueStr, 64)
			if err != nil {
				return 0, fmt.Errorf("invalid numeric value: %s", valueStr)
			}
			return int64(value * float64(multiplier)), nil
		}
	}

	// No suffix, assume bytes
	value, err := strconv.ParseInt(memory, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid numeric value: %s", memory)
	}

	return value, nil
}

// parseCPUToCores parses CPU strings like "1000m", "0.5", "2" to CPU cores
func parseCPUToCores(cpu string) (float64, error) {
	if cpu == "" {
		return 0, fmt.Errorf("empty CPU string")
	}

	// Handle millicores (e.g., "1000m" = 1 core)
	if strings.HasSuffix(cpu, "m") {
		milliStr := strings.TrimSuffix(cpu, "m")
		milli, err := strconv.ParseFloat(milliStr, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid millicores value: %s", milliStr)
		}
		return milli / 1000.0, nil
	}

	// Handle direct core count
	cores, err := strconv.ParseFloat(cpu, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid CPU cores value: %s", cpu)
	}

	return cores, nil
}

// Helper functions for pointer types
func int64Ptr(i int64) *int64 {
	return &i
}

func boolPtr(b bool) *bool {
	return &b
}