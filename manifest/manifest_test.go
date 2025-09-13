package manifest

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/bibin-skaria/ossb/internal/types"
	"github.com/bibin-skaria/ossb/layers"
)

func TestNewGenerator(t *testing.T) {
	tests := []struct {
		name    string
		options *GeneratorOptions
		want    *GeneratorOptions
	}{
		{
			name:    "nil options uses defaults",
			options: nil,
			want:    DefaultGeneratorOptions(),
		},
		{
			name: "custom options preserved",
			options: &GeneratorOptions{
				DefaultUser:       "app",
				DefaultWorkingDir: "/app",
				DefaultShell:      []string{"/bin/bash", "-c"},
				IncludeHistory:    false,
			},
			want: &GeneratorOptions{
				DefaultUser:       "app",
				DefaultWorkingDir: "/app",
				DefaultShell:      []string{"/bin/bash", "-c"},
				IncludeHistory:    false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			generator := NewGenerator(tt.options)
			if generator == nil {
				t.Fatal("NewGenerator returned nil")
			}
			
			if generator.options.DefaultUser != tt.want.DefaultUser {
				t.Errorf("DefaultUser = %v, want %v", generator.options.DefaultUser, tt.want.DefaultUser)
			}
			
			if generator.options.DefaultWorkingDir != tt.want.DefaultWorkingDir {
				t.Errorf("DefaultWorkingDir = %v, want %v", generator.options.DefaultWorkingDir, tt.want.DefaultWorkingDir)
			}
			
			if generator.options.IncludeHistory != tt.want.IncludeHistory {
				t.Errorf("IncludeHistory = %v, want %v", generator.options.IncludeHistory, tt.want.IncludeHistory)
			}
		})
	}
}

