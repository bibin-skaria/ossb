package exporters

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bibin-skaria/ossb/internal/types"
	"github.com/bibin-skaria/ossb/layers"
	"github.com/bibin-skaria/ossb/manifest"
)

type TarExporter struct{}

func init() {
	RegisterExporter("tar", &TarExporter{})
}

func (e *TarExporter) Export(result *types.BuildResult, config *types.BuildConfig, workDir string) error {
	layersDir := filepath.Join(workDir, "layers")
	
	var outputPath string
	if len(config.Tags) > 0 {
		// Sanitize tag name for filesystem
		tagName := strings.ReplaceAll(config.Tags[0], "/", "_")
		tagName = strings.ReplaceAll(tagName, ":", "_")
		outputPath = filepath.Join(workDir, tagName+".tar")
	} else {
		outputPath = filepath.Join(workDir, "image.tar")
	}

	// Create tar file with optional compression
	tarFile, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create tar file: %v", err)
	}
	defer tarFile.Close()

	var writer io.Writer = tarFile
	var gzWriter *gzip.Writer

	// Add gzip compression if requested
	if e.shouldCompress(config) {
		gzWriter = gzip.NewWriter(tarFile)
		writer = gzWriter
		defer gzWriter.Close()
		
		// Update output path to reflect compression
		outputPath = outputPath + ".gz"
	}

	tarWriter := tar.NewWriter(writer)
	defer tarWriter.Close()

	// Collect and process layers
	layerObjects, err := e.collectAndProcessLayers(layersDir)
	if err != nil {
		return fmt.Errorf("failed to collect layers: %v", err)
	}

	// Determine platform
	platform := types.Platform{OS: "linux", Architecture: "amd64"}
	if len(config.Platforms) > 0 {
		platform = config.Platforms[0]
	}

	// Generate image configuration and manifest
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

	// Add manifest and config to tar
	if err := e.addManifestToTar(tarWriter, imageManifest, imageConfig, generator); err != nil {
		return fmt.Errorf("failed to add manifest to tar: %v", err)
	}

	// Add layer blobs to tar
	if err := e.addLayerBlobsToTar(tarWriter, layerObjects); err != nil {
		return fmt.Errorf("failed to add layer blobs to tar: %v", err)
	}

	// Add layer directories to tar (for compatibility)
	if err := e.addLayersToTar(tarWriter, layersDir); err != nil {
		return fmt.Errorf("failed to add layers to tar: %v", err)
	}

	// Add metadata file
	if err := e.addMetadataToTar(tarWriter, result, config); err != nil {
		return fmt.Errorf("failed to add metadata to tar: %v", err)
	}

	result.OutputPath = outputPath
	return nil
}

func (e *TarExporter) addLayersToTar(tarWriter *tar.Writer, layersDir string) error {
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
			if err := e.addDirectoryToTar(tarWriter, layerPath, ""); err != nil {
				return fmt.Errorf("failed to add layer %s: %v", entry.Name(), err)
			}
		}
	}

	return nil
}

func (e *TarExporter) addDirectoryToTar(tarWriter *tar.Writer, srcDir, prefix string) error {
	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}

		if relPath == "." {
			return nil
		}

		tarPath := filepath.Join(prefix, relPath)
		tarPath = filepath.ToSlash(tarPath)

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}

		header.Name = tarPath

		if info.IsDir() {
			header.Name += "/"
			if err := tarWriter.WriteHeader(header); err != nil {
				return err
			}
			return nil
		}

		if info.Mode().IsRegular() {
			if err := tarWriter.WriteHeader(header); err != nil {
				return err
			}

			file, err := os.Open(path)
			if err != nil {
				return err
			}
			defer file.Close()

			if _, err := io.Copy(tarWriter, file); err != nil {
				return err
			}
		}

		return nil
	})
}

func (e *TarExporter) shouldCompress(config *types.BuildConfig) bool {
	// Check if compression is requested via output format or config
	return strings.HasSuffix(config.Output, ".gz") || strings.Contains(config.Output, "compressed")
}

