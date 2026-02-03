package provider

import (
	"strings"
	"sync"
)

// defaultRegistry is the global singleton registry
var defaultRegistry *Registry

// DefaultRegistry returns the global provider registry, initializing it on first call
// The registry is pre-populated with Replicate and Generic providers
func DefaultRegistry() *Registry {
	if defaultRegistry == nil {
		defaultRegistry = NewRegistry()
		// Note: providers are registered by init() functions in their respective packages
		// via RegisterProvider(), or can be set up explicitly
	}
	return defaultRegistry
}

// RegisterProvider adds a provider to the default registry
// This should be called from init() functions in provider packages
func RegisterProvider(p Provider) {
	DefaultRegistry().Register(p)
}

// Registry manages provider lookup and registration
type Registry struct {
	providers []Provider
	mu        sync.RWMutex
}

// NewRegistry creates a new Registry with no providers registered
func NewRegistry() *Registry {
	return &Registry{
		providers: make([]Provider, 0),
	}
}

// Register adds a provider to the registry
// Providers are checked in registration order, so register more specific
// providers before generic fallback providers
func (r *Registry) Register(p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers = append(r.providers, p)
}

// ForImage returns the provider for a given image name
// It extracts the registry host from the image and delegates to ForHost
func (r *Registry) ForImage(image string) Provider {
	host := ExtractHost(image)
	return r.ForHost(host)
}

// ForHost returns the provider for a given registry host
// Returns the first provider that matches, or nil if none match
func (r *Registry) ForHost(host string) Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, p := range r.providers {
		if p.MatchesRegistry(host) {
			return p
		}
	}
	return nil
}

// ExtractHost extracts the registry host from an image name
// Examples:
//   - "r8.im/user/model" -> "r8.im"
//   - "ghcr.io/owner/repo:tag" -> "ghcr.io"
//   - "gcr.io/project/image" -> "gcr.io"
//   - "docker.io/library/nginx" -> "docker.io"
//   - "nginx" -> "docker.io" (Docker Hub default)
//   - "myregistry.com:5000/image" -> "myregistry.com:5000"
//   - "localhost:5000/image" -> "localhost:5000"
func ExtractHost(image string) string {
	// Handle empty image
	if image == "" {
		return "docker.io"
	}

	// Remove digest first (@sha256:...)
	if idx := strings.Index(image, "@"); idx != -1 {
		image = image[:idx]
	}

	// Get the first component (everything before the first slash)
	// If there's no slash, it's a Docker Hub image (e.g., "nginx" or "nginx:latest")
	firstComponent, _, found := strings.Cut(image, "/")
	if !found {
		return "docker.io"
	}

	// Check if it looks like a registry host:
	// - Contains a dot (e.g., gcr.io, ghcr.io, r8.im, myregistry.com)
	// - Contains a colon (e.g., localhost:5000, myregistry.com:5000)
	// - Is "localhost"
	if strings.Contains(firstComponent, ".") ||
		strings.Contains(firstComponent, ":") ||
		firstComponent == "localhost" {
		return firstComponent
	}

	// Otherwise it's a Docker Hub user/image (e.g., "user/image")
	return "docker.io"
}
