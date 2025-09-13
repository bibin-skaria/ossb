package layers

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
)

// DefaultLayerManager implements the LayerManager interface
type DefaultLayerManager struct {
	config LayerConfig
	cache  map[string]*Layer // Digest -> Layer cache for deduplication
}

// NewLayerManager creates a new DefaultLayerManager
func NewLayerManager(config LayerConfig) *DefaultLayerManager {
	if config.Compression == "" {
		config.Compression = CompressionGzip
	}
	
	return &DefaultLayerManager{
		config: config,
		cache:  make(map[string]*Layer),
	}
}

// CreateLayer creates a new OCI-compliant layer from filesystem changes
func (lm *DefaultLayerManager) CreateLayer(changes []FileChange) (*Layer, error) {
	if len(changes) == 0 && lm.config.SkipEmpty {
		return &Layer{
			EmptyLayer: true,
			Created:    time.Now(),
			MediaType:  lm.config.Compression.GetMediaType(),
		}, nil
	}

	// Sort changes by path for consistent layer generation
	sort.Slice(changes, func(i, j int) bool {
		return changes[i].Path < changes[j].Path
	})

	// Create tar archive with changes
	var buf bytes.Buffer
	tarWriter := tar.NewWriter(&buf)

	for _, change := range changes {
		if err := lm.addChangeToTar(tarWriter, change); err != nil {
			tarWriter.Close()
			return nil, NewLayerError("create", "", fmt.Errorf("failed to add change %s: %v", change.Path, err))
		}
	}

	if err := tarWriter.Close(); err != nil {
		return nil, NewLayerError("create", "", fmt.Errorf("failed to close tar writer: %v", err))
	}

	// Compress the tar archive
	compressed, err := lm.compressData(buf.Bytes())
	if err != nil {
		return nil, NewLayerError("create", "", fmt.Errorf("failed to compress layer: %v", err))
	}

	// Calculate digest
	digest, size, err := DigestFromReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, NewLayerError("create", "", fmt.Errorf("failed to calculate digest: %v", err))
	}

	// Check if we already have this layer (deduplication)
	if existingLayer, exists := lm.cache[digest]; exists {
		return existingLayer, nil
	}

	layer := &Layer{
		Digest:    digest,
		Size:      size,
		MediaType: lm.config.Compression.GetMediaType(),
		Blob:      io.NopCloser(bytes.NewReader(compressed)),
		Created:   time.Now(),
	}

	if lm.config.Timestamp != nil {
		layer.Created = *lm.config.Timestamp
	}

	// Cache the layer for deduplication
	lm.cache[digest] = layer

	return layer, nil
}

// addChangeToTar adds a single file change to the tar archive
func (lm *DefaultLayerManager) addChangeToTar(tw *tar.Writer, change FileChange) error {
	header := &tar.Header{
		Name:     strings.TrimPrefix(change.Path, "/"),
		Mode:     int64(change.Mode & 0777), // Only keep permission bits
		ModTime:  change.Timestamp,
		Uid:      change.UID,
		Gid:      change.GID,
		Linkname: change.Linkname,
	}

	switch change.Type {
	case ChangeTypeAdd, ChangeTypeModify:
		if change.Mode.IsDir() {
			header.Typeflag = tar.TypeDir
			header.Size = 0
		} else if change.Mode&os.ModeSymlink != 0 {
			header.Typeflag = tar.TypeSymlink
			header.Size = 0
		} else if change.Mode.IsRegular() {
			header.Typeflag = tar.TypeReg
			header.Size = change.Size
		} else {
			return fmt.Errorf("unsupported file mode: %v", change.Mode)
		}

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		if change.Content != nil && header.Typeflag == tar.TypeReg && header.Size > 0 {
			written, err := io.Copy(tw, change.Content)
			if err != nil {
				return err
			}
			if written != header.Size {
				return fmt.Errorf("size mismatch: expected %d bytes, wrote %d bytes", header.Size, written)
			}
		}

	case ChangeTypeDelete:
		// Create whiteout file for deletions
		whiteoutName := filepath.Dir(header.Name) + "/.wh." + filepath.Base(header.Name)
		whiteoutHeader := &tar.Header{
			Name:     whiteoutName,
			Mode:     0644,
			ModTime:  change.Timestamp,
			Typeflag: tar.TypeReg,
			Size:     0,
		}

		if err := tw.WriteHeader(whiteoutHeader); err != nil {
			return err
		}

	default:
		return fmt.Errorf("unknown change type: %v", change.Type)
	}

	return nil
}

// compressData compresses data according to the configured compression type
func (lm *DefaultLayerManager) compressData(data []byte) ([]byte, error) {
	switch lm.config.Compression {
	case CompressionNone:
		return data, nil

	case CompressionGzip:
		var buf bytes.Buffer
		gzWriter := gzip.NewWriter(&buf)
		if _, err := gzWriter.Write(data); err != nil {
			return nil, err
		}
		if err := gzWriter.Close(); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil

	case CompressionZstd:
		encoder, err := zstd.NewWriter(nil)
		if err != nil {
			return nil, err
		}
		defer encoder.Close()
		return encoder.EncodeAll(data, nil), nil

	default:
		return nil, fmt.Errorf("unsupported compression type: %v", lm.config.Compression)
	}
}

