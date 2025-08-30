package exporters

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/bibin-skaria/ossb/internal/types"
)

type TarExporter struct{}

func init() {
	RegisterExporter("tar", &TarExporter{})
}

func (e *TarExporter) Export(result *types.BuildResult, config *types.BuildConfig, workDir string) error {
	layersDir := filepath.Join(workDir, "layers")
	
	var outputPath string
	if len(config.Tags) > 0 {
		outputPath = filepath.Join(workDir, config.Tags[0]+".tar")
	} else {
		outputPath = filepath.Join(workDir, "image.tar")
	}

	tarFile, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create tar file: %v", err)
	}
	defer tarFile.Close()

	tarWriter := tar.NewWriter(tarFile)
	defer tarWriter.Close()

	if err := e.addLayersToTar(tarWriter, layersDir); err != nil {
		return fmt.Errorf("failed to add layers to tar: %v", err)
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