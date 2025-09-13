package security

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSecretManager_StoreAndRetrieve(t *testing.T) {
	sm := NewSecretManager()
	defer sm.Cleanup()

	tests := []struct {
		name        string
		key         string
		value       string
		expectError bool
	}{
		{
			name:        "valid_secret",
			key:         "test-secret",
			value:       "secret-value-123",
			expectError: false,
		},
		{
			name:        "empty_key",
			key:         "",
			value:       "secret-value",
			expectError: true,
		},
		{
			name:        "key_too_long",
			key:         strings.Repeat("a", 257),
			value:       "secret-value",
			expectError: true,
		},
		{
			name:        "invalid_key_chars",
			key:         "invalid@key",
			value:       "secret-value",
			expectError: true,
		},
		{
			name:        "valid_key_with_allowed_chars",
			key:         "valid-key_123.test",
			value:       "secret-value",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := sm.StoreSecret(tt.key, tt.value)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				} else {
					// Test retrieval
					retrieved, err := sm.GetSecret(tt.key)
					if err != nil {
						t.Errorf("Failed to retrieve secret: %v", err)
					} else if retrieved != tt.value {
						t.Errorf("Retrieved value %q doesn't match stored value %q", retrieved, tt.value)
					}

					// Test existence check
					if !sm.HasSecret(tt.key) {
						t.Errorf("HasSecret returned false for existing secret")
					}
				}
			}
		})
	}
}

func TestSecretManager_RemoveSecret(t *testing.T) {
	sm := NewSecretManager()
	defer sm.Cleanup()

	key := "test-secret"
	value := "secret-value"

	// Store secret
	if err := sm.StoreSecret(key, value); err != nil {
		t.Fatalf("Failed to store secret: %v", err)
	}

	// Verify it exists
	if !sm.HasSecret(key) {
		t.Fatal("Secret should exist before removal")
	}

	// Remove secret
	sm.RemoveSecret(key)

	// Verify it's gone
	if sm.HasSecret(key) {
		t.Error("Secret should not exist after removal")
	}

	// Try to retrieve (should fail)
	_, err := sm.GetSecret(key)
	if err == nil {
		t.Error("Expected error when retrieving removed secret")
	}
}

func TestSecretManager_ListSecrets(t *testing.T) {
	sm := NewSecretManager()
	defer sm.Cleanup()

	secrets := map[string]string{
		"secret1": "value1",
		"secret2": "value2",
		"secret3": "value3",
	}

	// Store secrets
	for key, value := range secrets {
		if err := sm.StoreSecret(key, value); err != nil {
			t.Fatalf("Failed to store secret %s: %v", key, err)
		}
	}

	// List secrets
	keys := sm.ListSecrets()

	if len(keys) != len(secrets) {
		t.Errorf("Expected %d secrets, got %d", len(secrets), len(keys))
	}

	// Check all keys are present
	keyMap := make(map[string]bool)
	for _, key := range keys {
		keyMap[key] = true
	}

	for expectedKey := range secrets {
		if !keyMap[expectedKey] {
			t.Errorf("Expected key %s not found in list", expectedKey)
		}
	}
}

func TestSecretManager_RotateSecrets(t *testing.T) {
	sm := NewSecretManager()
	defer sm.Cleanup()

	// Store an old secret (simulate by creating and then waiting)
	oldKey := "old-secret"
	newKey := "new-secret"

	if err := sm.StoreSecret(oldKey, "old-value"); err != nil {
		t.Fatalf("Failed to store old secret: %v", err)
	}

	// Wait a bit and store new secret
	time.Sleep(10 * time.Millisecond)

	if err := sm.StoreSecret(newKey, "new-value"); err != nil {
		t.Fatalf("Failed to store new secret: %v", err)
	}

	// Rotate secrets older than 5ms (should remove old secret)
	sm.RotateSecrets(5 * time.Millisecond)

	// Old secret should be gone
	if sm.HasSecret(oldKey) {
		t.Error("Old secret should have been rotated out")
	}

	// New secret should still exist
	if !sm.HasSecret(newKey) {
		t.Error("New secret should still exist after rotation")
	}
}

