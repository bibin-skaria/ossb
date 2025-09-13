// Package manifest provides OCI-compliant image manifest and configuration generation.
//
// This package implements the OCI Image Specification v1.0+ for generating:
// - Image configurations from Dockerfile instructions
// - Image manifests with layer references and platform information
// - Multi-architecture manifest lists
// - Proper digest calculation and validation
//
// The main entry point is the Generator type, which provides all manifest
// generation operations with full OCI compliance.
//
// Example usage:
//
//	generator := manifest.NewGenerator()
//	
//	// Generate image config from Dockerfile instructions
//	config, err := generator.GenerateImageConfig(instructions, platform)
//	if err != nil {
//		return err
//	}
//	
//	// Generate image manifest
//	manifest, err := generator.GenerateImageManifest(config, layers)
//	if err != nil {
//		return err
//	}
//
// The package ensures full OCI Image Specification compliance and provides
// comprehensive validation for all generated artifacts.
package manifest

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/bibin-skaria/ossb/internal/types"
	"github.com/bibin-skaria/ossb/layers"
)

// Generator provides OCI manifest and configuration generation
type Generator struct {
	// Options for manifest generation
	options *GeneratorOptions
}

// GeneratorOptions configures the manifest generator
type GeneratorOptions struct {
	// DefaultUser sets the default user for containers
	DefaultUser string
	// DefaultWorkingDir sets the default working directory
	DefaultWorkingDir string
	// DefaultShell sets the default shell for RUN instructions
	DefaultShell []string
	// IncludeHistory controls whether to include history in image config
	IncludeHistory bool
	// Timestamp for reproducible builds (if nil, uses current time)
	Timestamp *time.Time
}

// DefaultGeneratorOptions returns sensible defaults for manifest generation
func DefaultGeneratorOptions() *GeneratorOptions {
	return &GeneratorOptions{
		DefaultUser:       "",
		DefaultWorkingDir: "/",
		DefaultShell:      []string{"/bin/sh", "-c"},
		IncludeHistory:    true,
		Timestamp:         nil,
	}
}

// NewGenerator creates a new manifest generator with the given options
func NewGenerator(options *GeneratorOptions) *Generator {
	if options == nil {
		options = DefaultGeneratorOptions()
	}

	return &Generator{
		options: options,
	}
}

// GenerateImageConfig generates an OCI image configuration from Dockerfile instructions
func (g *Generator) GenerateImageConfig(instructions []types.DockerfileInstruction, platform types.Platform) (*ImageConfig, error) {
	if len(instructions) == 0 {
		return nil, &ManifestError{
			Type:      ErrorTypeValidation,
			Operation: "generate_config",
			Message:   "no Dockerfile instructions provided",
		}
	}

	// Initialize config with defaults
	config := &ImageConfig{
		Architecture: platform.Architecture,
		OS:           platform.OS,
		Config: ContainerConfig{
			User:       g.options.DefaultUser,
			WorkingDir: g.options.DefaultWorkingDir,
			Env:        []string{},
			Cmd:        []string{},
			Entrypoint: []string{},
			ExposedPorts: make(map[string]struct{}),
			Volumes:    make(map[string]struct{}),
			Labels:     make(map[string]string),
		},
		RootFS: RootFS{
			Type:    "layers",
			DiffIDs: []string{},
		},
		History: []HistoryEntry{},
	}

	// Set platform-specific defaults
	if platform.Variant != "" {
		config.Variant = platform.Variant
	}

	// Set creation timestamp
	timestamp := time.Now().UTC()
	if g.options.Timestamp != nil {
		timestamp = *g.options.Timestamp
	}
	config.Created = timestamp.Format(time.RFC3339Nano)

	// Process Dockerfile instructions
	for _, instruction := range instructions {
		if err := g.processInstruction(config, instruction); err != nil {
			return nil, &ManifestError{
				Type:      ErrorTypeValidation,
				Operation: "process_instruction",
				Message:   fmt.Sprintf("failed to process %s instruction: %v", instruction.Command, err),
				Cause:     err,
			}
		}
	}

	// Validate the generated config
	if err := g.ValidateImageConfig(config); err != nil {
		return nil, &ManifestError{
			Type:      ErrorTypeValidation,
			Operation: "validate_config",
			Message:   fmt.Sprintf("generated config is invalid: %v", err),
			Cause:     err,
		}
	}

	return config, nil
}

