package layers

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewLayerManager(t *testing.T) {
	config := LayerConfig{
		Compression: CompressionGzip,
		SkipEmpty:   true,
	}

	lm := NewLayerManager(config)
	if lm == nil {
		t.Fatal("NewLayerManager returned nil")
	}

	if lm.config.Compression != CompressionGzip {
		t.Errorf("Expected compression %v, got %v", CompressionGzip, lm.config.Compression)
	}

	if !lm.config.SkipEmpty {
		t.Error("Expected SkipEmpty to be true")
	}
}

func TestCreateEmptyLayer(t *testing.T) {
	config := LayerConfig{
		Compression: CompressionGzip,
		SkipEmpty:   true,
	}

	lm := NewLayerManager(config)
	layer, err := lm.CreateLayer([]FileChange{})
	if err != nil {
		t.Fatalf("CreateLayer failed: %v", err)
	}

	if !layer.EmptyLayer {
		t.Error("Expected empty layer")
	}

	if layer.MediaType != CompressionGzip.GetMediaType() {
		t.Errorf("Expected media type %s, got %s", CompressionGzip.GetMediaType(), layer.MediaType)
	}
}

func TestCreateLayerWithChanges(t *testing.T) {
	config := LayerConfig{
		Compression: CompressionGzip,
		SkipEmpty:   false,
	}

	lm := NewLayerManager(config)

	// Create test changes
	content := strings.NewReader("Hello, World!")
	changes := []FileChange{
		{
			Path:      "/test.txt",
			Type:      ChangeTypeAdd,
			Mode:      0644,
			Size:      13,
			Content:   content,
			Timestamp: time.Now(),
		},
		{
			Path:      "/testdir",
			Type:      ChangeTypeAdd,
			Mode:      os.ModeDir | 0755,
			Size:      0,
			Timestamp: time.Now(),
		},
	}

	layer, err := lm.CreateLayer(changes)
	if err != nil {
		t.Fatalf("CreateLayer failed: %v", err)
	}

	if layer.EmptyLayer {
		t.Error("Expected non-empty layer")
	}

	if layer.Digest == "" {
		t.Error("Expected layer digest to be set")
	}

	if layer.Size <= 0 {
		t.Error("Expected layer size to be positive")
	}

	if layer.MediaType != MediaTypeImageLayerGzip {
		t.Errorf("Expected media type %s, got %s", MediaTypeImageLayerGzip, layer.MediaType)
	}
}

func TestCreateLayerWithDeletion(t *testing.T) {
	config := LayerConfig{
		Compression: CompressionNone,
		SkipEmpty:   false,
	}

	lm := NewLayerManager(config)

	changes := []FileChange{
		{
			Path:      "/deleted.txt",
			Type:      ChangeTypeDelete,
			Mode:      0644,
			Timestamp: time.Now(),
		},
	}

	layer, err := lm.CreateLayer(changes)
	if err != nil {
		t.Fatalf("CreateLayer failed: %v", err)
	}

	if layer.EmptyLayer {
		t.Error("Expected non-empty layer")
	}

	// Verify whiteout file is created in the layer
	if layer.Blob == nil {
		t.Fatal("Layer blob is nil")
	}

	// Read the layer content to verify whiteout file
	data, err := io.ReadAll(layer.Blob)
	if err != nil {
		t.Fatalf("Failed to read layer blob: %v", err)
	}

	// The layer should contain a whiteout file
	if !bytes.Contains(data, []byte(".wh.deleted.txt")) {
		t.Error("Expected whiteout file in layer")
	}
}

