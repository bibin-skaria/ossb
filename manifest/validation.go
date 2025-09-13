package manifest

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/bibin-skaria/ossb/layers"
)

// OCI specification constants for validation
const (
	// Maximum sizes according to OCI spec
	MaxManifestSize     = 4 * 1024 * 1024  // 4MB
	MaxConfigSize       = 8 * 1024 * 1024  // 8MB
	MaxManifestListSize = 4 * 1024 * 1024  // 4MB
	
	// Schema versions
	OCISchemaVersion    = 2
	DockerSchemaVersion = 2
	
	// Digest format
	DigestRegexPattern = `^sha256:[a-f0-9]{64}$`
)

var (
	// Compiled regex for digest validation
	digestRegex = regexp.MustCompile(DigestRegexPattern)
	
	// Valid media types
	validManifestMediaTypes = map[string]bool{
		MediaTypeOCIManifest:        true,
		MediaTypeDockerManifest:     true,
	}
	
	validIndexMediaTypes = map[string]bool{
		MediaTypeOCIIndex:           true,
		MediaTypeDockerManifestList: true,
	}
	
	validConfigMediaTypes = map[string]bool{
		MediaTypeOCIConfig:      true,
		MediaTypeDockerConfig:   true,
	}
	
	validLayerMediaTypes = map[string]bool{
		MediaTypeOCILayer:                    true,
		MediaTypeOCILayerGzip:                true,
		MediaTypeOCILayerZstd:                true,
		MediaTypeDockerLayer:                 true,
		layers.MediaTypeImageLayerNonDistrib: true,
	}
)

// ValidateImageManifest validates an OCI image manifest
func (g *Generator) ValidateImageManifest(manifest *ImageManifest) error {
	if manifest == nil {
		return &ManifestError{
			Type:      ErrorTypeValidation,
			Operation: "validate_manifest",
			Message:   "manifest cannot be nil",
		}
	}
	
	// Validate schema version
	if manifest.SchemaVersion != OCISchemaVersion {
		return &ManifestError{
			Type:      ErrorTypeValidation,
			Operation: "validate_schema_version",
			Message:   fmt.Sprintf("invalid schema version: expected %d, got %d", OCISchemaVersion, manifest.SchemaVersion),
		}
	}
	
	// Validate media type
	if !validManifestMediaTypes[manifest.MediaType] {
		return &ManifestError{
			Type:      ErrorTypeValidation,
			Operation: "validate_media_type",
			Message:   fmt.Sprintf("invalid manifest media type: %s", manifest.MediaType),
		}
	}
	
	// Validate config descriptor
	if err := g.validateDescriptor(&manifest.Config, "config"); err != nil {
		return err
	}
	
	// Validate config media type
	if !validConfigMediaTypes[manifest.Config.MediaType] {
		return &ManifestError{
			Type:      ErrorTypeValidation,
			Operation: "validate_config_media_type",
			Message:   fmt.Sprintf("invalid config media type: %s", manifest.Config.MediaType),
		}
	}
	
	// Validate layers
	if len(manifest.Layers) == 0 {
		return &ManifestError{
			Type:      ErrorTypeValidation,
			Operation: "validate_layers",
			Message:   "manifest must have at least one layer",
		}
	}
	
	for i, layer := range manifest.Layers {
		if err := g.validateDescriptor(&layer, fmt.Sprintf("layer[%d]", i)); err != nil {
			return err
		}
		
		// Validate layer media type
		if !validLayerMediaTypes[layer.MediaType] {
			return &ManifestError{
				Type:      ErrorTypeValidation,
				Operation: "validate_layer_media_type",
				Message:   fmt.Sprintf("invalid layer media type at index %d: %s", i, layer.MediaType),
			}
		}
	}
	
	// Validate annotations if present
	if err := g.validateAnnotations(manifest.Annotations, "manifest"); err != nil {
		return err
	}
	
	return nil
}

