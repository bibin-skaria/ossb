package registry

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"

	"github.com/bibin-skaria/ossb/internal/types"
)

func TestNewClient(t *testing.T) {
	tests := []struct {
		name    string
		options *ClientOptions
		want    *Client
	}{
		{
			name:    "nil options uses defaults",
			options: nil,
			want: &Client{
				options: DefaultClientOptions(),
				auth:    authn.Anonymous,
			},
		},
		{
			name: "custom options",
			options: &ClientOptions{
				UserAgent: "test-agent",
				Timeout:   10 * time.Second,
			},
			want: &Client{
				options: &ClientOptions{
					UserAgent: "test-agent",
					Timeout:   10 * time.Second,
				},
				auth: authn.Anonymous,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewClient(tt.options)
			
			if got.auth != tt.want.auth {
				t.Errorf("NewClient() auth = %v, want %v", got.auth, tt.want.auth)
			}
			
			if tt.options == nil {
				// Check that defaults were applied
				if got.options.UserAgent != "ossb/1.0" {
					t.Errorf("NewClient() UserAgent = %v, want %v", got.options.UserAgent, "ossb/1.0")
				}
				if got.options.Timeout != 30*time.Second {
					t.Errorf("NewClient() Timeout = %v, want %v", got.options.Timeout, 30*time.Second)
				}
			} else {
				if got.options.UserAgent != tt.options.UserAgent {
					t.Errorf("NewClient() UserAgent = %v, want %v", got.options.UserAgent, tt.options.UserAgent)
				}
				if got.options.Timeout != tt.options.Timeout {
					t.Errorf("NewClient() Timeout = %v, want %v", got.options.Timeout, tt.options.Timeout)
				}
			}
		})
	}
}

func TestClient_SetAuthenticator(t *testing.T) {
	client := NewClient(nil)
	
	basicAuth := &authn.Basic{Username: "user", Password: "pass"}
	client.SetAuthenticator(basicAuth)
	
	if client.auth != basicAuth {
		t.Errorf("SetAuthenticator() did not set authenticator correctly")
	}
}

func TestDefaultClientOptions(t *testing.T) {
	opts := DefaultClientOptions()
	
	if opts.UserAgent != "ossb/1.0" {
		t.Errorf("DefaultClientOptions() UserAgent = %v, want %v", opts.UserAgent, "ossb/1.0")
	}
	
	if opts.Timeout != 30*time.Second {
		t.Errorf("DefaultClientOptions() Timeout = %v, want %v", opts.Timeout, 30*time.Second)
	}
	
	if opts.RetryConfig == nil {
		t.Error("DefaultClientOptions() RetryConfig is nil")
	} else {
		if opts.RetryConfig.MaxRetries != 3 {
			t.Errorf("DefaultClientOptions() MaxRetries = %v, want %v", opts.RetryConfig.MaxRetries, 3)
		}
		if opts.RetryConfig.InitialInterval != 1*time.Second {
			t.Errorf("DefaultClientOptions() InitialInterval = %v, want %v", opts.RetryConfig.InitialInterval, 1*time.Second)
		}
		if opts.RetryConfig.MaxInterval != 30*time.Second {
			t.Errorf("DefaultClientOptions() MaxInterval = %v, want %v", opts.RetryConfig.MaxInterval, 30*time.Second)
		}
		if opts.RetryConfig.Multiplier != 2.0 {
			t.Errorf("DefaultClientOptions() Multiplier = %v, want %v", opts.RetryConfig.Multiplier, 2.0)
		}
	}
	
	if opts.InsecureRegistries == nil {
		t.Error("DefaultClientOptions() InsecureRegistries is nil")
	}
	
	if opts.Mirrors == nil {
		t.Error("DefaultClientOptions() Mirrors is nil")
	}
}

