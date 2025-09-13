package security

import (
	"crypto/rand"
	"crypto/subtle"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// SecretManager handles secure storage and management of secrets
type SecretManager struct {
	secrets map[string]*SecureString
	mutex   sync.RWMutex
}

// SecureString represents a string stored in secure memory
type SecureString struct {
	data     []byte
	length   int
	locked   bool
	accessed time.Time
	created  time.Time
}

// NewSecretManager creates a new secret manager
func NewSecretManager() *SecretManager {
	return &SecretManager{
		secrets: make(map[string]*SecureString),
	}
}

// StoreSecret stores a secret securely in memory
func (sm *SecretManager) StoreSecret(key, value string) error {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	// Validate key
	if err := validateSecretKey(key); err != nil {
		return fmt.Errorf("invalid secret key: %v", err)
	}

	// Create secure string
	secureStr, err := NewSecureString(value)
	if err != nil {
		return fmt.Errorf("failed to create secure string: %v", err)
	}

	// Clean up existing secret if it exists
	if existing, exists := sm.secrets[key]; exists {
		existing.Destroy()
	}

	sm.secrets[key] = secureStr
	return nil
}

// GetSecret retrieves a secret from secure memory
func (sm *SecretManager) GetSecret(key string) (string, error) {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	secureStr, exists := sm.secrets[key]
	if !exists {
		return "", fmt.Errorf("secret not found: %s", key)
	}

	return secureStr.String()
}

// HasSecret checks if a secret exists
func (sm *SecretManager) HasSecret(key string) bool {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	_, exists := sm.secrets[key]
	return exists
}

// RemoveSecret removes a secret from memory
func (sm *SecretManager) RemoveSecret(key string) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	if secureStr, exists := sm.secrets[key]; exists {
		secureStr.Destroy()
		delete(sm.secrets, key)
	}
}

// ListSecrets returns a list of secret keys (not values)
func (sm *SecretManager) ListSecrets() []string {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	keys := make([]string, 0, len(sm.secrets))
	for key := range sm.secrets {
		keys = append(keys, key)
	}
	return keys
}

// Cleanup destroys all secrets and clears memory
func (sm *SecretManager) Cleanup() {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	for key, secureStr := range sm.secrets {
		secureStr.Destroy()
		delete(sm.secrets, key)
	}
}

// RotateSecrets removes secrets older than the specified duration
func (sm *SecretManager) RotateSecrets(maxAge time.Duration) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	now := time.Now()
	for key, secureStr := range sm.secrets {
		if now.Sub(secureStr.created) > maxAge {
			secureStr.Destroy()
			delete(sm.secrets, key)
		}
	}
}

// GetSecretStats returns statistics about stored secrets
func (sm *SecretManager) GetSecretStats() map[string]interface{} {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	stats := map[string]interface{}{
		"total_secrets": len(sm.secrets),
		"locked_secrets": 0,
		"oldest_secret": time.Time{},
		"newest_secret": time.Time{},
	}

	var oldest, newest time.Time
	lockedCount := 0

	for _, secureStr := range sm.secrets {
		if secureStr.locked {
			lockedCount++
		}

		if oldest.IsZero() || secureStr.created.Before(oldest) {
			oldest = secureStr.created
		}

		if newest.IsZero() || secureStr.created.After(newest) {
			newest = secureStr.created
		}
	}

	stats["locked_secrets"] = lockedCount
	if !oldest.IsZero() {
		stats["oldest_secret"] = oldest
	}
	if !newest.IsZero() {
		stats["newest_secret"] = newest
	}

	return stats
}

