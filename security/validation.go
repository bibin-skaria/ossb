package security

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/bibin-skaria/ossb/internal/types"
)

// InputValidator provides comprehensive input validation for all user-provided data
type InputValidator struct {
	maxDockerfileSize int64
	maxBuildArgSize   int
	maxBuildArgCount  int
	allowedCommands   map[string]bool
	blockedPatterns   []*regexp.Regexp
}

// NewInputValidator creates a new input validator with default security settings
func NewInputValidator() *InputValidator {
	// Compile blocked patterns for security
	blockedPatterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)(rm\s+-rf\s+/)`),                    // Dangerous rm commands
		regexp.MustCompile(`(?i)(chmod\s+777)`),                     // Overly permissive chmod
		regexp.MustCompile(`(?i)(curl.*\|\s*sh)`),                   // Pipe to shell
		regexp.MustCompile(`(?i)(wget.*\|\s*sh)`),                   // Pipe to shell
		regexp.MustCompile(`(?i)(eval\s+\$)`),                       // Dynamic evaluation
		regexp.MustCompile(`(?i)(\$\(.*\))`),                        // Command substitution in dangerous contexts
		regexp.MustCompile(`(?i)(nc\s+.*-e)`),                       // Netcat with execute
		regexp.MustCompile(`(?i)(python.*-c.*os\.system)`),          // Python system calls
		regexp.MustCompile(`(?i)(perl.*-e.*system)`),                // Perl system calls
		regexp.MustCompile(`(?i)(ruby.*-e.*system)`),                // Ruby system calls
		regexp.MustCompile(`(?i)(node.*-e.*child_process)`),         // Node.js child process
		regexp.MustCompile(`(?i)(bash.*-c.*\$\{)`),                  // Bash variable expansion
		regexp.MustCompile(`(?i)(sh.*-c.*\$\{)`),                    // Shell variable expansion
		regexp.MustCompile(`(?i)(/proc/.*mem)`),                     // Memory access
		regexp.MustCompile(`(?i)(/dev/.*)`),                         // Device access
		regexp.MustCompile(`(?i)(mount.*--bind)`),                   // Bind mounts
		regexp.MustCompile(`(?i)(chroot)`),                          // Chroot operations
		regexp.MustCompile(`(?i)(sudo|su\s)`),                       // Privilege escalation
		regexp.MustCompile(`(?i)(setuid|setgid)`),                   // Set UID/GID
		regexp.MustCompile(`(?i)(iptables|ip6tables)`),              // Firewall manipulation
		regexp.MustCompile(`(?i)(modprobe|insmod|rmmod)`),           // Kernel module manipulation
		regexp.MustCompile(`(?i)(sysctl)`),                          // System configuration
		regexp.MustCompile(`(?i)(systemctl|service)`),               // System service control
		regexp.MustCompile(`(?i)(crontab)`),                         // Cron manipulation
		regexp.MustCompile(`(?i)(passwd|shadow)`),                   // Password file access
		regexp.MustCompile(`(?i)(ssh-keygen.*-f\s*/)`),              // SSH key generation in root
		regexp.MustCompile(`(?i)(openssl.*-out\s*/)`),               // SSL cert generation in root
	}

	allowedCommands := map[string]bool{
		"FROM":        true,
		"RUN":         true,
		"CMD":         true,
		"LABEL":       true,
		"EXPOSE":      true,
		"ENV":         true,
		"ADD":         true,
		"COPY":        true,
		"ENTRYPOINT":  true,
		"VOLUME":      true,
		"USER":        true,
		"WORKDIR":     true,
		"ARG":         true,
		"ONBUILD":     true,
		"STOPSIGNAL":  true,
		"HEALTHCHECK": true,
		"SHELL":       true,
	}

	return &InputValidator{
		maxDockerfileSize: 10 * 1024 * 1024, // 10MB
		maxBuildArgSize:   1024,              // 1KB per build arg
		maxBuildArgCount:  100,               // Max 100 build args
		allowedCommands:   allowedCommands,
		blockedPatterns:   blockedPatterns,
	}
}

// ValidateDockerfile validates a Dockerfile for security issues
func (v *InputValidator) ValidateDockerfile(dockerfilePath string) error {
	// Check file size
	info, err := os.Stat(dockerfilePath)
	if err != nil {
		return fmt.Errorf("failed to stat Dockerfile: %v", err)
	}

	if info.Size() > v.maxDockerfileSize {
		return fmt.Errorf("Dockerfile too large: %d bytes (max %d bytes)", info.Size(), v.maxDockerfileSize)
	}

	// Read and validate content
	file, err := os.Open(dockerfilePath)
	if err != nil {
		return fmt.Errorf("failed to open Dockerfile: %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineNum := 0
	var errors []string

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Validate line length
		if len(line) > 8192 {
			errors = append(errors, fmt.Sprintf("line %d: line too long (%d chars, max 8192)", lineNum, len(line)))
			continue
		}

		// Validate command
		if err := v.validateDockerfileLine(line, lineNum); err != nil {
			errors = append(errors, err.Error())
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading Dockerfile: %v", err)
	}

	if len(errors) > 0 {
		return fmt.Errorf("Dockerfile validation failed:\n%s", strings.Join(errors, "\n"))
	}

	return nil
}

// validateDockerfileLine validates a single Dockerfile line
func (v *InputValidator) validateDockerfileLine(line string, lineNum int) error {
	// Extract command
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return nil
	}

	command := strings.ToUpper(parts[0])

	// Check if command is allowed
	if !v.allowedCommands[command] {
		return fmt.Errorf("line %d: unknown or disallowed command: %s", lineNum, command)
	}

	// Special validation for specific commands
	switch command {
	case "FROM":
		return v.validateFromInstruction(line, lineNum)
	case "RUN":
		return v.validateRunInstruction(line, lineNum)
	case "USER":
		return v.validateUserInstruction(line, lineNum)
	case "ADD", "COPY":
		return v.validateCopyInstruction(line, lineNum)
	case "ENV", "ARG":
		return v.validateEnvInstruction(line, lineNum)
	}

	return nil
}

// validateFromInstruction validates FROM instructions
func (v *InputValidator) validateFromInstruction(line string, lineNum int) error {
	// Extract image reference
	parts := strings.Fields(line)
	if len(parts) < 2 {
		return fmt.Errorf("line %d: FROM instruction missing image", lineNum)
	}

	imageRef := parts[1]

	// Validate image reference format
	if err := v.validateImageReference(imageRef); err != nil {
		return fmt.Errorf("line %d: invalid image reference: %v", lineNum, err)
	}

	return nil
}

// validateRunInstruction validates RUN instructions for security issues
func (v *InputValidator) validateRunInstruction(line string, lineNum int) error {
	// Check for blocked patterns
	for _, pattern := range v.blockedPatterns {
		if pattern.MatchString(line) {
			return fmt.Errorf("line %d: potentially dangerous command pattern detected", lineNum)
		}
	}

	return nil
}

// validateUserInstruction validates USER instructions
func (v *InputValidator) validateUserInstruction(line string, lineNum int) error {
	parts := strings.Fields(line)
	if len(parts) < 2 {
		return fmt.Errorf("line %d: USER instruction missing user specification", lineNum)
	}

	userSpec := parts[1]

	// Check for root user
	if userSpec == "root" || userSpec == "0" || userSpec == "0:0" {
		return fmt.Errorf("line %d: running as root user is not allowed in secure builds", lineNum)
	}

	return nil
}

// validateCopyInstruction validates COPY/ADD instructions
func (v *InputValidator) validateCopyInstruction(line string, lineNum int) error {
	// Check for dangerous source patterns
	if strings.Contains(line, "../") {
		return fmt.Errorf("line %d: path traversal detected in COPY/ADD instruction", lineNum)
	}

	return nil
}

// validateEnvInstruction validates ENV/ARG instructions
func (v *InputValidator) validateEnvInstruction(line string, lineNum int) error {
	// Check for sensitive environment variable names
	sensitiveVars := []string{
		"PASSWORD", "PASSWD", "SECRET", "TOKEN", "KEY", "API_KEY",
	}

	upperLine := strings.ToUpper(line)
	for _, sensitiveVar := range sensitiveVars {
		if strings.Contains(upperLine, sensitiveVar) {
			return fmt.Errorf("line %d: potentially sensitive environment variable: %s", lineNum, sensitiveVar)
		}
	}

	return nil
}

// ValidateBuildArgs validates build arguments for security issues
func (v *InputValidator) ValidateBuildArgs(buildArgs map[string]string) error {
	if len(buildArgs) > v.maxBuildArgCount {
		return fmt.Errorf("too many build arguments: %d (max %d)", len(buildArgs), v.maxBuildArgCount)
	}

	for key, value := range buildArgs {
		// Validate key
		if err := v.validateBuildArgKey(key); err != nil {
			return fmt.Errorf("invalid build arg key %q: %v", key, err)
		}

		// Validate value
		if err := v.validateBuildArgValue(key, value); err != nil {
			return fmt.Errorf("invalid build arg value for %q: %v", key, err)
		}
	}

	return nil
}

// validateBuildArgKey validates a build argument key
func (v *InputValidator) validateBuildArgKey(key string) error {
	if len(key) == 0 {
		return fmt.Errorf("empty key")
	}

	if len(key) > 128 {
		return fmt.Errorf("key too long: %d chars (max 128)", len(key))
	}

	// Check for valid characters (alphanumeric, underscore, dash)
	for _, r := range key {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-' {
			return fmt.Errorf("invalid character in key: %c", r)
		}
	}

	return nil
}

// validateBuildArgValue validates a build argument value
func (v *InputValidator) validateBuildArgValue(key, value string) error {
	if len(value) > v.maxBuildArgSize {
		return fmt.Errorf("value too long: %d chars (max %d)", len(value), v.maxBuildArgSize)
	}

	// Check for command injection patterns
	dangerousPatterns := []string{
		"$(", "`", ";", "&&", "||", "|", ">", "<", "&",
	}

	for _, pattern := range dangerousPatterns {
		if strings.Contains(value, pattern) {
			return fmt.Errorf("potentially dangerous pattern in value: %s", pattern)
		}
	}

	return nil
}