// ValidateManifestList validates an OCI manifest list
func (g *Generator) ValidateManifestList(manifestList *ManifestList) error {
	if manifestList == nil {
		return &ManifestError{
			Type:      ErrorTypeValidation,
			Operation: "validate_manifest_list",
			Message:   "manifest list cannot be nil",
		}
	}
	
	// Validate schema version
	if manifestList.SchemaVersion != OCISchemaVersion {
		return &ManifestError{
			Type:      ErrorTypeValidation,
			Operation: "validate_schema_version",
			Message:   fmt.Sprintf("invalid schema version: expected %d, got %d", OCISchemaVersion, manifestList.SchemaVersion),
		}
	}
	
	// Validate media type
	if !validIndexMediaTypes[manifestList.MediaType] {
		return &ManifestError{
			Type:      ErrorTypeValidation,
			Operation: "validate_media_type",
			Message:   fmt.Sprintf("invalid manifest list media type: %s", manifestList.MediaType),
		}
	}
	
	// Validate manifests
	if len(manifestList.Manifests) == 0 {
		return &ManifestError{
			Type:      ErrorTypeValidation,
			Operation: "validate_manifests",
			Message:   "manifest list must have at least one manifest",
		}
	}
	
	platformsSeen := make(map[string]bool)
	
	for i, platformManifest := range manifestList.Manifests {
		if err := g.validatePlatformManifest(&platformManifest); err != nil {
			return &ManifestError{
				Type:      ErrorTypeValidation,
				Operation: "validate_platform_manifest",
				Message:   fmt.Sprintf("invalid platform manifest at index %d: %v", i, err),
				Cause:     err,
			}
		}
		
		// Check for duplicate platforms
		platformKey := platformManifest.Platform.String()
		if platformsSeen[platformKey] {
			return &ManifestError{
				Type:      ErrorTypeValidation,
				Operation: "validate_platform_uniqueness",
				Message:   fmt.Sprintf("duplicate platform in manifest list: %s", platformKey),
			}
		}
		platformsSeen[platformKey] = true
	}
	
	// Validate annotations if present
	if err := g.validateAnnotations(manifestList.Annotations, "manifest_list"); err != nil {
		return err
	}
	
	return nil
}

// ValidateImageConfig validates an OCI image configuration
func (g *Generator) ValidateImageConfig(config *ImageConfig) error {
	if config == nil {
		return &ManifestError{
			Type:      ErrorTypeValidation,
			Operation: "validate_config",
			Message:   "config cannot be nil",
		}
	}
	
	// Validate required fields
	if config.Architecture == "" {
		return &ManifestError{
			Type:      ErrorTypeValidation,
			Operation: "validate_architecture",
			Message:   "architecture cannot be empty",
		}
	}
	
	if config.OS == "" {
		return &ManifestError{
			Type:      ErrorTypeValidation,
			Operation: "validate_os",
			Message:   "OS cannot be empty",
		}
	}
	
	// Validate created timestamp format
	if config.Created != "" {
		if _, err := time.Parse(time.RFC3339Nano, config.Created); err != nil {
			return &ManifestError{
				Type:      ErrorTypeValidation,
				Operation: "validate_created_timestamp",
				Message:   fmt.Sprintf("invalid created timestamp format: %s", config.Created),
				Cause:     err,
			}
		}
	}
	
	// Validate rootfs
	if err := g.validateRootFS(&config.RootFS); err != nil {
		return err
	}
	
	// Validate container config
	if err := g.validateContainerConfig(&config.Config); err != nil {
		return err
	}
	
	// Validate history entries
	for i, entry := range config.History {
		if err := g.validateHistoryEntry(&entry, i); err != nil {
			return err
		}
	}
	
	return nil
}

// validateDescriptor validates an OCI descriptor
func (g *Generator) validateDescriptor(desc *Descriptor, context string) error {
	if desc == nil {
		return &ManifestError{
			Type:      ErrorTypeValidation,
			Operation: "validate_descriptor",
			Message:   fmt.Sprintf("%s descriptor cannot be nil", context),
		}
	}
	
	// Validate media type
	if desc.MediaType == "" {
		return &ManifestError{
			Type:      ErrorTypeValidation,
			Operation: "validate_media_type",
			Message:   fmt.Sprintf("%s media type cannot be empty", context),
		}
	}
	
	// Validate size
	if desc.Size < 0 {
		return &ManifestError{
			Type:      ErrorTypeValidation,
			Operation: "validate_size",
			Message:   fmt.Sprintf("%s size cannot be negative: %d", context, desc.Size),
		}
	}
	
	// Validate digest
	if err := g.validateDigest(desc.Digest, context); err != nil {
		return err
	}
	
	// Validate annotations if present
	if err := g.validateAnnotations(desc.Annotations, context); err != nil {
		return err
	}
	
	return nil
}

