package registry

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

// AuthProvider handles authentication for registry operations
type AuthProvider struct {
	config *RegistryConfig
}

// NewAuthProvider creates a new authentication provider
func NewAuthProvider(config *RegistryConfig) *AuthProvider {
	if config == nil {
		config = &RegistryConfig{
			Registries: make(map[string]*RegistryAuth),
			Insecure:   []string{},
			Mirrors:    make(map[string][]string),
		}
	}
	return &AuthProvider{config: config}
}

// GetAuthenticator returns an authenticator for the given registry
func (a *AuthProvider) GetAuthenticator(ctx context.Context, registry string) (authn.Authenticator, error) {
	// Try multiple credential sources in order of preference
	authenticators := []func(string) (authn.Authenticator, error){
		a.getFromConfig,
		a.getFromEnvironment,
		a.getFromDockerConfig,
		a.getFromKubernetesSecrets,
	}

	for _, getAuth := range authenticators {
		if auth, err := getAuth(registry); err == nil && auth != authn.Anonymous {
			return auth, nil
		}
	}

	// Fall back to anonymous authentication
	return authn.Anonymous, nil
}

// getFromConfig gets credentials from the registry configuration
func (a *AuthProvider) getFromConfig(registry string) (authn.Authenticator, error) {
	if a.config.Registries == nil {
		return authn.Anonymous, fmt.Errorf("no registry config")
	}

	regAuth, exists := a.config.Registries[registry]
	if !exists {
		return authn.Anonymous, fmt.Errorf("registry not found in config")
	}

	if regAuth.Username != "" && regAuth.Password != "" {
		return &authn.Basic{
			Username: regAuth.Username,
			Password: regAuth.Password,
		}, nil
	}

	if regAuth.Token != "" {
		return &authn.Bearer{Token: regAuth.Token}, nil
	}

	return authn.Anonymous, fmt.Errorf("no valid credentials in config")
}

// getFromEnvironment gets credentials from environment variables
func (a *AuthProvider) getFromEnvironment(registry string) (authn.Authenticator, error) {
	// Check for registry-specific environment variables
	envPrefix := strings.ToUpper(strings.ReplaceAll(registry, ".", "_"))
	envPrefix = strings.ReplaceAll(envPrefix, "-", "_")

	username := os.Getenv(envPrefix + "_USERNAME")
	password := os.Getenv(envPrefix + "_PASSWORD")
	token := os.Getenv(envPrefix + "_TOKEN")

	if username != "" && password != "" {
		return &authn.Basic{
			Username: username,
			Password: password,
		}, nil
	}

	if token != "" {
		return &authn.Bearer{Token: token}, nil
	}

	// Check for generic Docker environment variables
	if registry == "docker.io" || registry == "index.docker.io" {
		username = os.Getenv("DOCKER_USERNAME")
		password = os.Getenv("DOCKER_PASSWORD")
		token = os.Getenv("DOCKER_TOKEN")

		if username != "" && password != "" {
			return &authn.Basic{
				Username: username,
				Password: password,
			}, nil
		}

		if token != "" {
			return &authn.Bearer{Token: token}, nil
		}
	}

	return authn.Anonymous, fmt.Errorf("no credentials in environment")
}

// getFromDockerConfig gets credentials from Docker configuration files
func (a *AuthProvider) getFromDockerConfig(registry string) (authn.Authenticator, error) {
	// Try multiple Docker config locations
	configPaths := []string{
		os.Getenv("DOCKER_CONFIG"),
		filepath.Join(os.Getenv("HOME"), ".docker"),
		"/root/.docker", // For containers running as root
	}

	for _, configPath := range configPaths {
		if configPath == "" {
			continue
		}

		configFile := filepath.Join(configPath, "config.json")
		if auth, err := a.readDockerConfigFile(configFile, registry); err == nil {
			return auth, nil
		}
	}

	return authn.Anonymous, fmt.Errorf("no Docker config found")
}

