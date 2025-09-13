# Manifest Package

The manifest package provides OCI-compliant image manifest and configuration generation for OSSB (Open Source Slim Builder). This package implements the OCI Image Specification v1.0+ for generating image configurations, manifests, and multi-architecture manifest lists.

## Features

- **Image Configuration Generation**: Creates OCI image configurations from Dockerfile instructions with proper metadata
- **Image Manifest Generation**: Generates OCI image manifests with layer references and platform information
- **Multi-Architecture Support**: Creates manifest lists for multi-architecture builds
- **Full OCI Compliance**: Ensures compliance with OCI Image Specification v1.0+
- **Comprehensive Validation**: Validates all generated artifacts for correctness
- **Digest Calculation**: Proper SHA256 digest calculation for all components

## Usage

### Basic Usage

```go
package main

import (
    "fmt"
    "github.com/bibin-skaria/ossb/manifest"
    "github.com/bibin-skaria/ossb/internal/types"
    "github.com/bibin-skaria/ossb/layers"
)

func main() {
    // Create a new manifest generator
    generator := manifest.NewGenerator(nil)
    
    // Define Dockerfile instructions
    instructions := []types.DockerfileInstruction{
        {Command: "FROM", Value: "alpine:latest", Line: 1},
        {Command: "RUN", Value: "apk add --no-cache curl", Line: 2},
        {Command: "WORKDIR", Value: "/app", Line: 3},
        {Command: "CMD", Value: `["echo", "hello world"]`, Line: 4},
    }
    
    // Define target platform
    platform := types.Platform{
        OS:           "linux",
        Architecture: "amd64",
    }
    
    // Generate image configuration
    config, err := generator.GenerateImageConfig(instructions, platform)
    if err != nil {
        panic(err)
    }
    
    // Create sample layers (in real usage, these come from the build process)
    layers := []*layers.Layer{
        {
            Digest:    "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
            Size:      1024,
            MediaType: layers.MediaTypeImageLayerGzip,
            CreatedBy: "FROM alpine:latest",
        },
        {
            Digest:    "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
            Size:      2048,
            MediaType: layers.MediaTypeImageLayerGzip,
            CreatedBy: "RUN apk add --no-cache curl",
        },
    }
    
    // Generate image manifest
    manifest, err := generator.GenerateImageManifest(config, layers)
    if err != nil {
        panic(err)
    }
    
    fmt.Printf("Generated manifest with %d layers\n", len(manifest.Layers))
}
```

### Multi-Architecture Builds

```go
// Create platform manifests for different architectures
amd64Manifest := &manifest.ImageManifest{
    SchemaVersion: 2,
    MediaType:     manifest.MediaTypeOCIManifest,
    // ... other fields
}

arm64Manifest := &manifest.ImageManifest{
    SchemaVersion: 2,
    MediaType:     manifest.MediaTypeOCIManifest,
    // ... other fields
}

// Create platform manifest entries
platformManifests := []manifest.PlatformManifest{
    {
        MediaType: manifest.MediaTypeOCIManifest,
        Size:      1024,
        Digest:    "sha256:amd64digest...",
        Platform: manifest.Platform{
            Architecture: "amd64",
            OS:           "linux",
        },
    },
    {
        MediaType: manifest.MediaTypeOCIManifest,
        Size:      1536,
        Digest:    "sha256:arm64digest...",
        Platform: manifest.Platform{
            Architecture: "arm64",
            OS:           "linux",
        },
    },
}

// Generate manifest list
manifestList, err := generator.GenerateManifestList(platformManifests)
if err != nil {
    panic(err)
}

fmt.Printf("Generated manifest list with %d platforms\n", len(manifestList.Manifests))
```

### Custom Generator Options

```go
options := &manifest.GeneratorOptions{
    DefaultUser:       "app",
    DefaultWorkingDir: "/app",
    DefaultShell:      []string{"/bin/bash", "-c"},
    IncludeHistory:    true,
    Timestamp:         &customTimestamp, // For reproducible builds
}

generator := manifest.NewGenerator(options)
```

