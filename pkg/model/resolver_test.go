package model

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/registry"
)

// mockDocker implements command.Command for testing.
type mockDocker struct {
	inspectFunc func(ctx context.Context, ref string) (*image.InspectResponse, error)
}

func (m *mockDocker) Inspect(ctx context.Context, ref string) (*image.InspectResponse, error) {
	if m.inspectFunc != nil {
		return m.inspectFunc(ctx, ref)
	}
	return nil, errors.New("not implemented")
}

// Implement other command.Command methods as panics (not needed for resolver tests).
func (m *mockDocker) Pull(ctx context.Context, ref string, force bool) (*image.InspectResponse, error) {
	panic("not implemented")
}

func (m *mockDocker) Push(ctx context.Context, ref string) error {
	panic("not implemented")
}

func (m *mockDocker) LoadUserInformation(ctx context.Context, registryHost string) (*command.UserInfo, error) {
	panic("not implemented")
}

func (m *mockDocker) ImageExists(ctx context.Context, ref string) (bool, error) {
	panic("not implemented")
}

func (m *mockDocker) ContainerLogs(ctx context.Context, containerID string, w io.Writer) error {
	panic("not implemented")
}

func (m *mockDocker) ContainerInspect(ctx context.Context, id string) (*container.InspectResponse, error) {
	panic("not implemented")
}

func (m *mockDocker) ContainerStop(ctx context.Context, containerID string) error {
	panic("not implemented")
}

func (m *mockDocker) ImageBuild(ctx context.Context, options command.ImageBuildOptions) (string, error) {
	panic("not implemented")
}

func (m *mockDocker) Run(ctx context.Context, options command.RunOptions) error {
	panic("not implemented")
}

func (m *mockDocker) ContainerStart(ctx context.Context, options command.RunOptions) (string, error) {
	panic("not implemented")
}

// mockRegistry implements registry.Client for testing.
type mockRegistry struct {
	inspectFunc func(ctx context.Context, ref string, platform *registry.Platform) (*registry.ManifestResult, error)
}

func (m *mockRegistry) Inspect(ctx context.Context, ref string, platform *registry.Platform) (*registry.ManifestResult, error) {
	if m.inspectFunc != nil {
		return m.inspectFunc(ctx, ref, platform)
	}
	return nil, errors.New("not implemented")
}

func (m *mockRegistry) GetImage(ctx context.Context, ref string, platform *registry.Platform) (v1.Image, error) {
	panic("not implemented")
}

func (m *mockRegistry) Exists(ctx context.Context, ref string) (bool, error) {
	panic("not implemented")
}

// mockFactory implements Factory for testing.
type mockFactory struct {
	name      string
	buildFunc func(ctx context.Context, src *Source, opts BuildOptions) (*Image, error)
}

func (f *mockFactory) Build(ctx context.Context, src *Source, opts BuildOptions) (*Image, error) {
	if f.buildFunc != nil {
		return f.buildFunc(ctx, src, opts)
	}
	return &Image{Reference: opts.ImageName, Source: ImageSourceBuild}, nil
}

func (f *mockFactory) Name() string {
	return f.name
}

func TestNewResolver(t *testing.T) {
	docker := &mockDocker{}
	reg := &mockRegistry{}

	resolver := NewResolver(docker, reg)

	require.NotNil(t, resolver)
	require.Equal(t, "dockerfile", resolver.factory.Name())
}

func TestResolver_WithFactory(t *testing.T) {
	docker := &mockDocker{}
	reg := &mockRegistry{}

	resolver := NewResolver(docker, reg)
	require.Equal(t, "dockerfile", resolver.factory.Name())

	customFactory := &mockFactory{name: "custom"}
	result := resolver.WithFactory(customFactory)

	// WithFactory returns the same resolver for chaining
	require.Same(t, resolver, result)
	require.Equal(t, "custom", resolver.factory.Name())
}