// GenerateImageManifest generates an OCI image manifest from config and layers
func (g *Generator) GenerateImageManifest(config *ImageConfig, layers []*layers.Layer) (*ImageManifest, error) {
	if config == nil {
		return nil, &ManifestError{
			Type:      ErrorTypeValidation,
			Operation: "generate_manifest",
			Message:   "image config cannot be nil",
		}
	}

	// Serialize config to calculate digest and size
	configBytes, err := json.Marshal(config)
	if err != nil {
		return nil, &ManifestError{
			Type:      ErrorTypeValidation,
			Operation: "serialize_config",
			Message:   fmt.Sprintf("failed to serialize config: %v", err),
			Cause:     err,
		}
	}

	configDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(configBytes))

	// Create manifest
	manifest := &ImageManifest{
		SchemaVersion: 2,
		MediaType:     MediaTypeOCIManifest,
		Config: Descriptor{
			MediaType: MediaTypeOCIConfig,
			Size:      int64(len(configBytes)),
			Digest:    configDigest,
		},
		Layers: make([]Descriptor, len(layers)),
	}

	// Add layer descriptors
	for i, layer := range layers {
		if layer == nil {
			return nil, &ManifestError{
				Type:      ErrorTypeValidation,
				Operation: "process_layer",
				Message:   fmt.Sprintf("layer %d is nil", i),
			}
		}

		manifest.Layers[i] = Descriptor{
			MediaType: layer.MediaType,
			Size:      layer.Size,
			Digest:    layer.Digest,
		}

		// Add annotations if present
		if len(layer.Annotations) > 0 {
			manifest.Layers[i].Annotations = make(map[string]string)
			for k, v := range layer.Annotations {
				manifest.Layers[i].Annotations[k] = v
			}
		}
	}

	// Validate the generated manifest
	if err := g.ValidateImageManifest(manifest); err != nil {
		return nil, &ManifestError{
			Type:      ErrorTypeValidation,
			Operation: "validate_manifest",
			Message:   fmt.Sprintf("generated manifest is invalid: %v", err),
			Cause:     err,
		}
	}

	return manifest, nil
}

// GenerateManifestList generates a multi-architecture manifest list
func (g *Generator) GenerateManifestList(manifests []PlatformManifest) (*ManifestList, error) {
	if len(manifests) == 0 {
		return nil, &ManifestError{
			Type:      ErrorTypeValidation,
			Operation: "generate_manifest_list",
			Message:   "no platform manifests provided",
		}
	}

	manifestList := &ManifestList{
		SchemaVersion: 2,
		MediaType:     MediaTypeOCIIndex,
		Manifests:     make([]PlatformManifest, len(manifests)),
	}

	// Copy and validate platform manifests
	for i, platformManifest := range manifests {
		// Validate platform manifest
		if err := g.validatePlatformManifest(&platformManifest); err != nil {
			return nil, &ManifestError{
				Type:      ErrorTypeValidation,
				Operation: "validate_platform_manifest",
				Message:   fmt.Sprintf("platform manifest %d is invalid: %v", i, err),
				Cause:     err,
			}
		}

		manifestList.Manifests[i] = platformManifest
	}

	// Sort manifests by platform for consistent ordering
	sort.Slice(manifestList.Manifests, func(i, j int) bool {
		a, b := manifestList.Manifests[i], manifestList.Manifests[j]
		if a.Platform.OS != b.Platform.OS {
			return a.Platform.OS < b.Platform.OS
		}
		if a.Platform.Architecture != b.Platform.Architecture {
			return a.Platform.Architecture < b.Platform.Architecture
		}
		return a.Platform.Variant < b.Platform.Variant
	})

	// Validate the generated manifest list
	if err := g.ValidateManifestList(manifestList); err != nil {
		return nil, &ManifestError{
			Type:      ErrorTypeValidation,
			Operation: "validate_manifest_list",
			Message:   fmt.Sprintf("generated manifest list is invalid: %v", err),
			Cause:     err,
		}
	}

	return manifestList, nil
}