// validateImageReference validates container image references
func (v *InputValidator) validateImageReference(imageRef string) error {
	if len(imageRef) == 0 {
		return fmt.Errorf("empty image reference")
	}

	if len(imageRef) > 512 {
		return fmt.Errorf("image reference too long: %d chars (max 512)", len(imageRef))
	}

	return nil
}

// ValidateOperation validates an operation for security issues
func (v *InputValidator) ValidateOperation(operation *types.Operation) error {
	if operation == nil {
		return fmt.Errorf("nil operation")
	}

	// Validate operation type
	validTypes := map[types.OperationType]bool{
		types.OperationTypeSource:   true,
		types.OperationTypeExec:     true,
		types.OperationTypeFile:     true,
		types.OperationTypeMeta:     true,
		types.OperationTypePull:     true,
		types.OperationTypeExtract:  true,
		types.OperationTypeLayer:    true,
		types.OperationTypeManifest: true,
		types.OperationTypePush:     true,
	}

	if !validTypes[operation.Type] {
		return fmt.Errorf("invalid operation type: %s", operation.Type)
	}

	return nil
}

// containsSensitiveData checks if a value contains potentially sensitive data
func (v *InputValidator) containsSensitiveData(value string) bool {
	// Check for common secret patterns
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)^[A-Za-z0-9+/]{40,}={0,2}$`),                    // Base64 encoded secrets
		regexp.MustCompile(`(?i)^[a-f0-9]{32,}$`),                               // Hex encoded secrets
		regexp.MustCompile(`(?i)^sk-[a-zA-Z0-9]{48}$`),                          // OpenAI API keys
		regexp.MustCompile(`(?i)^ghp_[a-zA-Z0-9]{36}$`),                         // GitHub personal access tokens
		regexp.MustCompile(`(?i)^AKIA[0-9A-Z]{16}$`),                            // AWS access keys
		regexp.MustCompile(`(?i)^[a-zA-Z0-9+/]{40}$`),                           // AWS secret keys
	}

	for _, pattern := range patterns {
		if pattern.MatchString(value) {
			return true
		}
	}

	return false
}

// validatePath validates file/directory paths
func (v *InputValidator) validatePath(path string) error {
	if len(path) > 4096 {
		return fmt.Errorf("path too long: %d chars (max 4096)", len(path))
	}

	// Check for path traversal
	if strings.Contains(path, "../") || strings.Contains(path, "..\\") {
		return fmt.Errorf("path traversal detected: %s", path)
	}

	// Check for null bytes
	if strings.Contains(path, "\x00") {
		return fmt.Errorf("null byte in path")
	}

	// Check for sensitive paths
	sensitivePaths := []string{
		"/etc/passwd", "/etc/shadow", "/etc/hosts",
		"/proc/", "/sys/", "/dev/",
		"/root/", "/home/root/",
		"/.ssh/", "/.aws/", "/.docker/",
	}

	for _, sensitivePath := range sensitivePaths {
		if strings.HasPrefix(path, sensitivePath) {
			return fmt.Errorf("access to sensitive path not allowed: %s", sensitivePath)
		}
	}

	return nil
}

// isNumeric checks if a string represents a numeric value
func isNumeric(s string) bool {
	_, err := strconv.Atoi(s)
	return err == nil
}