// ExtractLayer extracts a layer to the specified target path
func (lm *DefaultLayerManager) ExtractLayer(layer *Layer, targetPath string) error {
	if layer.EmptyLayer {
		return nil // Nothing to extract
	}

	if layer.Blob == nil {
		return NewLayerError("extract", layer.Digest, fmt.Errorf("layer blob is nil"))
	}

	// Decompress the layer data
	decompressed, err := lm.decompressLayer(layer)
	if err != nil {
		return NewLayerError("extract", layer.Digest, fmt.Errorf("failed to decompress: %v", err))
	}
	defer decompressed.Close()

	// Extract tar archive
	tarReader := tar.NewReader(decompressed)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return NewLayerError("extract", layer.Digest, fmt.Errorf("failed to read tar header: %v", err))
		}

		if err := lm.extractTarEntry(tarReader, header, targetPath); err != nil {
			return NewLayerError("extract", layer.Digest, fmt.Errorf("failed to extract %s: %v", header.Name, err))
		}
	}

	return nil
}

// decompressLayer decompresses a layer based on its media type
func (lm *DefaultLayerManager) decompressLayer(layer *Layer) (io.ReadCloser, error) {
	switch layer.MediaType {
	case MediaTypeImageLayer:
		return layer.Blob, nil

	case MediaTypeImageLayerGzip:
		return gzip.NewReader(layer.Blob)

	case MediaTypeImageLayerZstd:
		decoder, err := zstd.NewReader(layer.Blob)
		if err != nil {
			return nil, err
		}
		return io.NopCloser(decoder), nil

	default:
		return nil, fmt.Errorf("unsupported media type: %s", layer.MediaType)
	}
}

// extractTarEntry extracts a single tar entry to the filesystem
func (lm *DefaultLayerManager) extractTarEntry(tr *tar.Reader, header *tar.Header, basePath string) error {
	targetPath := filepath.Join(basePath, header.Name)

	// Handle whiteout files (deletions)
	if strings.Contains(filepath.Base(header.Name), ".wh.") {
		return lm.handleWhiteout(header.Name, basePath)
	}

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		return err
	}

	switch header.Typeflag {
	case tar.TypeDir:
		return os.MkdirAll(targetPath, os.FileMode(header.Mode))

	case tar.TypeReg:
		file, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
		if err != nil {
			return err
		}
		defer file.Close()

		if _, err := io.Copy(file, tr); err != nil {
			return err
		}

		return os.Chtimes(targetPath, header.ModTime, header.ModTime)

	case tar.TypeSymlink:
		return os.Symlink(header.Linkname, targetPath)

	default:
		return fmt.Errorf("unsupported tar entry type: %v", header.Typeflag)
	}
}

// handleWhiteout processes whiteout files for layer deletions
func (lm *DefaultLayerManager) handleWhiteout(whiteoutPath, basePath string) error {
	dir := filepath.Dir(whiteoutPath)
	filename := filepath.Base(whiteoutPath)

	if !strings.HasPrefix(filename, ".wh.") {
		return fmt.Errorf("invalid whiteout file: %s", filename)
	}

	// Extract the original filename
	originalName := strings.TrimPrefix(filename, ".wh.")
	targetPath := filepath.Join(basePath, dir, originalName)

	// Remove the file/directory
	return os.RemoveAll(targetPath)
}

// OptimizeLayers optimizes a sequence of layers by removing redundant operations
func (lm *DefaultLayerManager) OptimizeLayers(layers []*Layer) ([]*Layer, error) {
	if len(layers) <= 1 {
		return layers, nil
	}

	// For now, just remove empty layers
	optimized := make([]*Layer, 0, len(layers))
	for _, layer := range layers {
		if !layer.EmptyLayer || layer.Size > 0 {
			optimized = append(optimized, layer)
		}
	}

	return optimized, nil
}

// DeduplicateLayers removes duplicate layers based on their digest
func (lm *DefaultLayerManager) DeduplicateLayers(layers []*Layer) ([]*Layer, error) {
	seen := make(map[string]bool)
	deduplicated := make([]*Layer, 0, len(layers))

	for _, layer := range layers {
		if !seen[layer.Digest] {
			seen[layer.Digest] = true
			deduplicated = append(deduplicated, layer)
		}
	}

	return deduplicated, nil
}

// GetLayerDigest returns the digest of a layer
func (lm *DefaultLayerManager) GetLayerDigest(layer *Layer) (string, error) {
	if layer.Digest == "" {
		return "", NewLayerError("digest", "", fmt.Errorf("layer digest is empty"))
	}
	return layer.Digest, nil
}

// GetLayerSize returns the size of a layer
func (lm *DefaultLayerManager) GetLayerSize(layer *Layer) (int64, error) {
	return layer.Size, nil
}

// ValidateLayer validates that a layer conforms to OCI specifications
func (lm *DefaultLayerManager) ValidateLayer(layer *Layer) error {
	if layer == nil {
		return NewLayerError("validate", "", fmt.Errorf("layer is nil"))
	}

	if layer.EmptyLayer {
		return nil // Empty layers are valid
	}

	// Validate digest format
	if err := ValidateDigest(layer.Digest); err != nil {
		return NewLayerError("validate", layer.Digest, err)
	}

	// Validate media type
	validMediaTypes := []string{
		MediaTypeImageLayer,
		MediaTypeImageLayerGzip,
		MediaTypeImageLayerZstd,
		MediaTypeImageLayerNonDistrib,
	}

	validMediaType := false
	for _, mt := range validMediaTypes {
		if layer.MediaType == mt {
			validMediaType = true
			break
		}
	}

	if !validMediaType {
		return NewLayerError("validate", layer.Digest, fmt.Errorf("invalid media type: %s", layer.MediaType))
	}

	// Validate size
	if layer.Size < 0 {
		return NewLayerError("validate", layer.Digest, fmt.Errorf("invalid size: %d", layer.Size))
	}

	return nil
}