// processInstruction processes a single Dockerfile instruction and updates the config
func (g *Generator) processInstruction(config *ImageConfig, instruction types.DockerfileInstruction) error {
	switch strings.ToUpper(instruction.Command) {
	case "FROM":
		// FROM instructions don't directly affect the final config
		// but we record them in history
		if g.options.IncludeHistory {
			config.History = append(config.History, HistoryEntry{
				Created:   config.Created,
				CreatedBy: fmt.Sprintf("%s %s", instruction.Command, instruction.Value),
			})
		}

	case "RUN":
		// RUN instructions create layers and are recorded in history
		if g.options.IncludeHistory {
			config.History = append(config.History, HistoryEntry{
				Created:   config.Created,
				CreatedBy: fmt.Sprintf("%s %s", instruction.Command, instruction.Value),
			})
		}

	case "CMD":
		// CMD sets the default command
		config.Config.Cmd = parseShellForm(instruction.Value)

	case "ENTRYPOINT":
		// ENTRYPOINT sets the entrypoint
		config.Config.Entrypoint = parseShellForm(instruction.Value)

	case "ENV":
		// ENV sets environment variables
		if err := g.processEnvInstruction(config, instruction.Value); err != nil {
			return err
		}

	case "EXPOSE":
		// EXPOSE declares ports
		if err := g.processExposeInstruction(config, instruction.Value); err != nil {
			return err
		}

	case "VOLUME":
		// VOLUME declares volumes
		if err := g.processVolumeInstruction(config, instruction.Value); err != nil {
			return err
		}

	case "WORKDIR":
		// WORKDIR sets working directory
		config.Config.WorkingDir = instruction.Value

	case "USER":
		// USER sets the user
		config.Config.User = instruction.Value

	case "LABEL":
		// LABEL sets metadata labels
		if err := g.processLabelInstruction(config, instruction.Value); err != nil {
			return err
		}

	case "COPY", "ADD":
		// COPY/ADD instructions create layers and are recorded in history
		if g.options.IncludeHistory {
			config.History = append(config.History, HistoryEntry{
				Created:   config.Created,
				CreatedBy: fmt.Sprintf("%s %s", instruction.Command, instruction.Value),
			})
		}

	case "SHELL":
		// SHELL sets the default shell
		config.Config.Shell = parseShellForm(instruction.Value)

	case "STOPSIGNAL":
		// STOPSIGNAL sets the stop signal
		config.Config.StopSignal = instruction.Value

	case "HEALTHCHECK":
		// HEALTHCHECK sets health check configuration
		if err := g.processHealthcheckInstruction(config, instruction.Value); err != nil {
			return err
		}

	case "ONBUILD":
		// ONBUILD instructions are stored for child images
		config.Config.OnBuild = append(config.Config.OnBuild, instruction.Value)

	default:
		// Unknown instruction - record in history if enabled
		if g.options.IncludeHistory {
			config.History = append(config.History, HistoryEntry{
				Created:   config.Created,
				CreatedBy: fmt.Sprintf("%s %s", instruction.Command, instruction.Value),
				Comment:   "unknown instruction",
			})
		}
	}

	return nil
}

// processEnvInstruction processes ENV instructions
func (g *Generator) processEnvInstruction(config *ImageConfig, value string) error {
	// Parse ENV instruction - can be "KEY=value" or "KEY value"
	parts := strings.SplitN(value, "=", 2)
	if len(parts) == 2 {
		// KEY=value format
		config.Config.Env = append(config.Config.Env, value)
	} else {
		// KEY value format - split on first space
		parts = strings.SplitN(value, " ", 2)
		if len(parts) == 2 {
			config.Config.Env = append(config.Config.Env, fmt.Sprintf("%s=%s", parts[0], parts[1]))
		} else {
			return fmt.Errorf("invalid ENV instruction format: %s", value)
		}
	}
	return nil
}

// processExposeInstruction processes EXPOSE instructions
func (g *Generator) processExposeInstruction(config *ImageConfig, value string) error {
	// Parse exposed ports - can be "80" or "80/tcp" or "80/udp"
	ports := strings.Fields(value)
	for _, port := range ports {
		// Normalize port format
		if !strings.Contains(port, "/") {
			port = port + "/tcp"
		}
		config.Config.ExposedPorts[port] = struct{}{}
	}
	return nil
}

