package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// RegistryV2Client implements the Docker Registry HTTP API V2 for push operations
type RegistryV2Client struct {
	client *Client
}

// PushOptions configures push operations
type PushOptions struct {
	// SkipTLSVerify skips TLS certificate verification
	SkipTLSVerify bool
	// ChunkSize for chunked uploads (0 = no chunking)
	ChunkSize int64
	// MaxRetries for failed operations
	MaxRetries int
	// ProgressCallback for upload progress
	ProgressCallback func(stage string, progress float64)
}

// NewRegistryV2Client creates a new registry v2 API client
func NewRegistryV2Client(client *Client) *RegistryV2Client {
	return &RegistryV2Client{
		client: client,
	}
}

// PushBlobFromReader pushes a blob to the registry from an io.Reader
func (r *RegistryV2Client) PushBlobFromReader(ctx context.Context, ref ImageReference, digest string, size int64, content io.Reader, progressCallback func(uploaded, total int64)) error {
	// Step 1: Check if blob already exists
	exists, err := r.BlobExists(ctx, ref, digest)
	if err != nil {
		return fmt.Errorf("failed to check blob existence: %v", err)
	}
	if exists {
		if progressCallback != nil {
			progressCallback(size, size)
		}
		return nil // Blob already exists
	}

	// Step 2: Initiate blob upload
	uploadURL, err := r.InitiateBlobUpload(ctx, ref)
	if err != nil {
		return fmt.Errorf("failed to initiate blob upload: %v", err)
	}

	// Step 3: Upload blob content
	if err := r.UploadBlob(ctx, uploadURL, digest, size, content, progressCallback); err != nil {
		return fmt.Errorf("failed to upload blob: %v", err)
	}

	return nil
}

// BlobExists checks if a blob exists in the registry
func (r *RegistryV2Client) BlobExists(ctx context.Context, ref ImageReference, digest string) (bool, error) {
	// Construct blob URL: /v2/<name>/blobs/<digest>
	blobURL := r.buildBlobURL(ref, digest)

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, blobURL, nil)
	if err != nil {
		return false, fmt.Errorf("failed to create request: %v", err)
	}

	// Add authentication
	if err := r.addAuth(req, ref.Registry); err != nil {
		return false, fmt.Errorf("failed to add authentication: %v", err)
	}

	transport := r.client.options.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	
	resp, err := transport.RoundTrip(req)
	if err != nil {
		return false, fmt.Errorf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	case http.StatusUnauthorized:
		// For new repositories, Docker Hub may return 401 for blob existence checks
		// We'll treat this as "blob doesn't exist" and proceed with upload
		fmt.Printf("Debug: Got 401 for blob existence check, treating as not found\n")
		return false, nil
	default:
		return false, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
}

// InitiateBlobUpload initiates a blob upload and returns the upload URL
func (r *RegistryV2Client) InitiateBlobUpload(ctx context.Context, ref ImageReference) (string, error) {
	// Construct upload URL: /v2/<name>/blobs/uploads/
	uploadURL := r.buildUploadURL(ref)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %v", err)
	}

	// Add authentication
	if err := r.addAuth(req, ref.Registry); err != nil {
		return "", fmt.Errorf("failed to add authentication: %v", err)
	}

	transport := r.client.options.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	
	resp, err := transport.RoundTrip(req)
	if err != nil {
		return "", fmt.Errorf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		fmt.Printf("Debug: InitiateBlobUpload failed with status %d, body: %s\n", resp.StatusCode, string(body))
		return "", fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(body))
	}

	// Get upload URL from Location header
	location := resp.Header.Get("Location")
	if location == "" {
		return "", fmt.Errorf("missing Location header in upload response")
	}

	return location, nil
}

// UploadBlob uploads blob content to the given upload URL
func (r *RegistryV2Client) UploadBlob(ctx context.Context, uploadURL, digest string, size int64, content io.Reader, progressCallback func(uploaded, total int64)) error {
	// Add digest parameter to upload URL
	u, err := url.Parse(uploadURL)
	if err != nil {
		return fmt.Errorf("failed to parse upload URL: %v", err)
	}

	query := u.Query()
	query.Set("digest", digest)
	u.RawQuery = query.Encode()

	// Create progress reader if callback provided
	var reader io.Reader = content
	if progressCallback != nil {
		reader = &progressReader{
			reader:   content,
			total:    size,
			callback: progressCallback,
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u.String(), reader)
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Content-Length", strconv.FormatInt(size, 10))

	// Add authentication
	if err := r.addAuth(req, extractRegistryFromURL(uploadURL)); err != nil {
		return fmt.Errorf("failed to add authentication: %v", err)
	}

	transport := r.client.options.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	
	resp, err := transport.RoundTrip(req)
	if err != nil {
		return fmt.Errorf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(body))
	}

	return nil
}