// readDockerConfigFile reads and parses a Docker config file
func (a *AuthProvider) readDockerConfigFile(configFile, registry string) (authn.Authenticator, error) {
	data, err := os.ReadFile(configFile)
	if err != nil {
		return authn.Anonymous, err
	}

	var config struct {
		Auths map[string]struct {
			Auth     string `json:"auth"`
			Username string `json:"username"`
			Password string `json:"password"`
		} `json:"auths"`
		CredHelpers map[string]string `json:"credHelpers"`
		CredsStore  string            `json:"credsStore"`
	}

	if err := json.Unmarshal(data, &config); err != nil {
		return authn.Anonymous, err
	}

	// Normalize registry name for lookup
	registryKey := registry
	if registry == "docker.io" {
		registryKey = "https://index.docker.io/v1/"
	}

	// Try direct auth lookup
	if auth, exists := config.Auths[registryKey]; exists {
		if auth.Auth != "" {
			// Decode base64 auth
			decoded, err := base64.StdEncoding.DecodeString(auth.Auth)
			if err != nil {
				return authn.Anonymous, err
			}
			parts := strings.SplitN(string(decoded), ":", 2)
			if len(parts) == 2 {
				return &authn.Basic{
					Username: parts[0],
					Password: parts[1],
				}, nil
			}
		}
		if auth.Username != "" && auth.Password != "" {
			return &authn.Basic{
				Username: auth.Username,
				Password: auth.Password,
			}, nil
		}
	}

	// Try alternative registry keys
	alternativeKeys := []string{
		registry,
		"https://" + registry,
		"https://" + registry + "/v1/",
		"https://" + registry + "/v2/",
	}

	for _, key := range alternativeKeys {
		if auth, exists := config.Auths[key]; exists {
			if auth.Auth != "" {
				decoded, err := base64.StdEncoding.DecodeString(auth.Auth)
				if err != nil {
					continue
				}
				parts := strings.SplitN(string(decoded), ":", 2)
				if len(parts) == 2 {
					return &authn.Basic{
						Username: parts[0],
						Password: parts[1],
					}, nil
				}
			}
			if auth.Username != "" && auth.Password != "" {
				return &authn.Basic{
					Username: auth.Username,
					Password: auth.Password,
				}, nil
			}
		}
	}

	return authn.Anonymous, fmt.Errorf("no auth found for registry %s", registry)
}

// getFromKubernetesSecrets gets credentials from mounted Kubernetes secrets
func (a *AuthProvider) getFromKubernetesSecrets(registry string) (authn.Authenticator, error) {
	// Common paths where Kubernetes mounts secrets
	secretPaths := []string{
		"/var/run/secrets/registry",
		"/etc/registry-secrets",
		"/run/secrets/registry",
	}

	for _, secretPath := range secretPaths {
		if auth, err := a.readKubernetesSecret(secretPath, registry); err == nil {
			return auth, nil
		}
	}

	// Try reading from Docker config secret (common pattern)
	dockerConfigPath := "/var/run/secrets/kubernetes.io/dockerconfigjson/.dockerconfigjson"
	if auth, err := a.readDockerConfigSecret(dockerConfigPath, registry); err == nil {
		return auth, nil
	}

	return authn.Anonymous, fmt.Errorf("no Kubernetes secrets found")
}

// readKubernetesSecret reads credentials from a Kubernetes secret directory
func (a *AuthProvider) readKubernetesSecret(secretPath, registry string) (authn.Authenticator, error) {
	if _, err := os.Stat(secretPath); os.IsNotExist(err) {
		return authn.Anonymous, err
	}

	// Try to read username and password files
	username, err := os.ReadFile(filepath.Join(secretPath, "username"))
	if err != nil {
		return authn.Anonymous, err
	}

	password, err := os.ReadFile(filepath.Join(secretPath, "password"))
	if err != nil {
		// Try token instead
		token, tokenErr := os.ReadFile(filepath.Join(secretPath, "token"))
		if tokenErr != nil {
			return authn.Anonymous, tokenErr
		}
		return &authn.Bearer{Token: strings.TrimSpace(string(token))}, nil
	}

	return &authn.Basic{
		Username: strings.TrimSpace(string(username)),
		Password: strings.TrimSpace(string(password)),
	}, nil
}

// readDockerConfigSecret reads a Docker config from a Kubernetes secret
func (a *AuthProvider) readDockerConfigSecret(configPath, registry string) (authn.Authenticator, error) {
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return authn.Anonymous, err
	}

	return a.readDockerConfigFile(configPath, registry)
}