// processVolumeInstruction processes VOLUME instructions
func (g *Generator) processVolumeInstruction(config *ImageConfig, value string) error {
	// Parse volumes - can be JSON array or space-separated
	if strings.HasPrefix(value, "[") {
		// JSON array format
		var volumes []string
		if err := json.Unmarshal([]byte(value), &volumes); err != nil {
			return fmt.Errorf("invalid VOLUME JSON format: %v", err)
		}
		for _, volume := range volumes {
			config.Config.Volumes[volume] = struct{}{}
		}
	} else {
		// Space-separated format
		volumes := strings.Fields(value)
		for _, volume := range volumes {
			config.Config.Volumes[volume] = struct{}{}
		}
	}
	return nil
}

// processLabelInstruction processes LABEL instructions
func (g *Generator) processLabelInstruction(config *ImageConfig, value string) error {
	// Parse labels - can be "key=value" or "key value" or multiple pairs
	pairs := parseKeyValuePairs(value)
	for key, val := range pairs {
		config.Config.Labels[key] = val
	}
	return nil
}

// processHealthcheckInstruction processes HEALTHCHECK instructions
func (g *Generator) processHealthcheckInstruction(config *ImageConfig, value string) error {
	// Parse HEALTHCHECK instruction
	if strings.HasPrefix(value, "NONE") {
		// Disable healthcheck
		config.Config.Healthcheck = &HealthConfig{
			Test: []string{"NONE"},
		}
	} else {
		// Parse healthcheck options and command
		healthcheck := &HealthConfig{
			Test: []string{},
		}
		
		// This is a simplified parser - in a real implementation,
		// you'd want more sophisticated parsing of healthcheck options
		if strings.Contains(value, "CMD") {
			cmdIndex := strings.Index(value, "CMD")
			cmd := strings.TrimSpace(value[cmdIndex+3:])
			healthcheck.Test = append(healthcheck.Test, "CMD-SHELL", cmd)
		}
		
		config.Config.Healthcheck = healthcheck
	}
	return nil
}

// parseShellForm parses shell form commands (JSON array or shell string)
func parseShellForm(value string) []string {
	value = strings.TrimSpace(value)
	
	// Check if it's JSON array format
	if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
		var result []string
		if err := json.Unmarshal([]byte(value), &result); err == nil {
			return result
		}
	}
	
	// Shell form - return as single string to be processed by shell
	return []string{"/bin/sh", "-c", value}
}

// parseKeyValuePairs parses key=value pairs from a string
func parseKeyValuePairs(value string) map[string]string {
	result := make(map[string]string)
	
	// Simple parser for key=value pairs
	// In a real implementation, you'd want more sophisticated parsing
	// to handle quoted values, escaped characters, etc.
	pairs := strings.Fields(value)
	for _, pair := range pairs {
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) == 2 {
			key := strings.Trim(parts[0], `"'`)
			val := strings.Trim(parts[1], `"'`)
			result[key] = val
		}
	}
	
	return result
}

// AddLayerToConfig adds a layer to the image configuration
func (g *Generator) AddLayerToConfig(config *ImageConfig, layer *layers.Layer) error {
	if config == nil {
		return &ManifestError{
			Type:      ErrorTypeValidation,
			Operation: "add_layer",
			Message:   "config cannot be nil",
		}
	}
	
	if layer == nil {
		return &ManifestError{
			Type:      ErrorTypeValidation,
			Operation: "add_layer",
			Message:   "layer cannot be nil",
		}
	}
	
	// Add layer diff ID to rootfs
	// For OCI, diff ID is the digest of the uncompressed layer
	diffID := layer.Digest
	if !strings.HasPrefix(diffID, "sha256:") {
		return &ManifestError{
			Type:      ErrorTypeValidation,
			Operation: "add_layer",
			Message:   fmt.Sprintf("invalid layer digest format: %s", diffID),
		}
	}
	
	config.RootFS.DiffIDs = append(config.RootFS.DiffIDs, diffID)
	
	// Add history entry if enabled
	if g.options.IncludeHistory {
		historyEntry := HistoryEntry{
			Created: config.Created,
		}
		
		if layer.CreatedBy != "" {
			historyEntry.CreatedBy = layer.CreatedBy
		}
		
		if layer.Comment != "" {
			historyEntry.Comment = layer.Comment
		}
		
		if layer.EmptyLayer {
			historyEntry.EmptyLayer = true
		}
		
		config.History = append(config.History, historyEntry)
	}
	
	return nil
}