func TestResolver_Load_LocalOnly_Found(t *testing.T) {
	docker := &mockDocker{
		inspectFunc: func(ctx context.Context, ref string) (*image.InspectResponse, error) {
			return &image.InspectResponse{
				ID: "sha256:abc123",
				Config: &container.Config{
					Labels: map[string]string{
						LabelConfig:  `{"build":{"python_version":"3.11"}}`,
						LabelVersion: "0.10.0",
					},
				},
			}, nil
		},
	}
	reg := &mockRegistry{}

	resolver := NewResolver(docker, reg)
	ref, err := ParseRef("my-image:latest")
	require.NoError(t, err)

	model, err := resolver.Load(context.Background(), ref, LocalOnly())

	require.NoError(t, err)
	require.NotNil(t, model)
	require.Equal(t, ImageSourceLocal, model.Image.Source)
	require.Equal(t, "0.10.0", model.CogVersion)
	require.NotNil(t, model.Config)
	require.Equal(t, "3.11", model.Config.Build.PythonVersion)
}

func TestResolver_Load_LocalOnly_NotFound(t *testing.T) {
	docker := &mockDocker{
		inspectFunc: func(ctx context.Context, ref string) (*image.InspectResponse, error) {
			return nil, errors.New("No such image: my-image:latest")
		},
	}
	reg := &mockRegistry{
		inspectFunc: func(ctx context.Context, ref string, platform *registry.Platform) (*registry.ManifestResult, error) {
			t.Fatal("should not call registry when LocalOnly")
			return nil, nil
		},
	}

	resolver := NewResolver(docker, reg)
	ref, err := ParseRef("my-image:latest")
	require.NoError(t, err)

	_, err = resolver.Load(context.Background(), ref, LocalOnly())

	require.Error(t, err)
	require.Contains(t, err.Error(), "not found locally")
}

func TestResolver_Load_RemoteOnly_Found(t *testing.T) {
	docker := &mockDocker{
		inspectFunc: func(ctx context.Context, ref string) (*image.InspectResponse, error) {
			t.Fatal("should not call docker.Inspect when RemoteOnly")
			return nil, nil
		},
	}
	reg := &mockRegistry{
		inspectFunc: func(ctx context.Context, ref string, platform *registry.Platform) (*registry.ManifestResult, error) {
			return &registry.ManifestResult{
				SchemaVersion: 2,
				MediaType:     "application/vnd.docker.distribution.manifest.v2+json",
				Config:        "sha256:configdigest",
			}, nil
		},
	}

	resolver := NewResolver(docker, reg)
	ref, err := ParseRef("r8.im/user/model")
	require.NoError(t, err)

	model, err := resolver.Load(context.Background(), ref, RemoteOnly())

	require.NoError(t, err)
	require.NotNil(t, model)
	require.Equal(t, ImageSourceRemote, model.Image.Source)
}

func TestResolver_Load_RemoteOnly_NotFound(t *testing.T) {
	docker := &mockDocker{}
	reg := &mockRegistry{
		inspectFunc: func(ctx context.Context, ref string, platform *registry.Platform) (*registry.ManifestResult, error) {
			return nil, errors.New("manifest unknown")
		},
	}

	resolver := NewResolver(docker, reg)
	ref, err := ParseRef("r8.im/user/model")
	require.NoError(t, err)

	_, err = resolver.Load(context.Background(), ref, RemoteOnly())

	require.Error(t, err)
	require.Contains(t, err.Error(), "not found in registry")
}

func TestResolver_Load_PreferLocal_FoundLocally(t *testing.T) {
	localCalled := false
	remoteCalled := false

	docker := &mockDocker{
		inspectFunc: func(ctx context.Context, ref string) (*image.InspectResponse, error) {
			localCalled = true
			return &image.InspectResponse{
				ID: "sha256:local123",
				Config: &container.Config{
					Labels: map[string]string{
						LabelVersion: "0.9.0",
					},
				},
			}, nil
		},
	}
	reg := &mockRegistry{
		inspectFunc: func(ctx context.Context, ref string, platform *registry.Platform) (*registry.ManifestResult, error) {
			remoteCalled = true
			return nil, nil
		},
	}

	resolver := NewResolver(docker, reg)
	ref, err := ParseRef("my-image:latest")
	require.NoError(t, err)

	model, err := resolver.Load(context.Background(), ref) // default is PreferLocal

	require.NoError(t, err)
	require.True(t, localCalled, "should try local first")
	require.False(t, remoteCalled, "should not call remote when local succeeds")
	require.Equal(t, ImageSourceLocal, model.Image.Source)
}

