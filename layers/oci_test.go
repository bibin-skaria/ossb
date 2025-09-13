package layers

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

// TestOCILayerCompliance tests that created layers comply with OCI Image Specification
func TestOCILayerCompliance(t *testing.T) {
	config := LayerConfig{
		Compression: CompressionGzip,
		SkipEmpty:   false,
	}

	lm := NewLayerManager(config)

	// Create test changes that represent typical Dockerfile operations
	changes := []FileChange{
		{
			Path:      "/usr/bin/myapp",
			Type:      ChangeTypeAdd,
			Mode:      0755,
			Size:      876, // strings.Repeat("binary", 146) = 6 * 146 = 876 bytes
			Content:   strings.NewReader(strings.Repeat("binary", 146)),
			Timestamp: time.Unix(1234567890, 0), // Fixed timestamp for reproducibility
		},
		{
			Path:      "/etc/config.json",
			Type:      ChangeTypeAdd,
			Mode:      0644,
			Size:      30, // Actual size of `{"key": "value", "number": 42}`
			Content:   strings.NewReader(`{"key": "value", "number": 42}`),
			Timestamp: time.Unix(1234567890, 0),
		},
		{
			Path:      "/var/log",
			Type:      ChangeTypeAdd,
			Mode:      os.ModeDir | 0755, // Directory mode
			Size:      0,
			Timestamp: time.Unix(1234567890, 0),
		},
	}

	layer, err := lm.CreateLayer(changes)
	if err != nil {
		t.Fatalf("CreateLayer failed: %v", err)
	}

	// Test 1: Validate layer structure
	if err := lm.ValidateLayer(layer); err != nil {
		t.Errorf("Layer validation failed: %v", err)
	}

	// Test 2: Verify digest format (SHA256)
	if !strings.HasPrefix(layer.Digest, "sha256:") {
		t.Errorf("Expected SHA256 digest, got: %s", layer.Digest)
	}

	if len(layer.Digest) != 71 { // "sha256:" + 64 hex chars
		t.Errorf("Invalid digest length: %d", len(layer.Digest))
	}

	// Test 3: Verify media type is OCI compliant
	expectedMediaType := MediaTypeImageLayerGzip
	if layer.MediaType != expectedMediaType {
		t.Errorf("Expected media type %s, got %s", expectedMediaType, layer.MediaType)
	}

	// Test 4: Verify digest matches actual content
	if layer.Blob == nil {
		t.Fatal("Layer blob is nil")
	}

	// Read the blob content
	blobData, err := io.ReadAll(layer.Blob)
	if err != nil {
		t.Fatalf("Failed to read blob: %v", err)
	}

	// Calculate digest of the blob
	hasher := sha256.New()
	hasher.Write(blobData)
	calculatedDigest := fmt.Sprintf("sha256:%x", hasher.Sum(nil))

	if layer.Digest != calculatedDigest {
		t.Errorf("Digest mismatch: expected %s, calculated %s", layer.Digest, calculatedDigest)
	}

	// Test 5: Verify blob size matches
	if layer.Size != int64(len(blobData)) {
		t.Errorf("Size mismatch: expected %d, got %d", len(blobData), layer.Size)
	}

	// Test 6: Verify the blob is valid gzip-compressed tar
	gzReader, err := gzip.NewReader(bytes.NewReader(blobData))
	if err != nil {
		t.Fatalf("Failed to create gzip reader: %v", err)
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)
	fileCount := 0
	foundFiles := make(map[string]bool)

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Failed to read tar header: %v", err)
		}

		fileCount++
		foundFiles[header.Name] = true

		// Verify header fields are properly set
		if header.ModTime.Unix() != 1234567890 {
			t.Errorf("Unexpected modification time for %s: %v", header.Name, header.ModTime)
		}

		// Verify file modes
		switch header.Name {
		case "usr/bin/myapp":
			if header.Mode != 0755 {
				t.Errorf("Unexpected mode for myapp: %o", header.Mode)
			}
			if header.Typeflag != tar.TypeReg {
				t.Errorf("Unexpected type for myapp: %v", header.Typeflag)
			}
		case "etc/config.json":
			if header.Mode != 0644 {
				t.Errorf("Unexpected mode for config.json: %o", header.Mode)
			}
		case "var/log":
			if header.Typeflag != tar.TypeDir {
				t.Errorf("Expected directory type for var/log, got: %v", header.Typeflag)
			}
		}
	}

	// Verify all expected files are present
	expectedFiles := []string{"usr/bin/myapp", "etc/config.json", "var/log"}
	for _, expectedFile := range expectedFiles {
		if !foundFiles[expectedFile] {
			t.Errorf("Expected file %s not found in layer", expectedFile)
		}
	}

	if fileCount != len(expectedFiles) {
		t.Errorf("Expected %d files in layer, found %d", len(expectedFiles), fileCount)
	}
}