// CalculateManifestDigest calculates the digest of a manifest
func (g *Generator) CalculateManifestDigest(manifest *ImageManifest) (string, error) {
	if manifest == nil {
		return "", &ManifestError{
			Type:      ErrorTypeValidation,
			Operation: "calculate_digest",
			Message:   "manifest cannot be nil",
		}
	}
	
	// Serialize manifest to canonical JSON
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		return "", &ManifestError{
			Type:      ErrorTypeValidation,
			Operation: "serialize_manifest",
			Message:   fmt.Sprintf("failed to serialize manifest: %v", err),
			Cause:     err,
		}
	}
	
	// Calculate SHA256 digest
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256(manifestBytes))
	return digest, nil
}

// CalculateManifestListDigest calculates the digest of a manifest list
func (g *Generator) CalculateManifestListDigest(manifestList *ManifestList) (string, error) {
	if manifestList == nil {
		return "", &ManifestError{
			Type:      ErrorTypeValidation,
			Operation: "calculate_digest",
			Message:   "manifest list cannot be nil",
		}
	}
	
	// Serialize manifest list to canonical JSON
	manifestListBytes, err := json.Marshal(manifestList)
	if err != nil {
		return "", &ManifestError{
			Type:      ErrorTypeValidation,
			Operation: "serialize_manifest_list",
			Message:   fmt.Sprintf("failed to serialize manifest list: %v", err),
			Cause:     err,
		}
	}
	
	// Calculate SHA256 digest
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256(manifestListBytes))
	return digest, nil
}

// CalculateConfigDigest calculates the digest of an image config
func (g *Generator) CalculateConfigDigest(config *ImageConfig) (string, error) {
	if config == nil {
		return "", &ManifestError{
			Type:      ErrorTypeValidation,
			Operation: "calculate_digest",
			Message:   "config cannot be nil",
		}
	}
	
	// Serialize config to canonical JSON
	configBytes, err := json.Marshal(config)
	if err != nil {
		return "", &ManifestError{
			Type:      ErrorTypeValidation,
			Operation: "serialize_config",
			Message:   fmt.Sprintf("failed to serialize config: %v", err),
			Cause:     err,
		}
	}
	
	// Calculate SHA256 digest
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256(configBytes))
	return digest, nil
}

// SerializeManifest serializes a manifest to JSON bytes
func (g *Generator) SerializeManifest(manifest *ImageManifest) ([]byte, error) {
	if manifest == nil {
		return nil, &ManifestError{
			Type:      ErrorTypeValidation,
			Operation: "serialize_manifest",
			Message:   "manifest cannot be nil",
		}
	}
	
	return json.Marshal(manifest)
}

// SerializeManifestList serializes a manifest list to JSON bytes
func (g *Generator) SerializeManifestList(manifestList *ManifestList) ([]byte, error) {
	if manifestList == nil {
		return nil, &ManifestError{
			Type:      ErrorTypeValidation,
			Operation: "serialize_manifest_list",
			Message:   "manifest list cannot be nil",
		}
	}
	
	return json.Marshal(manifestList)
}

// SerializeConfig serializes an image config to JSON bytes
func (g *Generator) SerializeConfig(config *ImageConfig) ([]byte, error) {
	if config == nil {
		return nil, &ManifestError{
			Type:      ErrorTypeValidation,
			Operation: "serialize_config",
			Message:   "config cannot be nil",
		}
	}
	
	return json.Marshal(config)
}

// CreatePlatformManifest creates a platform manifest from an image manifest
func (g *Generator) CreatePlatformManifest(manifest *ImageManifest, platform types.Platform) (*PlatformManifest, error) {
	if manifest == nil {
		return nil, &ManifestError{
			Type:      ErrorTypeValidation,
			Operation: "create_platform_manifest",
			Message:   "manifest cannot be nil",
		}
	}
	
	// Calculate manifest digest and size
	manifestBytes, err := g.SerializeManifest(manifest)
	if err != nil {
		return nil, err
	}
	
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256(manifestBytes))
	
	platformManifest := &PlatformManifest{
		MediaType: manifest.MediaType,
		Size:      int64(len(manifestBytes)),
		Digest:    digest,
		Platform: Platform{
			Architecture: platform.Architecture,
			OS:           platform.OS,
			Variant:      platform.Variant,
		},
	}
	
	return platformManifest, nil
}