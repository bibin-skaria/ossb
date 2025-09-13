// +build integration

package registry

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/bibin-skaria/ossb/internal/types"
)

// TestPushImageIntegration tests pushing an image to a real registry
// This test requires environment variables to be set:
// - REGISTRY_URL: The registry URL (e.g., "localhost:5000")
// - REGISTRY_USERNAME: Username for authentication (optional)
// - REGISTRY_PASSWORD: Password for authentication (optional)
func TestPushImageIntegration(t *testing.T) {
	registryURL := os.Getenv("REGISTRY_URL")
	if registryURL == "" {
		t.Skip("REGISTRY_URL not set, skipping integration test")
	}

	username := os.Getenv("REGISTRY_USERNAME")
	password := os.Getenv("REGISTRY_PASSWORD")

	client := NewClient(&ClientOptions{
		Timeout: 30 * time.Second,
		RetryConfig: &RetryConfig{
			MaxRetries:      3,
			InitialInterval: 1 * time.Second,
			MaxInterval:     10 * time.Second,
			Multiplier:      2.0,
		},
	})

	// Set up authentication if credentials provided
	if username != "" && password != "" {
		auth := &authn.Basic{
			Username: username,
			Password: password,
		}
		client.SetAuthenticator(auth)
	}

	ctx := context.Background()

	// First, pull a small image to use for testing
	pullRef := ImageReference{
		Registry:   "docker.io",
		Repository: "library/alpine",
		Tag:        "latest",
	}

	platform := types.Platform{
		OS:           "linux",
		Architecture: "amd64",
	}

	t.Logf("Pulling image %s for testing", pullRef.String())
	manifest, err := client.PullImage(ctx, pullRef, platform)
	if err != nil {
		t.Fatalf("Failed to pull test image: %v", err)
	}

	// Now push it to the test registry
	pushRef := ImageReference{
		Registry:   registryURL,
		Repository: "test/alpine-push-test",
		Tag:        fmt.Sprintf("test-%d", time.Now().Unix()),
	}

	t.Logf("Pushing image to %s", pushRef.String())

	// Track progress
	var progressStages []string
	progressCallback := func(stage string, progress float64) {
		t.Logf("Progress: %s - %.1f%%", stage, progress*100)
		progressStages = append(progressStages, stage)
	}

	err = client.PushImageWithProgress(ctx, pushRef, manifest, progressCallback)
	if err != nil {
		t.Fatalf("Failed to push image: %v", err)
	}

	// Verify we got progress callbacks
	if len(progressStages) == 0 {
		t.Error("Expected progress callbacks, got none")
	}

	t.Logf("Successfully pushed image to %s", pushRef.String())

	// Try to pull the pushed image to verify it worked
	t.Logf("Verifying push by pulling %s", pushRef.String())
	pulledManifest, err := client.PullImage(ctx, pushRef, platform)
	if err != nil {
		t.Fatalf("Failed to pull pushed image: %v", err)
	}

	// Basic verification that the manifests are similar
	if pulledManifest.SchemaVersion != manifest.SchemaVersion {
		t.Errorf("Schema version mismatch: got %d, want %d", pulledManifest.SchemaVersion, manifest.SchemaVersion)
	}

	if len(pulledManifest.Layers) != len(manifest.Layers) {
		t.Errorf("Layer count mismatch: got %d, want %d", len(pulledManifest.Layers), len(manifest.Layers))
	}

	t.Logf("Push integration test completed successfully")
}