// PushManifest pushes a manifest to the registry
func (r *RegistryV2Client) PushManifest(ctx context.Context, ref ImageReference, manifest interface{}, mediaType string) error {
	// Serialize manifest to JSON
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("failed to serialize manifest: %v", err)
	}

	// Construct manifest URL: /v2/<name>/manifests/<reference>
	manifestURL := r.buildManifestURL(ref)

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, manifestURL, bytes.NewReader(manifestBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", mediaType)
	req.Header.Set("Content-Length", strconv.Itoa(len(manifestBytes)))

	// Add authentication
	if err := r.addAuth(req, ref.Registry); err != nil {
		return fmt.Errorf("failed to add authentication: %v", err)
	}

	transport := r.client.options.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	
	resp, err := transport.RoundTrip(req)
	if err != nil {
		return fmt.Errorf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(body))
	}

	return nil
}

// buildBlobURL constructs the blob URL for a given reference and digest
func (r *RegistryV2Client) buildBlobURL(ref ImageReference, digest string) string {
	registry := ref.Registry
	if registry == "docker.io" {
		registry = "registry-1.docker.io"
	}

	// Use HTTP for localhost/test registries, HTTPS for others
	scheme := "https"
	if strings.Contains(registry, "localhost") || strings.Contains(registry, "127.0.0.1") {
		scheme = "http"
	}

	return fmt.Sprintf("%s://%s/v2/%s/blobs/%s", scheme, registry, ref.Repository, digest)
}

// buildUploadURL constructs the upload initiation URL for a given reference
func (r *RegistryV2Client) buildUploadURL(ref ImageReference) string {
	registry := ref.Registry
	if registry == "docker.io" {
		registry = "registry-1.docker.io"
	}

	// Use HTTP for localhost/test registries, HTTPS for others
	scheme := "https"
	if strings.Contains(registry, "localhost") || strings.Contains(registry, "127.0.0.1") {
		scheme = "http"
	}

	return fmt.Sprintf("%s://%s/v2/%s/blobs/uploads/", scheme, registry, ref.Repository)
}

// buildManifestURL constructs the manifest URL for a given reference
func (r *RegistryV2Client) buildManifestURL(ref ImageReference) string {
	registry := ref.Registry
	if registry == "docker.io" {
		registry = "registry-1.docker.io"
	}

	reference := ref.Tag
	if ref.Digest != "" {
		reference = ref.Digest
	}

	// Use HTTP for localhost/test registries, HTTPS for others
	scheme := "https"
	if strings.Contains(registry, "localhost") || strings.Contains(registry, "127.0.0.1") {
		scheme = "http"
	}

	return fmt.Sprintf("%s://%s/v2/%s/manifests/%s", scheme, registry, ref.Repository, reference)
}

// addAuth adds authentication to the request
func (r *RegistryV2Client) addAuth(req *http.Request, registry string) error {
	// Use the client's authenticator
	if r.client.auth != nil {
		auth, err := r.client.auth.Authorization()
		if err != nil {
			return fmt.Errorf("failed to get authorization: %v", err)
		}
		if auth != nil {
			// The AuthConfig has Username and Password fields for basic auth
			if auth.Username != "" && auth.Password != "" {
				if os.Getenv("OSSB_DEBUG") != "" {
					fmt.Printf("Debug: Adding basic auth for user: %s to request: %s\n", auth.Username, req.URL.String())
				}
				req.SetBasicAuth(auth.Username, auth.Password)
			} else if auth.RegistryToken != "" {
				if os.Getenv("OSSB_DEBUG") != "" {
					fmt.Printf("Debug: Adding bearer token to request: %s\n", req.URL.String())
				}
				req.Header.Set("Authorization", "Bearer "+auth.RegistryToken)
			}
		} else {
			if os.Getenv("OSSB_DEBUG") != "" {
				fmt.Printf("Debug: No auth config available for request: %s\n", req.URL.String())
			}
		}
	} else {
		if os.Getenv("OSSB_DEBUG") != "" {
			fmt.Printf("Debug: No authenticator set for request: %s\n", req.URL.String())
		}
	}
	return nil
}

// extractRegistryFromURL extracts the registry hostname from a URL
func extractRegistryFromURL(urlStr string) string {
	u, err := url.Parse(urlStr)
	if err != nil {
		return ""
	}
	return u.Host
}

// progressReader wraps an io.Reader to provide progress callbacks
type progressReader struct {
	reader   io.Reader
	total    int64
	read     int64
	callback func(uploaded, total int64)
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.reader.Read(p)
	pr.read += int64(n)
	if pr.callback != nil {
		pr.callback(pr.read, pr.total)
	}
	return n, err
}