func (e *TarExporter) collectAndProcessLayers(layersDir string) ([]*layers.Layer, error) {
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

func (e *TarExporter) detectLayerChanges(layerPath string) ([]layers.FileChange, error) {
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

func (e *TarExporter) createInstructionsFromMetadata(metadata map[string]string) []types.DockerfileInstruction {
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

func (e *TarExporter) addManifestToTar(tarWriter *tar.Writer, imageManifest *manifest.ImageManifest, imageConfig *manifest.ImageConfig, generator *manifest.Generator) error {
	// Add image config
	configData, err := generator.SerializeConfig(imageConfig)
	if err != nil {
		return err
	}

	configDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(configData))
	configHeader := &tar.Header{
		Name:     "blobs/sha256/" + configDigest[7:],
		Mode:     0644,
		Size:     int64(len(configData)),
		ModTime:  time.Now(),
		Typeflag: tar.TypeReg,
	}

	if err := tarWriter.WriteHeader(configHeader); err != nil {
		return err
	}
	if _, err := tarWriter.Write(configData); err != nil {
		return err
	}

	// Add image manifest
	manifestData, err := generator.SerializeManifest(imageManifest)
	if err != nil {
		return err
	}

	manifestDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(manifestData))
	manifestHeader := &tar.Header{
		Name:     "blobs/sha256/" + manifestDigest[7:],
		Mode:     0644,
		Size:     int64(len(manifestData)),
		ModTime:  time.Now(),
		Typeflag: tar.TypeReg,
	}

	if err := tarWriter.WriteHeader(manifestHeader); err != nil {
		return err
	}
	if _, err := tarWriter.Write(manifestData); err != nil {
		return err
	}

	// Add OCI layout
	layoutData := map[string]interface{}{
		"imageLayoutVersion": "1.0.0",
	}
	layoutBytes, _ := json.Marshal(layoutData)
	layoutHeader := &tar.Header{
		Name:     "oci-layout",
		Mode:     0644,
		Size:     int64(len(layoutBytes)),
		ModTime:  time.Now(),
		Typeflag: tar.TypeReg,
	}

	if err := tarWriter.WriteHeader(layoutHeader); err != nil {
		return err
	}
	if _, err := tarWriter.Write(layoutBytes); err != nil {
		return err
	}

	// Add index.json
	indexData := map[string]interface{}{
		"schemaVersion": 2,
		"manifests": []map[string]interface{}{
			{
				"mediaType": "application/vnd.oci.image.manifest.v1+json",
				"digest":    manifestDigest,
				"size":      len(manifestData),
			},
		},
	}
	indexBytes, _ := json.Marshal(indexData)
	indexHeader := &tar.Header{
		Name:     "index.json",
		Mode:     0644,
		Size:     int64(len(indexBytes)),
		ModTime:  time.Now(),
		Typeflag: tar.TypeReg,
	}

	if err := tarWriter.WriteHeader(indexHeader); err != nil {
		return err
	}
	if _, err := tarWriter.Write(indexBytes); err != nil {
		return err
	}

	return nil
}

func (e *TarExporter) addLayerBlobsToTar(tarWriter *tar.Writer, layerObjects []*layers.Layer) error {
	for _, layer := range layerObjects {
		if layer.Blob == nil {
			continue
		}

		// Read layer blob data
		blobData, err := io.ReadAll(layer.Blob)
		if err != nil {
			return fmt.Errorf("failed to read layer blob: %v", err)
		}
		layer.Blob.Close()

		// Add blob to tar
		blobHeader := &tar.Header{
			Name:     "blobs/sha256/" + layer.Digest[7:],
			Mode:     0644,
			Size:     int64(len(blobData)),
			ModTime:  layer.Created,
			Typeflag: tar.TypeReg,
		}

		if err := tarWriter.WriteHeader(blobHeader); err != nil {
			return err
		}
		if _, err := tarWriter.Write(blobData); err != nil {
			return err
		}
	}

	return nil
}

func (e *TarExporter) addMetadataToTar(tarWriter *tar.Writer, result *types.BuildResult, config *types.BuildConfig) error {
	metadata := map[string]interface{}{
		"build_result": result,
		"build_config": config,
		"exported_at":  time.Now().Format(time.RFC3339),
		"exporter":     "tar",
	}

	metadataBytes, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}

	metadataHeader := &tar.Header{
		Name:     "ossb-metadata.json",
		Mode:     0644,
		Size:     int64(len(metadataBytes)),
		ModTime:  time.Now(),
		Typeflag: tar.TypeReg,
	}

	if err := tarWriter.WriteHeader(metadataHeader); err != nil {
		return err
	}
	if _, err := tarWriter.Write(metadataBytes); err != nil {
		return err
	}

	return nil
}

func (e *TarExporter) addFileToTar(tarWriter *tar.Writer, filePath, tarPath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return err
	}

	header, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return err
	}

	header.Name = strings.TrimPrefix(tarPath, "/")

	if err := tarWriter.WriteHeader(header); err != nil {
		return err
	}

	if info.IsDir() {
		return nil
	}

	_, err = io.Copy(tarWriter, file)
	return err
}