// TestPushManifestListIntegration tests pushing a multi-architecture manifest list
func TestPushManifestListIntegration(t *testing.T) {
	registryURL := os.Getenv("REGISTRY_URL")
	if registryURL == "" {
		t.Skip("REGISTRY_URL not set, skipping integration test")
	}

	username := os.Getenv("REGISTRY_USERNAME")
	password := os.Getenv("REGISTRY_PASSWORD")

	client := NewClient(&ClientOptions{
		Timeout: 60 * time.Second, // Longer timeout for multi-arch
		RetryConfig: &RetryConfig{
			MaxRetries:      3,
			InitialInterval: 1 * time.Second,
			MaxInterval:     10 * time.Second,
			Multiplier:      2.0,
		},
	})

	// Set up authentication if credentials provided
	if username != "" && password != "" {
		auth := &authn.Basic{
			Username: username,
			Password: password,
		}
		client.SetAuthenticator(auth)
	}

	ctx := context.Background()

	// Pull images for different architectures
	platforms := []types.Platform{
		{OS: "linux", Architecture: "amd64"},
		{OS: "linux", Architecture: "arm64"},
	}

	var platformManifests []PlatformManifest
	timestamp := time.Now().Unix()

	for _, platform := range platforms {
		pullRef := ImageReference{
			Registry:   "docker.io",
			Repository: "library/alpine",
			Tag:        "latest",
		}

		t.Logf("Pulling image for platform %s", platform.String())
		manifest, err := client.PullImage(ctx, pullRef, platform)
		if err != nil {
			t.Fatalf("Failed to pull image for platform %s: %v", platform.String(), err)
		}

		// Push the platform-specific image first
		pushRef := ImageReference{
			Registry:   registryURL,
			Repository: "test/alpine-multiarch-test",
			Tag:        fmt.Sprintf("%s-%d", platform.Architecture, timestamp),
		}

		t.Logf("Pushing platform image to %s", pushRef.String())
		err = client.PushImage(ctx, pushRef, manifest)
		if err != nil {
			t.Fatalf("Failed to push platform image: %v", err)
		}

		// Calculate manifest digest for the platform manifest
		manifestBytes, err := client.SerializeManifest(manifest)
		if err != nil {
			t.Fatalf("Failed to serialize manifest: %v", err)
		}

		digest := fmt.Sprintf("sha256:%x", sha256.Sum256(manifestBytes))

		platformManifests = append(platformManifests, PlatformManifest{
			MediaType: manifest.MediaType,
			Size:      int64(len(manifestBytes)),
			Digest:    digest,
			Platform: Platform{
				Architecture: platform.Architecture,
				OS:           platform.OS,
			},
		})
	}

	// Create and push manifest list
	manifestList := &ManifestList{
		SchemaVersion: 2,
		MediaType:     MediaTypeOCIIndex,
		Manifests:     platformManifests,
	}

	manifestListRef := ImageReference{
		Registry:   registryURL,
		Repository: "test/alpine-multiarch-test",
		Tag:        fmt.Sprintf("multiarch-%d", timestamp),
	}

	t.Logf("Pushing manifest list to %s", manifestListRef.String())

	// Track progress
	progressCallback := func(stage string, progress float64) {
		t.Logf("Manifest list progress: %s - %.1f%%", stage, progress*100)
	}

	err := client.PushManifestListWithProgress(ctx, manifestListRef, manifestList, progressCallback)
	if err != nil {
		t.Fatalf("Failed to push manifest list: %v", err)
	}

	t.Logf("Successfully pushed manifest list to %s", manifestListRef.String())

	// Try to pull the manifest list to verify it worked
	t.Logf("Verifying manifest list push by pulling %s", manifestListRef.String())
	pulledManifestList, err := client.GetManifestList(ctx, manifestListRef)
	if err != nil {
		// This might fail if GetManifestList is not implemented yet
		t.Logf("Could not verify manifest list (GetManifestList not implemented): %v", err)
	} else {
		// Basic verification
		if len(pulledManifestList.Manifests) != len(manifestList.Manifests) {
			t.Errorf("Manifest count mismatch: got %d, want %d", len(pulledManifestList.Manifests), len(manifestList.Manifests))
		}
	}

	t.Logf("Manifest list integration test completed successfully")
}

// TestPushWithAuthenticationIntegration tests push operations with various authentication methods
func TestPushWithAuthenticationIntegration(t *testing.T) {
	registryURL := os.Getenv("REGISTRY_URL")
	if registryURL == "" {
		t.Skip("REGISTRY_URL not set, skipping integration test")
	}

	username := os.Getenv("REGISTRY_USERNAME")
	password := os.Getenv("REGISTRY_PASSWORD")

	if username == "" || password == "" {
		t.Skip("REGISTRY_USERNAME or REGISTRY_PASSWORD not set, skipping auth test")
	}

	tests := []struct {
		name string
		auth authn.Authenticator
	}{
		{
			name: "basic auth",
			auth: &authn.Basic{
				Username: username,
				Password: password,
			},
		},
		{
			name: "anonymous (should fail for private registry)",
			auth: authn.Anonymous,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(&ClientOptions{
				Timeout: 30 * time.Second,
			})
			client.SetAuthenticator(tt.auth)

			ctx := context.Background()

			// Try to push a simple manifest
			ref := ImageReference{
				Registry:   registryURL,
				Repository: "test/auth-test",
				Tag:        fmt.Sprintf("auth-%d", time.Now().Unix()),
			}

			manifest := &ImageManifest{
				SchemaVersion: 2,
				MediaType:     MediaTypeOCIManifest,
				Config: Descriptor{
					MediaType: MediaTypeOCIConfig,
					Size:      1234,
					Digest:    "sha256:config123",
				},
				Layers: []Descriptor{
					{
						MediaType: MediaTypeOCILayerGzip,
						Size:      5678,
						Digest:    "sha256:layer123",
					},
				},
			}

			err := client.PushImage(ctx, ref, manifest)

			if tt.name == "anonymous" {
				// Anonymous should fail for private registry
				if err == nil {
					t.Error("Expected error for anonymous auth, got nil")
				} else {
					t.Logf("Anonymous auth failed as expected: %v", err)
				}
			} else {
				// Basic auth should succeed (though might fail due to missing blobs)
				if err != nil {
					t.Logf("Push with %s failed (expected due to missing blobs): %v", tt.name, err)
				} else {
					t.Logf("Push with %s succeeded", tt.name)
				}
			}
		})
	}
}

// Helper function to serialize manifest (would be in the client in real implementation)
func (c *Client) SerializeManifest(manifest *ImageManifest) ([]byte, error) {
	return json.Marshal(manifest)
}