func TestExtractLayer(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "layer_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := LayerConfig{
		Compression: CompressionNone,
		SkipEmpty:   false,
	}

	lm := NewLayerManager(config)

	// Create a layer with test content
	content := strings.NewReader("Test content")
	changes := []FileChange{
		{
			Path:      "/extracted.txt",
			Type:      ChangeTypeAdd,
			Mode:      0644,
			Size:      12,
			Content:   content,
			Timestamp: time.Now(),
		},
	}

	layer, err := lm.CreateLayer(changes)
	if err != nil {
		t.Fatalf("CreateLayer failed: %v", err)
	}

	// Extract the layer
	extractDir := filepath.Join(tempDir, "extract")
	if err := os.MkdirAll(extractDir, 0755); err != nil {
		t.Fatalf("Failed to create extract dir: %v", err)
	}

	if err := lm.ExtractLayer(layer, extractDir); err != nil {
		t.Fatalf("ExtractLayer failed: %v", err)
	}

	// Verify extracted file
	extractedFile := filepath.Join(extractDir, "extracted.txt")
	if _, err := os.Stat(extractedFile); os.IsNotExist(err) {
		t.Error("Extracted file does not exist")
	}

	// Verify file content
	extractedContent, err := os.ReadFile(extractedFile)
	if err != nil {
		t.Fatalf("Failed to read extracted file: %v", err)
	}

	if string(extractedContent) != "Test content" {
		t.Errorf("Expected 'Test content', got '%s'", string(extractedContent))
	}
}

func TestLayerDeduplication(t *testing.T) {
	config := LayerConfig{
		Compression: CompressionNone,
		SkipEmpty:   false,
	}

	lm := NewLayerManager(config)

	// Create identical changes
	content1 := strings.NewReader("Same content")
	content2 := strings.NewReader("Same content")

	changes := []FileChange{
		{
			Path:      "/same.txt",
			Type:      ChangeTypeAdd,
			Mode:      0644,
			Size:      12,
			Content:   content1,
			Timestamp: time.Unix(1234567890, 0), // Fixed timestamp for consistency
		},
	}

	changes2 := []FileChange{
		{
			Path:      "/same.txt",
			Type:      ChangeTypeAdd,
			Mode:      0644,
			Size:      12,
			Content:   content2,
			Timestamp: time.Unix(1234567890, 0), // Same timestamp
		},
	}

	layer1, err := lm.CreateLayer(changes)
	if err != nil {
		t.Fatalf("CreateLayer failed: %v", err)
	}

	layer2, err := lm.CreateLayer(changes2)
	if err != nil {
		t.Fatalf("CreateLayer failed: %v", err)
	}

	// Layers should have the same digest (deduplication)
	if layer1.Digest != layer2.Digest {
		t.Errorf("Expected same digest for identical layers, got %s and %s", layer1.Digest, layer2.Digest)
	}

	// Test DeduplicateLayers function
	layers := []*Layer{layer1, layer2}
	deduplicated, err := lm.DeduplicateLayers(layers)
	if err != nil {
		t.Fatalf("DeduplicateLayers failed: %v", err)
	}

	if len(deduplicated) != 1 {
		t.Errorf("Expected 1 deduplicated layer, got %d", len(deduplicated))
	}
}

func TestValidateLayer(t *testing.T) {
	config := LayerConfig{
		Compression: CompressionGzip,
	}

	lm := NewLayerManager(config)

	// Test valid layer
	validLayer := &Layer{
		Digest:    "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
		Size:      100,
		MediaType: MediaTypeImageLayerGzip,
		Created:   time.Now(),
	}

	if err := lm.ValidateLayer(validLayer); err != nil {
		t.Errorf("ValidateLayer failed for valid layer: %v", err)
	}

	// Test invalid digest
	invalidDigestLayer := &Layer{
		Digest:    "invalid-digest",
		Size:      100,
		MediaType: MediaTypeImageLayerGzip,
		Created:   time.Now(),
	}

	if err := lm.ValidateLayer(invalidDigestLayer); err == nil {
		t.Error("Expected validation error for invalid digest")
	}

	// Test invalid media type
	invalidMediaTypeLayer := &Layer{
		Digest:    "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
		Size:      100,
		MediaType: "invalid/media-type",
		Created:   time.Now(),
	}

	if err := lm.ValidateLayer(invalidMediaTypeLayer); err == nil {
		t.Error("Expected validation error for invalid media type")
	}

	// Test negative size
	negativeSizeLayer := &Layer{
		Digest:    "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
		Size:      -1,
		MediaType: MediaTypeImageLayerGzip,
		Created:   time.Now(),
	}

	if err := lm.ValidateLayer(negativeSizeLayer); err == nil {
		t.Error("Expected validation error for negative size")
	}

	// Test empty layer (should be valid)
	emptyLayer := &Layer{
		EmptyLayer: true,
		Created:    time.Now(),
	}

	if err := lm.ValidateLayer(emptyLayer); err != nil {
		t.Errorf("ValidateLayer failed for empty layer: %v", err)
	}
}