// TestOCIWhiteoutCompliance tests that whiteout files comply with OCI spec
func TestOCIWhiteoutCompliance(t *testing.T) {
	config := LayerConfig{
		Compression: CompressionNone, // Use no compression for easier inspection
		SkipEmpty:   false,
	}

	lm := NewLayerManager(config)

	// Create changes with deletions
	changes := []FileChange{
		{
			Path:      "/usr/bin/oldapp",
			Type:      ChangeTypeDelete,
			Mode:      0755,
			Timestamp: time.Unix(1234567890, 0),
		},
		{
			Path:      "/etc/old-config.conf",
			Type:      ChangeTypeDelete,
			Mode:      0644,
			Timestamp: time.Unix(1234567890, 0),
		},
		{
			Path:      "/var/cache/data",
			Type:      ChangeTypeDelete,
			Mode:      0755,
			Timestamp: time.Unix(1234567890, 0),
		},
	}

	layer, err := lm.CreateLayer(changes)
	if err != nil {
		t.Fatalf("CreateLayer failed: %v", err)
	}

	// Read and inspect the layer content
	blobData, err := io.ReadAll(layer.Blob)
	if err != nil {
		t.Fatalf("Failed to read blob: %v", err)
	}

	tarReader := tar.NewReader(bytes.NewReader(blobData))
	whiteoutFiles := make(map[string]string) // whiteout file -> original file

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Failed to read tar header: %v", err)
		}

		// Check if this is a whiteout file
		if strings.Contains(header.Name, ".wh.") {
			whiteoutFiles[header.Name] = strings.Replace(header.Name, ".wh.", "", 1)

			// Verify whiteout file properties according to OCI spec
			if header.Typeflag != tar.TypeReg {
				t.Errorf("Whiteout file %s should be regular file, got type %v", header.Name, header.Typeflag)
			}

			if header.Size != 0 {
				t.Errorf("Whiteout file %s should have size 0, got %d", header.Name, header.Size)
			}

			// Verify whiteout file naming convention
			expectedWhiteoutName := ""
			switch header.Name {
			case "usr/bin/.wh.oldapp":
				expectedWhiteoutName = "usr/bin/.wh.oldapp"
			case "etc/.wh.old-config.conf":
				expectedWhiteoutName = "etc/.wh.old-config.conf"
			case "var/cache/.wh.data":
				expectedWhiteoutName = "var/cache/.wh.data"
			default:
				t.Errorf("Unexpected whiteout file: %s", header.Name)
			}

			if header.Name != expectedWhiteoutName {
				t.Errorf("Unexpected whiteout file name: %s", header.Name)
			}
		}
	}

	// Verify all deletions created whiteout files
	expectedWhiteouts := 3
	if len(whiteoutFiles) != expectedWhiteouts {
		t.Errorf("Expected %d whiteout files, found %d", expectedWhiteouts, len(whiteoutFiles))
	}
}

// TestOCIEmptyLayerCompliance tests empty layer compliance
func TestOCIEmptyLayerCompliance(t *testing.T) {
	config := LayerConfig{
		Compression: CompressionGzip,
		SkipEmpty:   true,
	}

	lm := NewLayerManager(config)

	// Create empty layer
	layer, err := lm.CreateLayer([]FileChange{})
	if err != nil {
		t.Fatalf("CreateLayer failed: %v", err)
	}

	// Verify empty layer properties
	if !layer.EmptyLayer {
		t.Error("Expected empty layer flag to be true")
	}

	if layer.Size != 0 {
		t.Errorf("Expected empty layer size to be 0, got %d", layer.Size)
	}

	if layer.Digest != "" {
		t.Errorf("Expected empty layer digest to be empty, got %s", layer.Digest)
	}

	// Validate empty layer
	if err := lm.ValidateLayer(layer); err != nil {
		t.Errorf("Empty layer validation failed: %v", err)
	}
}

