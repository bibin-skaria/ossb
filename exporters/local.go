package exporters

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/bibin-skaria/ossb/internal/types"
	"github.com/bibin-skaria/ossb/layers"
	"github.com/bibin-skaria/ossb/manifest"
)

type LocalExporter struct{}

func init() {
	RegisterExporter("local", &LocalExporter{})
}

func (e *LocalExporter) Export(result *types.BuildResult, config *types.BuildConfig, workDir string) error {
	layersDir := filepath.Join(workDir, "layers")
	
	var outputPath string
	if len(config.Tags) > 0 {
		// Sanitize tag name for filesystem
		tagName := strings.ReplaceAll(config.Tags[0], "/", "_")
		tagName = strings.ReplaceAll(tagName, ":", "_")
		outputPath = filepath.Join(workDir, "output", tagName)
	} else {
		outputPath = filepath.Join(workDir, "output", "image")
	}

	if err := os.MkdirAll(outputPath, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %v", err)
	}

	// Create filesystem root directory
	rootfsPath := filepath.Join(outputPath, "rootfs")
	if err := os.MkdirAll(rootfsPath, 0755); err != nil {
		return fmt.Errorf("failed to create rootfs directory: %v", err)
	}

	// Collect and process layers
	layerObjects, err := e.collectAndProcessLayers(layersDir)
	if err != nil {
		return fmt.Errorf("failed to collect layers: %v", err)
	}

	// Extract layers in order to build the final filesystem
	layerManager := layers.NewLayerManager(layers.LayerConfig{
		Compression: layers.CompressionGzip,
		SkipEmpty:   true,
	})

	for i, layer := range layerObjects {
		if err := layerManager.ExtractLayer(layer, rootfsPath); err != nil {
			return fmt.Errorf("failed to extract layer %d: %v", i, err)
		}
	}

	// Merge layers using traditional approach as fallback
	if err := e.mergeLayers(layersDir, rootfsPath); err != nil {
		return fmt.Errorf("failed to merge layers: %v", err)
	}

	// Generate and save image metadata
	if err := e.saveImageMetadata(result, config, outputPath, layerObjects); err != nil {
		return fmt.Errorf("failed to save image metadata: %v", err)
	}

	// Create extraction manifest for reproducibility
	if err := e.createExtractionManifest(outputPath, layerObjects, config); err != nil {
		return fmt.Errorf("failed to create extraction manifest: %v", err)
	}

	result.OutputPath = outputPath
	return nil
}

func (e *LocalExporter) mergeLayers(layersDir, outputDir string) error {
	entries, err := os.ReadDir(layersDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			layerPath := filepath.Join(layersDir, entry.Name())
			if err := e.copyLayer(layerPath, outputDir); err != nil {
				return fmt.Errorf("failed to copy layer %s: %v", entry.Name(), err)
			}
		}
	}

	return nil
}

func (e *LocalExporter) copyLayer(layerDir, outputDir string) error {
	return filepath.Walk(layerDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(layerDir, path)
		if err != nil {
			return err
		}

		if relPath == "." {
			return nil
		}

		destPath := filepath.Join(outputDir, relPath)

		if info.IsDir() {
			return os.MkdirAll(destPath, info.Mode())
		}

		if info.Mode().IsRegular() {
			return e.copyFile(path, destPath, info.Mode())
		}

		return nil
	})
}

func (e *LocalExporter) collectAndProcessLayers(layersDir string) ([]*layers.Layer, error) {
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
			changes, err := e.detectLayerChanges(layerPath)
			if err != nil {
				return nil, fmt.Errorf("failed to detect changes in layer %s: %v", entry.Name(), err)
			}

			if len(changes) == 0 {
				continue // Skip empty layers
			}

			// Create layer from changes
			layer, err := layerManager.CreateLayer(changes)
			if err != nil {
				return nil, fmt.Errorf("failed to create layer %s: %v", entry.Name(), err)
			}

			layerObjects = append(layerObjects, layer)
		}
	}

	return layerObjects, nil
}

func (e *LocalExporter) detectLayerChanges(layerPath string) ([]layers.FileChange, error) {
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

		// Get UID/GID if available
		if stat, ok := info.Sys().(*syscall.Stat_t); ok {
			change.UID = int(stat.Uid)
			change.GID = int(stat.Gid)
		}

		if info.IsDir() {
			change.Size = 0
		} else if info.Mode().IsRegular() {
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			change.Content = file
		} else if info.Mode()&os.ModeSymlink != 0 {
			linkTarget, err := os.Readlink(path)
			if err != nil {
				return err
			}
			change.Linkname = linkTarget
		}

		changes = append(changes, change)
		return nil
	})

	return changes, err
}

