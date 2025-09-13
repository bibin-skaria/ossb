package registry

import (
	"fmt"
	"strings"

	"github.com/google/go-containerregistry/pkg/v1"
)

// ImageReference represents a container image reference
type ImageReference struct {
	Registry   string `json:"registry"`
	Repository string `json:"repository"`
	Tag        string `json:"tag,omitempty"`
	Digest     string `json:"digest,omitempty"`
}

// String returns the full image reference string
func (r ImageReference) String() string {
	var ref strings.Builder
	
	if r.Registry != "" && r.Registry != "docker.io" {
		ref.WriteString(r.Registry)
		ref.WriteString("/")
	}
	
	ref.WriteString(r.Repository)
	
	if r.Digest != "" {
		ref.WriteString("@")
		ref.WriteString(r.Digest)
	} else if r.Tag != "" {
		ref.WriteString(":")
		ref.WriteString(r.Tag)
	} else {
		ref.WriteString(":latest")
	}
	
	return ref.String()
}

// ParseImageReference parses a string into an ImageReference
func ParseImageReference(ref string) (ImageReference, error) {
	if ref == "" {
		return ImageReference{}, fmt.Errorf("image reference cannot be empty")
	}

	imageRef := ImageReference{}
	original := ref
	
	// Handle digest references (image@sha256:...)
	if strings.Contains(ref, "@") {
		parts := strings.SplitN(ref, "@", 2)
		ref = parts[0]
		imageRef.Digest = parts[1]
		
		// Validate digest format
		if err := validateDigest(imageRef.Digest); err != nil {
			return ImageReference{}, fmt.Errorf("invalid digest format: %v", err)
		}
	}
	
	// Handle tag references first (but be careful with registry:port)
	if imageRef.Digest == "" && strings.Contains(ref, ":") {
		// Find the last colon to handle registry:port/repo:tag correctly
		lastColon := strings.LastIndex(ref, ":")
		beforeColon := ref[:lastColon]
		afterColon := ref[lastColon+1:]
		
		// If there's no slash before the last colon, or if the part after colon doesn't contain slash,
		// then this colon is likely separating tag
		if !strings.Contains(beforeColon, "/") || !strings.Contains(afterColon, "/") {
			// Check if this looks like a registry:port by seeing if beforeColon contains . or if afterColon is numeric
			if strings.Contains(beforeColon, ".") && !strings.Contains(afterColon, "/") {
				// This might be registry.com:tag, which is invalid, or registry.com:port/repo
				if strings.Contains(afterColon, "/") {
					// registry.com:port/repo case - don't split on this colon
					ref = original
				} else {
					// Assume it's image:tag
					ref = beforeColon
					imageRef.Tag = afterColon
				}
			} else {
				// Normal image:tag case
				ref = beforeColon
				imageRef.Tag = afterColon
			}
		}
	}
	
	// Set default tag if none specified
	if imageRef.Tag == "" && imageRef.Digest == "" {
		imageRef.Tag = "latest"
	}
	
	// Validate tag format if present
	if imageRef.Tag != "" {
		if err := validateTag(imageRef.Tag); err != nil {
			return ImageReference{}, fmt.Errorf("invalid tag format: %v", err)
		}
	}
	
	// Parse registry and repository
	parts := strings.SplitN(ref, "/", 2)
	if len(parts) == 1 {
		// No slash - just image name, assume Docker Hub
		imageRef.Registry = "docker.io"
		imageRef.Repository = "library/" + parts[0]
	} else {
		// Has slash, first part might be registry
		if strings.Contains(parts[0], ".") || strings.Contains(parts[0], ":") {
			// First part looks like a registry
			imageRef.Registry = parts[0]
			imageRef.Repository = parts[1]
		} else {
			// No registry, assume Docker Hub
			imageRef.Registry = "docker.io"
			imageRef.Repository = ref
		}
	}
	
	// Validate repository format
	if err := validateRepository(imageRef.Repository); err != nil {
		return ImageReference{}, fmt.Errorf("invalid repository format: %v", err)
	}
	
	return imageRef, nil
}

// validateDigest validates that a digest has the correct format
func validateDigest(digest string) error {
	if digest == "" {
		return fmt.Errorf("digest cannot be empty")
	}
	
	// Digest should be in format algorithm:hex
	parts := strings.SplitN(digest, ":", 2)
	if len(parts) != 2 {
		return fmt.Errorf("digest must be in format algorithm:hex")
	}
	
	algorithm, hex := parts[0], parts[1]
	
	// Validate algorithm
	switch algorithm {
	case "sha256", "sha512", "sha1", "md5":
		// Valid algorithms
	default:
		return fmt.Errorf("unsupported digest algorithm: %s", algorithm)
	}
	
	// Validate hex string
	if len(hex) == 0 {
		return fmt.Errorf("digest hex cannot be empty")
	}
	
	// Check if hex contains only valid characters
	for _, char := range hex {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f') || (char >= 'A' && char <= 'F')) {
			return fmt.Errorf("digest hex contains invalid character: %c", char)
		}
	}
	
	return nil
}

