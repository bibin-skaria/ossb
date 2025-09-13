package registry

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/bibin-skaria/ossb/internal/types"
)

// ExamplePullAndExtractImage demonstrates how to pull and extract a base image
func ExamplePullAndExtractImage() {
	// Create a registry client
	client := NewClient(DefaultClientOptions())
	
	// Set up context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Parse image reference
	imageRef, err := ParseImageReference("alpine:latest")
	if err != nil {
		log.Fatalf("Failed to parse image reference: %v", err)
	}

	// Validate the reference
	if err := imageRef.Validate(); err != nil {
		log.Fatalf("Invalid image reference: %v", err)
	}

	fmt.Printf("Pulling image: %s\n", imageRef.String())

	// Define target platform
	platform := types.Platform{
		OS:           "linux",
		Architecture: "amd64",
	}

	// Pull the image
	manifest, err := client.PullImage(ctx, imageRef, platform)
	if err != nil {
		log.Fatalf("Failed to pull image: %v", err)
	}

	fmt.Printf("Successfully pulled image with %d layers\n", len(manifest.Layers))
	fmt.Printf("Config digest: %s\n", manifest.Config.Digest)

	// Print layer information
	for i, layer := range manifest.Layers {
		fmt.Printf("Layer %d: %s (size: %d bytes)\n", i, layer.Digest, layer.Size)
	}

	// Create extraction directory
	extractDir := filepath.Join(os.TempDir(), "ossb-extracted-image")
	if err := os.RemoveAll(extractDir); err != nil {
		log.Printf("Warning: failed to clean extraction directory: %v", err)
	}

	fmt.Printf("Extracting image to: %s\n", extractDir)

	// Extract image to directory
	if err := client.ExtractImageToDirectory(ctx, manifest, extractDir); err != nil {
		log.Fatalf("Failed to extract image: %v", err)
	}

	fmt.Println("Image extracted successfully!")

	// List extracted contents
	fmt.Println("Extracted contents:")
	err = filepath.Walk(extractDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relPath, _ := filepath.Rel(extractDir, path)
		if info.IsDir() {
			fmt.Printf("  [DIR]  %s/\n", relPath)
		} else {
			fmt.Printf("  [FILE] %s (%d bytes)\n", relPath, info.Size())
		}
		return nil
	})
	if err != nil {
		log.Printf("Warning: failed to list extracted contents: %v", err)
	}

	// Demonstrate blob download with progress
	if len(manifest.Layers) > 0 {
		fmt.Println("\nDownloading first layer blob with progress...")
		
		blobPath := filepath.Join(os.TempDir(), "ossb-layer-blob")
		layerDigest := manifest.Layers[0].Digest
		
		var lastProgress int64
		progressCallback := func(downloaded, total int64) {
			// Only print progress every 100KB to avoid spam
			if downloaded-lastProgress > 100*1024 || total != -1 {
				if total > 0 {
					percent := float64(downloaded) / float64(total) * 100
					fmt.Printf("  Progress: %.1f%% (%d/%d bytes)\n", percent, downloaded, total)
				} else {
					fmt.Printf("  Downloaded: %d bytes\n", downloaded)
				}
				lastProgress = downloaded
			}
		}
		
		if err := client.DownloadBlob(ctx, imageRef, layerDigest, blobPath, progressCallback); err != nil {
			log.Printf("Warning: failed to download blob: %v", err)
		} else {
			fmt.Printf("Blob downloaded to: %s\n", blobPath)
		}
	}

	fmt.Println("\nExample completed successfully!")
}