func TestClient_withRetry(t *testing.T) {
	tests := []struct {
		name        string
		retryConfig *RetryConfig
		fn          func() error
		wantRetries int
		wantErr     bool
	}{
		{
			name: "success on first try",
			retryConfig: &RetryConfig{
				MaxRetries:      3,
				InitialInterval: 1 * time.Millisecond,
				MaxInterval:     10 * time.Millisecond,
				Multiplier:      2.0,
			},
			fn: func() error {
				return nil
			},
			wantRetries: 0,
			wantErr:     false,
		},
		{
			name: "success on second try",
			retryConfig: &RetryConfig{
				MaxRetries:      3,
				InitialInterval: 1 * time.Millisecond,
				MaxInterval:     10 * time.Millisecond,
				Multiplier:      2.0,
			},
			fn: func() func() error {
				attempts := 0
				return func() error {
					attempts++
					if attempts == 1 {
						return fmt.Errorf("temporary error")
					}
					return nil
				}
			}(),
			wantRetries: 1,
			wantErr:     false,
		},
		{
			name: "max retries exceeded",
			retryConfig: &RetryConfig{
				MaxRetries:      2,
				InitialInterval: 1 * time.Millisecond,
				MaxInterval:     10 * time.Millisecond,
				Multiplier:      2.0,
			},
			fn: func() error {
				return fmt.Errorf("persistent error")
			},
			wantRetries: 2,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(&ClientOptions{
				RetryConfig: tt.retryConfig,
			})
			
			ctx := context.Background()
			err := client.withRetry(ctx, tt.fn)
			
			if (err != nil) != tt.wantErr {
				t.Errorf("withRetry() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestClient_withRetry_ContextCancellation(t *testing.T) {
	client := NewClient(&ClientOptions{
		RetryConfig: &RetryConfig{
			MaxRetries:      5,
			InitialInterval: 100 * time.Millisecond,
			MaxInterval:     1 * time.Second,
			Multiplier:      2.0,
		},
	})
	
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	
	err := client.withRetry(ctx, func() error {
		return fmt.Errorf("always fails")
	})
	
	if err != context.DeadlineExceeded {
		t.Errorf("withRetry() error = %v, want %v", err, context.DeadlineExceeded)
	}
}

func TestIsRetryableError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "network error is retryable",
			err: &RegistryError{
				Type: ErrorTypeNetwork,
			},
			want: true,
		},
		{
			name: "auth error is not retryable",
			err: &RegistryError{
				Type: ErrorTypeAuthentication,
			},
			want: false,
		},
		{
			name: "not found error is not retryable",
			err: &RegistryError{
				Type: ErrorTypeNotFound,
			},
			want: false,
		},
		{
			name: "validation error is not retryable",
			err: &RegistryError{
				Type: ErrorTypeValidation,
			},
			want: false,
		},
		{
			name: "unknown error is retryable",
			err:  fmt.Errorf("some unknown error"),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRetryableError(tt.err); got != tt.want {
				t.Errorf("isRetryableError() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Mock registry server for testing
func createMockRegistryServer(t *testing.T) *httptest.Server {
	mux := http.NewServeMux()
	
	// Mock manifest endpoint
	mux.HandleFunc("/v2/library/alpine/manifests/latest", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/vnd.docker.distribution.manifest.v2+json")
			w.WriteHeader(http.StatusOK)
			manifest := `{
				"schemaVersion": 2,
				"mediaType": "application/vnd.docker.distribution.manifest.v2+json",
				"config": {
					"mediaType": "application/vnd.docker.container.image.v1+json",
					"size": 1469,
					"digest": "sha256:c5b1261d6d3e43071626931fc004f70149baeba2c8ec672bd4f27761f8e566aa"
				},
				"layers": [
					{
						"mediaType": "application/vnd.docker.image.rootfs.diff.tar.gzip",
						"size": 2797612,
						"digest": "sha256:b49b96bfa4b2c4b3b8b4b8b4b8b4b8b4b8b4b8b4b8b4b8b4b8b4b8b4b8b4b8b4"
					}
				]
			}`
			w.Write([]byte(manifest))
		} else {
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	
	// Mock blob endpoint
	mux.HandleFunc("/v2/library/alpine/blobs/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/octet-stream")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("mock blob data"))
		} else {
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	
	// Mock auth endpoint (returns 401 to test auth handling)
	mux.HandleFunc("/v2/private/repo/manifests/latest", func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth == "" {
			w.Header().Set("WWW-Authenticate", `Bearer realm="https://auth.docker.io/token",service="registry.docker.io",scope="repository:private/repo:pull"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		
		if !strings.HasPrefix(auth, "Basic ") && !strings.HasPrefix(auth, "Bearer ") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		
		// Return a simple manifest for authenticated requests
		w.Header().Set("Content-Type", "application/vnd.docker.distribution.manifest.v2+json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"schemaVersion": 2, "mediaType": "application/vnd.docker.distribution.manifest.v2+json"}`))
	})
	
	// Mock v2 endpoint
	mux.HandleFunc("/v2/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	
	return httptest.NewServer(mux)
}

func TestClient_PullImage_MockRegistry(t *testing.T) {
	// Note: This test is limited because we can't easily mock the go-containerregistry
	// remote package. In a real implementation, we would need to create more sophisticated
	// mocks or use integration tests with a real registry.
	
	client := NewClient(nil)
	ctx := context.Background()
	
	ref := ImageReference{
		Registry:   "nonexistent.registry.com",
		Repository: "test/image",
		Tag:        "latest",
	}
	
	platform := types.Platform{
		OS:           "linux",
		Architecture: "amd64",
	}
	
	// This should fail because the registry doesn't exist
	_, err := client.PullImage(ctx, ref, platform)
	if err == nil {
		t.Error("PullImage() expected error for nonexistent registry, got nil")
	}
	
	// Check that it's a RegistryError
	if regErr, ok := err.(*RegistryError); ok {
		if regErr.Type != ErrorTypeNetwork {
			t.Errorf("PullImage() error type = %v, want %v", regErr.Type, ErrorTypeNetwork)
		}
		if regErr.Operation != "pull_image" {
			t.Errorf("PullImage() operation = %v, want %v", regErr.Operation, "pull_image")
		}
	} else {
		t.Errorf("PullImage() error type = %T, want *RegistryError", err)
	}
}

func TestClient_GetImageManifest_InvalidReference(t *testing.T) {
	client := NewClient(nil)
	ctx := context.Background()
	
	// Test with invalid reference
	ref := ImageReference{
		Registry:   "",
		Repository: "",
		Tag:        "",
	}
	
	platform := types.Platform{
		OS:           "linux",
		Architecture: "amd64",
	}
	
	_, err := client.GetImageManifest(ctx, ref, platform)
	if err == nil {
		t.Error("GetImageManifest() expected error for invalid reference, got nil")
	}
	
	if regErr, ok := err.(*RegistryError); ok {
		if regErr.Type != ErrorTypeValidation {
			t.Errorf("GetImageManifest() error type = %v, want %v", regErr.Type, ErrorTypeValidation)
		}
	} else {
		t.Errorf("GetImageManifest() error type = %T, want *RegistryError", err)
	}
}

func TestClient_PushManifestList_ValidationError(t *testing.T) {
	client := NewClient(nil)
	ctx := context.Background()
	
	ref := ImageReference{
		Registry:   "example.com",
		Repository: "test/image",
		Tag:        "latest",
	}
	
	manifestList := &ManifestList{
		SchemaVersion: 2,
		MediaType:     "application/vnd.docker.distribution.manifest.list.v2+json",
	}
	
	err := client.PushManifestList(ctx, ref, manifestList)
	if err == nil {
		t.Error("PushManifestList() expected error for empty manifest list, got nil")
	}
	
	if regErr, ok := err.(*RegistryError); ok {
		if regErr.Type != ErrorTypeValidation {
			t.Errorf("PushManifestList() error type = %v, want %v", regErr.Type, ErrorTypeValidation)
		}
		if !strings.Contains(regErr.Message, "at least one manifest") {
			t.Errorf("PushManifestList() message = %v, want to contain 'at least one manifest'", regErr.Message)
		}
	} else {
		t.Errorf("PushManifestList() error type = %T, want *RegistryError", err)
	}
}

func TestClient_GetManifestList_NotImplemented(t *testing.T) {
	client := NewClient(nil)
	ctx := context.Background()
	
	ref := ImageReference{
		Registry:   "example.com",
		Repository: "test/image",
		Tag:        "latest",
	}
	
	_, err := client.GetManifestList(ctx, ref)
	if err == nil {
		t.Error("GetManifestList() expected error for not implemented, got nil")
	}
	
	if regErr, ok := err.(*RegistryError); ok {
		if regErr.Type != ErrorTypeValidation {
			t.Errorf("GetManifestList() error type = %v, want %v", regErr.Type, ErrorTypeValidation)
		}
		if !strings.Contains(regErr.Message, "not yet implemented") {
			t.Errorf("GetManifestList() message = %v, want to contain 'not yet implemented'", regErr.Message)
		}
	} else {
		t.Errorf("GetManifestList() error type = %T, want *RegistryError", err)
	}
}