// validateTag validates that a tag has the correct format
func validateTag(tag string) error {
	if tag == "" {
		return fmt.Errorf("tag cannot be empty")
	}
	
	// Tag length should be reasonable
	if len(tag) > 128 {
		return fmt.Errorf("tag too long (max 128 characters)")
	}
	
	// Tag should not start with . or -
	if strings.HasPrefix(tag, ".") || strings.HasPrefix(tag, "-") {
		return fmt.Errorf("tag cannot start with '.' or '-'")
	}
	
	// Tag should contain only valid characters
	for _, char := range tag {
		if !((char >= 'a' && char <= 'z') || 
			 (char >= 'A' && char <= 'Z') || 
			 (char >= '0' && char <= '9') || 
			 char == '.' || char == '-' || char == '_') {
			return fmt.Errorf("tag contains invalid character: %c", char)
		}
	}
	
	return nil
}

// validateRepository validates that a repository has the correct format
func validateRepository(repository string) error {
	if repository == "" {
		return fmt.Errorf("repository cannot be empty")
	}
	
	// Repository length should be reasonable
	if len(repository) > 255 {
		return fmt.Errorf("repository too long (max 255 characters)")
	}
	
	// Split by / and validate each component
	components := strings.Split(repository, "/")
	for _, component := range components {
		if component == "" {
			return fmt.Errorf("repository component cannot be empty")
		}
		
		// Component should not start with . or -
		if strings.HasPrefix(component, ".") || strings.HasPrefix(component, "-") {
			return fmt.Errorf("repository component cannot start with '.' or '-'")
		}
		
		// Component should contain only valid characters
		for _, char := range component {
			if !((char >= 'a' && char <= 'z') || 
				 (char >= 'A' && char <= 'Z') || 
				 (char >= '0' && char <= '9') || 
				 char == '.' || char == '-' || char == '_') {
				return fmt.Errorf("repository component contains invalid character: %c", char)
			}
		}
	}
	
	return nil
}

// Validate validates the ImageReference
func (r ImageReference) Validate() error {
	if r.Registry == "" {
		return fmt.Errorf("registry cannot be empty")
	}
	
	if r.Repository == "" {
		return fmt.Errorf("repository cannot be empty")
	}
	
	if r.Tag == "" && r.Digest == "" {
		return fmt.Errorf("either tag or digest must be specified")
	}
	
	if r.Tag != "" && r.Digest != "" {
		return fmt.Errorf("cannot specify both tag and digest")
	}
	
	if r.Tag != "" {
		if err := validateTag(r.Tag); err != nil {
			return err
		}
	}
	
	if r.Digest != "" {
		if err := validateDigest(r.Digest); err != nil {
			return err
		}
	}
	
	if err := validateRepository(r.Repository); err != nil {
		return err
	}
	
	return nil
}