func TestGenerateImageConfig(t *testing.T) {
	generator := NewGenerator(nil)
	platform := types.Platform{
		OS:           "linux",
		Architecture: "amd64",
	}

	tests := []struct {
		name         string
		instructions []types.DockerfileInstruction
		wantErr      bool
		validate     func(*testing.T, *ImageConfig)
	}{
		{
			name:         "empty instructions",
			instructions: []types.DockerfileInstruction{},
			wantErr:      true,
		},
		{
			name: "basic FROM instruction",
			instructions: []types.DockerfileInstruction{
				{Command: "FROM", Value: "alpine:latest", Line: 1},
			},
			wantErr: false,
			validate: func(t *testing.T, config *ImageConfig) {
				if config.Architecture != "amd64" {
					t.Errorf("Architecture = %v, want amd64", config.Architecture)
				}
				if config.OS != "linux" {
					t.Errorf("OS = %v, want linux", config.OS)
				}
				if len(config.History) != 1 {
					t.Errorf("History length = %v, want 1", len(config.History))
				}
			},
		},
		{
			name: "ENV instruction",
			instructions: []types.DockerfileInstruction{
				{Command: "FROM", Value: "alpine:latest", Line: 1},
				{Command: "ENV", Value: "PATH=/usr/local/bin:$PATH", Line: 2},
			},
			wantErr: false,
			validate: func(t *testing.T, config *ImageConfig) {
				found := false
				for _, env := range config.Config.Env {
					if env == "PATH=/usr/local/bin:$PATH" {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("ENV PATH not found in config.Env: %v", config.Config.Env)
				}
			},
		},
		{
			name: "EXPOSE instruction",
			instructions: []types.DockerfileInstruction{
				{Command: "FROM", Value: "alpine:latest", Line: 1},
				{Command: "EXPOSE", Value: "80 443/tcp", Line: 2},
			},
			wantErr: false,
			validate: func(t *testing.T, config *ImageConfig) {
				if _, exists := config.Config.ExposedPorts["80/tcp"]; !exists {
					t.Errorf("Port 80/tcp not found in ExposedPorts")
				}
				if _, exists := config.Config.ExposedPorts["443/tcp"]; !exists {
					t.Errorf("Port 443/tcp not found in ExposedPorts")
				}
			},
		},
		{
			name: "WORKDIR instruction",
			instructions: []types.DockerfileInstruction{
				{Command: "FROM", Value: "alpine:latest", Line: 1},
				{Command: "WORKDIR", Value: "/app", Line: 2},
			},
			wantErr: false,
			validate: func(t *testing.T, config *ImageConfig) {
				if config.Config.WorkingDir != "/app" {
					t.Errorf("WorkingDir = %v, want /app", config.Config.WorkingDir)
				}
			},
		},
		{
			name: "CMD instruction",
			instructions: []types.DockerfileInstruction{
				{Command: "FROM", Value: "alpine:latest", Line: 1},
				{Command: "CMD", Value: `["echo", "hello"]`, Line: 2},
			},
			wantErr: false,
			validate: func(t *testing.T, config *ImageConfig) {
				expected := []string{"echo", "hello"}
				if len(config.Config.Cmd) != len(expected) {
					t.Errorf("Cmd length = %v, want %v", len(config.Config.Cmd), len(expected))
					return
				}
				for i, cmd := range expected {
					if config.Config.Cmd[i] != cmd {
						t.Errorf("Cmd[%d] = %v, want %v", i, config.Config.Cmd[i], cmd)
					}
				}
			},
		},
		{
			name: "LABEL instruction",
			instructions: []types.DockerfileInstruction{
				{Command: "FROM", Value: "alpine:latest", Line: 1},
				{Command: "LABEL", Value: `version="1.0" maintainer="test@example.com"`, Line: 2},
			},
			wantErr: false,
			validate: func(t *testing.T, config *ImageConfig) {
				if config.Config.Labels["version"] != "1.0" {
					t.Errorf("Label version = %v, want 1.0", config.Config.Labels["version"])
				}
				if config.Config.Labels["maintainer"] != "test@example.com" {
					t.Errorf("Label maintainer = %v, want test@example.com", config.Config.Labels["maintainer"])
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := generator.GenerateImageConfig(tt.instructions, platform)
			
			if tt.wantErr {
				if err == nil {
					t.Errorf("GenerateImageConfig() error = nil, wantErr %v", tt.wantErr)
				}
				return
			}
			
			if err != nil {
				t.Errorf("GenerateImageConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			
			if config == nil {
				t.Fatal("GenerateImageConfig() returned nil config")
			}
			
			if tt.validate != nil {
				tt.validate(t, config)
			}
		})
	}
}

func TestGenerateImageManifest(t *testing.T) {
	generator := NewGenerator(nil)
	
	// Create a sample config
	config := &ImageConfig{
		Created:      time.Now().UTC().Format(time.RFC3339Nano),
		Architecture: "amd64",
		OS:           "linux",
		Config: ContainerConfig{
			Env: []string{"PATH=/usr/local/bin:/usr/bin:/bin"},
		},
		RootFS: RootFS{
			Type:    "layers",
			DiffIDs: []string{},
		},
	}
	
	// Create sample layers
	testLayers := []*layers.Layer{
		{
			Digest:    "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			Size:      1024,
			MediaType: layers.MediaTypeImageLayerGzip,
		},
		{
			Digest:    "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			Size:      2048,
			MediaType: layers.MediaTypeImageLayerGzip,
		},
	}

	tests := []struct {
		name    string
		config  *ImageConfig
		layers  []*layers.Layer
		wantErr bool
		validate func(*testing.T, *ImageManifest)
	}{
		{
			name:    "nil config",
			config:  nil,
			layers:  testLayers,
			wantErr: true,
		},
		{
			name:    "valid config and layers",
			config:  config,
			layers:  testLayers,
			wantErr: false,
			validate: func(t *testing.T, manifest *ImageManifest) {
				if manifest.SchemaVersion != 2 {
					t.Errorf("SchemaVersion = %v, want 2", manifest.SchemaVersion)
				}
				if manifest.MediaType != MediaTypeOCIManifest {
					t.Errorf("MediaType = %v, want %v", manifest.MediaType, MediaTypeOCIManifest)
				}
				if len(manifest.Layers) != 2 {
					t.Errorf("Layers length = %v, want 2", len(manifest.Layers))
				}
				if manifest.Config.MediaType != MediaTypeOCIConfig {
					t.Errorf("Config MediaType = %v, want %v", manifest.Config.MediaType, MediaTypeOCIConfig)
				}
			},
		},
		{
			name:   "nil layer",
			config: config,
			layers: []*layers.Layer{
				testLayers[0],
				nil,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manifest, err := generator.GenerateImageManifest(tt.config, tt.layers)
			
			if tt.wantErr {
				if err == nil {
					t.Errorf("GenerateImageManifest() error = nil, wantErr %v", tt.wantErr)
				}
				return
			}
			
			if err != nil {
				t.Errorf("GenerateImageManifest() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			
			if manifest == nil {
				t.Fatal("GenerateImageManifest() returned nil manifest")
			}
			
			if tt.validate != nil {
				tt.validate(t, manifest)
			}
		})
	}
}

func TestGenerateManifestList(t *testing.T) {
	generator := NewGenerator(nil)
	
	// Create sample platform manifests
	manifests := []PlatformManifest{
		{
			MediaType: MediaTypeOCIManifest,
			Size:      1024,
			Digest:    "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			Platform: Platform{
				Architecture: "amd64",
				OS:           "linux",
			},
		},
		{
			MediaType: MediaTypeOCIManifest,
			Size:      1536,
			Digest:    "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			Platform: Platform{
				Architecture: "arm64",
				OS:           "linux",
			},
		},
	}

	tests := []struct {
		name      string
		manifests []PlatformManifest
		wantErr   bool
		validate  func(*testing.T, *ManifestList)
	}{
		{
			name:      "empty manifests",
			manifests: []PlatformManifest{},
			wantErr:   true,
		},
		{
			name:      "valid manifests",
			manifests: manifests,
			wantErr:   false,
			validate: func(t *testing.T, manifestList *ManifestList) {
				if manifestList.SchemaVersion != 2 {
					t.Errorf("SchemaVersion = %v, want 2", manifestList.SchemaVersion)
				}
				if manifestList.MediaType != MediaTypeOCIIndex {
					t.Errorf("MediaType = %v, want %v", manifestList.MediaType, MediaTypeOCIIndex)
				}
				if len(manifestList.Manifests) != 2 {
					t.Errorf("Manifests length = %v, want 2", len(manifestList.Manifests))
				}
				
				// Check that manifests are sorted by platform
				if manifestList.Manifests[0].Platform.Architecture != "amd64" {
					t.Errorf("First manifest architecture = %v, want amd64", manifestList.Manifests[0].Platform.Architecture)
				}
				if manifestList.Manifests[1].Platform.Architecture != "arm64" {
					t.Errorf("Second manifest architecture = %v, want arm64", manifestList.Manifests[1].Platform.Architecture)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manifestList, err := generator.GenerateManifestList(tt.manifests)
			
			if tt.wantErr {
				if err == nil {
					t.Errorf("GenerateManifestList() error = nil, wantErr %v", tt.wantErr)
				}
				return
			}
			
			if err != nil {
				t.Errorf("GenerateManifestList() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			
			if manifestList == nil {
				t.Fatal("GenerateManifestList() returned nil manifest list")
			}
			
			if tt.validate != nil {
				tt.validate(t, manifestList)
			}
		})
	}
}

func TestCalculateManifestDigest(t *testing.T) {
	generator := NewGenerator(nil)
	
	manifest := &ImageManifest{
		SchemaVersion: 2,
		MediaType:     MediaTypeOCIManifest,
		Config: Descriptor{
			MediaType: MediaTypeOCIConfig,
			Size:      1024,
			Digest:    "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
		},
		Layers: []Descriptor{
			{
				MediaType: layers.MediaTypeImageLayerGzip,
				Size:      2048,
				Digest:    "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			},
		},
	}

	tests := []struct {
		name     string
		manifest *ImageManifest
		wantErr  bool
	}{
		{
			name:     "nil manifest",
			manifest: nil,
			wantErr:  true,
		},
		{
			name:     "valid manifest",
			manifest: manifest,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			digest, err := generator.CalculateManifestDigest(tt.manifest)
			
			if tt.wantErr {
				if err == nil {
					t.Errorf("CalculateManifestDigest() error = nil, wantErr %v", tt.wantErr)
				}
				return
			}
			
			if err != nil {
				t.Errorf("CalculateManifestDigest() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			
			if !strings.HasPrefix(digest, "sha256:") {
				t.Errorf("Digest does not start with sha256:, got %v", digest)
			}
			
			if len(digest) != 71 { // "sha256:" + 64 hex chars
				t.Errorf("Digest length = %v, want 71", len(digest))
			}
		})
	}
}

func TestAddLayerToConfig(t *testing.T) {
	generator := NewGenerator(nil)
	
	config := &ImageConfig{
		Created:      time.Now().UTC().Format(time.RFC3339Nano),
		Architecture: "amd64",
		OS:           "linux",
		Config:       ContainerConfig{},
		RootFS: RootFS{
			Type:    "layers",
			DiffIDs: []string{},
		},
		History: []HistoryEntry{},
	}
	
	layer := &layers.Layer{
		Digest:    "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
		Size:      1024,
		MediaType: layers.MediaTypeImageLayerGzip,
		CreatedBy: "RUN echo hello",
		Comment:   "test layer",
	}

	tests := []struct {
		name    string
		config  *ImageConfig
		layer   *layers.Layer
		wantErr bool
		validate func(*testing.T, *ImageConfig)
	}{
		{
			name:    "nil config",
			config:  nil,
			layer:   layer,
			wantErr: true,
		},
		{
			name:    "nil layer",
			config:  config,
			layer:   nil,
			wantErr: true,
		},
		{
			name:    "valid config and layer",
			config:  config,
			layer:   layer,
			wantErr: false,
			validate: func(t *testing.T, config *ImageConfig) {
				if len(config.RootFS.DiffIDs) != 1 {
					t.Errorf("DiffIDs length = %v, want 1", len(config.RootFS.DiffIDs))
				}
				if config.RootFS.DiffIDs[0] != layer.Digest {
					t.Errorf("DiffID = %v, want %v", config.RootFS.DiffIDs[0], layer.Digest)
				}
				if len(config.History) != 1 {
					t.Errorf("History length = %v, want 1", len(config.History))
				}
				if config.History[0].CreatedBy != layer.CreatedBy {
					t.Errorf("History CreatedBy = %v, want %v", config.History[0].CreatedBy, layer.CreatedBy)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := generator.AddLayerToConfig(tt.config, tt.layer)
			
			if tt.wantErr {
				if err == nil {
					t.Errorf("AddLayerToConfig() error = nil, wantErr %v", tt.wantErr)
				}
				return
			}
			
			if err != nil {
				t.Errorf("AddLayerToConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			
			if tt.validate != nil {
				tt.validate(t, tt.config)
			}
		})
	}
}

func TestSerializeManifest(t *testing.T) {
	generator := NewGenerator(nil)
	
	manifest := &ImageManifest{
		SchemaVersion: 2,
		MediaType:     MediaTypeOCIManifest,
		Config: Descriptor{
			MediaType: MediaTypeOCIConfig,
			Size:      1024,
			Digest:    "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
		},
		Layers: []Descriptor{
			{
				MediaType: layers.MediaTypeImageLayerGzip,
				Size:      2048,
				Digest:    "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			},
		},
	}

	tests := []struct {
		name     string
		manifest *ImageManifest
		wantErr  bool
	}{
		{
			name:     "nil manifest",
			manifest: nil,
			wantErr:  true,
		},
		{
			name:     "valid manifest",
			manifest: manifest,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := generator.SerializeManifest(tt.manifest)
			
			if tt.wantErr {
				if err == nil {
					t.Errorf("SerializeManifest() error = nil, wantErr %v", tt.wantErr)
				}
				return
			}
			
			if err != nil {
				t.Errorf("SerializeManifest() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			
			if len(data) == 0 {
				t.Error("SerializeManifest() returned empty data")
			}
			
			// Verify it's valid JSON
			var parsed ImageManifest
			if err := json.Unmarshal(data, &parsed); err != nil {
				t.Errorf("SerializeManifest() produced invalid JSON: %v", err)
			}
		})
	}
}

func TestCreatePlatformManifest(t *testing.T) {
	generator := NewGenerator(nil)
	
	manifest := &ImageManifest{
		SchemaVersion: 2,
		MediaType:     MediaTypeOCIManifest,
		Config: Descriptor{
			MediaType: MediaTypeOCIConfig,
			Size:      1024,
			Digest:    "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
		},
		Layers: []Descriptor{
			{
				MediaType: layers.MediaTypeImageLayerGzip,
				Size:      2048,
				Digest:    "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			},
		},
	}
	
	platform := types.Platform{
		OS:           "linux",
		Architecture: "amd64",
	}

	tests := []struct {
		name     string
		manifest *ImageManifest
		platform types.Platform
		wantErr  bool
		validate func(*testing.T, *PlatformManifest)
	}{
		{
			name:     "nil manifest",
			manifest: nil,
			platform: platform,
			wantErr:  true,
		},
		{
			name:     "valid manifest and platform",
			manifest: manifest,
			platform: platform,
			wantErr:  false,
			validate: func(t *testing.T, pm *PlatformManifest) {
				if pm.MediaType != manifest.MediaType {
					t.Errorf("MediaType = %v, want %v", pm.MediaType, manifest.MediaType)
				}
				if pm.Platform.OS != platform.OS {
					t.Errorf("Platform.OS = %v, want %v", pm.Platform.OS, platform.OS)
				}
				if pm.Platform.Architecture != platform.Architecture {
					t.Errorf("Platform.Architecture = %v, want %v", pm.Platform.Architecture, platform.Architecture)
				}
				if pm.Size <= 0 {
					t.Errorf("Size = %v, want > 0", pm.Size)
				}
				if !strings.HasPrefix(pm.Digest, "sha256:") {
					t.Errorf("Digest = %v, want sha256: prefix", pm.Digest)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			platformManifest, err := generator.CreatePlatformManifest(tt.manifest, tt.platform)
			
			if tt.wantErr {
				if err == nil {
					t.Errorf("CreatePlatformManifest() error = nil, wantErr %v", tt.wantErr)
				}
				return
			}
			
			if err != nil {
				t.Errorf("CreatePlatformManifest() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			
			if platformManifest == nil {
				t.Fatal("CreatePlatformManifest() returned nil platform manifest")
			}
			
			if tt.validate != nil {
				tt.validate(t, platformManifest)
			}
		})
	}
}