package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRegistryV2Client_BlobExists(t *testing.T) {
	tests := []struct {
		name           string
		digest         string
		serverResponse func(w http.ResponseWriter, r *http.Request)
		want           bool
		wantErr        bool
	}{
		{
			name:   "blob exists",
			digest: "sha256:abc123",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodHead {
					t.Errorf("Expected HEAD request, got %s", r.Method)
				}
				if !strings.Contains(r.URL.Path, "sha256:abc123") {
					t.Errorf("Expected digest in URL path, got %s", r.URL.Path)
				}
				w.WriteHeader(http.StatusOK)
			},
			want:    true,
			wantErr: false,
		},
		{
			name:   "blob does not exist",
			digest: "sha256:def456",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			want:    false,
			wantErr: false,
		},
		{
			name:   "server error",
			digest: "sha256:error",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			want:    false,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(tt.serverResponse))
			defer server.Close()

			client := NewClient(&ClientOptions{
				Transport: &http.Transport{},
			})
			v2Client := NewRegistryV2Client(client)

			// Parse server URL to get host
			serverURL := strings.TrimPrefix(server.URL, "http://")
			ref := ImageReference{
				Registry:   serverURL,
				Repository: "test/image",
				Tag:        "latest",
			}

			got, err := v2Client.BlobExists(context.Background(), ref, tt.digest)
			if (err != nil) != tt.wantErr {
				t.Errorf("BlobExists() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("BlobExists() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRegistryV2Client_InitiateBlobUpload(t *testing.T) {
	tests := []struct {
		name           string
		serverResponse func(w http.ResponseWriter, r *http.Request)
		wantURL        string
		wantErr        bool
	}{
		{
			name: "successful initiation",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("Expected POST request, got %s", r.Method)
				}
				if !strings.HasSuffix(r.URL.Path, "/blobs/uploads/") {
					t.Errorf("Expected uploads path, got %s", r.URL.Path)
				}
				w.Header().Set("Location", "/v2/test/image/blobs/uploads/uuid-123")
				w.WriteHeader(http.StatusAccepted)
			},
			wantURL: "/v2/test/image/blobs/uploads/uuid-123",
			wantErr: false,
		},
		{
			name: "missing location header",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusAccepted)
			},
			wantURL: "",
			wantErr: true,
		},
		{
			name: "server error",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			wantURL: "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(tt.serverResponse))
			defer server.Close()

			client := NewClient(&ClientOptions{
				Transport: &http.Transport{},
			})
			v2Client := NewRegistryV2Client(client)

			serverURL := strings.TrimPrefix(server.URL, "http://")
			ref := ImageReference{
				Registry:   serverURL,
				Repository: "test/image",
				Tag:        "latest",
			}

			got, err := v2Client.InitiateBlobUpload(context.Background(), ref)
			if (err != nil) != tt.wantErr {
				t.Errorf("InitiateBlobUpload() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.wantURL {
				t.Errorf("InitiateBlobUpload() = %v, want %v", got, tt.wantURL)
			}
		})
	}
}