func TestSecretManager_GetSecretStats(t *testing.T) {
	sm := NewSecretManager()
	defer sm.Cleanup()

	// Store some secrets
	secrets := map[string]string{
		"secret1": "value1",
		"secret2": "value2",
		"secret3": "value3",
	}

	for key, value := range secrets {
		if err := sm.StoreSecret(key, value); err != nil {
			t.Fatalf("Failed to store secret %s: %v", key, err)
		}
	}

	stats := sm.GetSecretStats()

	totalSecrets, ok := stats["total_secrets"].(int)
	if !ok || totalSecrets != len(secrets) {
		t.Errorf("Expected total_secrets to be %d, got %v", len(secrets), stats["total_secrets"])
	}

	if _, ok := stats["oldest_secret"].(time.Time); !ok {
		t.Error("Expected oldest_secret to be a time.Time")
	}

	if _, ok := stats["newest_secret"].(time.Time); !ok {
		t.Error("Expected newest_secret to be a time.Time")
	}
}

func TestSecureString_BasicOperations(t *testing.T) {
	value := "test-secret-value"
	ss, err := NewSecureString(value)
	if err != nil {
		t.Fatalf("Failed to create secure string: %v", err)
	}
	defer ss.Destroy()

	// Test String()
	retrieved, err := ss.String()
	if err != nil {
		t.Errorf("Failed to get string value: %v", err)
	} else if retrieved != value {
		t.Errorf("Retrieved value %q doesn't match original %q", retrieved, value)
	}

	// Test Bytes()
	retrievedBytes, err := ss.Bytes()
	if err != nil {
		t.Errorf("Failed to get bytes value: %v", err)
	} else if string(retrievedBytes) != value {
		t.Errorf("Retrieved bytes %q don't match original %q", string(retrievedBytes), value)
	}

	// Test Length()
	if ss.Length() != len(value) {
		t.Errorf("Length() returned %d, expected %d", ss.Length(), len(value))
	}

	// Test CompareSecure()
	if !ss.CompareSecure(value) {
		t.Error("CompareSecure should return true for matching value")
	}

	if ss.CompareSecure("different-value") {
		t.Error("CompareSecure should return false for different value")
	}

	// Test IsDestroyed() before destruction
	if ss.IsDestroyed() {
		t.Error("IsDestroyed should return false before destruction")
	}
}

func TestSecureString_Destroy(t *testing.T) {
	value := "test-secret-value"
	ss, err := NewSecureString(value)
	if err != nil {
		t.Fatalf("Failed to create secure string: %v", err)
	}

	// Destroy the secure string
	ss.Destroy()

	// Test IsDestroyed() after destruction
	if !ss.IsDestroyed() {
		t.Error("IsDestroyed should return true after destruction")
	}

	// Test that operations fail after destruction
	_, err = ss.String()
	if err == nil {
		t.Error("String() should fail after destruction")
	}

	_, err = ss.Bytes()
	if err == nil {
		t.Error("Bytes() should fail after destruction")
	}

	// CompareSecure should return false after destruction
	if ss.CompareSecure(value) {
		t.Error("CompareSecure should return false after destruction")
	}
}

func TestSecureString_EdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		value       string
		expectError bool
	}{
		{
			name:        "empty_value",
			value:       "",
			expectError: true,
		},
		{
			name:        "very_large_value",
			value:       strings.Repeat("a", 65*1024), // 65KB
			expectError: true,
		},
		{
			name:        "max_size_value",
			value:       strings.Repeat("a", 64*1024), // 64KB
			expectError: false,
		},
		{
			name:        "normal_value",
			value:       "normal-secret-value",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ss, err := NewSecureString(tt.value)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				if ss != nil {
					ss.Destroy()
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if ss != nil {
					defer ss.Destroy()
					
					retrieved, err := ss.String()
					if err != nil {
						t.Errorf("Failed to retrieve value: %v", err)
					} else if retrieved != tt.value {
						t.Errorf("Retrieved value doesn't match original")
					}
				}
			}
		})
	}
}

