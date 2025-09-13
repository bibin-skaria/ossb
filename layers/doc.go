// Package layers provides OCI-compliant layer management functionality for container image building.
//
// This package implements the layer management system required for building container images
// according to the OCI Image Specification. It provides functionality for:
//
//   - Creating filesystem layers from file changes
//   - Detecting changes between filesystem states
//   - Compressing and decompressing layers using various algorithms
//   - Validating layer compliance with OCI specifications
//   - Optimizing and deduplicating layers
//   - Handling whiteout files for deletions
//
// # Layer Creation
//
// Layers are created from a list of FileChange objects that represent filesystem modifications:
//
//	config := LayerConfig{
//		Compression: CompressionGzip,
//		SkipEmpty:   true,
//	}
//	
//	lm := NewLayerManager(config)
//	
//	changes := []FileChange{
//		{
//			Path:    "/app/binary",
//			Type:    ChangeTypeAdd,
//			Mode:    0755,
//			Content: fileReader,
//		},
//	}
//	
//	layer, err := lm.CreateLayer(changes)
//
// # Change Detection
//
// The package can automatically detect changes between two filesystem states:
//
//	changes, err := lm.DetectChanges("/old/filesystem", "/new/filesystem")
//	if err != nil {
//		log.Fatal(err)
//	}
//	
//	layer, err := lm.CreateLayer(changes)
//
// # Layer Extraction
//
// Layers can be extracted to a target filesystem:
//
//	err := lm.ExtractLayer(layer, "/target/path")
//	if err != nil {
//		log.Fatal(err)
//	}
//
// # OCI Compliance
//
// All layers created by this package comply with the OCI Image Specification v1.0+:
//
//   - Proper SHA256 digest calculation
//   - Correct media type assignment based on compression
//   - Valid tar archive format with proper headers
//   - Whiteout file handling for deletions
//   - Support for various compression algorithms (gzip, zstd, none)
//
// # Supported Compression Types
//
//   - CompressionNone: Uncompressed tar archives
//   - CompressionGzip: Gzip-compressed tar archives (default)
//   - CompressionZstd: Zstandard-compressed tar archives
//
// # Error Handling
//
// The package provides structured error handling through the LayerError type,
// which includes context about the operation that failed and the layer involved.
//
// # Thread Safety
//
// The LayerManager is not thread-safe. If concurrent access is required,
// external synchronization must be provided.
package layers