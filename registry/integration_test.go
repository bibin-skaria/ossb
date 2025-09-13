// +build integration

package registry

import (
	"context"
	"testing"
	"time"

	"github.com/bibin-skaria/ossb/internal/types"
)

// TestRegistryClientIntegration tests the registry client with a real registry
// Run with: go test -tags=integration ./registry/...
func TestRegistryClientIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	client := NewClient(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Test parsing a real image reference
	ref, err := ParseImageReference("alpine:latest")
	if err != nil {
		t.Fatalf("ParseImageReference() error = %v", err)
	}

	if ref.Registry != "docker.io" {
		t.Errorf("Registry = %v, want %v", ref.Registry, "docker.io")
	}
	if ref.Repository != "library/alpine" {
		t.Errorf("Repository = %v, want %v", ref.Repository, "library/alpine")
	}
	if ref.Tag != "latest" {
		t.Errorf("Tag = %v, want %v", ref.Tag, "latest")
	}

	// Test getting image manifest (this will fail without proper auth, but tests the flow)
	platform := types.Platform{
		OS:           "linux",
		Architecture: "amd64",
	}

	// This will likely fail due to rate limiting or auth, but we can test the error handling
	_, err = client.GetImageManifest(ctx, ref, platform)
	if err != nil {
		// Expected to fail in CI/testing environment
		t.Logf("GetImageManifest failed as expected: %v", err)
		
		// Verify it's a proper RegistryError
		if regErr, ok := err.(*RegistryError); ok {
			t.Logf("Got RegistryError: type=%s, operation=%s", regErr.Type, regErr.Operation)
		} else {
			t.Errorf("Expected RegistryError, got %T", err)
		}
	} else {
		t.Log("GetImageManifest succeeded unexpectedly")
	}
}

// TestAuthProviderIntegration tests credential discovery
func TestAuthProviderIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	authProvider := NewAuthProvider(nil)
	ctx := context.Background()

	// Test credential discovery for Docker Hub
	auth, err := authProvider.GetAuthenticator(ctx, "docker.io")
	if err != nil {
		t.Fatalf("GetAuthenticator() error = %v", err)
	}

	// Should at least return anonymous auth
	if auth == nil {
		t.Error("GetAuthenticator() returned nil authenticator")
	}

	// Test credential discovery
	creds, err := authProvider.DiscoverCredentials(ctx, "docker.io")
	if err != nil {
		t.Fatalf("DiscoverCredentials() error = %v", err)
	}

	// Should return empty credentials for anonymous
	if creds == nil {
		t.Error("DiscoverCredentials() returned nil credentials")
	}
}

// TestRegistryConfigIntegration tests loading registry configuration
func TestRegistryConfigIntegration(t *testing.T) {
	config, err := LoadRegistryConfig()
	if err != nil {
		t.Fatalf("LoadRegistryConfig() error = %v", err)
	}

	if config == nil {
		t.Error("LoadRegistryConfig() returned nil config")
	}

	// Should have default values
	if config.Registries == nil {
		t.Error("Config.Registries is nil")
	}

	if config.Insecure == nil {
		t.Error("Config.Insecure is nil")
	}

	if config.Mirrors == nil {
		t.Error("Config.Mirrors is nil")
	}
}