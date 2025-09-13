package manifest

import (
	"testing"
	"time"

	"github.com/bibin-skaria/ossb/layers"
)

func TestValidateImageManifest(t *testing.T) {
	generator := NewGenerator(nil)

	validManifest := &ImageManifest{
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
		errType  ErrorType
	}{
		{
			name:     "nil manifest",
			manifest: nil,
			wantErr:  true,
			errType:  ErrorTypeValidation,
		},
		{
			name:     "valid manifest",
			manifest: validManifest,
			wantErr:  false,
		},
		{
			name: "invalid schema version",
			manifest: &ImageManifest{
				SchemaVersion: 1,
				MediaType:     MediaTypeOCIManifest,
				Config:        validManifest.Config,
				Layers:        validManifest.Layers,
			},
			wantErr: true,
			errType: ErrorTypeValidation,
		},
		{
			name: "invalid media type",
			manifest: &ImageManifest{
				SchemaVersion: 2,
				MediaType:     "invalid/media-type",
				Config:        validManifest.Config,
				Layers:        validManifest.Layers,
			},
			wantErr: true,
			errType: ErrorTypeValidation,
		},
		{
			name: "invalid config media type",
			manifest: &ImageManifest{
				SchemaVersion: 2,
				MediaType:     MediaTypeOCIManifest,
				Config: Descriptor{
					MediaType: "invalid/config-type",
					Size:      1024,
					Digest:    "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
				},
				Layers: validManifest.Layers,
			},
			wantErr: true,
			errType: ErrorTypeValidation,
		},
		{
			name: "invalid config digest",
			manifest: &ImageManifest{
				SchemaVersion: 2,
				MediaType:     MediaTypeOCIManifest,
				Config: Descriptor{
					MediaType: MediaTypeOCIConfig,
					Size:      1024,
					Digest:    "invalid-digest",
				},
				Layers: validManifest.Layers,
			},
			wantErr: true,
			errType: ErrorTypeValidation,
		},
		{
			name: "negative config size",
			manifest: &ImageManifest{
				SchemaVersion: 2,
				MediaType:     MediaTypeOCIManifest,
				Config: Descriptor{
					MediaType: MediaTypeOCIConfig,
					Size:      -1,
					Digest:    "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
				},
				Layers: validManifest.Layers,
			},
			wantErr: true,
			errType: ErrorTypeValidation,
		},
		{
			name: "no layers",
			manifest: &ImageManifest{
				SchemaVersion: 2,
				MediaType:     MediaTypeOCIManifest,
				Config:        validManifest.Config,
				Layers:        []Descriptor{},
			},
			wantErr: true,
			errType: ErrorTypeValidation,
		},
		{
			name: "invalid layer media type",
			manifest: &ImageManifest{
				SchemaVersion: 2,
				MediaType:     MediaTypeOCIManifest,
				Config:        validManifest.Config,
				Layers: []Descriptor{
					{
						MediaType: "invalid/layer-type",
						Size:      2048,
						Digest:    "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
					},
				},
			},
			wantErr: true,
			errType: ErrorTypeValidation,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := generator.ValidateImageManifest(tt.manifest)
			
			if tt.wantErr {
				if err == nil {
					t.Errorf("ValidateImageManifest() error = nil, wantErr %v", tt.wantErr)
					return
				}
				
				if manifestErr, ok := err.(*ManifestError); ok {
					if manifestErr.Type != tt.errType {
						t.Errorf("Error type = %v, want %v", manifestErr.Type, tt.errType)
					}
				} else {
					t.Errorf("Expected ManifestError, got %T", err)
				}
			} else {
				if err != nil {
					t.Errorf("ValidateImageManifest() error = %v, wantErr %v", err, tt.wantErr)
				}
			}
		})
	}
}