// validatePlatformManifest validates a platform manifest
func (g *Generator) validatePlatformManifest(platformManifest *PlatformManifest) error {
	if platformManifest == nil {
		return &ManifestError{
			Type:      ErrorTypeValidation,
			Operation: "validate_platform_manifest",
			Message:   "platform manifest cannot be nil",
		}
	}
	
	// Validate media type
	if !validManifestMediaTypes[platformManifest.MediaType] {
		return &ManifestError{
			Type:      ErrorTypeValidation,
			Operation: "validate_media_type",
			Message:   fmt.Sprintf("invalid platform manifest media type: %s", platformManifest.MediaType),
		}
	}
	
	// Validate size
	if platformManifest.Size <= 0 {
		return &ManifestError{
			Type:      ErrorTypeValidation,
			Operation: "validate_size",
			Message:   fmt.Sprintf("platform manifest size must be positive: %d", platformManifest.Size),
		}
	}
	
	// Validate digest
	if err := g.validateDigest(platformManifest.Digest, "platform_manifest"); err != nil {
		return err
	}
	
	// Validate platform
	if err := g.validatePlatform(&platformManifest.Platform); err != nil {
		return err
	}
	
	// Validate annotations if present
	if err := g.validateAnnotations(platformManifest.Annotations, "platform_manifest"); err != nil {
		return err
	}
	
	return nil
}

// validatePlatform validates a platform specification
func (g *Generator) validatePlatform(platform *Platform) error {
	if platform == nil {
		return &ManifestError{
			Type:      ErrorTypeValidation,
			Operation: "validate_platform",
			Message:   "platform cannot be nil",
		}
	}
	
	// Validate required fields
	if platform.Architecture == "" {
		return &ManifestError{
			Type:      ErrorTypeValidation,
			Operation: "validate_architecture",
			Message:   "platform architecture cannot be empty",
		}
	}
	
	if platform.OS == "" {
		return &ManifestError{
			Type:      ErrorTypeValidation,
			Operation: "validate_os",
			Message:   "platform OS cannot be empty",
		}
	}
	
	// Validate architecture values
	validArchitectures := map[string]bool{
		"386":     true,
		"amd64":   true,
		"arm":     true,
		"arm64":   true,
		"ppc64le": true,
		"s390x":   true,
		"mips64le": true,
		"riscv64": true,
	}
	
	if !validArchitectures[platform.Architecture] {
		return &ManifestError{
			Type:      ErrorTypeValidation,
			Operation: "validate_architecture",
			Message:   fmt.Sprintf("unsupported architecture: %s", platform.Architecture),
		}
	}
	
	// Validate OS values
	validOSes := map[string]bool{
		"linux":   true,
		"windows": true,
		"darwin":  true,
		"freebsd": true,
		"netbsd":  true,
		"openbsd": true,
		"solaris": true,
	}
	
	if !validOSes[platform.OS] {
		return &ManifestError{
			Type:      ErrorTypeValidation,
			Operation: "validate_os",
			Message:   fmt.Sprintf("unsupported OS: %s", platform.OS),
		}
	}
	
	// Validate ARM variants if present
	if platform.Architecture == "arm" && platform.Variant != "" {
		validARMVariants := map[string]bool{
			"v6": true,
			"v7": true,
			"v8": true,
		}
		
		if !validARMVariants[platform.Variant] {
			return &ManifestError{
				Type:      ErrorTypeValidation,
				Operation: "validate_variant",
				Message:   fmt.Sprintf("unsupported ARM variant: %s", platform.Variant),
			}
		}
	}
	
	return nil
}

// validateDigest validates a digest format
func (g *Generator) validateDigest(digest, context string) error {
	if digest == "" {
		return &ManifestError{
			Type:      ErrorTypeValidation,
			Operation: "validate_digest",
			Message:   fmt.Sprintf("%s digest cannot be empty", context),
		}
	}
	
	if !digestRegex.MatchString(digest) {
		return &ManifestError{
			Type:      ErrorTypeValidation,
			Operation: "validate_digest_format",
			Message:   fmt.Sprintf("%s digest has invalid format: %s", context, digest),
		}
	}
	
	return nil
}

