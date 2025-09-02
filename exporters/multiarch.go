package exporters

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/bibin-skaria/ossb/internal/types"
)

type MultiArchExporter struct{}

func init() {
	RegisterExporter("multiarch", &MultiArchExporter{})
}

type OCIIndex struct {
	SchemaVersion int                   `json:"schemaVersion"`
	MediaType     string                `json:"mediaType"`
	Manifests     []OCIManifestRef      `json:"manifests"`
	Annotations   map[string]string     `json:"annotations,omitempty"`
}

type OCIManifestRef struct {
	MediaType string                `json:"mediaType"`
	Digest    string                `json:"digest"`
	Size      int64                 `json:"size"`
	Platform  OCIPlatformDescriptor `json:"platform,omitempty"`
}

type OCIPlatformDescriptor struct {
	Architecture string   `json:"architecture"`
	OS           string   `json:"os"`
	Variant      string   `json:"variant,omitempty"`
	Features     []string `json:"features,omitempty"`
	OSVersion    string   `json:"os.version,omitempty"`
	OSFeatures   []string `json:"os.features,omitempty"`
}

func (e *MultiArchExporter) Export(result *types.BuildResult, config *types.BuildConfig, workDir string) error {
	if !result.MultiArch || len(result.PlatformResults) <= 1 {
		imageExporter := &ImageExporter{}
		return imageExporter.Export(result, config, workDir)
	}

	imageDir := filepath.Join(workDir, "multiarch")
	if err := os.MkdirAll(imageDir, 0755); err != nil {
		return fmt.Errorf("failed to create multiarch directory: %v", err)
	}

	var manifestRefs []OCIManifestRef
	
	for platformStr, platformResult := range result.PlatformResults {
		if !platformResult.Success {
			continue
		}

		platform := types.ParsePlatform(platformStr)
		
		manifest, err := e.buildPlatformManifest(platform, platformResult, config, workDir)
		if err != nil {
			return fmt.Errorf("failed to build manifest for %s: %v", platformStr, err)
		}

		manifestData, err := json.Marshal(manifest)
		if err != nil {
			return fmt.Errorf("failed to marshal manifest for %s: %v", platformStr, err)
		}

		manifestDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(manifestData))
		manifestPath := filepath.Join(imageDir, "manifests", manifestDigest[7:]+".json")
		
		if err := os.MkdirAll(filepath.Dir(manifestPath), 0755); err != nil {
			return fmt.Errorf("failed to create manifest directory: %v", err)
		}
		
		if err := os.WriteFile(manifestPath, manifestData, 0644); err != nil {
			return fmt.Errorf("failed to write manifest for %s: %v", platformStr, err)
		}

		manifestRef := OCIManifestRef{
			MediaType: "application/vnd.oci.image.manifest.v1+json",
			Digest:    manifestDigest,
			Size:      int64(len(manifestData)),
			Platform: OCIPlatformDescriptor{
				Architecture: platform.Architecture,
				OS:           platform.OS,
				Variant:      platform.Variant,
			},
		}
		
		manifestRefs = append(manifestRefs, manifestRef)
	}

	if len(manifestRefs) == 0 {
		return fmt.Errorf("no successful platform builds to export")
	}

	index := &OCIIndex{
		SchemaVersion: 2,
		MediaType:     "application/vnd.oci.image.index.v1+json",
		Manifests:     manifestRefs,
		Annotations: map[string]string{
			"org.opencontainers.image.created": time.Now().Format(time.RFC3339),
		},
	}

	if len(config.Tags) > 0 {
		index.Annotations["org.opencontainers.image.ref.name"] = config.Tags[0]
		index.Annotations["org.opencontainers.image.title"] = config.Tags[0]
	}

	indexData, err := json.Marshal(index)
	if err != nil {
		return fmt.Errorf("failed to marshal image index: %v", err)
	}

	indexPath := filepath.Join(imageDir, "index.json")
	if err := os.WriteFile(indexPath, indexData, 0644); err != nil {
		return fmt.Errorf("failed to write image index: %v", err)
	}

	indexDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(indexData))
	
	result.OutputPath = imageDir
	result.ManifestListID = indexDigest
	if len(config.Tags) > 0 {
		result.ImageID = config.Tags[0] + "@" + indexDigest
	} else {
		result.ImageID = indexDigest
	}

	if config.Push && config.Registry != "" {
		if err := e.pushMultiArchImage(index, config, imageDir); err != nil {
			return fmt.Errorf("failed to push multi-arch image: %v", err)
		}
	}

	return nil
}