func TestResolver_Load_PreferLocal_Fallback(t *testing.T) {
	localCalled := false
	remoteCalled := false

	docker := &mockDocker{
		inspectFunc: func(ctx context.Context, ref string) (*image.InspectResponse, error) {
			localCalled = true
			return nil, errors.New("No such image")
		},
	}
	reg := &mockRegistry{
		inspectFunc: func(ctx context.Context, ref string, platform *registry.Platform) (*registry.ManifestResult, error) {
			remoteCalled = true
			return &registry.ManifestResult{
				SchemaVersion: 2,
			}, nil
		},
	}

	resolver := NewResolver(docker, reg)
	ref, err := ParseRef("my-image:latest")
	require.NoError(t, err)

	model, err := resolver.Load(context.Background(), ref) // default is PreferLocal

	require.NoError(t, err)
	require.True(t, localCalled, "should try local first")
	require.True(t, remoteCalled, "should fall back to remote")
	require.Equal(t, ImageSourceRemote, model.Image.Source)
}

func TestResolver_Load_PreferLocal_NoFallbackOnRealError(t *testing.T) {
	docker := &mockDocker{
		inspectFunc: func(ctx context.Context, ref string) (*image.InspectResponse, error) {
			return nil, errors.New("connection refused") // Real error, not "not found"
		},
	}
	reg := &mockRegistry{
		inspectFunc: func(ctx context.Context, ref string, platform *registry.Platform) (*registry.ManifestResult, error) {
			t.Fatal("should not fall back to remote on real error")
			return nil, nil
		},
	}

	resolver := NewResolver(docker, reg)
	ref, err := ParseRef("my-image:latest")
	require.NoError(t, err)

	_, err = resolver.Load(context.Background(), ref)

	require.Error(t, err)
	require.Contains(t, err.Error(), "connection refused")
}

func TestResolver_Load_PreferRemote_FoundRemotely(t *testing.T) {
	localCalled := false
	remoteCalled := false

	docker := &mockDocker{
		inspectFunc: func(ctx context.Context, ref string) (*image.InspectResponse, error) {
			localCalled = true
			return nil, nil
		},
	}
	reg := &mockRegistry{
		inspectFunc: func(ctx context.Context, ref string, platform *registry.Platform) (*registry.ManifestResult, error) {
			remoteCalled = true
			return &registry.ManifestResult{
				SchemaVersion: 2,
			}, nil
		},
	}

	resolver := NewResolver(docker, reg)
	ref, err := ParseRef("my-image:latest")
	require.NoError(t, err)

	model, err := resolver.Load(context.Background(), ref, PreferRemote())

	require.NoError(t, err)
	require.False(t, localCalled, "should not try local when remote succeeds")
	require.True(t, remoteCalled, "should try remote first")
	require.Equal(t, ImageSourceRemote, model.Image.Source)
}

func TestResolver_Load_PreferRemote_Fallback(t *testing.T) {
	localCalled := false
	remoteCalled := false

	docker := &mockDocker{
		inspectFunc: func(ctx context.Context, ref string) (*image.InspectResponse, error) {
			localCalled = true
			return &image.InspectResponse{
				ID: "sha256:local123",
				Config: &container.Config{
					Labels: map[string]string{},
				},
			}, nil
		},
	}
	reg := &mockRegistry{
		inspectFunc: func(ctx context.Context, ref string, platform *registry.Platform) (*registry.ManifestResult, error) {
			remoteCalled = true
			return nil, errors.New("manifest unknown")
		},
	}

	resolver := NewResolver(docker, reg)
	ref, err := ParseRef("my-image:latest")
	require.NoError(t, err)

	model, err := resolver.Load(context.Background(), ref, PreferRemote())

	require.NoError(t, err)
	require.True(t, remoteCalled, "should try remote first")
	require.True(t, localCalled, "should fall back to local")
	require.Equal(t, ImageSourceLocal, model.Image.Source)
}