// validateAnnotations validates annotation key-value pairs
func (g *Generator) validateAnnotations(annotations map[string]string, context string) error {
	if annotations == nil {
		return nil
	}
	
	for key, value := range annotations {
		// Validate key format (reverse domain notation recommended)
		if key == "" {
			return &ManifestError{
				Type:      ErrorTypeValidation,
				Operation: "validate_annotation_key",
				Message:   fmt.Sprintf("%s annotation key cannot be empty", context),
			}
		}
		
		// Check for reserved annotation keys
		if strings.HasPrefix(key, "org.opencontainers.") {
			return &ManifestError{
				Type:      ErrorTypeValidation,
				Operation: "validate_annotation_key",
				Message:   fmt.Sprintf("%s annotation key uses reserved prefix: %s", context, key),
			}
		}
		
		// Validate value (can be empty, but not nil in this context)
		_ = value // Value validation is minimal for annotations
	}
	
	return nil
}

// validateRootFS validates the root filesystem configuration
func (g *Generator) validateRootFS(rootfs *RootFS) error {
	if rootfs == nil {
		return &ManifestError{
			Type:      ErrorTypeValidation,
			Operation: "validate_rootfs",
			Message:   "rootfs cannot be nil",
		}
	}
	
	// Validate type
	if rootfs.Type != "layers" {
		return &ManifestError{
			Type:      ErrorTypeValidation,
			Operation: "validate_rootfs_type",
			Message:   fmt.Sprintf("invalid rootfs type: expected 'layers', got '%s'", rootfs.Type),
		}
	}
	
	// Validate diff IDs
	for i, diffID := range rootfs.DiffIDs {
		if err := g.validateDigest(diffID, fmt.Sprintf("rootfs.diff_ids[%d]", i)); err != nil {
			return err
		}
	}
	
	return nil
}

// validateContainerConfig validates the container configuration
func (g *Generator) validateContainerConfig(config *ContainerConfig) error {
	if config == nil {
		return &ManifestError{
			Type:      ErrorTypeValidation,
			Operation: "validate_container_config",
			Message:   "container config cannot be nil",
		}
	}
	
	// Validate exposed ports format
	for port := range config.ExposedPorts {
		if !isValidPortSpec(port) {
			return &ManifestError{
				Type:      ErrorTypeValidation,
				Operation: "validate_exposed_port",
				Message:   fmt.Sprintf("invalid exposed port format: %s", port),
			}
		}
	}
	
	// Validate environment variables format
	for i, env := range config.Env {
		if !strings.Contains(env, "=") {
			return &ManifestError{
				Type:      ErrorTypeValidation,
				Operation: "validate_env_var",
				Message:   fmt.Sprintf("invalid environment variable format at index %d: %s", i, env),
			}
		}
	}
	
	// Validate working directory
	if config.WorkingDir != "" && !strings.HasPrefix(config.WorkingDir, "/") {
		return &ManifestError{
			Type:      ErrorTypeValidation,
			Operation: "validate_working_dir",
			Message:   fmt.Sprintf("working directory must be absolute path: %s", config.WorkingDir),
		}
	}
	
	return nil
}

// validateHistoryEntry validates a history entry
func (g *Generator) validateHistoryEntry(entry *HistoryEntry, index int) error {
	if entry == nil {
		return &ManifestError{
			Type:      ErrorTypeValidation,
			Operation: "validate_history_entry",
			Message:   fmt.Sprintf("history entry at index %d cannot be nil", index),
		}
	}
	
	// Validate created timestamp format if present
	if entry.Created != "" {
		if _, err := time.Parse(time.RFC3339Nano, entry.Created); err != nil {
			return &ManifestError{
				Type:      ErrorTypeValidation,
				Operation: "validate_history_created",
				Message:   fmt.Sprintf("invalid created timestamp in history entry %d: %s", index, entry.Created),
				Cause:     err,
			}
		}
	}
	
	return nil
}

// isValidPortSpec validates a port specification (e.g., "80/tcp", "443/udp")
func isValidPortSpec(portSpec string) bool {
	parts := strings.Split(portSpec, "/")
	if len(parts) != 2 {
		return false
	}
	
	port, protocol := parts[0], parts[1]
	
	// Validate port number
	if port == "" {
		return false
	}
	
	// Validate protocol
	validProtocols := map[string]bool{
		"tcp":  true,
		"udp":  true,
		"sctp": true,
	}
	
	return validProtocols[protocol]
}