func TestRegistryV2Client_PushManifest(t *testing.T) {
	tests := []struct {
		name           string
		manifest       *ImageManifest
		mediaType      string
		serverResponse func(w http.ResponseWriter, r *http.Request)
		wantErr        bool
	}{
		{
			name: "successful push",
			manifest: &ImageManifest{
				SchemaVersion: 2,
				MediaType:     MediaTypeOCIManifest,
				Config: Descriptor{
					MediaType: MediaTypeOCIConfig,
					Size:      1234,
					Digest:    "sha256:config123",
				},
				Layers: []Descriptor{
					{
						MediaType: MediaTypeOCILayer,
						Size:      5678,
						Digest:    "sha256:layer123",
					},
				},
			},
			mediaType: MediaTypeOCIManifest,
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPut {
					t.Errorf("Expected PUT request, got %s", r.Method)
				}
				if !strings.Contains(r.URL.Path, "/manifests/") {
					t.Errorf("Expected manifests path, got %s", r.URL.Path)
				}
				
				contentType := r.Header.Get("Content-Type")
				if contentType != MediaTypeOCIManifest {
					t.Errorf("Expected Content-Type %s, got %s", MediaTypeOCIManifest, contentType)
				}

				// Read and validate manifest
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Errorf("Failed to read request body: %v", err)
				}

				var manifest ImageManifest
				if err := json.Unmarshal(body, &manifest); err != nil {
					t.Errorf("Failed to unmarshal manifest: %v", err)
				}

				if manifest.SchemaVersion != 2 {
					t.Errorf("Expected schema version 2, got %d", manifest.SchemaVersion)
				}

				w.WriteHeader(http.StatusCreated)
			},
			wantErr: false,
		},
		{
			name: "server error",
			manifest: &ImageManifest{
				SchemaVersion: 2,
				MediaType:     MediaTypeOCIManifest,
			},
			mediaType: MediaTypeOCIManifest,
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte("Internal server error"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(tt.serverResponse))
			defer server.Close()

			client := NewClient(&ClientOptions{
				Transport: &http.Transport{},
			})
			v2Client := NewRegistryV2Client(client)

			serverURL := strings.TrimPrefix(server.URL, "http://")
			ref := ImageReference{
				Registry:   serverURL,
				Repository: "test/image",
				Tag:        "latest",
			}

			err := v2Client.PushManifest(context.Background(), ref, tt.manifest, tt.mediaType)
			if (err != nil) != tt.wantErr {
				t.Errorf("PushManifest() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestClient_PushImageWithProgress(t *testing.T) {
	// Create a mock server that handles manifest push
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/manifests/") {
			w.WriteHeader(http.StatusCreated)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewClient(&ClientOptions{
		Transport: &http.Transport{},
	})

	serverURL := strings.TrimPrefix(server.URL, "http://")
	ref := ImageReference{
		Registry:   serverURL,
		Repository: "test/image",
		Tag:        "latest",
	}

	manifest := &ImageManifest{
		SchemaVersion: 2,
		MediaType:     MediaTypeOCIManifest,
		Config: Descriptor{
			MediaType: MediaTypeOCIConfig,
			Size:      1234,
			Digest:    "sha256:config123",
		},
		Layers: []Descriptor{
			{
				MediaType: MediaTypeOCILayer,
				Size:      5678,
				Digest:    "sha256:layer123",
			},
		},
	}

	// Test progress callback
	var progressStages []string
	var progressValues []float64
	progressCallback := func(stage string, progress float64) {
		progressStages = append(progressStages, stage)
		progressValues = append(progressValues, progress)
	}

	err := client.PushImageWithProgress(context.Background(), ref, manifest, progressCallback)
	
	// We expect this to fail because we don't have blob content, but we should get progress callbacks
	if err == nil {
		t.Error("Expected error for missing blob content, got nil")
	}

	// Check that we got some progress callbacks
	if len(progressStages) == 0 {
		t.Error("Expected progress callbacks, got none")
	}

	// Check that progress starts at 0
	if len(progressValues) > 0 && progressValues[0] != 0.0 {
		t.Errorf("Expected first progress value to be 0.0, got %f", progressValues[0])
	}
}

func TestClient_PushManifestListWithProgress(t *testing.T) {
	client := NewClient(nil)

	ref := ImageReference{
		Registry:   "example.com",
		Repository: "test/image",
		Tag:        "latest",
	}

	// Test with empty manifest list to trigger validation error
	manifestList := &ManifestList{
		SchemaVersion: 2,
		MediaType:     MediaTypeOCIIndex,
		Manifests:     []PlatformManifest{}, // Empty list
	}

	// Test progress callback
	var progressStages []string
	var progressValues []float64
	progressCallback := func(stage string, progress float64) {
		progressStages = append(progressStages, stage)
		progressValues = append(progressValues, progress)
	}

	err := client.PushManifestListWithProgress(context.Background(), ref, manifestList, progressCallback)
	if err == nil {
		t.Error("Expected error for empty manifest list, got nil")
	}

	// Check that we got at least one progress callback (starting)
	if len(progressStages) == 0 {
		t.Error("Expected at least one progress callback, got none")
	}

	// Check that progress starts at 0
	if len(progressValues) > 0 {
		if progressValues[0] != 0.0 {
			t.Errorf("Expected first progress value to be 0.0, got %f", progressValues[0])
		}
	}
}

func TestProgressReader(t *testing.T) {
	data := []byte("hello world")
	var uploaded, total int64
	
	callback := func(u, t int64) {
		uploaded = u
		total = t
	}

	reader := &progressReader{
		reader:   bytes.NewReader(data),
		total:    int64(len(data)),
		callback: callback,
	}

	// Read all data
	result, err := io.ReadAll(reader)
	if err != nil {
		t.Errorf("ReadAll() error = %v", err)
	}

	if !bytes.Equal(result, data) {
		t.Errorf("ReadAll() = %s, want %s", string(result), string(data))
	}

	if uploaded != int64(len(data)) {
		t.Errorf("Progress callback uploaded = %d, want %d", uploaded, len(data))
	}

	if total != int64(len(data)) {
		t.Errorf("Progress callback total = %d, want %d", total, len(data))
	}
}

func TestClient_PushImage_ValidationErrors(t *testing.T) {
	client := NewClient(nil)
	ctx := context.Background()

	tests := []struct {
		name     string
		ref      ImageReference
		manifest *ImageManifest
		wantErr  bool
		errType  ErrorType
	}{
		{
			name:     "nil manifest",
			ref:      ImageReference{Registry: "example.com", Repository: "test", Tag: "latest"},
			manifest: nil,
			wantErr:  true,
			errType:  ErrorTypeValidation,
		},
		{
			name: "invalid reference",
			ref:  ImageReference{Registry: "", Repository: "", Tag: ""},
			manifest: &ImageManifest{
				SchemaVersion: 2,
				MediaType:     MediaTypeOCIManifest,
			},
			wantErr: true,
			errType: ErrorTypeBlob, // This will be a blob error because it tries to push blobs first
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := client.PushImage(ctx, tt.ref, tt.manifest)
			if (err != nil) != tt.wantErr {
				t.Errorf("PushImage() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if err != nil {
				if regErr, ok := err.(*RegistryError); ok {
					if regErr.Type != tt.errType {
						t.Errorf("PushImage() error type = %v, want %v", regErr.Type, tt.errType)
					}
				} else {
					t.Errorf("PushImage() error type = %T, want *RegistryError", err)
				}
			}
		})
	}
}

func TestClient_PushManifestList_ValidationErrors(t *testing.T) {
	client := NewClient(nil)
	ctx := context.Background()

	tests := []struct {
		name         string
		ref          ImageReference
		manifestList *ManifestList
		wantErr      bool
		errType      ErrorType
	}{
		{
			name:         "nil manifest list",
			ref:          ImageReference{Registry: "example.com", Repository: "test", Tag: "latest"},
			manifestList: nil,
			wantErr:      true,
			errType:      ErrorTypeValidation,
		},
		{
			name: "empty manifest list",
			ref:  ImageReference{Registry: "example.com", Repository: "test", Tag: "latest"},
			manifestList: &ManifestList{
				SchemaVersion: 2,
				MediaType:     MediaTypeOCIIndex,
				Manifests:     []PlatformManifest{},
			},
			wantErr: true,
			errType: ErrorTypeValidation,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := client.PushManifestList(ctx, tt.ref, tt.manifestList)
			if (err != nil) != tt.wantErr {
				t.Errorf("PushManifestList() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if err != nil {
				if regErr, ok := err.(*RegistryError); ok {
					if regErr.Type != tt.errType {
						t.Errorf("PushManifestList() error type = %v, want %v", regErr.Type, tt.errType)
					}
				} else {
					t.Errorf("PushManifestList() error type = %T, want *RegistryError", err)
				}
			}
		})
	}
}

// Benchmark tests for performance validation
func BenchmarkProgressReader(b *testing.B) {
	data := make([]byte, 1024*1024) // 1MB of data
	for i := range data {
		data[i] = byte(i % 256)
	}

	callback := func(uploaded, total int64) {
		// Minimal callback
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reader := &progressReader{
			reader:   bytes.NewReader(data),
			total:    int64(len(data)),
			callback: callback,
		}

		io.Copy(io.Discard, reader)
	}
}