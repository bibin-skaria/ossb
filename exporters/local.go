package exporters

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/bibin-skaria/ossb/internal/types"
)

type LocalExporter struct{}

func init() {
	RegisterExporter("local", &LocalExporter{})
}

func (e *LocalExporter) Export(result *types.BuildResult, config *types.BuildConfig, workDir string) error {
	layersDir := filepath.Join(workDir, "layers")
	
	var outputPath string
	if len(config.Tags) > 0 {
		outputPath = filepath.Join(workDir, "output", config.Tags[0])
	} else {
		outputPath = filepath.Join(workDir, "output", "image")
	}

	if err := os.MkdirAll(outputPath, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %v", err)
	}

	if err := e.mergeLayers(layersDir, outputPath); err != nil {
		return fmt.Errorf("failed to merge layers: %v", err)
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

	_, err = io.Copy(destFile, srcFile)
	return err
}