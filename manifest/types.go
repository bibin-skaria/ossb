package manifest

import (
	"fmt"
	"time"

	"github.com/bibin-skaria/ossb/internal/types"
	"github.com/bibin-skaria/ossb/layers"
)

// OCI media types for manifests and configurations
const (
	// OCI Image Manifest
	MediaTypeOCIManifest = "application/vnd.oci.image.manifest.v1+json"
	// OCI Image Index (manifest list)
	MediaTypeOCIIndex = "application/vnd.oci.image.index.v1+json"
	// OCI Image Configuration
	MediaTypeOCIConfig = "application/vnd.oci.image.config.v1+json"
	// OCI Layer media types
	MediaTypeOCILayer     = "application/vnd.oci.image.layer.v1.tar"
	MediaTypeOCILayerGzip = "application/vnd.oci.image.layer.v1.tar+gzip"
	MediaTypeOCILayerZstd = "application/vnd.oci.image.layer.v1.tar+zstd"
	
	// Docker media types for compatibility
	MediaTypeDockerManifest     = "application/vnd.docker.distribution.manifest.v2+json"
	MediaTypeDockerManifestList = "application/vnd.docker.distribution.manifest.list.v2+json"
	MediaTypeDockerConfig       = "application/vnd.docker.container.image.v1+json"
	MediaTypeDockerLayer        = "application/vnd.docker.image.rootfs.diff.tar.gzip"
)

