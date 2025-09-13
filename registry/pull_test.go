package registry

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bibin-skaria/ossb/internal/types"
)

func TestPullImage(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	client := NewClient(DefaultClientOptions())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	tests := []struct {
		name     string
		ref      string
		platform types.Platform
		wantErr  bool
	}{
		{
			name:     "pull alpine latest amd64",
			ref:      "alpine:latest",
			platform: types.Platform{OS: "linux", Architecture: "amd64"},
			wantErr:  false,
		},
		{
			name:     "pull alpine latest arm64",
			ref:      "alpine:latest",
			platform: types.Platform{OS: "linux", Architecture: "arm64"},
			wantErr:  false,
		},
		{
			name:     "pull busybox with digest",
			ref:      "busybox@sha256:3fbc632167424a6d997e74f52b878d7cc478225cffac6bc977eedfe51c7f4e79",
			platform: types.Platform{OS: "linux", Architecture: "amd64"},
			wantErr:  false,
		},
		{
			name:     "pull non-existent image",
			ref:      "nonexistent/image:latest",
			platform: types.Platform{OS: "linux", Architecture: "amd64"},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			imageRef, err := ParseImageReference(tt.ref)
			if err != nil {
				t.Fatalf("failed to parse image reference: %v", err)
			}

			manifest, err := client.PullImage(ctx, imageRef, tt.platform)
			if (err != nil) != tt.wantErr {
				t.Errorf("PullImage() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if manifest == nil {
					t.Error("expected manifest to be non-nil")
					return
				}

				if manifest.SchemaVersion == 0 {
					t.Error("expected schema version to be set")
				}

				if len(manifest.Layers) == 0 {
					t.Error("expected at least one layer")
				}

				if manifest.Config.Digest == "" {
					t.Error("expected config digest to be set")
				}

				// Verify layer digests are valid
				for i, layer := range manifest.Layers {
					if layer.Digest == "" {
						t.Errorf("layer %d has empty digest", i)
					}
					if layer.Size <= 0 {
						t.Errorf("layer %d has invalid size: %d", i, layer.Size)
					}
				}
			}
		})
	}
}

func TestExtractImageToDirectory(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	client := NewClient(DefaultClientOptions())
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// Pull a small image for testing
	imageRef, err := ParseImageReference("alpine:latest")
	if err != nil {
		t.Fatalf("failed to parse image reference: %v", err)
	}

	platform := types.Platform{OS: "linux", Architecture: "amd64"}
	manifest, err := client.PullImage(ctx, imageRef, platform)
	if err != nil {
		t.Fatalf("failed to pull image: %v", err)
	}

	// Create temporary directory for extraction
	tempDir, err := os.MkdirTemp("", "ossb-extract-test-*")
	if err != nil {
		t.Fatalf("failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Extract image
	err = client.ExtractImageToDirectory(ctx, manifest, tempDir)
	if err != nil {
		t.Fatalf("failed to extract image: %v", err)
	}

	// Verify extraction results
	configPath := filepath.Join(tempDir, "config.json")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("config.json was not created")
	}

	// Verify layer directories exist
	for i := range manifest.Layers {
		layerDir := filepath.Join(tempDir, "layer-"+string(rune('0'+i)))
		if _, err := os.Stat(layerDir); os.IsNotExist(err) {
			t.Errorf("layer directory %s was not created", layerDir)
		}
	}
}

func TestDownloadBlob(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	client := NewClient(DefaultClientOptions())
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// Pull an image to get blob digests
	imageRef, err := ParseImageReference("alpine:latest")
	if err != nil {
		t.Fatalf("failed to parse image reference: %v", err)
	}

	platform := types.Platform{OS: "linux", Architecture: "amd64"}
	manifest, err := client.PullImage(ctx, imageRef, platform)
	if err != nil {
		t.Fatalf("failed to pull image: %v", err)
	}

	if len(manifest.Layers) == 0 {
		t.Fatal("no layers found in manifest")
	}

	// Create temporary file for blob download
	tempFile, err := os.CreateTemp("", "ossb-blob-test-*")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tempFile.Name())
	tempFile.Close()

	// Download first layer blob
	layerDigest := manifest.Layers[0].Digest
	var downloadedBytes int64

	progressCallback := func(downloaded, total int64) {
		downloadedBytes = downloaded
		// total is -1 for unknown size, so we don't use it in this test
		_ = total
	}

	err = client.DownloadBlob(ctx, imageRef, layerDigest, tempFile.Name(), progressCallback)
	if err != nil {
		t.Fatalf("failed to download blob: %v", err)
	}

	// Verify file was created and has content
	fileInfo, err := os.Stat(tempFile.Name())
	if err != nil {
		t.Fatalf("failed to stat downloaded file: %v", err)
	}

	if fileInfo.Size() == 0 {
		t.Error("downloaded file is empty")
	}

	if downloadedBytes == 0 {
		t.Error("progress callback was not called")
	}

	t.Logf("Downloaded %d bytes", downloadedBytes)
}