// CreateAuthenticatorFromCredentials creates an authenticator from explicit credentials
func CreateAuthenticatorFromCredentials(creds *Credentials) authn.Authenticator {
	if creds == nil {
		return authn.Anonymous
	}

	if creds.Username != "" && creds.Password != "" {
		return &authn.Basic{
			Username: creds.Username,
			Password: creds.Password,
		}
	}

	if creds.Token != "" {
		return &authn.Bearer{Token: creds.Token}
	}

	if creds.IdentityToken != "" {
		return &authn.Bearer{Token: creds.IdentityToken}
	}

	if creds.RegistryToken != "" {
		return &authn.Bearer{Token: creds.RegistryToken}
	}

	return authn.Anonymous
}

// DiscoverCredentials attempts to discover credentials for a registry using all available methods
func (a *AuthProvider) DiscoverCredentials(ctx context.Context, registry string) (*Credentials, error) {
	auth, err := a.GetAuthenticator(ctx, registry)
	if err != nil {
		return nil, err
	}

	if auth == authn.Anonymous {
		return &Credentials{}, nil
	}

	// Try to extract credentials from the authenticator
	// This is a bit hacky but necessary since the authn package doesn't expose credentials
	switch auth.(type) {
	case *authn.Basic:
		// We can't extract the credentials from Basic auth without reflection
		// Return empty credentials but indicate success
		return &Credentials{}, nil
	case *authn.Bearer:
		// We can't extract the token from Bearer auth without reflection
		// Return empty credentials but indicate success
		return &Credentials{}, nil
	default:
		return &Credentials{}, nil
	}
}

// ValidateCredentials tests if credentials work for a given registry
func (a *AuthProvider) ValidateCredentials(ctx context.Context, registry string, creds *Credentials) error {
	auth := CreateAuthenticatorFromCredentials(creds)
	
	// Create a test reference to validate against
	testRef := fmt.Sprintf("%s/library/hello-world:latest", registry)
	if registry == "docker.io" {
		testRef = "hello-world:latest"
	}

	ref, err := name.ParseReference(testRef)
	if err != nil {
		return fmt.Errorf("failed to parse test reference: %v", err)
	}

	// Try to get the registry info (this will test authentication)
	registryName, err := name.NewRegistry(registry)
	if err != nil {
		return fmt.Errorf("failed to create registry reference: %v", err)
	}

	// Use the go-containerregistry remote package to test auth
	// This is a lightweight operation that will fail fast if auth is wrong
	remoteOpts := []remote.Option{
		remote.WithAuth(auth),
		remote.WithContext(ctx),
	}
	
	// Try to get the manifest - this will validate authentication
	_, err = remote.Get(ref, remoteOpts...)
	if err != nil {
		return &RegistryError{
			Type:      ErrorTypeAuthentication,
			Operation: "validate_credentials",
			Registry:  registry,
			Message:   fmt.Sprintf("credential validation failed: %v", err),
			Cause:     err,
		}
	}

	_ = registryName // Use the variable to avoid unused error
	return nil
}

// LoadRegistryConfig loads registry configuration from various sources
func LoadRegistryConfig() (*RegistryConfig, error) {
	config := &RegistryConfig{
		Registries: make(map[string]*RegistryAuth),
		Insecure:   []string{},
		Mirrors:    make(map[string][]string),
	}

	// Try to load from environment variables
	if err := loadConfigFromEnv(config); err != nil {
		// Non-fatal, continue with other sources
	}

	// Try to load from config files
	configPaths := []string{
		os.Getenv("OSSB_REGISTRY_CONFIG"),
		filepath.Join(os.Getenv("HOME"), ".ossb", "registry.json"),
		"/etc/ossb/registry.json",
	}

	for _, configPath := range configPaths {
		if configPath == "" {
			continue
		}
		if err := loadConfigFromFile(config, configPath); err == nil {
			break
		}
	}

	return config, nil
}

// loadConfigFromEnv loads configuration from environment variables
func loadConfigFromEnv(config *RegistryConfig) error {
	if defaultReg := os.Getenv("OSSB_DEFAULT_REGISTRY"); defaultReg != "" {
		config.DefaultRegistry = defaultReg
	}

	if insecure := os.Getenv("OSSB_INSECURE_REGISTRIES"); insecure != "" {
		config.Insecure = strings.Split(insecure, ",")
	}

	return nil
}

// loadConfigFromFile loads configuration from a JSON file
func loadConfigFromFile(config *RegistryConfig, configPath string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}

	return json.Unmarshal(data, config)
}