func (e *MultiArchExporter) buildPlatformManifest(platform types.Platform, platformResult *types.PlatformResult, config *types.BuildConfig, workDir string) (*OCIManifest, error) {
	layersDir := filepath.Join(workDir, "layers", platform.String())
	
	layers, err := e.collectPlatformLayers(layersDir, platform)
	if err != nil {
		return nil, fmt.Errorf("failed to collect layers for %s: %v", platform.String(), err)
	}

	imageConfig := &OCIImageConfig{
		Created:      time.Now(),
		Architecture: platform.Architecture,
		OS:           platform.OS,
		Config:       e.buildContainerConfig(config, platform),
		RootFS: OCIRootFS{
			Type:    "layers",
			DiffIDs: layers,
		},
		History: e.buildPlatformHistory(platform),
	}

	if platform.Variant != "" {
		imageConfig.Variant = platform.Variant
	}

	configData, err := json.Marshal(imageConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal image config: %v", err)
	}

	configDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(configData))
	configPath := filepath.Join(workDir, "multiarch", "blobs", configDigest[7:]+".json")
	
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %v", err)
	}
	
	if err := os.WriteFile(configPath, configData, 0644); err != nil {
		return nil, fmt.Errorf("failed to write config: %v", err)
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
			"org.opencontainers.image.platform": platform.String(),
		},
	}

	return manifest, nil
}

func (e *MultiArchExporter) collectPlatformLayers(layersDir string, platform types.Platform) ([]string, error) {
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
			layerHash := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(entry.Name()+platform.String())))
			layers = append(layers, layerHash)
		}
	}

	return layers, nil
}

func (e *MultiArchExporter) buildContainerConfig(config *types.BuildConfig, platform types.Platform) OCIContainerConfig {
	containerConfig := OCIContainerConfig{
		Labels: make(map[string]string),
	}

	containerConfig.Labels["org.opencontainers.image.platform"] = platform.String()
	containerConfig.Labels["org.opencontainers.image.architecture"] = platform.Architecture
	containerConfig.Labels["org.opencontainers.image.os"] = platform.OS
	
	if platform.Variant != "" {
		containerConfig.Labels["org.opencontainers.image.variant"] = platform.Variant
	}

	return containerConfig
}

func (e *MultiArchExporter) buildPlatformHistory(platform types.Platform) []OCIHistory {
	return []OCIHistory{
		{
			Created:   time.Now(),
			CreatedBy: fmt.Sprintf("ossb multiarch build for %s", platform.String()),
			Comment:   fmt.Sprintf("Multi-architecture build layer for %s", platform.String()),
		},
	}
}

func (e *MultiArchExporter) pushMultiArchImage(index *OCIIndex, config *types.BuildConfig, imageDir string) error {
	if len(config.Tags) == 0 {
		return fmt.Errorf("no tags specified for push")
	}

	for _, tag := range config.Tags {
		if !strings.Contains(tag, config.Registry) {
			tag = config.Registry + "/" + tag
		}

		cmd := fmt.Sprintf("skopeo copy oci:%s:%s docker://%s", imageDir, "latest", tag)
		
		if err := e.runCommand(cmd); err != nil {
			return fmt.Errorf("failed to push %s: %v", tag, err)
		}
	}

	return nil
}

func (e *MultiArchExporter) runCommand(command string) error {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return fmt.Errorf("empty command")
	}

	cmd := exec.Command(parts[0], parts[1:]...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("command failed: %v, output: %s", err, string(output))
	}

	return nil
}

type OCIImageConfigMultiArch struct {
	*OCIImageConfig
	Variant string `json:"variant,omitempty"`
}