## Supported Dockerfile Instructions

The manifest generator supports all standard Dockerfile instructions:

- **FROM**: Base image specification (recorded in history)
- **RUN**: Command execution (creates layers, recorded in history)
- **CMD**: Default command
- **ENTRYPOINT**: Container entrypoint
- **ENV**: Environment variables
- **EXPOSE**: Exposed ports
- **VOLUME**: Volume declarations
- **WORKDIR**: Working directory
- **USER**: User specification
- **LABEL**: Metadata labels
- **COPY/ADD**: File operations (creates layers, recorded in history)
- **SHELL**: Default shell
- **STOPSIGNAL**: Stop signal
- **HEALTHCHECK**: Health check configuration
- **ONBUILD**: Trigger instructions

## Validation

The package provides comprehensive validation for all generated artifacts:

```go
// Validate image manifest
err := generator.ValidateImageManifest(manifest)
if err != nil {
    fmt.Printf("Manifest validation failed: %v\n", err)
}

// Validate image configuration
err = generator.ValidateImageConfig(config)
if err != nil {
    fmt.Printf("Config validation failed: %v\n", err)
}

// Validate manifest list
err = generator.ValidateManifestList(manifestList)
if err != nil {
    fmt.Printf("Manifest list validation failed: %v\n", err)
}
```

## Digest Calculation

The package provides utilities for calculating digests:

```go
// Calculate manifest digest
digest, err := generator.CalculateManifestDigest(manifest)
if err != nil {
    panic(err)
}
fmt.Printf("Manifest digest: %s\n", digest)

// Calculate config digest
configDigest, err := generator.CalculateConfigDigest(config)
if err != nil {
    panic(err)
}
fmt.Printf("Config digest: %s\n", configDigest)
```

## Serialization

Convert manifests and configurations to JSON:

```go
// Serialize manifest to JSON
manifestBytes, err := generator.SerializeManifest(manifest)
if err != nil {
    panic(err)
}

// Serialize config to JSON
configBytes, err := generator.SerializeConfig(config)
if err != nil {
    panic(err)
}

// Serialize manifest list to JSON
manifestListBytes, err := generator.SerializeManifestList(manifestList)
if err != nil {
    panic(err)
}
```

## Error Handling

The package provides structured error handling with specific error types:

```go
if err != nil {
    if manifestErr, ok := err.(*manifest.ManifestError); ok {
        switch manifestErr.Type {
        case manifest.ErrorTypeValidation:
            fmt.Printf("Validation error: %s\n", manifestErr.Message)
        case manifest.ErrorTypeGeneration:
            fmt.Printf("Generation error: %s\n", manifestErr.Message)
        case manifest.ErrorTypeDigest:
            fmt.Printf("Digest calculation error: %s\n", manifestErr.Message)
        case manifest.ErrorTypeSerialization:
            fmt.Printf("Serialization error: %s\n", manifestErr.Message)
        }
    }
}
```

## OCI Compliance

This package ensures full compliance with:

- **OCI Image Specification v1.0+**
- **OCI Distribution Specification**
- **Docker Image Manifest V2, Schema 2** (for compatibility)

All generated manifests, configurations, and manifest lists conform to these specifications and pass validation with official OCI tools.

## Testing

The package includes comprehensive tests covering:

- Manifest generation from various Dockerfile instruction combinations
- Multi-architecture manifest list creation
- Validation of all OCI specification requirements
- Digest calculation accuracy
- Error handling scenarios
- Edge cases and malformed inputs

Run tests with:

```bash
go test ./manifest/... -v
```

## Integration

This package is designed to integrate seamlessly with other OSSB components:

- **layers**: Provides layer information for manifest generation
- **internal/types**: Provides common types and platform definitions
- **registry**: Uses generated manifests for push operations
- **engine**: Orchestrates the build process using manifest generation

The manifest package serves as a critical component in OSSB's container image building pipeline, ensuring that all generated artifacts are OCI-compliant and ready for distribution.