func TestValidateManifestList(t *testing.T) {
	generator := NewGenerator(nil)

	validManifestList := &ManifestList{
		SchemaVersion: 2,
		MediaType:     MediaTypeOCIIndex,
		Manifests: []PlatformManifest{
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
		},
	}

	tests := []struct {
		name         string
		manifestList *ManifestList
		wantErr      bool
		errType      ErrorType
	}{
		{
			name:         "nil manifest list",
			manifestList: nil,
			wantErr:      true,
			errType:      ErrorTypeValidation,
		},
		{
			name:         "valid manifest list",
			manifestList: validManifestList,
			wantErr:      false,
		},
		{
			name: "invalid schema version",
			manifestList: &ManifestList{
				SchemaVersion: 1,
				MediaType:     MediaTypeOCIIndex,
				Manifests:     validManifestList.Manifests,
			},
			wantErr: true,
			errType: ErrorTypeValidation,
		},
		{
			name: "invalid media type",
			manifestList: &ManifestList{
				SchemaVersion: 2,
				MediaType:     "invalid/media-type",
				Manifests:     validManifestList.Manifests,
			},
			wantErr: true,
			errType: ErrorTypeValidation,
		},
		{
			name: "no manifests",
			manifestList: &ManifestList{
				SchemaVersion: 2,
				MediaType:     MediaTypeOCIIndex,
				Manifests:     []PlatformManifest{},
			},
			wantErr: true,
			errType: ErrorTypeValidation,
		},
		{
			name: "duplicate platforms",
			manifestList: &ManifestList{
				SchemaVersion: 2,
				MediaType:     MediaTypeOCIIndex,
				Manifests: []PlatformManifest{
					validManifestList.Manifests[0],
					validManifestList.Manifests[0], // Duplicate
				},
			},
			wantErr: true,
			errType: ErrorTypeValidation,
		},
		{
			name: "invalid platform manifest",
			manifestList: &ManifestList{
				SchemaVersion: 2,
				MediaType:     MediaTypeOCIIndex,
				Manifests: []PlatformManifest{
					{
						MediaType: "invalid/media-type",
						Size:      1024,
						Digest:    "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
						Platform: Platform{
							Architecture: "amd64",
							OS:           "linux",
						},
					},
				},
			},
			wantErr: true,
			errType: ErrorTypeValidation,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := generator.ValidateManifestList(tt.manifestList)
			
			if tt.wantErr {
				if err == nil {
					t.Errorf("ValidateManifestList() error = nil, wantErr %v", tt.wantErr)
					return
				}
				
				if manifestErr, ok := err.(*ManifestError); ok {
					if manifestErr.Type != tt.errType {
						t.Errorf("Error type = %v, want %v", manifestErr.Type, tt.errType)
					}
				} else {
					t.Errorf("Expected ManifestError, got %T", err)
				}
			} else {
				if err != nil {
					t.Errorf("ValidateManifestList() error = %v, wantErr %v", err, tt.wantErr)
				}
			}
		})
	}
}

