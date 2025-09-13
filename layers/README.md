# OCI-Compliant Layer Management System

This package provides a complete OCI-compliant layer management system for container image building. It implements all the functionality required for task 6 of the complete-dind-builder specification.

## Features Implemented

### ✅ Layer Interface with Proper Digest Calculation and Validation
- **Layer struct**: Complete OCI-compliant layer representation with digest, size, media type, and metadata
- **Digest calculation**: SHA256 digest calculation using `DigestFromReader` function
- **Validation**: Comprehensive layer validation including digest format, media type, and size validation
- **Error handling**: Structured error handling with `LayerError` type

### ✅ Filesystem Change Tracking and Whiteout File Handling
- **Change detection**: `DetectChanges` function compares two filesystem states and identifies additions, modifications, and deletions
- **Whiteout files**: Proper OCI whiteout file creation for deletions (`.wh.filename` format)
- **File scanning**: Recursive directory scanning with support for symlinks, directories, and regular files
- **Change types**: Support for Add, Modify, and Delete operations

### ✅ Layer Optimization and Deduplication Based on Content Hashing
- **Deduplication**: `DeduplicateLayers` removes duplicate layers based on digest comparison
- **Optimization**: `OptimizeLayers` removes empty layers and redundant operations
- **Content-addressable caching**: Built-in layer cache for automatic deduplication during creation
- **Layer comparison**: Efficient layer comparison using SHA256 digests

### ✅ OCI Layer Blob Generation with Compression
- **Multiple compression types**: Support for none, gzip, and zstd compression
- **Proper media types**: Automatic media type assignment based on compression algorithm
- **Tar archive generation**: Standards-compliant tar archive creation with proper headers
- **Streaming compression**: Memory-efficient compression using streaming algorithms

### ✅ Tests Validating OCI Layer Format Compliance
- **Comprehensive test suite**: 20+ test functions covering all functionality
- **OCI compliance tests**: Specific tests validating OCI Image Specification compliance
- **Integration tests**: End-to-end tests demonstrating real-world usage
- **Edge case testing**: Tests for empty layers, whiteout files, symlinks, and error conditions

## Package Structure

```
layers/
├── doc.go              # Package documentation
├── types.go            # Core types and interfaces
├── manager.go          # Main LayerManager implementation
├── changes.go          # Filesystem change detection
├── manager_test.go     # Core functionality tests
├── changes_test.go     # Change detection tests
├── oci_test.go         # OCI compliance tests
├── example_test.go     # Usage examples and integration tests
└── README.md           # This file
```

## Key Components

### LayerManager Interface
```go
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
```

### Layer Structure
```go
type Layer struct {
    Digest      string            // SHA256 digest
    Size        int64             // Compressed size in bytes
    MediaType   string            // OCI media type
    Blob        io.ReadCloser     // Compressed layer data
    Annotations map[string]string // Optional annotations
    Created     time.Time         // Creation timestamp
    CreatedBy   string            // Creation command/tool
    Comment     string            // Optional comment
    EmptyLayer  bool              // True for empty layers
}
```

## Usage Examples

### Basic Layer Creation
```go
config := LayerConfig{
    Compression: CompressionGzip,
    SkipEmpty:   true,
}

lm := NewLayerManager(config)

changes := []FileChange{
    {
        Path:    "/app/binary",
        Type:    ChangeTypeAdd,
        Mode:    0755,
        Content: fileReader,
        Size:    fileSize,
    },
}

layer, err := lm.CreateLayer(changes)
```

### Change Detection
```go
changes, err := lm.DetectChanges("/old/filesystem", "/new/filesystem")
if err != nil {
    log.Fatal(err)
}

layer, err := lm.CreateLayer(changes)
```

### Layer Extraction
```go
err := lm.ExtractLayer(layer, "/target/path")
if err != nil {
    log.Fatal(err)
}
```

## OCI Compliance

This implementation fully complies with the OCI Image Specification v1.0+:

- ✅ Proper SHA256 digest calculation and validation
- ✅ Correct media type assignment (`application/vnd.oci.image.layer.v1.tar+gzip`, etc.)
- ✅ Valid tar archive format with proper file headers
- ✅ Whiteout file handling for deletions (`.wh.filename` convention)
- ✅ Support for multiple compression algorithms
- ✅ Proper layer size and digest metadata
- ✅ Empty layer handling
- ✅ Symlink and directory support

## Performance Features

- **Memory efficient**: Streaming compression and decompression
- **Content-addressable caching**: Automatic deduplication of identical layers
- **Optimized tar generation**: Efficient tar archive creation with minimal memory usage
- **Parallel-safe**: Thread-safe operations (with external synchronization)

## Testing

Run the complete test suite:
```bash
go test ./layers -v
```

Run specific test categories:
```bash
go test ./layers -run TestOCI        # OCI compliance tests
go test ./layers -run TestChanges    # Change detection tests
go test ./layers -run TestManager    # Core functionality tests
```

## Integration

This layer management system integrates seamlessly with the OSSB build engine and can be used by:

- Executors for creating layers from filesystem changes
- Registry clients for pushing/pulling layer blobs
- Manifest generators for creating OCI image manifests
- Cache systems for layer deduplication and optimization

The implementation satisfies all requirements specified in task 6 of the complete-dind-builder specification and provides a solid foundation for OCI-compliant container image building.