// NewSecureString creates a new secure string
func NewSecureString(value string) (*SecureString, error) {
	if len(value) == 0 {
		return nil, fmt.Errorf("empty value")
	}

	if len(value) > 64*1024 {
		return nil, fmt.Errorf("value too large: %d bytes (max 64KB)", len(value))
	}

	// Allocate secure memory
	data := make([]byte, len(value))
	copy(data, []byte(value))

	ss := &SecureString{
		data:     data,
		length:   len(value),
		created:  time.Now(),
		accessed: time.Now(),
	}

	// Try to lock memory to prevent swapping
	if err := ss.lockMemory(); err != nil {
		// Log warning but don't fail - memory locking might not be available
		fmt.Printf("Warning: failed to lock secret memory: %v\n", err)
	}

	return ss, nil
}

// String returns the string value (updates access time)
func (ss *SecureString) String() (string, error) {
	if ss.data == nil {
		return "", fmt.Errorf("secure string has been destroyed")
	}

	ss.accessed = time.Now()
	return string(ss.data), nil
}

// Bytes returns the byte slice (updates access time)
func (ss *SecureString) Bytes() ([]byte, error) {
	if ss.data == nil {
		return nil, fmt.Errorf("secure string has been destroyed")
	}

	ss.accessed = time.Now()
	result := make([]byte, len(ss.data))
	copy(result, ss.data)
	return result, nil
}

// Length returns the length of the secret
func (ss *SecureString) Length() int {
	return ss.length
}

// IsDestroyed checks if the secure string has been destroyed
func (ss *SecureString) IsDestroyed() bool {
	return ss.data == nil
}

// Destroy securely wipes the secret from memory
func (ss *SecureString) Destroy() {
	if ss.data == nil {
		return
	}

	// Overwrite with random data multiple times
	ss.secureWipe()

	// Unlock memory if it was locked
	if ss.locked {
		ss.unlockMemory()
	}

	// Clear the slice
	ss.data = nil
	ss.length = 0
}

// lockMemory attempts to lock the memory to prevent swapping
func (ss *SecureString) lockMemory() error {
	if len(ss.data) == 0 {
		return nil
	}

	// Try to lock memory (Unix-like systems)
	if runtime.GOOS != "windows" {
		ptr := uintptr(unsafe.Pointer(&ss.data[0]))
		size := uintptr(len(ss.data))

		if _, _, err := syscall.Syscall(syscall.SYS_MLOCK, ptr, size, 0); err != 0 {
			return fmt.Errorf("mlock failed: %v", err)
		}

		ss.locked = true
	}

	return nil
}

// unlockMemory unlocks the memory
func (ss *SecureString) unlockMemory() {
	if !ss.locked || len(ss.data) == 0 {
		return
	}

	if runtime.GOOS != "windows" {
		ptr := uintptr(unsafe.Pointer(&ss.data[0]))
		size := uintptr(len(ss.data))
		syscall.Syscall(syscall.SYS_MUNLOCK, ptr, size, 0)
	}

	ss.locked = false
}

// secureWipe overwrites memory with random data multiple times
func (ss *SecureString) secureWipe() {
	if len(ss.data) == 0 {
		return
	}

	// Multiple passes with different patterns
	patterns := []func([]byte){
		func(b []byte) { // Random data
			rand.Read(b)
		},
		func(b []byte) { // All zeros
			for i := range b {
				b[i] = 0x00
			}
		},
		func(b []byte) { // All ones
			for i := range b {
				b[i] = 0xFF
			}
		},
		func(b []byte) { // Alternating pattern
			for i := range b {
				b[i] = 0xAA
			}
		},
		func(b []byte) { // Inverse alternating pattern
			for i := range b {
				b[i] = 0x55
			}
		},
		func(b []byte) { // Final random pass
			rand.Read(b)
		},
	}

	for _, pattern := range patterns {
		pattern(ss.data)
		runtime.KeepAlive(ss.data) // Prevent optimization
	}
}

// CompareSecure performs constant-time comparison of secrets
func (ss *SecureString) CompareSecure(other string) bool {
	if ss.data == nil {
		return false
	}

	ss.accessed = time.Now()
	return subtle.ConstantTimeCompare(ss.data, []byte(other)) == 1
}

