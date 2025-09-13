# Registry Package

The registry package provides comprehensive container registry client functionality for OSSB (Open Source Slim Builder). It implements authentication, image pulling/pushing, and OCI-compliant operations.

## Features

- **Multi-source Authentication**: Supports credential discovery from:
  - Registry configuration files
  - Environment variables
  - Docker config files (`~/.docker/config.json`)
  - Kubernetes secrets
- **OCI Compliance**: Full support for OCI image specifications
- **Error Handling**: Comprehensive error types with retry logic
- **Multi-architecture**: Support for platform-specific image operations
- **Registry Support**: Works with Docker Hub, private registries, and cloud registries

## Usage

### Basic Client Usage

```go
package main

import (
    "context"
    "fmt"
    
    "github.com/bibin-skaria/ossb/registry"
    "github.com/bibin-skaria/ossb/internal/types"
)

func main() {
    // Create a client with default options
    client := registry.NewClient(nil)
    
    // Set up authentication
    authProvider := registry.NewAuthProvider(nil)
    auth, err := authProvider.GetAuthenticator(context.Background(), "docker.io")
    if err != nil {
        panic(err)
    }
    client.SetAuthenticator(auth)
    
    // Parse image reference
    ref, err := registry.ParseImageReference("alpine:latest")
    if err != nil {
        panic(err)
    }
    
    // Pull image manifest
    platform := types.Platform{OS: "linux", Architecture: "amd64"}
    manifest, err := client.PullImage(context.Background(), ref, platform)
    if err != nil {
        panic(err)
    }
    
    fmt.Printf("Pulled image: %d layers\n", len(manifest.Layers))
}
```

### Authentication Configuration

#### Environment Variables

```bash
# Docker Hub
export DOCKER_USERNAME=myuser
export DOCKER_PASSWORD=mypass

# Private registry
export REGISTRY_EXAMPLE_COM_USERNAME=user
export REGISTRY_EXAMPLE_COM_PASSWORD=pass
```

#### Docker Config File

The client automatically reads from `~/.docker/config.json`:

```json
{
  "auths": {
    "https://index.docker.io/v1/": {
      "auth": "base64encodedcredentials"
    },
    "registry.example.com": {
      "username": "user",
      "password": "pass"
    }
  }
}
```

#### Kubernetes Secrets

Mount registry credentials as Kubernetes secrets:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: registry-secret
type: Opaque
data:
  username: <base64-encoded-username>
  password: <base64-encoded-password>
---
apiVersion: v1
kind: Pod
spec:
  containers:
  - name: ossb
    image: ossb:latest
    volumeMounts:
    - name: registry-secret
      mountPath: /var/run/secrets/registry
  volumes:
  - name: registry-secret
    secret:
      secretName: registry-secret
```

### Registry Configuration

Create a registry configuration file at `~/.ossb/registry.json`:

```json
{
  "default_registry": "docker.io",
  "registries": {
    "docker.io": {
      "username": "myuser",
      "password": "mypass"
    },
    "registry.example.com": {
      "token": "mytoken"
    }
  },
  "insecure": ["localhost:5000"],
  "mirrors": {
    "docker.io": ["mirror1.example.com", "mirror2.example.com"]
  }
}
```

## API Reference

### Client

```go
type Client struct {
    // ...
}

func NewClient(options *ClientOptions) *Client
func (c *Client) SetAuthenticator(auth authn.Authenticator)
func (c *Client) PullImage(ctx context.Context, ref ImageReference, platform types.Platform) (*ImageManifest, error)
func (c *Client) PushImage(ctx context.Context, ref ImageReference, manifest *ImageManifest) error
func (c *Client) GetImageManifest(ctx context.Context, ref ImageReference, platform types.Platform) (*ImageManifest, error)
func (c *Client) GetBlob(ctx context.Context, ref ImageReference, digest string) (io.ReadCloser, error)
```

### AuthProvider

```go
type AuthProvider struct {
    // ...
}

func NewAuthProvider(config *RegistryConfig) *AuthProvider
func (a *AuthProvider) GetAuthenticator(ctx context.Context, registry string) (authn.Authenticator, error)
func (a *AuthProvider) DiscoverCredentials(ctx context.Context, registry string) (*Credentials, error)
func (a *AuthProvider) ValidateCredentials(ctx context.Context, registry string, creds *Credentials) error
```

### Types

```go
type ImageReference struct {
    Registry   string
    Repository string
    Tag        string
    Digest     string
}

type ImageManifest struct {
    SchemaVersion int
    MediaType     string
    Config        Descriptor
    Layers        []Descriptor
}

type Credentials struct {
    Username      string
    Password      string
    Token         string
    IdentityToken string
    RegistryToken string
}
```

## Error Handling

The package provides comprehensive error handling with specific error types:

```go
type RegistryError struct {
    Type      ErrorType // network, authentication, not_found, etc.
    Operation string    // pull_image, push_image, etc.
    Registry  string    // registry hostname
    Message   string    // human-readable message
    Cause     error     // underlying error
}
```

Error types:
- `ErrorTypeNetwork`: Network-related errors (retryable)
- `ErrorTypeAuthentication`: Authentication failures (not retryable)
- `ErrorTypeAuthorization`: Authorization failures (not retryable)
- `ErrorTypeNotFound`: Resource not found (not retryable)
- `ErrorTypeValidation`: Input validation errors (not retryable)
- `ErrorTypeManifest`: Manifest parsing errors
- `ErrorTypeBlob`: Blob operation errors

## Testing

Run unit tests:
```bash
go test ./registry/...
```

Run integration tests (requires network access):
```bash
go test -tags=integration ./registry/...
```

## Implementation Status

✅ **Completed**:
- Basic registry client interface
- Multi-source authentication (config, env, Docker config, K8s secrets)
- Image reference parsing and validation
- Error handling with retry logic
- Comprehensive unit tests
- OCI-compliant data structures

🔧 **In Progress** (Future Tasks):
- Multi-architecture manifest list support
- Image pushing operations
- Blob streaming and caching
- Registry mirror support
- Advanced authentication methods

## Dependencies

- `github.com/google/go-containerregistry`: Core registry operations
- Standard Go libraries for HTTP, JSON, and file operations

## Security Considerations

- Credentials are never logged or exposed in error messages
- Supports secure credential storage via Kubernetes secrets
- TLS verification enabled by default (configurable for development)
- Proper handling of authentication tokens and refresh