// Descriptor represents an OCI descriptor
type Descriptor struct {
	MediaType   string            `json:"mediaType"`
	Size        int64             `json:"size"`
	Digest      string            `json:"digest"`
	URLs        []string          `json:"urls,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

// ImageManifest represents an OCI image manifest
type ImageManifest struct {
	SchemaVersion int          `json:"schemaVersion"`
	MediaType     string       `json:"mediaType"`
	Config        Descriptor   `json:"config"`
	Layers        []Descriptor `json:"layers"`
	Annotations   map[string]string `json:"annotations,omitempty"`
	
	// Internal field to store the actual image
	image v1.Image `json:"-"`
}

// ManifestList represents an OCI manifest list (multi-arch)
type ManifestList struct {
	SchemaVersion int                    `json:"schemaVersion"`
	MediaType     string                 `json:"mediaType"`
	Manifests     []PlatformManifest     `json:"manifests"`
	Annotations   map[string]string      `json:"annotations,omitempty"`
}

// PlatformManifest represents a platform-specific manifest in a manifest list
type PlatformManifest struct {
	MediaType string            `json:"mediaType"`
	Size      int64             `json:"size"`
	Digest    string            `json:"digest"`
	Platform  Platform          `json:"platform"`
	URLs      []string          `json:"urls,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

// Platform represents a target platform
type Platform struct {
	Architecture string   `json:"architecture"`
	OS           string   `json:"os"`
	OSVersion    string   `json:"os.version,omitempty"`
	OSFeatures   []string `json:"os.features,omitempty"`
	Variant      string   `json:"variant,omitempty"`
	Features     []string `json:"features,omitempty"`
}

// String returns a string representation of the platform
func (p Platform) String() string {
	if p.Variant != "" {
		return fmt.Sprintf("%s/%s/%s", p.OS, p.Architecture, p.Variant)
	}
	return fmt.Sprintf("%s/%s", p.OS, p.Architecture)
}

// Matches checks if this platform matches another platform
func (p Platform) Matches(other Platform) bool {
	if p.OS != "" && other.OS != "" && p.OS != other.OS {
		return false
	}
	
	if p.Architecture != "" && other.Architecture != "" && p.Architecture != other.Architecture {
		return false
	}
	
	if p.Variant != "" && other.Variant != "" && p.Variant != other.Variant {
		return false
	}
	
	return true
}

// IsCompatible checks if this platform is compatible with another platform
func (p Platform) IsCompatible(other Platform) bool {
	// Exact match
	if p.Matches(other) {
		return true
	}
	
	// Architecture compatibility rules
	if p.OS == other.OS {
		switch {
		case p.Architecture == "amd64" && other.Architecture == "386":
			return true
		case p.Architecture == "arm64" && other.Architecture == "arm" && other.Variant == "v8":
			return true
		case p.Architecture == "arm" && other.Architecture == "arm":
			// ARM variant compatibility
			return isARMVariantCompatible(p.Variant, other.Variant)
		}
	}
	
	return false
}

// isARMVariantCompatible checks ARM variant compatibility
func isARMVariantCompatible(platform, target string) bool {
	// ARM variants in order of capability (higher can run lower)
	variants := []string{"v6", "v7", "v8"}
	
	platformIdx := -1
	targetIdx := -1
	
	for i, v := range variants {
		if platform == v {
			platformIdx = i
		}
		if target == v {
			targetIdx = i
		}
	}
	
	// If we can't determine compatibility, assume compatible
	if platformIdx == -1 || targetIdx == -1 {
		return true
	}
	
	// Higher platform variants can run lower target variants
	return platformIdx >= targetIdx
}

// Credentials represents authentication credentials
type Credentials struct {
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	Token    string `json:"token,omitempty"`
	// IdentityToken is used for OAuth2 authentication
	IdentityToken string `json:"identitytoken,omitempty"`
	// RegistryToken is used for registry-specific tokens
	RegistryToken string `json:"registrytoken,omitempty"`
}

// RegistryConfig represents registry configuration
type RegistryConfig struct {
	// DefaultRegistry is the default registry to use when none is specified
	DefaultRegistry string `json:"default_registry,omitempty"`
	// Registries maps registry hostnames to their configurations
	Registries map[string]*RegistryAuth `json:"registries,omitempty"`
	// Insecure lists registries that should skip TLS verification
	Insecure []string `json:"insecure,omitempty"`
	// Mirrors maps registry hostnames to their mirror URLs
	Mirrors map[string][]string `json:"mirrors,omitempty"`
}

// RegistryAuth represents authentication configuration for a registry
type RegistryAuth struct {
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	Token    string `json:"token,omitempty"`
	// AuthFile points to a Docker config file
	AuthFile string `json:"auth_file,omitempty"`
	// CredentialsStore specifies a credentials store to use
	CredentialsStore string `json:"credentials_store,omitempty"`
}

// ErrorType represents the type of registry error
type ErrorType string

const (
	ErrorTypeNetwork        ErrorType = "network"
	ErrorTypeAuthentication ErrorType = "authentication"
	ErrorTypeAuthorization  ErrorType = "authorization"
	ErrorTypeNotFound       ErrorType = "not_found"
	ErrorTypeValidation     ErrorType = "validation"
	ErrorTypeManifest       ErrorType = "manifest"
	ErrorTypeBlob           ErrorType = "blob"
	ErrorTypeUnknown        ErrorType = "unknown"
)

// RegistryError represents an error from registry operations
type RegistryError struct {
	Type      ErrorType `json:"type"`
	Operation string    `json:"operation"`
	Registry  string    `json:"registry,omitempty"`
	Message   string    `json:"message"`
	Cause     error     `json:"-"`
}

// Error implements the error interface
func (e *RegistryError) Error() string {
	if e.Registry != "" {
		return fmt.Sprintf("registry error [%s] %s on %s: %s", e.Type, e.Operation, e.Registry, e.Message)
	}
	return fmt.Sprintf("registry error [%s] %s: %s", e.Type, e.Operation, e.Message)
}

// Unwrap returns the underlying error
func (e *RegistryError) Unwrap() error {
	return e.Cause
}

// IsRetryable returns true if the error might succeed on retry
func (e *RegistryError) IsRetryable() bool {
	switch e.Type {
	case ErrorTypeNetwork:
		return true
	case ErrorTypeAuthentication, ErrorTypeAuthorization, ErrorTypeNotFound, ErrorTypeValidation:
		return false
	default:
		return true
	}
}