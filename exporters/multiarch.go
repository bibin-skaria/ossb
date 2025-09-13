package exporters

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/bibin-skaria/ossb/internal/types"
	"github.com/bibin-skaria/ossb/layers"
	"github.com/bibin-skaria/ossb/manifest"
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
	// If not multi-arch or only one platform, delegate to ImageExporter
	if !result.MultiArch || len(result.PlatformResults) <= 1 {
		imageExporter := &ImageExporter{}
		return imageExporter.Export(result, config, workDir)
	}

	// Create OCI image layout directory structure
	imageDir := filepath.Join(workDir, "multiarch")
	blobsDir := filepath.Join(imageDir, "blobs", "sha256")
	if err := os.MkdirAll(blobsDir, 0755); err != nil {
		return fmt.Errorf("failed to create OCI layout directories: %v", err)
	}

	// Create OCI layout file
	layoutData := map[string]interface{}{
		"imageLayoutVersion": "1.0.0",
	}
	layoutBytes, _ := json.Marshal(layoutData)
	if err := os.WriteFile(filepath.Join(imageDir, "oci-layout"), layoutBytes, 0644); err != nil {
		return fmt.Errorf("failed to write OCI layout: %v", err)
	}

	generator := manifest.NewGenerator(manifest.DefaultGeneratorOptions())
	var platformManifests []manifest.PlatformManifest
	
	for platformStr, platformResult := range result.PlatformResults {
		if !platformResult.Success {
			continue
		}

		platform := types.ParsePlatform(platformStr)
		
		// Build platform-specific manifest
		platformManifest, err := e.buildPlatformManifest(platform, platformResult, config, workDir, blobsDir, generator)
		if err != nil {
			return fmt.Errorf("failed to build manifest for %s: %v", platformStr, err)
		}

		platformManifests = append(platformManifests, *platformManifest)
	}

	if len(platformManifests) == 0 {
		return fmt.Errorf("no successful platform builds to export")
	}

	// Generate manifest list
	manifestList, err := generator.GenerateManifestList(platformManifests)
	if err != nil {
		return fmt.Errorf("failed to generate manifest list: %v", err)
	}

	// Add annotations
	if manifestList.Annotations == nil {
		manifestList.Annotations = make(map[string]string)
	}
	manifestList.Annotations["org.opencontainers.image.created"] = time.Now().Format(time.RFC3339)
	if len(config.Tags) > 0 {
		manifestList.Annotations["org.opencontainers.image.ref.name"] = config.Tags[0]
		manifestList.Annotations["org.opencontainers.image.title"] = config.Tags[0]
	}

	// Serialize and store manifest list
	manifestListData, err := generator.SerializeManifestList(manifestList)
	if err != nil {
		return fmt.Errorf("failed to serialize manifest list: %v", err)
	}

	manifestListDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(manifestListData))
	manifestListPath := filepath.Join(blobsDir, manifestListDigest[7:])
	if err := os.WriteFile(manifestListPath, manifestListData, 0644); err != nil {
		return fmt.Errorf("failed to write manifest list blob: %v", err)
	}

	// Create index.json for OCI layout
	indexData := map[string]interface{}{
		"schemaVersion": 2,
		"manifests": []map[string]interface{}{
			{
				"mediaType": "application/vnd.oci.image.index.v1+json",
				"digest":    manifestListDigest,
				"size":      len(manifestListData),
				"annotations": map[string]string{
					"org.opencontainers.image.ref.name": e.getImageReference(config),
				},
			},
		},
	}
	indexBytes, _ := json.Marshal(indexData)
	if err := os.WriteFile(filepath.Join(imageDir, "index.json"), indexBytes, 0644); err != nil {
		return fmt.Errorf("failed to write index.json: %v", err)
	}

	// Update result
	result.OutputPath = imageDir
	result.ManifestListID = manifestListDigest
	if len(config.Tags) > 0 {
		result.ImageID = config.Tags[0] + "@" + manifestListDigest
	} else {
		result.ImageID = manifestListDigest
	}

	// Push if requested
	if config.Push && config.Registry != "" {
		if err := e.pushMultiArchImage(manifestList, config, imageDir); err != nil {
			return fmt.Errorf("failed to push multi-arch image: %v", err)
		}
	}

	return nil
}

