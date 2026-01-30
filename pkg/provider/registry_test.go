package provider

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/config"
)

// mockProvider implements Provider for testing
type mockProvider struct {
	name    string
	matches func(host string) bool
}

func (m *mockProvider) Name() string {
	return m.name
}

func (m *mockProvider) MatchesRegistry(host string) bool {
	return m.matches(host)
}

func (m *mockProvider) Login(ctx context.Context, registryHost string) error {
	return nil
}

func (m *mockProvider) PrePush(ctx context.Context, image string, cfg *config.Config) error {
	return nil
}

func (m *mockProvider) PostPush(ctx context.Context, image string, cfg *config.Config, pushErr error) error {
	return nil
}

func TestRegistry_ForHost(t *testing.T) {
	r := NewRegistry()

	replicateProvider := &mockProvider{
		name:    "replicate",
		matches: func(host string) bool { return host == "r8.im" },
	}
	genericProvider := &mockProvider{
		name:    "generic",
		matches: func(host string) bool { return true },
	}

	// Register replicate first (more specific), then generic (fallback)
	r.Register(replicateProvider)
	r.Register(genericProvider)

	t.Run("matches replicate", func(t *testing.T) {
		p := r.ForHost("r8.im")
		require.NotNil(t, p)
		require.Equal(t, "replicate", p.Name())
	})

	t.Run("falls back to generic", func(t *testing.T) {
		p := r.ForHost("ghcr.io")
		require.NotNil(t, p)
		require.Equal(t, "generic", p.Name())
	})

	t.Run("empty host falls back to generic", func(t *testing.T) {
		p := r.ForHost("")
		require.NotNil(t, p)
		require.Equal(t, "generic", p.Name())
	})
}

func TestRegistry_ForImage(t *testing.T) {
	r := NewRegistry()

	replicateProvider := &mockProvider{
		name:    "replicate",
		matches: func(host string) bool { return host == "r8.im" },
	}
	genericProvider := &mockProvider{
		name:    "generic",
		matches: func(host string) bool { return true },
	}

	r.Register(replicateProvider)
	r.Register(genericProvider)

	tests := []struct {
		image        string
		expectedName string
	}{
		{"r8.im/user/model", "replicate"},
		{"r8.im/user/model:v1", "replicate"},
		{"ghcr.io/owner/repo", "generic"},
		{"gcr.io/project/image:tag", "generic"},
		{"docker.io/library/nginx", "generic"},
		{"nginx", "generic"},
		{"myregistry.com/image", "generic"},
	}

	for _, tt := range tests {
		t.Run(tt.image, func(t *testing.T) {
			p := r.ForImage(tt.image)
			require.NotNil(t, p)
			require.Equal(t, tt.expectedName, p.Name())
		})
	}
}

func TestRegistry_NoProviders(t *testing.T) {
	r := NewRegistry()

	p := r.ForHost("any.registry.io")
	require.Nil(t, p)
}

func TestExtractHost(t *testing.T) {
	tests := []struct {
		image    string
		expected string
	}{
		// Replicate
		{"r8.im/user/model", "r8.im"},
		{"r8.im/user/model:v1", "r8.im"},
		{"r8.im/user/model:latest", "r8.im"},

		// GitHub Container Registry
		{"ghcr.io/owner/repo", "ghcr.io"},
		{"ghcr.io/owner/repo:tag", "ghcr.io"},

		// Google Container Registry
		{"gcr.io/project/image", "gcr.io"},
		{"gcr.io/project/image:tag", "gcr.io"},
		{"us.gcr.io/project/image", "us.gcr.io"},

		// Docker Hub explicit
		{"docker.io/library/nginx", "docker.io"},
		{"docker.io/user/image", "docker.io"},

		// Docker Hub implicit (no registry specified)
		{"nginx", "docker.io"},
		{"nginx:latest", "docker.io"},
		{"user/image", "docker.io"},
		{"user/image:tag", "docker.io"},

		// Custom registries
		{"myregistry.com/image", "myregistry.com"},
		{"myregistry.example.com/path/to/image", "myregistry.example.com"},

		// Registries with ports
		{"localhost:5000/image", "localhost:5000"},
		{"myregistry.com:5000/image", "myregistry.com:5000"},
		{"myregistry.com:5000/image:tag", "myregistry.com:5000"},

		// With digest
		{"ghcr.io/owner/repo@sha256:abc123", "ghcr.io"},

		// Edge cases
		{"", "docker.io"},
		{"localhost/image", "localhost"},
	}

	for _, tt := range tests {
		t.Run(tt.image, func(t *testing.T) {
			result := ExtractHost(tt.image)
			require.Equal(t, tt.expected, result, "ExtractHost(%q)", tt.image)
		})
	}
}