// ExampleMultiArchImagePull demonstrates pulling images for different architectures
func ExampleMultiArchImagePull() {
	client := NewClient(DefaultClientOptions())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	imageRef, err := ParseImageReference("alpine:latest")
	if err != nil {
		log.Fatalf("Failed to parse image reference: %v", err)
	}

	// Test different platforms
	platforms := []types.Platform{
		{OS: "linux", Architecture: "amd64"},
		{OS: "linux", Architecture: "arm64"},
		{OS: "linux", Architecture: "arm", Variant: "v7"},
	}

	fmt.Printf("Pulling %s for multiple architectures:\n", imageRef.String())

	for _, platform := range platforms {
		fmt.Printf("\nPulling for platform: %s\n", platform.String())
		
		manifest, err := client.PullImage(ctx, imageRef, platform)
		if err != nil {
			fmt.Printf("  Error: %v\n", err)
			continue
		}

		fmt.Printf("  Success! Layers: %d, Config: %s\n", 
			len(manifest.Layers), manifest.Config.Digest[:12]+"...")
		
		// Show layer sizes
		var totalSize int64
		for _, layer := range manifest.Layers {
			totalSize += layer.Size
		}
		fmt.Printf("  Total size: %d bytes (%.2f MB)\n", totalSize, float64(totalSize)/(1024*1024))
	}

	fmt.Println("\nMulti-arch pull example completed!")
}

// ExampleImageReferenceValidation demonstrates image reference parsing and validation
func ExampleImageReferenceValidation() {
	fmt.Println("Image Reference Validation Examples:")
	
	testRefs := []string{
		"alpine:latest",
		"docker.io/library/alpine:3.18",
		"gcr.io/my-project/my-app:v1.0.0",
		"alpine@sha256:3fbc632167424a6d997e74f52b878d7cc478225cffac6bc977eedfe51c7f4e79",
		"localhost:5000/test:latest",
		"", // Invalid - empty
		"alpine:invalid@tag", // Invalid - bad tag
		"alpine@invalid-digest", // Invalid - bad digest
	}

	for _, ref := range testRefs {
		fmt.Printf("\nTesting: '%s'\n", ref)
		
		imageRef, err := ParseImageReference(ref)
		if err != nil {
			fmt.Printf("  Parse error: %v\n", err)
			continue
		}

		fmt.Printf("  Parsed: registry=%s, repository=%s, tag=%s, digest=%s\n",
			imageRef.Registry, imageRef.Repository, imageRef.Tag, imageRef.Digest)
		
		if err := imageRef.Validate(); err != nil {
			fmt.Printf("  Validation error: %v\n", err)
		} else {
			fmt.Printf("  ✓ Valid reference: %s\n", imageRef.String())
		}
	}

	fmt.Println("\nValidation examples completed!")
}

// ExamplePlatformCompatibility demonstrates platform matching and compatibility
func ExamplePlatformCompatibility() {
	fmt.Println("Platform Compatibility Examples:")
	
	testCases := []struct {
		platform Platform
		target   Platform
		desc     string
	}{
		{
			Platform{OS: "linux", Architecture: "amd64"},
			Platform{OS: "linux", Architecture: "amd64"},
			"Exact match",
		},
		{
			Platform{OS: "linux", Architecture: "amd64"},
			Platform{OS: "linux", Architecture: "386"},
			"amd64 can run 386",
		},
		{
			Platform{OS: "linux", Architecture: "arm64"},
			Platform{OS: "linux", Architecture: "arm", Variant: "v8"},
			"arm64 can run armv8",
		},
		{
			Platform{OS: "linux", Architecture: "arm", Variant: "v7"},
			Platform{OS: "linux", Architecture: "arm", Variant: "v6"},
			"armv7 can run armv6",
		},
		{
			Platform{OS: "linux", Architecture: "arm", Variant: "v6"},
			Platform{OS: "linux", Architecture: "arm", Variant: "v7"},
			"armv6 cannot run armv7",
		},
		{
			Platform{OS: "linux", Architecture: "amd64"},
			Platform{OS: "windows", Architecture: "amd64"},
			"Different OS",
		},
	}

	for _, tc := range testCases {
		fmt.Printf("\n%s:\n", tc.desc)
		fmt.Printf("  Platform: %s\n", tc.platform.String())
		fmt.Printf("  Target:   %s\n", tc.target.String())
		fmt.Printf("  Matches:    %t\n", tc.platform.Matches(tc.target))
		fmt.Printf("  Compatible: %t\n", tc.platform.IsCompatible(tc.target))
	}

	fmt.Println("\nPlatform compatibility examples completed!")
}