package registry

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/bibin-skaria/ossb/internal/types"
)

// ExamplePushImage demonstrates how to push an image to a registry
func ExamplePushImage() {
	// Create a registry client
	client := NewClient(&ClientOptions{
		Timeout: 30 * time.Second,
		RetryConfig: &RetryConfig{
			MaxRetries:      3,
			InitialInterval: 1 * time.Second,
			MaxInterval:     10 * time.Second,
			Multiplier:      2.0,
		},
	})

	// Set up authentication (optional)
	// For Docker Hub, you would use your Docker Hub credentials
	// For other registries, use appropriate credentials
	auth := &authn.Basic{
		Username: "your-username",
		Password: "your-password",
	}
	client.SetAuthenticator(auth)

	ctx := context.Background()

	// First, pull an image to use as a base
	pullRef := ImageReference{
		Registry:   "docker.io",
		Repository: "library/alpine",
		Tag:        "latest",
	}

	platform := types.Platform{
		OS:           "linux",
		Architecture: "amd64",
	}

	fmt.Printf("Pulling image %s for pushing example...\n", pullRef.String())
	manifest, err := client.PullImage(ctx, pullRef, platform)
	if err != nil {
		log.Fatalf("Failed to pull image: %v", err)
	}

	fmt.Printf("Successfully pulled image with %d layers\n", len(manifest.Layers))

	// Now push it to a different location
	// NOTE: Change this to your own registry/repository
	pushRef := ImageReference{
		Registry:   "your-registry.com", // Change this to your registry
		Repository: "your-username/alpine-copy",
		Tag:        "latest",
	}

	fmt.Printf("Pushing image to %s...\n", pushRef.String())

	// Set up progress callback
	progressCallback := func(stage string, progress float64) {
		fmt.Printf("  Progress: %s - %.1f%%\n", stage, progress*100)
	}

	// Push the image with progress reporting
	err = client.PushImageWithProgress(ctx, pushRef, manifest, progressCallback)
	if err != nil {
		log.Fatalf("Failed to push image: %v", err)
	}

	fmt.Printf("Successfully pushed image to %s\n", pushRef.String())
}

// ExamplePushManifestList demonstrates how to push a multi-architecture manifest list
func ExamplePushManifestList() {
	client := NewClient(&ClientOptions{
		Timeout: 60 * time.Second, // Longer timeout for multi-arch
		RetryConfig: &RetryConfig{
			MaxRetries:      3,
			InitialInterval: 1 * time.Second,
			MaxInterval:     10 * time.Second,
			Multiplier:      2.0,
		},
	})

	// Set up authentication
	auth := &authn.Basic{
		Username: "your-username",
		Password: "your-password",
	}
	client.SetAuthenticator(auth)

	ctx := context.Background()

	// Pull images for different architectures
	platforms := []types.Platform{
		{OS: "linux", Architecture: "amd64"},
		{OS: "linux", Architecture: "arm64"},
	}

	var platformManifests []PlatformManifest

	for _, platform := range platforms {
		pullRef := ImageReference{
			Registry:   "docker.io",
			Repository: "library/alpine",
			Tag:        "latest",
		}

		fmt.Printf("Pulling image for platform %s...\n", platform.String())
		manifest, err := client.PullImage(ctx, pullRef, platform)
		if err != nil {
			log.Fatalf("Failed to pull image for platform %s: %v", platform.String(), err)
		}

		// Push the platform-specific image first
		pushRef := ImageReference{
			Registry:   "your-registry.com", // Change this to your registry
			Repository: "your-username/alpine-multiarch",
			Tag:        platform.Architecture,
		}

		fmt.Printf("Pushing platform image to %s...\n", pushRef.String())
		err = client.PushImage(ctx, pushRef, manifest)
		if err != nil {
			log.Fatalf("Failed to push platform image: %v", err)
		}

		// Create platform manifest entry
		// In a real implementation, you would calculate the actual digest
		platformManifests = append(platformManifests, PlatformManifest{
			MediaType: manifest.MediaType,
			Size:      1234, // This should be the actual manifest size
			Digest:    "sha256:example-digest-for-" + platform.Architecture, // This should be the actual digest
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
		Registry:   "your-registry.com", // Change this to your registry
		Repository: "your-username/alpine-multiarch",
		Tag:        "latest",
	}

	fmt.Printf("Pushing manifest list to %s...\n", manifestListRef.String())

	// Set up progress callback
	progressCallback := func(stage string, progress float64) {
		fmt.Printf("  Manifest list progress: %s - %.1f%%\n", stage, progress*100)
	}

	err := client.PushManifestListWithProgress(ctx, manifestListRef, manifestList, progressCallback)
	if err != nil {
		log.Fatalf("Failed to push manifest list: %v", err)
	}

	fmt.Printf("Successfully pushed manifest list to %s\n", manifestListRef.String())
}

// ExamplePushWithErrorRecovery demonstrates error handling and recovery during push operations
func ExamplePushWithErrorRecovery() {
	client := NewClient(&ClientOptions{
		Timeout: 30 * time.Second,
		RetryConfig: &RetryConfig{
			MaxRetries:      5, // More retries for error recovery
			InitialInterval: 2 * time.Second,
			MaxInterval:     30 * time.Second,
			Multiplier:      2.0,
		},
	})

	ctx := context.Background()

	// Example of handling different types of errors
	ref := ImageReference{
		Registry:   "nonexistent-registry.com",
		Repository: "test/image",
		Tag:        "latest",
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
				MediaType: MediaTypeOCILayer,
				Size:      5678,
				Digest:    "sha256:layer123",
			},
		},
	}

	fmt.Printf("Attempting to push to %s (this will fail)...\n", ref.String())

	err := client.PushImage(ctx, ref, manifest)
	if err != nil {
		if regErr, ok := err.(*RegistryError); ok {
			switch regErr.Type {
			case ErrorTypeNetwork:
				fmt.Printf("Network error: %s\n", regErr.Message)
				fmt.Println("Suggestion: Check network connectivity and registry URL")
			case ErrorTypeAuthentication:
				fmt.Printf("Authentication error: %s\n", regErr.Message)
				fmt.Println("Suggestion: Check credentials and authentication method")
			case ErrorTypeValidation:
				fmt.Printf("Validation error: %s\n", regErr.Message)
				fmt.Println("Suggestion: Check image reference and manifest format")
			default:
				fmt.Printf("Other error: %s\n", regErr.Message)
			}
		} else {
			fmt.Printf("Unexpected error: %v\n", err)
		}
	}

	fmt.Println("Error recovery example completed")
}