func (e *LocalExporter) saveImageMetadata(result *types.BuildResult, config *types.BuildConfig, outputPath string, layerObjects []*layers.Layer) error {
	// Determine platform
	platform := types.Platform{OS: "linux", Architecture: "amd64"}
	if len(config.Platforms) > 0 {
		platform = config.Platforms[0]
	}

	// Generate image configuration
	generator := manifest.NewGenerator(manifest.DefaultGeneratorOptions())
	instructions := e.createInstructionsFromMetadata(result.Metadata)
	
	imageConfig, err := generator.GenerateImageConfig(instructions, platform)
	if err != nil {
		return fmt.Errorf("failed to generate image config: %v", err)
	}

	// Add layers to config
	for _, layer := range layerObjects {
		if err := generator.AddLayerToConfig(imageConfig, layer); err != nil {
			return fmt.Errorf("failed to add layer to config: %v", err)
		}
	}

	// Ensure we have at least one layer (create empty layer if needed)
	if len(layerObjects) == 0 {
		layerManager := layers.NewLayerManager(layers.LayerConfig{
			Compression: layers.CompressionGzip,
			SkipEmpty:   false,
		})
		emptyLayer, err := layerManager.CreateLayer([]layers.FileChange{})
		if err != nil {
			return fmt.Errorf("failed to create empty layer: %v", err)
		}
		layerObjects = append(layerObjects, emptyLayer)
	}

	// Generate manifest
	imageManifest, err := generator.GenerateImageManifest(imageConfig, layerObjects)
	if err != nil {
		return fmt.Errorf("failed to generate manifest: %v", err)
	}

	// Save image config
	configData, err := generator.SerializeConfig(imageConfig)
	if err != nil {
		return err
	}
	configPath := filepath.Join(outputPath, "config.json")
	if err := os.WriteFile(configPath, configData, 0644); err != nil {
		return err
	}

	// Save manifest
	manifestData, err := generator.SerializeManifest(imageManifest)
	if err != nil {
		return err
	}
	manifestPath := filepath.Join(outputPath, "manifest.json")
	if err := os.WriteFile(manifestPath, manifestData, 0644); err != nil {
		return err
	}

	// Save build metadata
	metadata := map[string]interface{}{
		"build_result": result,
		"build_config": config,
		"exported_at":  time.Now().Format(time.RFC3339),
		"exporter":     "local",
		"platform":     platform,
		"layer_count":  len(layerObjects),
	}

	metadataBytes, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}
	metadataPath := filepath.Join(outputPath, "ossb-metadata.json")
	return os.WriteFile(metadataPath, metadataBytes, 0644)
}

func (e *LocalExporter) createInstructionsFromMetadata(metadata map[string]string) []types.DockerfileInstruction {
	var instructions []types.DockerfileInstruction
	
	if metadata == nil {
		return instructions
	}

	// Create basic FROM instruction
	instructions = append(instructions, types.DockerfileInstruction{
		Command: "FROM",
		Value:   "scratch",
		Line:    1,
	})

	// Add instructions based on metadata
	if workdir, exists := metadata["workdir"]; exists {
		instructions = append(instructions, types.DockerfileInstruction{
			Command: "WORKDIR",
			Value:   workdir,
			Line:    len(instructions) + 1,
		})
	}

	if user, exists := metadata["user"]; exists {
		instructions = append(instructions, types.DockerfileInstruction{
			Command: "USER",
			Value:   user,
			Line:    len(instructions) + 1,
		})
	}

	if cmd, exists := metadata["cmd"]; exists {
		instructions = append(instructions, types.DockerfileInstruction{
			Command: "CMD",
			Value:   cmd,
			Line:    len(instructions) + 1,
		})
	}

	if entrypoint, exists := metadata["entrypoint"]; exists {
		instructions = append(instructions, types.DockerfileInstruction{
			Command: "ENTRYPOINT",
			Value:   entrypoint,
			Line:    len(instructions) + 1,
		})
	}

	return instructions
}

func (e *LocalExporter) createExtractionManifest(outputPath string, layerObjects []*layers.Layer, config *types.BuildConfig) error {
	manifest := map[string]interface{}{
		"version":     "1.0",
		"format":      "local-filesystem",
		"created":     time.Now().Format(time.RFC3339),
		"layers":      make([]map[string]interface{}, len(layerObjects)),
		"rootfs_path": "rootfs",
	}

	if len(config.Tags) > 0 {
		manifest["image_name"] = config.Tags[0]
	}

	// Add layer information
	for i, layer := range layerObjects {
		layerInfo := map[string]interface{}{
			"digest":     layer.Digest,
			"size":       layer.Size,
			"media_type": layer.MediaType,
			"created":    layer.Created.Format(time.RFC3339),
		}

		if layer.CreatedBy != "" {
			layerInfo["created_by"] = layer.CreatedBy
		}

		if layer.Comment != "" {
			layerInfo["comment"] = layer.Comment
		}

		if layer.EmptyLayer {
			layerInfo["empty_layer"] = true
		}

		if len(layer.Annotations) > 0 {
			layerInfo["annotations"] = layer.Annotations
		}

		manifest["layers"].([]map[string]interface{})[i] = layerInfo
	}

	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}

	manifestPath := filepath.Join(outputPath, "extraction-manifest.json")
	return os.WriteFile(manifestPath, manifestBytes, 0644)
}

func (e *LocalExporter) copyFile(src, dest string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}

	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	destFile, err := os.OpenFile(dest, os.O_RDWR|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer destFile.Close()

	if _, err = io.Copy(destFile, srcFile); err != nil {
		return err
	}

	// Preserve file permissions and timestamps
	srcInfo, err := srcFile.Stat()
	if err != nil {
		return err
	}

	if err := os.Chmod(dest, srcInfo.Mode()); err != nil {
		return err
	}

	return os.Chtimes(dest, srcInfo.ModTime(), srcInfo.ModTime())
}