func TestValidateImageConfig(t *testing.T) {
	generator := NewGenerator(nil)

	validConfig := &ImageConfig{
		Created:      time.Now().UTC().Format(time.RFC3339Nano),
		Architecture: "amd64",
		OS:           "linux",
		Config: ContainerConfig{
			Env:          []string{"PATH=/usr/bin:/bin"},
			ExposedPorts: map[string]struct{}{"80/tcp": {}},
			WorkingDir:   "/app",
		},
		RootFS: RootFS{
			Type:    "layers",
			DiffIDs: []string{"sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"},
		},
		History: []HistoryEntry{
			{
				Created:   time.Now().UTC().Format(time.RFC3339Nano),
				CreatedBy: "FROM alpine:latest",
			},
		},
	}

	tests := []struct {
		name    string
		config  *ImageConfig
		wantErr bool
		errType ErrorType
	}{
		{
			name:    "nil config",
			config:  nil,
			wantErr: true,
			errType: ErrorTypeValidation,
		},
		{
			name:    "valid config",
			config:  validConfig,
			wantErr: false,
		},
		{
			name: "empty architecture",
			config: &ImageConfig{
				Created:      validConfig.Created,
				Architecture: "",
				OS:           "linux",
				Config:       validConfig.Config,
				RootFS:       validConfig.RootFS,
			},
			wantErr: true,
			errType: ErrorTypeValidation,
		},
		{
			name: "empty OS",
			config: &ImageConfig{
				Created:      validConfig.Created,
				Architecture: "amd64",
				OS:           "",
				Config:       validConfig.Config,
				RootFS:       validConfig.RootFS,
			},
			wantErr: true,
			errType: ErrorTypeValidation,
		},
		{
			name: "invalid created timestamp",
			config: &ImageConfig{
				Created:      "invalid-timestamp",
				Architecture: "amd64",
				OS:           "linux",
				Config:       validConfig.Config,
				RootFS:       validConfig.RootFS,
			},
			wantErr: true,
			errType: ErrorTypeValidation,
		},
		{
			name: "invalid rootfs type",
			config: &ImageConfig{
				Created:      validConfig.Created,
				Architecture: "amd64",
				OS:           "linux",
				Config:       validConfig.Config,
				RootFS: RootFS{
					Type:    "invalid",
					DiffIDs: validConfig.RootFS.DiffIDs,
				},
			},
			wantErr: true,
			errType: ErrorTypeValidation,
		},
		{
			name: "invalid diff ID",
			config: &ImageConfig{
				Created:      validConfig.Created,
				Architecture: "amd64",
				OS:           "linux",
				Config:       validConfig.Config,
				RootFS: RootFS{
					Type:    "layers",
					DiffIDs: []string{"invalid-digest"},
				},
			},
			wantErr: true,
			errType: ErrorTypeValidation,
		},
		{
			name: "invalid exposed port",
			config: &ImageConfig{
				Created:      validConfig.Created,
				Architecture: "amd64",
				OS:           "linux",
				Config: ContainerConfig{
					ExposedPorts: map[string]struct{}{"invalid-port": {}},
				},
				RootFS: validConfig.RootFS,
			},
			wantErr: true,
			errType: ErrorTypeValidation,
		},
		{
			name: "invalid environment variable",
			config: &ImageConfig{
				Created:      validConfig.Created,
				Architecture: "amd64",
				OS:           "linux",
				Config: ContainerConfig{
					Env: []string{"INVALID_ENV_VAR"},
				},
				RootFS: validConfig.RootFS,
			},
			wantErr: true,
			errType: ErrorTypeValidation,
		},
		{
			name: "invalid working directory",
			config: &ImageConfig{
				Created:      validConfig.Created,
				Architecture: "amd64",
				OS:           "linux",
				Config: ContainerConfig{
					WorkingDir: "relative/path",
				},
				RootFS: validConfig.RootFS,
			},
			wantErr: true,
			errType: ErrorTypeValidation,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := generator.ValidateImageConfig(tt.config)
			
			if tt.wantErr {
				if err == nil {
					t.Errorf("ValidateImageConfig() error = nil, wantErr %v", tt.wantErr)
					return
				}
				
				if manifestErr, ok := err.(*ManifestError); ok {
					if manifestErr.Type != tt.errType {
						t.Errorf("Error type = %v, want %v", manifestErr.Type, tt.errType)
					}
				} else {
					t.Errorf("Expected ManifestError, got %T", err)
				}
			} else {
				if err != nil {
					t.Errorf("ValidateImageConfig() error = %v, wantErr %v", err, tt.wantErr)
				}
			}
		})
	}
}