func TestSecretLoader_LoadFromEnvironment(t *testing.T) {
	sm := NewSecretManager()
	defer sm.Cleanup()

	loader := NewSecretLoader(sm)

	// Set test environment variables
	prefix := "TEST_SECRET_"
	testSecrets := map[string]string{
		"API_KEY": "test-api-key-123",
		"TOKEN":   "test-token-456",
		"CONFIG":  "test-config-789",
	}

	// Set environment variables
	for key, value := range testSecrets {
		envKey := prefix + key
		if err := os.Setenv(envKey, value); err != nil {
			t.Fatalf("Failed to set environment variable %s: %v", envKey, err)
		}
		defer os.Unsetenv(envKey)
	}

	// Load secrets from environment
	if err := loader.LoadFromEnvironment(prefix); err != nil {
		t.Fatalf("Failed to load secrets from environment: %v", err)
	}

	// Verify secrets were loaded
	for key, expectedValue := range testSecrets {
		if !sm.HasSecret(key) {
			t.Errorf("Secret %s was not loaded", key)
			continue
		}

		actualValue, err := sm.GetSecret(key)
		if err != nil {
			t.Errorf("Failed to retrieve secret %s: %v", key, err)
			continue
		}

		if actualValue != expectedValue {
			t.Errorf("Secret %s: expected %q, got %q", key, expectedValue, actualValue)
		}
	}
}

func TestSecretLoader_LoadFromFile(t *testing.T) {
	sm := NewSecretManager()
	defer sm.Cleanup()

	loader := NewSecretLoader(sm)

	// Create temporary secret file
	tmpDir := t.TempDir()
	secretFile := filepath.Join(tmpDir, "secret.txt")
	secretValue := "file-secret-value-123"

	if err := os.WriteFile(secretFile, []byte(secretValue+"\n"), 0600); err != nil {
		t.Fatalf("Failed to create secret file: %v", err)
	}

	// Load secret from file
	secretKey := "file-secret"
	if err := loader.LoadFromFile(secretKey, secretFile); err != nil {
		t.Fatalf("Failed to load secret from file: %v", err)
	}

	// Verify secret was loaded (trailing newline should be removed)
	if !sm.HasSecret(secretKey) {
		t.Fatal("Secret was not loaded from file")
	}

	actualValue, err := sm.GetSecret(secretKey)
	if err != nil {
		t.Fatalf("Failed to retrieve secret: %v", err)
	}

	if actualValue != secretValue {
		t.Errorf("Expected %q, got %q", secretValue, actualValue)
	}
}

func TestSecretLoader_LoadFromKubernetesSecret(t *testing.T) {
	sm := NewSecretManager()
	defer sm.Cleanup()

	loader := NewSecretLoader(sm)

	// Create temporary Kubernetes secret directory structure
	tmpDir := t.TempDir()
	secretDir := filepath.Join(tmpDir, "secrets")
	if err := os.MkdirAll(secretDir, 0755); err != nil {
		t.Fatalf("Failed to create secret directory: %v", err)
	}

	// Create secret files
	secrets := map[string]string{
		"username": "test-user",
		"password": "test-password-123",
		"token":    "test-token-456",
	}

	for key, value := range secrets {
		secretFile := filepath.Join(secretDir, key)
		if err := os.WriteFile(secretFile, []byte(value), 0600); err != nil {
			t.Fatalf("Failed to create secret file %s: %v", key, err)
		}
	}

	// Load secrets from Kubernetes secret directory
	if err := loader.LoadFromKubernetesSecret(secretDir); err != nil {
		t.Fatalf("Failed to load secrets from Kubernetes directory: %v", err)
	}

	// Verify all secrets were loaded
	for key, expectedValue := range secrets {
		if !sm.HasSecret(key) {
			t.Errorf("Secret %s was not loaded", key)
			continue
		}

		actualValue, err := sm.GetSecret(key)
		if err != nil {
			t.Errorf("Failed to retrieve secret %s: %v", key, err)
			continue
		}

		if actualValue != expectedValue {
			t.Errorf("Secret %s: expected %q, got %q", key, expectedValue, actualValue)
		}
	}
}