// Descriptor represents an OCI descriptor
type Descriptor struct {
	MediaType   string            `json:"mediaType"`
	Size        int64             `json:"size"`
	Digest      string            `json:"digest"`
	URLs        []string          `json:"urls,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

// ImageManifest represents an OCI image manifest
type ImageManifest struct {
	SchemaVersion int               `json:"schemaVersion"`
	MediaType     string            `json:"mediaType"`
	Config        Descriptor        `json:"config"`
	Layers        []Descriptor      `json:"layers"`
	Annotations   map[string]string `json:"annotations,omitempty"`
}

// ManifestList represents an OCI manifest list (multi-arch)
type ManifestList struct {
	SchemaVersion int               `json:"schemaVersion"`
	MediaType     string            `json:"mediaType"`
	Manifests     []PlatformManifest `json:"manifests"`
	Annotations   map[string]string `json:"annotations,omitempty"`
}

// PlatformManifest represents a platform-specific manifest in a manifest list
type PlatformManifest struct {
	MediaType   string            `json:"mediaType"`
	Size        int64             `json:"size"`
	Digest      string            `json:"digest"`
	Platform    Platform          `json:"platform"`
	URLs        []string          `json:"urls,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

// Platform represents a target platform
type Platform struct {
	Architecture string   `json:"architecture"`
	OS           string   `json:"os"`
	OSVersion    string   `json:"os.version,omitempty"`
	OSFeatures   []string `json:"os.features,omitempty"`
	Variant      string   `json:"variant,omitempty"`
	Features     []string `json:"features,omitempty"`
}

// String returns a string representation of the platform
func (p Platform) String() string {
	if p.Variant != "" {
		return fmt.Sprintf("%s/%s/%s", p.OS, p.Architecture, p.Variant)
	}
	return fmt.Sprintf("%s/%s", p.OS, p.Architecture)
}

// ImageConfig represents an OCI image configuration
type ImageConfig struct {
	Created      string          `json:"created"`
	Author       string          `json:"author,omitempty"`
	Architecture string          `json:"architecture"`
	OS           string          `json:"os"`
	OSVersion    string          `json:"os.version,omitempty"`
	OSFeatures   []string        `json:"os.features,omitempty"`
	Variant      string          `json:"variant,omitempty"`
	Config       ContainerConfig `json:"config"`
	RootFS       RootFS          `json:"rootfs"`
	History      []HistoryEntry  `json:"history,omitempty"`
}

// ContainerConfig represents the container configuration
type ContainerConfig struct {
	User         string              `json:"User,omitempty"`
	ExposedPorts map[string]struct{} `json:"ExposedPorts,omitempty"`
	Env          []string            `json:"Env,omitempty"`
	Entrypoint   []string            `json:"Entrypoint,omitempty"`
	Cmd          []string            `json:"Cmd,omitempty"`
	Volumes      map[string]struct{} `json:"Volumes,omitempty"`
	WorkingDir   string              `json:"WorkingDir,omitempty"`
	Labels       map[string]string   `json:"Labels,omitempty"`
	StopSignal   string              `json:"StopSignal,omitempty"`
	Shell        []string            `json:"Shell,omitempty"`
	Healthcheck  *HealthConfig       `json:"Healthcheck,omitempty"`
	OnBuild      []string            `json:"OnBuild,omitempty"`
}

// RootFS represents the root filesystem configuration
type RootFS struct {
	Type    string   `json:"type"`
	DiffIDs []string `json:"diff_ids"`
}

// HistoryEntry represents a layer history entry
type HistoryEntry struct {
	Created    string `json:"created"`
	CreatedBy  string `json:"created_by,omitempty"`
	Author     string `json:"author,omitempty"`
	Comment    string `json:"comment,omitempty"`
	EmptyLayer bool   `json:"empty_layer,omitempty"`
}

// HealthConfig represents container health check configuration
type HealthConfig struct {
	Test        []string      `json:"Test,omitempty"`
	Interval    time.Duration `json:"Interval,omitempty"`
	Timeout     time.Duration `json:"Timeout,omitempty"`
	StartPeriod time.Duration `json:"StartPeriod,omitempty"`
	Retries     int           `json:"Retries,omitempty"`
}

// ManifestGenerator defines the interface for manifest generation operations
type ManifestGenerator interface {
	// Single architecture manifests
	GenerateImageManifest(config *ImageConfig, layers []*layers.Layer) (*ImageManifest, error)
	GenerateImageConfig(instructions []types.DockerfileInstruction, platform types.Platform) (*ImageConfig, error)
	
	// Multi-architecture manifests
	GenerateManifestList(manifests []PlatformManifest) (*ManifestList, error)
	
	// Validation
	ValidateImageManifest(manifest *ImageManifest) error
	ValidateManifestList(manifestList *ManifestList) error
	ValidateImageConfig(config *ImageConfig) error
	
	// Digest calculation
	CalculateManifestDigest(manifest *ImageManifest) (string, error)
	CalculateManifestListDigest(manifestList *ManifestList) (string, error)
	CalculateConfigDigest(config *ImageConfig) (string, error)
	
	// Serialization
	SerializeManifest(manifest *ImageManifest) ([]byte, error)
	SerializeManifestList(manifestList *ManifestList) ([]byte, error)
	SerializeConfig(config *ImageConfig) ([]byte, error)
	
	// Utility functions
	AddLayerToConfig(config *ImageConfig, layer *layers.Layer) error
	CreatePlatformManifest(manifest *ImageManifest, platform types.Platform) (*PlatformManifest, error)
}

// ErrorType represents the type of manifest error
type ErrorType string

const (
	ErrorTypeValidation ErrorType = "validation"
	ErrorTypeGeneration ErrorType = "generation"
	ErrorTypeDigest     ErrorType = "digest"
	ErrorTypeSerialization ErrorType = "serialization"
)

// ManifestError represents an error from manifest operations
type ManifestError struct {
	Type      ErrorType `json:"type"`
	Operation string    `json:"operation"`
	Message   string    `json:"message"`
	Cause     error     `json:"-"`
}

// Error implements the error interface
func (e *ManifestError) Error() string {
	return fmt.Sprintf("manifest error [%s] %s: %s", e.Type, e.Operation, e.Message)
}

// Unwrap returns the underlying error
func (e *ManifestError) Unwrap() error {
	return e.Cause
}

// IsValidationError returns true if this is a validation error
func (e *ManifestError) IsValidationError() bool {
	return e.Type == ErrorTypeValidation
}

// IsGenerationError returns true if this is a generation error
func (e *ManifestError) IsGenerationError() bool {
	return e.Type == ErrorTypeGeneration
}