func TestResolver_Load_PreferRemote_NoFallbackOnRealError(t *testing.T) {
	docker := &mockDocker{
		inspectFunc: func(ctx context.Context, ref string) (*image.InspectResponse, error) {
			t.Fatal("should not fall back to local on real error")
			return nil, nil
		},
	}
	reg := &mockRegistry{
		inspectFunc: func(ctx context.Context, ref string, platform *registry.Platform) (*registry.ManifestResult, error) {
			return nil, errors.New("authentication required")
		},
	}

	resolver := NewResolver(docker, reg)
	ref, err := ParseRef("my-image:latest")
	require.NoError(t, err)

	_, err = resolver.Load(context.Background(), ref, PreferRemote())

	require.Error(t, err)
	require.Contains(t, err.Error(), "authentication required")
}

func TestResolver_Load_WithPlatform(t *testing.T) {
	var capturedPlatform *registry.Platform

	docker := &mockDocker{}
	reg := &mockRegistry{
		inspectFunc: func(ctx context.Context, ref string, platform *registry.Platform) (*registry.ManifestResult, error) {
			capturedPlatform = platform
			return &registry.ManifestResult{
				SchemaVersion: 2,
			}, nil
		},
	}

	resolver := NewResolver(docker, reg)
	ref, err := ParseRef("my-image")
	require.NoError(t, err)

	platform := &registry.Platform{OS: "linux", Architecture: "amd64"}
	_, err = resolver.Load(context.Background(), ref, RemoteOnly(), WithPlatform(platform))

	require.NoError(t, err)
	require.NotNil(t, capturedPlatform)
	require.Equal(t, "linux", capturedPlatform.OS)
	require.Equal(t, "amd64", capturedPlatform.Architecture)
}

func TestResolver_Load_ParsesConfigFromLabels(t *testing.T) {
	docker := &mockDocker{
		inspectFunc: func(ctx context.Context, ref string) (*image.InspectResponse, error) {
			return &image.InspectResponse{
				ID: "sha256:abc123",
				Config: &container.Config{
					Labels: map[string]string{
						LabelConfig:  `{"build":{"gpu":true,"python_version":"3.12"},"predict":"predict.py:Predictor"}`,
						LabelVersion: "0.11.0",
					},
				},
			}, nil
		},
	}

	resolver := NewResolver(docker, &mockRegistry{})
	ref, err := ParseRef("my-image:latest")
	require.NoError(t, err)

	model, err := resolver.Load(context.Background(), ref, LocalOnly())

	require.NoError(t, err)
	require.NotNil(t, model.Config)
	require.NotNil(t, model.Config.Build)
	require.True(t, model.Config.Build.GPU)
	require.Equal(t, "3.12", model.Config.Build.PythonVersion)
	require.Equal(t, "predict.py:Predictor", model.Config.Predict)
}

func TestResolver_Load_InvalidConfigJSON(t *testing.T) {
	docker := &mockDocker{
		inspectFunc: func(ctx context.Context, ref string) (*image.InspectResponse, error) {
			return &image.InspectResponse{
				ID: "sha256:abc123",
				Config: &container.Config{
					Labels: map[string]string{
						LabelConfig: `{invalid json`,
					},
				},
			}, nil
		},
	}

	resolver := NewResolver(docker, &mockRegistry{})
	ref, err := ParseRef("my-image:latest")
	require.NoError(t, err)

	_, err = resolver.Load(context.Background(), ref, LocalOnly())

	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to parse cog config")
}