func TestCompressionTypes(t *testing.T) {
	testCases := []struct {
		compression CompressionType
		mediaType   string
	}{
		{CompressionNone, MediaTypeImageLayer},
		{CompressionGzip, MediaTypeImageLayerGzip},
		{CompressionZstd, MediaTypeImageLayerZstd},
	}

	for _, tc := range testCases {
		t.Run(string(tc.compression), func(t *testing.T) {
			config := LayerConfig{
				Compression: tc.compression,
				SkipEmpty:   false,
			}

			lm := NewLayerManager(config)

			changes := []FileChange{
				{
					Path:      "/test.txt",
					Type:      ChangeTypeAdd,
					Mode:      0644,
					Size:      15, // "Test compression" is actually 16 bytes, but let's use 15 to match content
					Content:   strings.NewReader("Test compressio"), // 15 bytes exactly
					Timestamp: time.Now(),
				},
			}

			layer, err := lm.CreateLayer(changes)
			if err != nil {
				t.Fatalf("CreateLayer failed: %v", err)
			}

			if layer.MediaType != tc.mediaType {
				t.Errorf("Expected media type %s, got %s", tc.mediaType, layer.MediaType)
			}

			// Test extraction to verify compression/decompression works
			tempDir, err := os.MkdirTemp("", "compression_test")
			if err != nil {
				t.Fatalf("Failed to create temp dir: %v", err)
			}
			defer os.RemoveAll(tempDir)

			if err := lm.ExtractLayer(layer, tempDir); err != nil {
				t.Fatalf("ExtractLayer failed: %v", err)
			}

			// Verify extracted content
			extractedFile := filepath.Join(tempDir, "test.txt")
			extractedContent, err := os.ReadFile(extractedFile)
			if err != nil {
				t.Fatalf("Failed to read extracted file: %v", err)
			}

			if string(extractedContent) != "Test compressio" {
				t.Errorf("Expected 'Test compressio', got '%s'", string(extractedContent))
			}
		})
	}
}

func TestOptimizeLayers(t *testing.T) {
	config := LayerConfig{
		Compression: CompressionNone,
	}

	lm := NewLayerManager(config)

	layers := []*Layer{
		{
			Digest:     "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			Size:       100,
			MediaType:  MediaTypeImageLayer,
			EmptyLayer: false,
		},
		{
			EmptyLayer: true,
			Size:       0,
			MediaType:  MediaTypeImageLayer,
		},
		{
			Digest:     "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			Size:       200,
			MediaType:  MediaTypeImageLayer,
			EmptyLayer: false,
		},
	}

	optimized, err := lm.OptimizeLayers(layers)
	if err != nil {
		t.Fatalf("OptimizeLayers failed: %v", err)
	}

	// Should remove the empty layer
	if len(optimized) != 2 {
		t.Errorf("Expected 2 optimized layers, got %d", len(optimized))
	}

	// Verify non-empty layers remain
	for _, layer := range optimized {
		if layer.EmptyLayer && layer.Size == 0 {
			t.Error("Empty layer should have been removed")
		}
	}
}