func TestImageReferenceValidation(t *testing.T) {
	tests := []struct {
		name    string
		ref     string
		wantErr bool
	}{
		{
			name:    "valid simple image",
			ref:     "alpine:latest",
			wantErr: false,
		},
		{
			name:    "valid registry image",
			ref:     "docker.io/library/alpine:latest",
			wantErr: false,
		},
		{
			name:    "valid digest reference",
			ref:     "alpine@sha256:3fbc632167424a6d997e74f52b878d7cc478225cffac6bc977eedfe51c7f4e79",
			wantErr: false,
		},
		{
			name:    "valid private registry",
			ref:     "gcr.io/my-project/my-image:v1.0.0",
			wantErr: false,
		},
		{
			name:    "empty reference",
			ref:     "",
			wantErr: true,
		},
		{
			name:    "invalid digest format",
			ref:     "alpine@invalid-digest",
			wantErr: true,
		},
		{
			name:    "invalid tag characters",
			ref:     "alpine:invalid@tag",
			wantErr: true,
		},
		{
			name:    "tag too long",
			ref:     "alpine:" + string(make([]byte, 130)),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			imageRef, err := ParseImageReference(tt.ref)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseImageReference() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if err := imageRef.Validate(); err != nil {
					t.Errorf("ImageReference.Validate() error = %v", err)
				}
			}
		})
	}
}

func TestPlatformMatching(t *testing.T) {
	tests := []struct {
		name     string
		platform Platform
		target   Platform
		wantMatch bool
		wantCompat bool
	}{
		{
			name:     "exact match",
			platform: Platform{OS: "linux", Architecture: "amd64"},
			target:   Platform{OS: "linux", Architecture: "amd64"},
			wantMatch: true,
			wantCompat: true,
		},
		{
			name:     "different os",
			platform: Platform{OS: "linux", Architecture: "amd64"},
			target:   Platform{OS: "windows", Architecture: "amd64"},
			wantMatch: false,
			wantCompat: false,
		},
		{
			name:     "amd64 compatible with 386",
			platform: Platform{OS: "linux", Architecture: "amd64"},
			target:   Platform{OS: "linux", Architecture: "386"},
			wantMatch: false,
			wantCompat: true,
		},
		{
			name:     "arm64 compatible with armv8",
			platform: Platform{OS: "linux", Architecture: "arm64"},
			target:   Platform{OS: "linux", Architecture: "arm", Variant: "v8"},
			wantMatch: false,
			wantCompat: true,
		},
		{
			name:     "armv7 compatible with armv6",
			platform: Platform{OS: "linux", Architecture: "arm", Variant: "v7"},
			target:   Platform{OS: "linux", Architecture: "arm", Variant: "v6"},
			wantMatch: false,
			wantCompat: true,
		},
		{
			name:     "armv6 not compatible with armv7",
			platform: Platform{OS: "linux", Architecture: "arm", Variant: "v6"},
			target:   Platform{OS: "linux", Architecture: "arm", Variant: "v7"},
			wantMatch: false,
			wantCompat: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotMatch := tt.platform.Matches(tt.target)
			if gotMatch != tt.wantMatch {
				t.Errorf("Platform.Matches() = %v, want %v", gotMatch, tt.wantMatch)
			}

			gotCompat := tt.platform.IsCompatible(tt.target)
			if gotCompat != tt.wantCompat {
				t.Errorf("Platform.IsCompatible() = %v, want %v", gotCompat, tt.wantCompat)
			}
		})
	}
}

func TestManifestListDetection(t *testing.T) {
	tests := []struct {
		name      string
		mediaType string
		want      bool
	}{
		{
			name:      "docker manifest list",
			mediaType: MediaTypeDockerManifestList,
			want:      true,
		},
		{
			name:      "oci index",
			mediaType: MediaTypeOCIIndex,
			want:      true,
		},
		{
			name:      "docker manifest",
			mediaType: MediaTypeDockerManifest,
			want:      false,
		},
		{
			name:      "oci manifest",
			mediaType: MediaTypeOCIManifest,
			want:      false,
		},
		{
			name:      "unknown type",
			mediaType: "application/unknown",
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isManifestList(tt.mediaType)
			if got != tt.want {
				t.Errorf("isManifestList() = %v, want %v", got, tt.want)
			}
		})
	}
}