func TestSecretInjector_InjectSecrets(t *testing.T) {
	sm := NewSecretManager()
	defer sm.Cleanup()

	injector := NewSecretInjector(sm)

	// Store test secrets
	secrets := map[string]string{
		"API_KEY":  "secret-api-key-123",
		"PASSWORD": "secret-password-456",
		"TOKEN":    "secret-token-789",
	}

	for key, value := range secrets {
		if err := sm.StoreSecret(key, value); err != nil {
			t.Fatalf("Failed to store secret %s: %v", key, err)
		}
	}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "single_secret",
			input:    "API key is ${API_KEY}",
			expected: "API key is secret-api-key-123",
		},
		{
			name:     "multiple_secrets",
			input:    "User: ${PASSWORD}, Token: ${TOKEN}",
			expected: "User: secret-password-456, Token: secret-token-789",
		},
		{
			name:     "no_secrets",
			input:    "This has no secrets",
			expected: "This has no secrets",
		},
		{
			name:     "nonexistent_secret",
			input:    "Missing: ${NONEXISTENT}",
			expected: "Missing: ${NONEXISTENT}", // Should leave placeholder
		},
		{
			name:     "mixed_secrets",
			input:    "Valid: ${API_KEY}, Invalid: ${MISSING}",
			expected: "Valid: secret-api-key-123, Invalid: ${MISSING}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := injector.InjectSecrets(tt.input)
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			} else if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestSecretInjector_InjectSecretsIntoEnvironment(t *testing.T) {
	sm := NewSecretManager()
	defer sm.Cleanup()

	injector := NewSecretInjector(sm)

	// Store test secrets
	if err := sm.StoreSecret("DB_PASSWORD", "secret-db-pass"); err != nil {
		t.Fatalf("Failed to store secret: %v", err)
	}

	env := map[string]string{
		"DATABASE_URL": "postgres://user:${DB_PASSWORD}@localhost/db",
		"API_ENDPOINT": "https://api.example.com",
		"DEBUG":        "true",
	}

	result, err := injector.InjectSecretsIntoEnvironment(env)
	if err != nil {
		t.Fatalf("Failed to inject secrets: %v", err)
	}

	expected := map[string]string{
		"DATABASE_URL": "postgres://user:secret-db-pass@localhost/db",
		"API_ENDPOINT": "https://api.example.com",
		"DEBUG":        "true",
	}

	for key, expectedValue := range expected {
		if actualValue, ok := result[key]; !ok {
			t.Errorf("Missing key %s in result", key)
		} else if actualValue != expectedValue {
			t.Errorf("Key %s: expected %q, got %q", key, expectedValue, actualValue)
		}
	}
}

func TestGlobalSecretManager(t *testing.T) {
	// Get global instance
	sm1 := GetGlobalSecretManager()
	sm2 := GetGlobalSecretManager()

	// Should be the same instance
	if sm1 != sm2 {
		t.Error("GetGlobalSecretManager should return the same instance")
	}

	// Test basic functionality
	testKey := "global-test-secret"
	testValue := "global-test-value"

	if err := sm1.StoreSecret(testKey, testValue); err != nil {
		t.Fatalf("Failed to store secret in global manager: %v", err)
	}

	// Should be accessible from second reference
	if !sm2.HasSecret(testKey) {
		t.Error("Secret should be accessible from second global manager reference")
	}

	retrievedValue, err := sm2.GetSecret(testKey)
	if err != nil {
		t.Errorf("Failed to retrieve secret from second reference: %v", err)
	} else if retrievedValue != testValue {
		t.Errorf("Retrieved value %q doesn't match stored value %q", retrievedValue, testValue)
	}

	// Cleanup
	sm1.RemoveSecret(testKey)
}

func BenchmarkSecretManager_StoreSecret(b *testing.B) {
	sm := NewSecretManager()
	defer sm.Cleanup()

	value := "benchmark-secret-value-123456789"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("secret-%d", i)
		sm.StoreSecret(key, value)
	}
}

func BenchmarkSecretManager_GetSecret(b *testing.B) {
	sm := NewSecretManager()
	defer sm.Cleanup()

	key := "benchmark-secret"
	value := "benchmark-secret-value-123456789"

	if err := sm.StoreSecret(key, value); err != nil {
		b.Fatalf("Failed to store secret: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sm.GetSecret(key)
	}
}

func BenchmarkSecureString_String(b *testing.B) {
	value := "benchmark-secret-value-123456789"
	ss, err := NewSecureString(value)
	if err != nil {
		b.Fatalf("Failed to create secure string: %v", err)
	}
	defer ss.Destroy()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ss.String()
	}
}

func BenchmarkSecureString_CompareSecure(b *testing.B) {
	value := "benchmark-secret-value-123456789"
	ss, err := NewSecureString(value)
	if err != nil {
		b.Fatalf("Failed to create secure string: %v", err)
	}
	defer ss.Destroy()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ss.CompareSecure(value)
	}
}