func TestValidatePlatform(t *testing.T) {
	generator := NewGenerator(nil)

	tests := []struct {
		name     string
		platform *Platform
		wantErr  bool
		errType  ErrorType
	}{
		{
			name:     "nil platform",
			platform: nil,
			wantErr:  true,
			errType:  ErrorTypeValidation,
		},
		{
			name: "valid platform",
			platform: &Platform{
				Architecture: "amd64",
				OS:           "linux",
			},
			wantErr: false,
		},
		{
			name: "valid platform with variant",
			platform: &Platform{
				Architecture: "arm",
				OS:           "linux",
				Variant:      "v7",
			},
			wantErr: false,
		},
		{
			name: "empty architecture",
			platform: &Platform{
				Architecture: "",
				OS:           "linux",
			},
			wantErr: true,
			errType: ErrorTypeValidation,
		},
		{
			name: "empty OS",
			platform: &Platform{
				Architecture: "amd64",
				OS:           "",
			},
			wantErr: true,
			errType: ErrorTypeValidation,
		},
		{
			name: "unsupported architecture",
			platform: &Platform{
				Architecture: "unsupported",
				OS:           "linux",
			},
			wantErr: true,
			errType: ErrorTypeValidation,
		},
		{
			name: "unsupported OS",
			platform: &Platform{
				Architecture: "amd64",
				OS:           "unsupported",
			},
			wantErr: true,
			errType: ErrorTypeValidation,
		},
		{
			name: "invalid ARM variant",
			platform: &Platform{
				Architecture: "arm",
				OS:           "linux",
				Variant:      "v9",
			},
			wantErr: true,
			errType: ErrorTypeValidation,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := generator.validatePlatform(tt.platform)
			
			if tt.wantErr {
				if err == nil {
					t.Errorf("validatePlatform() error = nil, wantErr %v", tt.wantErr)
					return
				}
				
				if manifestErr, ok := err.(*ManifestError); ok {
					if manifestErr.Type != tt.errType {
						t.Errorf("Error type = %v, want %v", manifestErr.Type, tt.errType)
					}
				} else {
					t.Errorf("Expected ManifestError, got %T", err)
				}
			} else {
				if err != nil {
					t.Errorf("validatePlatform() error = %v, wantErr %v", err, tt.wantErr)
				}
			}
		})
	}
}

func TestValidateDigest(t *testing.T) {
	generator := NewGenerator(nil)

	tests := []struct {
		name    string
		digest  string
		context string
		wantErr bool
	}{
		{
			name:    "empty digest",
			digest:  "",
			context: "test",
			wantErr: true,
		},
		{
			name:    "valid digest",
			digest:  "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			context: "test",
			wantErr: false,
		},
		{
			name:    "invalid digest format - no algorithm",
			digest:  "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			context: "test",
			wantErr: true,
		},
		{
			name:    "invalid digest format - wrong algorithm",
			digest:  "md5:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			context: "test",
			wantErr: true,
		},
		{
			name:    "invalid digest format - too short",
			digest:  "sha256:1234567890abcdef",
			context: "test",
			wantErr: true,
		},
		{
			name:    "invalid digest format - too long",
			digest:  "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef00",
			context: "test",
			wantErr: true,
		},
		{
			name:    "invalid digest format - invalid hex",
			digest:  "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdefg",
			context: "test",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := generator.validateDigest(tt.digest, tt.context)
			
			if tt.wantErr {
				if err == nil {
					t.Errorf("validateDigest() error = nil, wantErr %v", tt.wantErr)
				}
			} else {
				if err != nil {
					t.Errorf("validateDigest() error = %v, wantErr %v", err, tt.wantErr)
				}
			}
		})
	}
}

func TestIsValidPortSpec(t *testing.T) {
	tests := []struct {
		name     string
		portSpec string
		want     bool
	}{
		{
			name:     "valid tcp port",
			portSpec: "80/tcp",
			want:     true,
		},
		{
			name:     "valid udp port",
			portSpec: "53/udp",
			want:     true,
		},
		{
			name:     "valid sctp port",
			portSpec: "9999/sctp",
			want:     true,
		},
		{
			name:     "invalid - no protocol",
			portSpec: "80",
			want:     false,
		},
		{
			name:     "invalid - empty port",
			portSpec: "/tcp",
			want:     false,
		},
		{
			name:     "invalid - unsupported protocol",
			portSpec: "80/http",
			want:     false,
		},
		{
			name:     "invalid - multiple slashes",
			portSpec: "80/tcp/extra",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidPortSpec(tt.portSpec)
			if got != tt.want {
				t.Errorf("isValidPortSpec() = %v, want %v", got, tt.want)
			}
		})
	}
}