// TestOCIMediaTypeCompliance tests all supported media types
func TestOCIMediaTypeCompliance(t *testing.T) {
	testCases := []struct {
		compression CompressionType
		mediaType   string
	}{
		{CompressionNone, MediaTypeImageLayer},
		{CompressionGzip, MediaTypeImageLayerGzip},
		{CompressionZstd, MediaTypeImageLayerZstd},
	}

	for _, tc := range testCases {
		t.Run(string(tc.compression), func(t *testing.T) {
			config := LayerConfig{
				Compression: tc.compression,
				SkipEmpty:   false,
			}

			lm := NewLayerManager(config)

			changes := []FileChange{
				{
					Path:      "/test.txt",
					Type:      ChangeTypeAdd,
					Mode:      0644,
					Size:      5,
					Content:   strings.NewReader("test\n"),
					Timestamp: time.Unix(1234567890, 0),
				},
			}

			layer, err := lm.CreateLayer(changes)
			if err != nil {
				t.Fatalf("CreateLayer failed: %v", err)
			}

			// Verify media type
			if layer.MediaType != tc.mediaType {
				t.Errorf("Expected media type %s, got %s", tc.mediaType, layer.MediaType)
			}

			// Verify the layer can be validated
			if err := lm.ValidateLayer(layer); err != nil {
				t.Errorf("Layer validation failed: %v", err)
			}
		})
	}
}

// TestOCIDigestValidation tests digest validation according to OCI spec
func TestOCIDigestValidation(t *testing.T) {
	testCases := []struct {
		digest  string
		valid   bool
		name    string
	}{
		{
			digest: "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			valid:  true,
			name:   "valid SHA256 digest",
		},
		{
			digest: "sha256:invalid",
			valid:  false,
			name:   "invalid SHA256 digest (too short)",
		},
		{
			digest: "md5:1234567890abcdef1234567890abcdef",
			valid:  false,
			name:   "non-SHA256 digest",
		},
		{
			digest: "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			valid:  false,
			name:   "missing algorithm prefix",
		},
		{
			digest: "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdefg",
			valid:  false,
			name:   "invalid hex character",
		},
		{
			digest: "",
			valid:  false,
			name:   "empty digest",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateDigest(tc.digest)
			if tc.valid && err != nil {
				t.Errorf("Expected digest %s to be valid, got error: %v", tc.digest, err)
			}
			if !tc.valid && err == nil {
				t.Errorf("Expected digest %s to be invalid, but validation passed", tc.digest)
			}
		})
	}
}

// TestOCILayerExtraction tests that extracted layers maintain OCI compliance
func TestOCILayerExtraction(t *testing.T) {
	config := LayerConfig{
		Compression: CompressionGzip,
		SkipEmpty:   false,
	}

	lm := NewLayerManager(config)

	// Create a layer with various file types
	changes := []FileChange{
		{
			Path:      "/bin/executable",
			Type:      ChangeTypeAdd,
			Mode:      0755,
			Size:      100,
			Content:   strings.NewReader(strings.Repeat("x", 100)),
			Timestamp: time.Unix(1234567890, 0),
		},
		{
			Path:      "/etc/config",
			Type:      ChangeTypeAdd,
			Mode:      0644,
			Size:      19, // Actual size of "configuration data\n"
			Content:   strings.NewReader("configuration data\n"),
			Timestamp: time.Unix(1234567890, 0),
		},
		{
			Path:      "/var/lib/data",
			Type:      ChangeTypeAdd,
			Mode:      os.ModeDir | 0755, // Directory
			Size:      0,
			Timestamp: time.Unix(1234567890, 0),
		},
	}

	layer, err := lm.CreateLayer(changes)
	if err != nil {
		t.Fatalf("CreateLayer failed: %v", err)
	}

	// Extract to temporary directory
	tempDir := t.TempDir()
	if err := lm.ExtractLayer(layer, tempDir); err != nil {
		t.Fatalf("ExtractLayer failed: %v", err)
	}

	// Verify extracted files maintain their properties
	testCases := []struct {
		path         string
		expectedMode os.FileMode
		expectedSize int64
		isDir        bool
	}{
		{"/bin/executable", 0755, 100, false},
		{"/etc/config", 0644, 19, false},
		{"/var/lib/data", 0755, 0, true},
	}

	for _, tc := range testCases {
		fullPath := tempDir + tc.path
		stat, err := os.Stat(fullPath)
		if err != nil {
			t.Errorf("Failed to stat extracted file %s: %v", tc.path, err)
			continue
		}

		if tc.isDir != stat.IsDir() {
			t.Errorf("File %s: expected isDir=%v, got %v", tc.path, tc.isDir, stat.IsDir())
		}

		if !tc.isDir && stat.Size() != tc.expectedSize {
			t.Errorf("File %s: expected size %d, got %d", tc.path, tc.expectedSize, stat.Size())
		}

		// Note: File mode comparison is tricky due to OS differences
		// We'll just check that executable bit is preserved for executables
		if tc.path == "/bin/executable" && stat.Mode()&0111 == 0 {
			t.Errorf("Executable file %s lost execute permissions", tc.path)
		}
	}
}