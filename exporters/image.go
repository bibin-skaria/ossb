package exporters

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/bibin-skaria/ossb/internal/types"
)

type ImageExporter struct{}

func init() {
	RegisterExporter("image", &ImageExporter{})
}

type OCIManifest struct {
	SchemaVersion int                    `json:"schemaVersion"`
	MediaType     string                 `json:"mediaType"`
	Config        OCIDescriptor          `json:"config"`
	Layers        []OCIDescriptor        `json:"layers"`
	Annotations   map[string]string      `json:"annotations,omitempty"`
}

type OCIDescriptor struct {
	MediaType string `json:"mediaType"`
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
}

type OCIImageConfig struct {
	Created      time.Time         `json:"created"`
	Architecture string            `json:"architecture"`
	OS           string            `json:"os"`
	Config       OCIContainerConfig `json:"config"`
	RootFS       OCIRootFS         `json:"rootfs"`
	History      []OCIHistory      `json:"history"`
}

type OCIContainerConfig struct {
	User         string            `json:"User,omitempty"`
	ExposedPorts map[string]struct{} `json:"ExposedPorts,omitempty"`
	Env          []string          `json:"Env,omitempty"`
	Entrypoint   []string          `json:"Entrypoint,omitempty"`
	Cmd          []string          `json:"Cmd,omitempty"`
	Volumes      map[string]struct{} `json:"Volumes,omitempty"`
	WorkingDir   string            `json:"WorkingDir,omitempty"`
	Labels       map[string]string `json:"Labels,omitempty"`
}

type OCIRootFS struct {
	Type    string   `json:"type"`
	DiffIDs []string `json:"diff_ids"`
}

type OCIHistory struct {
	Created    time.Time `json:"created"`
	CreatedBy  string    `json:"created_by,omitempty"`
	Comment    string    `json:"comment,omitempty"`
	EmptyLayer bool      `json:"empty_layer,omitempty"`
}

func (e *ImageExporter) Export(result *types.BuildResult, config *types.BuildConfig, workDir string) error {
	imageDir := filepath.Join(workDir, "image")
	if err := os.MkdirAll(imageDir, 0755); err != nil {
		return fmt.Errorf("failed to create image directory: %v", err)
	}

	layersDir := filepath.Join(workDir, "layers")
	
	layers, err := e.collectLayers(layersDir)
	if err != nil {
		return fmt.Errorf("failed to collect layers: %v", err)
	}

	imageConfig := &OCIImageConfig{
		Created:      time.Now(),
		Architecture: "amd64",
		OS:           "linux",
		Config:       e.buildContainerConfig(result.Metadata),
		RootFS: OCIRootFS{
			Type:    "layers",
			DiffIDs: layers,
		},
		History: e.buildHistory(result),
	}

	configData, err := json.Marshal(imageConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal image config: %v", err)
	}

	configDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(configData))
	configPath := filepath.Join(imageDir, configDigest[7:]+".json")
	if err := os.WriteFile(configPath, configData, 0644); err != nil {
		return fmt.Errorf("failed to write config: %v", err)
	}

	layerDescriptors := make([]OCIDescriptor, len(layers))
	for i, layer := range layers {
		layerDescriptors[i] = OCIDescriptor{
			MediaType: "application/vnd.oci.image.layer.v1.tar",
			Digest:    layer,
			Size:      0, 
		}
	}

	manifest := &OCIManifest{
		SchemaVersion: 2,
		MediaType:     "application/vnd.oci.image.manifest.v1+json",
		Config: OCIDescriptor{
			MediaType: "application/vnd.oci.image.config.v1+json",
			Digest:    configDigest,
			Size:      int64(len(configData)),
		},
		Layers: layerDescriptors,
		Annotations: map[string]string{
			"org.opencontainers.image.created": time.Now().Format(time.RFC3339),
		},
	}

	if len(config.Tags) > 0 {
		manifest.Annotations["org.opencontainers.image.ref.name"] = config.Tags[0]
	}

	manifestData, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %v", err)
	}

	manifestPath := filepath.Join(imageDir, "manifest.json")
	if err := os.WriteFile(manifestPath, manifestData, 0644); err != nil {
		return fmt.Errorf("failed to write manifest: %v", err)
	}

	result.OutputPath = imageDir
	if len(config.Tags) > 0 {
		result.ImageID = config.Tags[0]
	} else {
		result.ImageID = configDigest
	}

	return nil
}

func (e *ImageExporter) collectLayers(layersDir string) ([]string, error) {
	var layers []string
	
	entries, err := os.ReadDir(layersDir)
	if os.IsNotExist(err) {
		return layers, nil
	}
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			layerHash := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(entry.Name())))
			layers = append(layers, layerHash)
		}
	}

	return layers, nil
}

func (e *ImageExporter) buildContainerConfig(metadata map[string]string) OCIContainerConfig {
	config := OCIContainerConfig{
		Labels: make(map[string]string),
	}

	if metadata == nil {
		return config
	}

	if user, exists := metadata["user"]; exists {
		config.User = user
	}

	if workdir, exists := metadata["workdir"]; exists {
		config.WorkingDir = workdir
	}

	if cmd, exists := metadata["cmd"]; exists {
		config.Cmd = []string{"/bin/sh", "-c", cmd}
	}

	if entrypoint, exists := metadata["entrypoint"]; exists {
		config.Entrypoint = []string{"/bin/sh", "-c", entrypoint}
	}

	if expose, exists := metadata["expose"]; exists {
		config.ExposedPorts = make(map[string]struct{})
		ports := parseCommaSeparated(expose)
		for _, port := range ports {
			config.ExposedPorts[port+"/tcp"] = struct{}{}
		}
	}

	if volume, exists := metadata["volume"]; exists {
		config.Volumes = make(map[string]struct{})
		volumes := parseCommaSeparated(volume)
		for _, vol := range volumes {
			config.Volumes[vol] = struct{}{}
		}
	}

	for key, value := range metadata {
		if key == "label" {
			config.Labels["custom"] = value
		} else if len(key) > 6 && key[:6] == "label." {
			config.Labels[key[6:]] = value
		}
	}

	return config
}

func (e *ImageExporter) buildHistory(result *types.BuildResult) []OCIHistory {
	return []OCIHistory{
		{
			Created:   time.Now(),
			CreatedBy: "ossb",
			Comment:   fmt.Sprintf("Built with OSSB - %d operations", result.Operations),
		},
	}
}

func parseCommaSeparated(value string) []string {
	if value == "" {
		return []string{}
	}
	
	parts := make([]string, 0)
	for _, part := range splitByComma(value) {
		if trimmed := trimSpace(part); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	
	return parts
}

func splitByComma(s string) []string {
	var result []string
	var current string
	
	for _, r := range s {
		if r == ',' {
			result = append(result, current)
			current = ""
		} else {
			current += string(r)
		}
	}
	
	if current != "" {
		result = append(result, current)
	}
	
	return result
}

func trimSpace(s string) string {
	start := 0
	end := len(s)
	
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}
	
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}
	
	return s[start:end]
}