package layers

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"time"
)

// ChangeType represents the type of filesystem change
type ChangeType string

const (
	ChangeTypeAdd    ChangeType = "A" // File added
	ChangeTypeModify ChangeType = "M" // File modified
	ChangeTypeDelete ChangeType = "D" // File deleted
)

// FileChange represents a single filesystem change
type FileChange struct {
	Path      string      `json:"path"`
	Type      ChangeType  `json:"type"`
	Mode      os.FileMode `json:"mode"`
	Content   io.Reader   `json:"-"` // Not serialized
	Size      int64       `json:"size"`
	Timestamp time.Time   `json:"timestamp"`
	UID       int         `json:"uid"`
	GID       int         `json:"gid"`
	Linkname  string      `json:"linkname,omitempty"` // For symlinks
}

// Layer represents an OCI-compliant filesystem layer
type Layer struct {
	Digest      string            `json:"digest"`
	Size        int64             `json:"size"`
	MediaType   string            `json:"mediaType"`
	Blob        io.ReadCloser     `json:"-"` // Not serialized
	Annotations map[string]string `json:"annotations,omitempty"`
	Created     time.Time         `json:"created"`
	CreatedBy   string            `json:"createdBy,omitempty"`
	Comment     string            `json:"comment,omitempty"`
	EmptyLayer  bool              `json:"emptyLayer,omitempty"`
}

// LayerManager interface defines operations for managing OCI layers
type LayerManager interface {
	// Layer creation
	CreateLayer(changes []FileChange) (*Layer, error)
	ExtractLayer(layer *Layer, targetPath string) error
	
	// Layer optimization
	OptimizeLayers(layers []*Layer) ([]*Layer, error)
	DeduplicateLayers(layers []*Layer) ([]*Layer, error)
	
	// Layer metadata
	GetLayerDigest(layer *Layer) (string, error)
	GetLayerSize(layer *Layer) (int64, error)
	ValidateLayer(layer *Layer) error
	
	// Filesystem operations
	DetectChanges(oldPath, newPath string) ([]FileChange, error)
	ApplyChanges(basePath string, changes []FileChange) error
}

// LayerConfig holds configuration for layer creation
type LayerConfig struct {
	Compression CompressionType `json:"compression"`
	MaxSize     int64           `json:"maxSize,omitempty"`
	SkipEmpty   bool            `json:"skipEmpty"`
	Timestamp   *time.Time      `json:"timestamp,omitempty"`
}

// CompressionType represents the compression algorithm used for layers
type CompressionType string

const (
	CompressionNone CompressionType = "none"
	CompressionGzip CompressionType = "gzip"
	CompressionZstd CompressionType = "zstd"
)

// OCI media types for layers
const (
	MediaTypeImageLayer           = "application/vnd.oci.image.layer.v1.tar"
	MediaTypeImageLayerGzip       = "application/vnd.oci.image.layer.v1.tar+gzip"
	MediaTypeImageLayerZstd       = "application/vnd.oci.image.layer.v1.tar+zstd"
	MediaTypeImageLayerNonDistrib = "application/vnd.oci.image.layer.nondistributable.v1.tar"
)

// GetMediaType returns the appropriate OCI media type for the compression
func (c CompressionType) GetMediaType() string {
	switch c {
	case CompressionGzip:
		return MediaTypeImageLayerGzip
	case CompressionZstd:
		return MediaTypeImageLayerZstd
	default:
		return MediaTypeImageLayer
	}
}

// LayerError represents errors that occur during layer operations
type LayerError struct {
	Operation string
	Layer     string
	Cause     error
}

func (e *LayerError) Error() string {
	if e.Layer != "" {
		return fmt.Sprintf("layer %s operation %s failed: %v", e.Layer, e.Operation, e.Cause)
	}
	return fmt.Sprintf("layer operation %s failed: %v", e.Operation, e.Cause)
}

// NewLayerError creates a new LayerError
func NewLayerError(operation, layer string, cause error) *LayerError {
	return &LayerError{
		Operation: operation,
		Layer:     layer,
		Cause:     cause,
	}
}

// DigestFromReader calculates SHA256 digest from a reader
func DigestFromReader(r io.Reader) (string, int64, error) {
	hasher := sha256.New()
	size, err := io.Copy(hasher, r)
	if err != nil {
		return "", 0, err
	}
	
	digest := fmt.Sprintf("sha256:%x", hasher.Sum(nil))
	return digest, size, nil
}

// ValidateDigest validates that a digest matches the expected format
func ValidateDigest(digest string) error {
	if len(digest) < 7 || digest[:7] != "sha256:" {
		return fmt.Errorf("invalid digest format: %s", digest)
	}
	
	if len(digest) != 71 { // "sha256:" + 64 hex characters
		return fmt.Errorf("invalid digest length: %s", digest)
	}
	
	return nil
}