// validateSecretKey validates a secret key
func validateSecretKey(key string) error {
	if len(key) == 0 {
		return fmt.Errorf("empty key")
	}

	if len(key) > 256 {
		return fmt.Errorf("key too long: %d chars (max 256)", len(key))
	}

	// Check for valid characters
	for _, r := range key {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || 
			 (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.') {
			return fmt.Errorf("invalid character in key: %c", r)
		}
	}

	return nil
}

// SecretLoader handles loading secrets from various sources
type SecretLoader struct {
	manager *SecretManager
}

// NewSecretLoader creates a new secret loader
func NewSecretLoader(manager *SecretManager) *SecretLoader {
	return &SecretLoader{
		manager: manager,
	}
}

// LoadFromEnvironment loads secrets from environment variables
func (sl *SecretLoader) LoadFromEnvironment(prefix string) error {
	for _, env := range os.Environ() {
		if !strings.HasPrefix(env, prefix) {
			continue
		}

		parts := strings.SplitN(env, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimPrefix(parts[0], prefix)
		value := parts[1]

		if err := sl.manager.StoreSecret(key, value); err != nil {
			return fmt.Errorf("failed to store secret %s: %v", key, err)
		}
	}

	return nil
}

// LoadFromFile loads a secret from a file
func (sl *SecretLoader) LoadFromFile(key, filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read secret file %s: %v", filePath, err)
	}

	// Remove trailing newline if present
	value := strings.TrimRight(string(data), "\n\r")

	return sl.manager.StoreSecret(key, value)
}

// LoadFromKubernetesSecret loads secrets from Kubernetes secret mount
func (sl *SecretLoader) LoadFromKubernetesSecret(secretPath string) error {
	entries, err := os.ReadDir(secretPath)
	if err != nil {
		return fmt.Errorf("failed to read secret directory %s: %v", secretPath, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		key := entry.Name()
		filePath := filepath.Join(secretPath, key)

		if err := sl.LoadFromFile(key, filePath); err != nil {
			return fmt.Errorf("failed to load secret %s: %v", key, err)
		}
	}

	return nil
}

// SecretInjector handles secure injection of secrets into operations
type SecretInjector struct {
	manager *SecretManager
}

// NewSecretInjector creates a new secret injector
func NewSecretInjector(manager *SecretManager) *SecretInjector {
	return &SecretInjector{
		manager: manager,
	}
}

// InjectSecrets replaces secret placeholders in strings with actual values
func (si *SecretInjector) InjectSecrets(input string) (string, error) {
	// Find secret placeholders in format ${SECRET_NAME}
	secretPattern := regexp.MustCompile(`\$\{([A-Za-z0-9_.-]+)\}`)
	
	result := secretPattern.ReplaceAllStringFunc(input, func(match string) string {
		// Extract secret name
		secretName := match[2 : len(match)-1] // Remove ${ and }
		
		// Get secret value
		value, err := si.manager.GetSecret(secretName)
		if err != nil {
			// Return placeholder if secret not found (don't expose error)
			return match
		}
		
		return value
	})

	return result, nil
}

// InjectSecretsIntoEnvironment injects secrets into environment variables
func (si *SecretInjector) InjectSecretsIntoEnvironment(env map[string]string) (map[string]string, error) {
	result := make(map[string]string)
	
	for key, value := range env {
		injectedValue, err := si.InjectSecrets(value)
		if err != nil {
			return nil, fmt.Errorf("failed to inject secrets into %s: %v", key, err)
		}
		result[key] = injectedValue
	}
	
	return result, nil
}

// Global secret manager instance
var globalSecretManager *SecretManager
var secretManagerOnce sync.Once

// GetGlobalSecretManager returns the global secret manager instance
func GetGlobalSecretManager() *SecretManager {
	secretManagerOnce.Do(func() {
		globalSecretManager = NewSecretManager()
		
		// Set up cleanup on program exit
		runtime.SetFinalizer(globalSecretManager, (*SecretManager).Cleanup)
	})
	
	return globalSecretManager
}