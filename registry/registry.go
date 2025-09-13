// Package registry provides container registry client functionality for OSSB.
//
// This package implements a comprehensive registry client that supports:
// - Authentication via multiple methods (config, environment, Docker config, Kubernetes secrets)
// - Image pulling and pushing operations
// - Multi-architecture manifest handling
// - Comprehensive error handling and retry logic
// - OCI-compliant image operations
//
// The main entry point is the Client type, which provides all registry operations.
// Authentication is handled by the AuthProvider type, which can discover credentials
// from various sources automatically.
//
// Example usage:
//
//	// Create a client with default options
//	client := registry.NewClient(nil)
//
//	// Set up authentication
//	authProvider := registry.NewAuthProvider(nil)
//	auth, err := authProvider.GetAuthenticator(ctx, "docker.io")
//	if err != nil {
//		return err
//	}
//	client.SetAuthenticator(auth)
//
//	// Pull an image
//	ref, _ := registry.ParseImageReference("alpine:latest")
//	platform := types.Platform{OS: "linux", Architecture: "amd64"}
//	manifest, err := client.PullImage(ctx, ref, platform)
//	if err != nil {
//		return err
//	}
//
// The package is designed to be used by the OSSB build engine to handle all
// container registry operations during the build process.
package registry

import (
	"context"
	"io"
	"strings"

	"github.com/bibin-skaria/ossb/internal/types"
)

// RegistryClient defines the interface for container registry operations
type RegistryClient interface {
	// Image operations
	PullImage(ctx context.Context, ref ImageReference, platform types.Platform) (*ImageManifest, error)
	PushImage(ctx context.Context, ref ImageReference, manifest *ImageManifest) error
	PushManifestList(ctx context.Context, ref ImageReference, manifestList *ManifestList) error

	// Metadata operations
	GetImageManifest(ctx context.Context, ref ImageReference, platform types.Platform) (*ImageManifest, error)
	GetManifestList(ctx context.Context, ref ImageReference) (*ManifestList, error)
	GetBlob(ctx context.Context, ref ImageReference, digest string) (io.ReadCloser, error)

	// Layer extraction and blob operations
	ExtractImageToDirectory(ctx context.Context, manifest *ImageManifest, targetDir string) error
	DownloadBlob(ctx context.Context, ref ImageReference, digest string, targetPath string, progressCallback func(downloaded, total int64)) error
}

// AuthenticationProvider defines the interface for credential discovery and management
type AuthenticationProvider interface {
	// DiscoverCredentials attempts to discover credentials for a registry
	DiscoverCredentials(ctx context.Context, registry string) (*Credentials, error)

	// ValidateCredentials tests if credentials work for a given registry
	ValidateCredentials(ctx context.Context, registry string, creds *Credentials) error
}

// Common media types for OCI and Docker images
const (
	// OCI Media Types
	MediaTypeOCIManifest     = "application/vnd.oci.image.manifest.v1+json"
	MediaTypeOCIIndex        = "application/vnd.oci.image.index.v1+json"
	MediaTypeOCIConfig       = "application/vnd.oci.image.config.v1+json"
	MediaTypeOCILayer        = "application/vnd.oci.image.layer.v1.tar+gzip"
	MediaTypeOCILayerZstd    = "application/vnd.oci.image.layer.v1.tar+zstd"
	MediaTypeOCILayerNonDist = "application/vnd.oci.image.layer.nondistributable.v1.tar+gzip"

	// Docker Media Types
	MediaTypeDockerManifest     = "application/vnd.docker.distribution.manifest.v2+json"
	MediaTypeDockerManifestList = "application/vnd.docker.distribution.manifest.list.v2+json"
	MediaTypeDockerConfig       = "application/vnd.docker.container.image.v1+json"
	MediaTypeDockerLayer        = "application/vnd.docker.image.rootfs.diff.tar.gzip"
	MediaTypeDockerLayerForeign = "application/vnd.docker.image.rootfs.foreign.diff.tar.gzip"

	// Legacy Docker Media Types
	MediaTypeDockerManifestV1       = "application/vnd.docker.distribution.manifest.v1+json"
	MediaTypeDockerManifestV1Signed = "application/vnd.docker.distribution.manifest.v1+prettyjws"
)

// Well-known registry hostnames
const (
	DockerHubRegistry = "docker.io"
	DockerHubIndex    = "index.docker.io"
	QuayRegistry      = "quay.io"
	GCRRegistry       = "gcr.io"
	ECRRegistry       = "amazonaws.com"
	ACRRegistry       = "azurecr.io"
)

// DefaultRegistryConfig returns a default registry configuration
func DefaultRegistryConfig() *RegistryConfig {
	return &RegistryConfig{
		DefaultRegistry: DockerHubRegistry,
		Registries:      make(map[string]*RegistryAuth),
		Insecure:        []string{},
		Mirrors:         make(map[string][]string),
	}
}

// IsOfficialImage returns true if the repository is an official Docker Hub image
func IsOfficialImage(repository string) bool {
	return !strings.Contains(repository, "/") || strings.HasPrefix(repository, "library/")
}

// NormalizeRegistry normalizes a registry hostname for consistent lookup
func NormalizeRegistry(registry string) string {
	switch registry {
	case "", "docker.io", "index.docker.io":
		return DockerHubRegistry
	default:
		return registry
	}
}

// IsInsecureRegistry checks if a registry should use insecure connections
func IsInsecureRegistry(registry string, insecureList []string) bool {
	for _, insecure := range insecureList {
		if registry == insecure {
			return true
		}
		// Support wildcard matching
		if strings.HasSuffix(insecure, "*") {
			prefix := strings.TrimSuffix(insecure, "*")
			if strings.HasPrefix(registry, prefix) {
				return true
			}
		}
	}
	return false
}

// GetRegistryMirrors returns the list of mirrors for a registry
func GetRegistryMirrors(registry string, mirrors map[string][]string) []string {
	if mirrorList, exists := mirrors[registry]; exists {
		return mirrorList
	}
	return nil
}