func (e *MultiArchExporter) buildPlatformManifest(platform types.Platform, platformResult *types.PlatformResult, config *types.BuildConfig, workDir, blobsDir string, generator *manifest.Generator) (*manifest.PlatformManifest, error) {
	layersDir := filepath.Join(workDir, "layers", platform.String())
	
	// Collect and process layers for this platform
	layerObjects, err := e.collectPlatformLayers(layersDir, platform, blobsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to collect layers for %s: %v", platform.String(), err)
	}

	// Create Dockerfile instructions from platform result metadata
	instructions := e.createPlatformInstructions(platformResult, platform)
	
	// Generate image configuration
	imageConfig, err := generator.GenerateImageConfig(instructions, platform)
	if err != nil {
		return nil, fmt.Errorf("failed to generate image config for %s: %v", platform.String(), err)
	}

	// Add layers to config
	for _, layer := range layerObjects {
		if err := generator.AddLayerToConfig(imageConfig, layer); err != nil {
			return nil, fmt.Errorf("failed to add layer to config for %s: %v", platform.String(), err)
		}
	}

	// Serialize and store config
	configData, err := generator.SerializeConfig(imageConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize config for %s: %v", platform.String(), err)
	}

	configDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(configData))
	configPath := filepath.Join(blobsDir, configDigest[7:])
	if err := os.WriteFile(configPath, configData, 0644); err != nil {
		return nil, fmt.Errorf("failed to write config blob for %s: %v", platform.String(), err)
	}

	// Generate image manifest
	imageManifest, err := generator.GenerateImageManifest(imageConfig, layerObjects)
	if err != nil {
		return nil, fmt.Errorf("failed to generate manifest for %s: %v", platform.String(), err)
	}

	// Add platform-specific annotations
	if imageManifest.Annotations == nil {
		imageManifest.Annotations = make(map[string]string)
	}
	imageManifest.Annotations["org.opencontainers.image.created"] = time.Now().Format(time.RFC3339)
	imageManifest.Annotations["org.opencontainers.image.platform"] = platform.String()

	// Serialize and store manifest
	manifestData, err := generator.SerializeManifest(imageManifest)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize manifest for %s: %v", platform.String(), err)
	}

	manifestDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(manifestData))
	manifestPath := filepath.Join(blobsDir, manifestDigest[7:])
	if err := os.WriteFile(manifestPath, manifestData, 0644); err != nil {
		return nil, fmt.Errorf("failed to write manifest blob for %s: %v", platform.String(), err)
	}

	// Create platform manifest
	platformManifest := &manifest.PlatformManifest{
		MediaType: "application/vnd.oci.image.manifest.v1+json",
		Size:      int64(len(manifestData)),
		Digest:    manifestDigest,
		Platform: manifest.Platform{
			Architecture: platform.Architecture,
			OS:           platform.OS,
			Variant:      platform.Variant,
		},
	}

	return platformManifest, nil
}