func TestResolver_Load_NoConfigLabel(t *testing.T) {
	docker := &mockDocker{
		inspectFunc: func(ctx context.Context, ref string) (*image.InspectResponse, error) {
			return &image.InspectResponse{
				ID: "sha256:abc123",
				Config: &container.Config{
					Labels: map[string]string{
						// No LabelConfig
						LabelVersion: "0.10.0",
					},
				},
			}, nil
		},
	}

	resolver := NewResolver(docker, &mockRegistry{})
	ref, err := ParseRef("my-image:latest")
	require.NoError(t, err)

	model, err := resolver.Load(context.Background(), ref, LocalOnly())

	require.NoError(t, err)
	require.Nil(t, model.Config) // Config should be nil when label is missing
	require.Equal(t, "0.10.0", model.CogVersion)
}

func TestResolver_LoadByID_Found(t *testing.T) {
	docker := &mockDocker{
		inspectFunc: func(ctx context.Context, ref string) (*image.InspectResponse, error) {
			// Verify the ID was passed directly (not mangled by ParseRef)
			require.Equal(t, "9056219a5fb2", ref)
			return &image.InspectResponse{
				ID: "sha256:9056219a5fb2abc123def456",
				Config: &container.Config{
					Labels: map[string]string{
						LabelConfig:  `{"build":{"python_version":"3.11"}}`,
						LabelVersion: "0.10.0",
					},
				},
			}, nil
		},
	}

	resolver := NewResolver(docker, &mockRegistry{})

	model, err := resolver.LoadByID(context.Background(), "9056219a5fb2")

	require.NoError(t, err)
	require.NotNil(t, model)
	require.Equal(t, ImageSourceLocal, model.Image.Source)
	require.Equal(t, "sha256:9056219a5fb2abc123def456", model.Image.Digest)
	require.Equal(t, "sha256:9056219a5fb2abc123def456", model.Image.Reference)
	require.Equal(t, "0.10.0", model.CogVersion)
	require.NotNil(t, model.Config)
	require.Equal(t, "3.11", model.Config.Build.PythonVersion)
}

func TestResolver_LoadByID_FullSHA(t *testing.T) {
	fullID := "sha256:9056219a5fb2abc123def456789"
	docker := &mockDocker{
		inspectFunc: func(ctx context.Context, ref string) (*image.InspectResponse, error) {
			require.Equal(t, fullID, ref)
			return &image.InspectResponse{
				ID: fullID,
				Config: &container.Config{
					Labels: map[string]string{},
				},
			}, nil
		},
	}

	resolver := NewResolver(docker, &mockRegistry{})

	model, err := resolver.LoadByID(context.Background(), fullID)

	require.NoError(t, err)
	require.NotNil(t, model)
	require.Equal(t, fullID, model.Image.Digest)
}

func TestResolver_LoadByID_NotFound(t *testing.T) {
	docker := &mockDocker{
		inspectFunc: func(ctx context.Context, ref string) (*image.InspectResponse, error) {
			return nil, errors.New("No such image: abc123")
		},
	}

	resolver := NewResolver(docker, &mockRegistry{})

	_, err := resolver.LoadByID(context.Background(), "abc123")

	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to load image by ID")
}

func TestIsNotFoundError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "No such image",
			err:      errors.New("No such image: my-image:latest"),
			expected: true,
		},
		{
			name:     "not found",
			err:      errors.New("image not found"),
			expected: true,
		},
		{
			name:     "manifest unknown",
			err:      errors.New("manifest unknown: repository does not exist"),
			expected: true,
		},
		{
			name:     "NAME_UNKNOWN",
			err:      errors.New("NAME_UNKNOWN: repository name not known to registry"),
			expected: true,
		},
		{
			name:     "connection refused",
			err:      errors.New("connection refused"),
			expected: false,
		},
		{
			name:     "authentication required",
			err:      errors.New("authentication required"),
			expected: false,
		},
		{
			name:     "context canceled",
			err:      context.Canceled,
			expected: false,
		},
		{
			name:     "context deadline exceeded",
			err:      context.DeadlineExceeded,
			expected: false,
		},
		{
			name:     "registry NotFoundError",
			err:      registry.NotFoundError,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isNotFoundError(tt.err)
			require.Equal(t, tt.expected, result)
		})
	}
}