func (e *MultiArchExporter) collectPlatformLayers(layersDir string, platform types.Platform, blobsDir string) ([]*layers.Layer, error) {
	var layerObjects []*layers.Layer
	
	entries, err := os.ReadDir(layersDir)
	if os.IsNotExist(err) {
		return layerObjects, nil
	}
	if err != nil {
		return nil, err
	}

	layerManager := layers.NewLayerManager(layers.LayerConfig{
		Compression: layers.CompressionGzip,
		SkipEmpty:   true,
	})

	for _, entry := range entries {
		if entry.IsDir() {
			layerPath := filepath.Join(layersDir, entry.Name())
			
			// Detect changes in the layer directory
			changes, err := e.detectPlatformLayerChanges(layerPath, platform)
			if err != nil {
				return nil, fmt.Errorf("failed to detect changes in layer %s for %s: %v", entry.Name(), platform.String(), err)
			}

			if len(changes) == 0 {
				continue // Skip empty layers
			}

			// Create layer from changes
			layer, err := layerManager.CreateLayer(changes)
			if err != nil {
				return nil, fmt.Errorf("failed to create layer %s for %s: %v", entry.Name(), platform.String(), err)
			}

			// Add platform-specific metadata
			if layer.Annotations == nil {
				layer.Annotations = make(map[string]string)
			}
			layer.Annotations["ossb.platform"] = platform.String()
			layer.CreatedBy = fmt.Sprintf("OSSB multiarch build for %s", platform.String())

			// Copy layer blob to blobs directory
			if layer.Blob != nil {
				blobPath := filepath.Join(blobsDir, layer.Digest[7:])
				if err := e.copyBlobToFile(layer.Blob, blobPath); err != nil {
					return nil, fmt.Errorf("failed to copy layer blob for %s: %v", platform.String(), err)
				}
				layer.Blob.Close()
			}

			layerObjects = append(layerObjects, layer)
		}
	}

	return layerObjects, nil
}

func (e *MultiArchExporter) detectPlatformLayerChanges(layerPath string, platform types.Platform) ([]layers.FileChange, error) {
	var changes []layers.FileChange
	
	err := filepath.Walk(layerPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(layerPath, path)
		if err != nil {
			return err
		}

		if relPath == "." {
			return nil
		}

		// Normalize path for OCI compliance
		relPath = filepath.ToSlash(relPath)
		if !strings.HasPrefix(relPath, "/") {
			relPath = "/" + relPath
		}

		change := layers.FileChange{
			Path:      relPath,
			Type:      layers.ChangeTypeAdd,
			Mode:      info.Mode(),
			Size:      info.Size(),
			Timestamp: info.ModTime(),
		}

		if info.IsDir() {
			change.Size = 0
		} else if info.Mode().IsRegular() {
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			change.Content = file
		}

		changes = append(changes, change)
		return nil
	})

	return changes, err
}

func (e *MultiArchExporter) copyBlobToFile(blob io.ReadCloser, destPath string) error {
	destFile, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, blob)
	return err
}

func (e *MultiArchExporter) createPlatformInstructions(platformResult *types.PlatformResult, platform types.Platform) []types.DockerfileInstruction {
	var instructions []types.DockerfileInstruction
	
	// Create basic FROM instruction
	instructions = append(instructions, types.DockerfileInstruction{
		Command: "FROM",
		Value:   "scratch",
		Line:    1,
	})

	// Add platform-specific metadata as labels
	instructions = append(instructions, types.DockerfileInstruction{
		Command: "LABEL",
		Value:   fmt.Sprintf("org.opencontainers.image.platform=%s", platform.String()),
		Line:    len(instructions) + 1,
	})

	instructions = append(instructions, types.DockerfileInstruction{
		Command: "LABEL",
		Value:   fmt.Sprintf("org.opencontainers.image.architecture=%s", platform.Architecture),
		Line:    len(instructions) + 1,
	})

	instructions = append(instructions, types.DockerfileInstruction{
		Command: "LABEL",
		Value:   fmt.Sprintf("org.opencontainers.image.os=%s", platform.OS),
		Line:    len(instructions) + 1,
	})

	if platform.Variant != "" {
		instructions = append(instructions, types.DockerfileInstruction{
			Command: "LABEL",
			Value:   fmt.Sprintf("org.opencontainers.image.variant=%s", platform.Variant),
			Line:    len(instructions) + 1,
		})
	}

	return instructions
}

func (e *MultiArchExporter) getImageReference(config *types.BuildConfig) string {
	if len(config.Tags) > 0 {
		return config.Tags[0]
	}
	return "latest"
}

func (e *MultiArchExporter) pushMultiArchImage(manifestList *manifest.ManifestList, config *types.BuildConfig, imageDir string) error {
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