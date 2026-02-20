package model

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	dockerspec "github.com/moby/docker-image-spec/specs-go/v1"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/registry"
)

// mockDocker implements command.Command for testing.
type mockDocker struct {
	inspectFunc func(ctx context.Context, ref string) (*image.InspectResponse, error)
	pullFunc    func(ctx context.Context, ref string, force bool) (*image.InspectResponse, error)
	pushFunc    func(ctx context.Context, ref string) error
}

func (m *mockDocker) Inspect(ctx context.Context, ref string) (*image.InspectResponse, error) {
	if m.inspectFunc != nil {
		return m.inspectFunc(ctx, ref)
	}
	return nil, errors.New("not implemented")
}

func (m *mockDocker) Pull(ctx context.Context, ref string, force bool) (*image.InspectResponse, error) {
	if m.pullFunc != nil {
		return m.pullFunc(ctx, ref, force)
	}
	return nil, errors.New("mockDocker.Pull not implemented")
}

func (m *mockDocker) Push(ctx context.Context, ref string) error {
	if m.pushFunc != nil {
		return m.pushFunc(ctx, ref)
	}
	return errors.New("mockDocker.Push not implemented")
}

func (m *mockDocker) LoadUserInformation(ctx context.Context, registryHost string) (*command.UserInfo, error) {
	return nil, errors.New("mockDocker.LoadUserInformation not implemented")
}

func (m *mockDocker) ImageExists(ctx context.Context, ref string) (bool, error) {
	return false, errors.New("mockDocker.ImageExists not implemented")
}

func (m *mockDocker) ContainerLogs(ctx context.Context, containerID string, w io.Writer) error {
	return errors.New("mockDocker.ContainerLogs not implemented")
}

func (m *mockDocker) ContainerInspect(ctx context.Context, id string) (*container.InspectResponse, error) {
	return nil, errors.New("mockDocker.ContainerInspect not implemented")
}

func (m *mockDocker) ContainerStop(ctx context.Context, containerID string) error {
	return errors.New("mockDocker.ContainerStop not implemented")
}

func (m *mockDocker) RemoveImage(ctx context.Context, ref string) error {
	return errors.New("mockDocker.RemoveImage not implemented")
}

func (m *mockDocker) ImageBuild(ctx context.Context, options command.ImageBuildOptions) (string, error) {
	return "", errors.New("mockDocker.ImageBuild not implemented")
}

func (m *mockDocker) Run(ctx context.Context, options command.RunOptions) error {
	return errors.New("mockDocker.Run not implemented")
}

func (m *mockDocker) ContainerStart(ctx context.Context, options command.RunOptions) (string, error) {
	return "", errors.New("mockDocker.ContainerStart not implemented")
}

// mockRegistry implements registry.Client for testing.
type mockRegistry struct {
	inspectFunc       func(ctx context.Context, ref string, platform *registry.Platform) (*registry.ManifestResult, error)
	getImageFunc      func(ctx context.Context, ref string, platform *registry.Platform) (v1.Image, error)
	getDescriptorFunc func(ctx context.Context, ref string) (v1.Descriptor, error)
	pushImageFunc     func(ctx context.Context, ref string, img v1.Image) error
	pushIndexFunc     func(ctx context.Context, ref string, idx v1.ImageIndex) error
	writeLayerFunc    func(ctx context.Context, opts registry.WriteLayerOptions) error
}

func (m *mockRegistry) Inspect(ctx context.Context, ref string, platform *registry.Platform) (*registry.ManifestResult, error) {
	if m.inspectFunc != nil {
		return m.inspectFunc(ctx, ref, platform)
	}
	return nil, registry.NotFoundError
}

func (m *mockRegistry) GetImage(ctx context.Context, ref string, platform *registry.Platform) (v1.Image, error) {
	if m.getImageFunc != nil {
		return m.getImageFunc(ctx, ref, platform)
	}
	return nil, errors.New("mockRegistry.GetImage not implemented")
}

func (m *mockRegistry) GetDescriptor(ctx context.Context, ref string) (v1.Descriptor, error) {
	if m.getDescriptorFunc != nil {
		return m.getDescriptorFunc(ctx, ref)
	}
	return v1.Descriptor{}, errors.New("mockRegistry.GetDescriptor not implemented")
}

func (m *mockRegistry) Exists(ctx context.Context, ref string) (bool, error) {
	return false, errors.New("mockRegistry.Exists not implemented")
}

func (m *mockRegistry) PushImage(ctx context.Context, ref string, img v1.Image) error {
	if m.pushImageFunc != nil {
		return m.pushImageFunc(ctx, ref, img)
	}
	return errors.New("mockRegistry.PushImage not implemented")
}

func (m *mockRegistry) PushIndex(ctx context.Context, ref string, idx v1.ImageIndex) error {
	if m.pushIndexFunc != nil {
		return m.pushIndexFunc(ctx, ref, idx)
	}
	return errors.New("mockRegistry.PushIndex not implemented")
}

func (m *mockRegistry) WriteLayer(ctx context.Context, opts registry.WriteLayerOptions) error {
	if m.writeLayerFunc != nil {
		return m.writeLayerFunc(ctx, opts)
	}
	// Default: no-op. The caller (WeightPusher) owns closing ProgressCh.
	return nil
}

// mockFactory implements Factory for testing.
type mockFactory struct {
	name      string
	buildFunc func(ctx context.Context, src *Source, opts BuildOptions) (*ImageArtifact, error)
}

func (f *mockFactory) Build(ctx context.Context, src *Source, opts BuildOptions) (*ImageArtifact, error) {
	if f.buildFunc != nil {
		return f.buildFunc(ctx, src, opts)
	}
	return &ImageArtifact{Reference: opts.ImageName, Source: ImageSourceBuild}, nil
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

func TestResolver_Inspect_LocalOnly_Found(t *testing.T) {
	docker := &mockDocker{
		inspectFunc: func(ctx context.Context, ref string) (*image.InspectResponse, error) {
			return &image.InspectResponse{
				ID: "sha256:abc123",
				Config: &dockerspec.DockerOCIImageConfig{
					ImageConfig: ocispec.ImageConfig{
						Labels: map[string]string{

							LabelConfig:  `{"build":{"python_version":"3.11"}}`,
							LabelVersion: "0.10.0",
						},
					},
				},
			}, nil
		},
	}
	reg := &mockRegistry{}

	resolver := NewResolver(docker, reg)
	ref, err := ParseRef("my-image:latest")
	require.NoError(t, err)

	model, err := resolver.Inspect(context.Background(), ref, LocalOnly())

	require.NoError(t, err)
	require.NotNil(t, model)
	require.Equal(t, ImageSourceLocal, model.Image.Source)
	require.Equal(t, "0.10.0", model.CogVersion)
	require.NotNil(t, model.Config)
	require.Equal(t, "3.11", model.Config.Build.PythonVersion)
}

func TestResolver_Inspect_LocalOnly_NotFound(t *testing.T) {
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

	_, err = resolver.Inspect(context.Background(), ref, LocalOnly())

	require.Error(t, err)
	require.Contains(t, err.Error(), "not found locally")
}

func TestResolver_Inspect_RemoteOnly_Found(t *testing.T) {
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
				Labels: map[string]string{
					LabelConfig:  `{"build":{"python_version":"3.11"}}`,
					LabelVersion: "0.10.0",
				},
			}, nil
		},
	}

	resolver := NewResolver(docker, reg)
	ref, err := ParseRef("r8.im/user/model")
	require.NoError(t, err)

	model, err := resolver.Inspect(context.Background(), ref, RemoteOnly())

	require.NoError(t, err)
	require.NotNil(t, model)
	require.Equal(t, ImageSourceRemote, model.Image.Source)
	require.Equal(t, "0.10.0", model.CogVersion)
}

func TestResolver_Inspect_RemoteOnly_NotFound(t *testing.T) {
	docker := &mockDocker{}
	reg := &mockRegistry{
		inspectFunc: func(ctx context.Context, ref string, platform *registry.Platform) (*registry.ManifestResult, error) {
			return nil, registry.NotFoundError
		},
	}

	resolver := NewResolver(docker, reg)
	ref, err := ParseRef("r8.im/user/model")
	require.NoError(t, err)

	_, err = resolver.Inspect(context.Background(), ref, RemoteOnly())

	require.Error(t, err)
	require.Contains(t, err.Error(), "not found in registry")
}

func TestResolver_Inspect_RemoteOnly_NotCogModel(t *testing.T) {
	docker := &mockDocker{}
	reg := &mockRegistry{
		inspectFunc: func(ctx context.Context, ref string, platform *registry.Platform) (*registry.ManifestResult, error) {
			return &registry.ManifestResult{
				SchemaVersion: 2,
				MediaType:     "application/vnd.docker.distribution.manifest.v2+json",
				Config:        "sha256:configdigest",
				Labels: map[string]string{
					// No Cog labels - just a regular image
					"maintainer": "someone@example.com",
				},
			}, nil
		},
	}

	resolver := NewResolver(docker, reg)
	ref, err := ParseRef("nginx:latest")
	require.NoError(t, err)

	_, err = resolver.Inspect(context.Background(), ref, RemoteOnly())

	require.Error(t, err)
	require.ErrorIs(t, err, ErrNotCogModel)
}

func TestResolver_Inspect_PreferLocal_FoundLocally(t *testing.T) {
	localCalled := false
	remoteCalled := false

	docker := &mockDocker{
		inspectFunc: func(ctx context.Context, ref string) (*image.InspectResponse, error) {
			localCalled = true
			return &image.InspectResponse{
				ID: "sha256:local123",
				Config: &dockerspec.DockerOCIImageConfig{
					ImageConfig: ocispec.ImageConfig{
						Labels: map[string]string{

							LabelConfig:  `{"build":{"python_version":"3.11"}}`,
							LabelVersion: "0.9.0",
						},
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

	model, err := resolver.Inspect(context.Background(), ref, PreferLocal())

	require.NoError(t, err)
	require.True(t, localCalled, "should try local first")
	require.False(t, remoteCalled, "should not call remote when local succeeds")
	require.Equal(t, ImageSourceLocal, model.Image.Source)
}

func TestResolver_Inspect_PreferLocal_Fallback(t *testing.T) {
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
				Labels: map[string]string{
					LabelConfig:  `{"build":{"python_version":"3.11"}}`,
					LabelVersion: "0.10.0",
				},
			}, nil
		},
	}

	resolver := NewResolver(docker, reg)
	ref, err := ParseRef("my-image:latest")
	require.NoError(t, err)

	model, err := resolver.Inspect(context.Background(), ref, PreferLocal())

	require.NoError(t, err)
	require.True(t, localCalled, "should try local first")
	require.True(t, remoteCalled, "should fall back to remote")
	require.Equal(t, ImageSourceRemote, model.Image.Source)
}

func TestResolver_Inspect_PreferLocal_NoFallbackOnRealError(t *testing.T) {
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

	_, err = resolver.Inspect(context.Background(), ref, PreferLocal())

	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to inspect local image")
	require.Contains(t, err.Error(), "connection refused")
}

func TestResolver_Inspect_PreferRemote_FoundRemotely(t *testing.T) {
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
				Labels: map[string]string{
					LabelConfig:  `{"build":{"python_version":"3.11"}}`,
					LabelVersion: "0.10.0",
				},
			}, nil
		},
	}

	resolver := NewResolver(docker, reg)
	ref, err := ParseRef("my-image:latest")
	require.NoError(t, err)

	model, err := resolver.Inspect(context.Background(), ref, PreferRemote())

	require.NoError(t, err)
	require.False(t, localCalled, "should not try local when remote succeeds")
	require.True(t, remoteCalled, "should try remote first")
	require.Equal(t, ImageSourceRemote, model.Image.Source)
}

func TestResolver_Inspect_PreferRemote_Fallback(t *testing.T) {
	localCalled := false
	remoteCalled := false

	docker := &mockDocker{
		inspectFunc: func(ctx context.Context, ref string) (*image.InspectResponse, error) {
			localCalled = true
			return &image.InspectResponse{
				ID: "sha256:local123",
				Config: &dockerspec.DockerOCIImageConfig{
					ImageConfig: ocispec.ImageConfig{
						Labels: map[string]string{

							LabelConfig:  `{"build":{"python_version":"3.11"}}`,
							LabelVersion: "0.10.0",
						},
					},
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

	model, err := resolver.Inspect(context.Background(), ref, PreferRemote())

	require.NoError(t, err)
	require.True(t, remoteCalled, "should try remote first")
	require.True(t, localCalled, "should fall back to local")
	require.Equal(t, ImageSourceLocal, model.Image.Source)
}

func TestResolver_Inspect_PreferRemote_NoFallbackOnRealError(t *testing.T) {
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

	_, err = resolver.Inspect(context.Background(), ref, PreferRemote())

	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to inspect remote image")
	require.Contains(t, err.Error(), "authentication required")
}

func TestResolver_Inspect_WithPlatform(t *testing.T) {
	var capturedPlatform *registry.Platform

	docker := &mockDocker{}
	reg := &mockRegistry{
		inspectFunc: func(ctx context.Context, ref string, platform *registry.Platform) (*registry.ManifestResult, error) {
			capturedPlatform = platform
			return &registry.ManifestResult{
				SchemaVersion: 2,
				Labels: map[string]string{
					LabelConfig:  `{"build":{"python_version":"3.11"}}`,
					LabelVersion: "0.10.0",
				},
			}, nil
		},
	}

	resolver := NewResolver(docker, reg)
	ref, err := ParseRef("my-image")
	require.NoError(t, err)

	platform := &registry.Platform{OS: "linux", Architecture: "amd64"}
	_, err = resolver.Inspect(context.Background(), ref, RemoteOnly(), WithPlatform(platform))

	require.NoError(t, err)
	require.NotNil(t, capturedPlatform)
	require.Equal(t, "linux", capturedPlatform.OS)
	require.Equal(t, "amd64", capturedPlatform.Architecture)
}

func TestResolver_Inspect_ParsesConfigFromLabels(t *testing.T) {
	docker := &mockDocker{
		inspectFunc: func(ctx context.Context, ref string) (*image.InspectResponse, error) {
			return &image.InspectResponse{
				ID: "sha256:abc123",
				Config: &dockerspec.DockerOCIImageConfig{
					ImageConfig: ocispec.ImageConfig{
						Labels: map[string]string{

							LabelConfig:  `{"build":{"gpu":true,"python_version":"3.12"},"predict":"predict.py:Predictor"}`,
							LabelVersion: "0.11.0",
						},
					},
				},
			}, nil
		},
	}

	resolver := NewResolver(docker, &mockRegistry{})
	ref, err := ParseRef("my-image:latest")
	require.NoError(t, err)

	model, err := resolver.Inspect(context.Background(), ref, LocalOnly())

	require.NoError(t, err)
	require.NotNil(t, model.Config)
	require.NotNil(t, model.Config.Build)
	require.True(t, model.Config.Build.GPU)
	require.Equal(t, "3.12", model.Config.Build.PythonVersion)
	require.Equal(t, "predict.py:Predictor", model.Config.Predict)
}

func TestResolver_Inspect_InvalidConfigJSON(t *testing.T) {
	docker := &mockDocker{
		inspectFunc: func(ctx context.Context, ref string) (*image.InspectResponse, error) {
			return &image.InspectResponse{
				ID: "sha256:abc123",
				Config: &dockerspec.DockerOCIImageConfig{
					ImageConfig: ocispec.ImageConfig{
						Labels: map[string]string{

							LabelConfig: `{invalid json`,
						},
					},
				},
			}, nil
		},
	}

	resolver := NewResolver(docker, &mockRegistry{})
	ref, err := ParseRef("my-image:latest")
	require.NoError(t, err)

	_, err = resolver.Inspect(context.Background(), ref, LocalOnly())

	require.Error(t, err)
	// Error should contain the JSON parse error message
	require.Contains(t, err.Error(), "invalid character")
}

func TestResolver_Inspect_NoConfigLabel_ReturnsErrNotCogModel(t *testing.T) {
	docker := &mockDocker{
		inspectFunc: func(ctx context.Context, ref string) (*image.InspectResponse, error) {
			return &image.InspectResponse{
				ID: "sha256:abc123",
				Config: &dockerspec.DockerOCIImageConfig{
					ImageConfig: ocispec.ImageConfig{
						Labels: map[string]string{

							// No LabelConfig - just version label
							LabelVersion: "0.10.0",
						},
					},
				},
			}, nil
		},
	}

	resolver := NewResolver(docker, &mockRegistry{})
	ref, err := ParseRef("my-image:latest")
	require.NoError(t, err)

	_, err = resolver.Inspect(context.Background(), ref, LocalOnly())

	// Without LabelConfig, image is not a valid Cog model
	require.Error(t, err)
	require.ErrorIs(t, err, ErrNotCogModel)
}

func TestResolver_Inspect_NotCogModel(t *testing.T) {
	docker := &mockDocker{
		inspectFunc: func(ctx context.Context, ref string) (*image.InspectResponse, error) {
			return &image.InspectResponse{
				ID: "sha256:abc123",
				Config: &dockerspec.DockerOCIImageConfig{
					ImageConfig: ocispec.ImageConfig{
						Labels: map[string]string{

							// No Cog labels at all - just some random image
							"maintainer": "someone@example.com",
						},
					},
				},
			}, nil
		},
	}

	resolver := NewResolver(docker, &mockRegistry{})
	ref, err := ParseRef("nginx:latest")
	require.NoError(t, err)

	_, err = resolver.Inspect(context.Background(), ref, LocalOnly())

	require.Error(t, err)
	require.ErrorIs(t, err, ErrNotCogModel)
	require.Contains(t, err.Error(), "nginx:latest")
}

func TestResolver_InspectByID_Found(t *testing.T) {
	docker := &mockDocker{
		inspectFunc: func(ctx context.Context, ref string) (*image.InspectResponse, error) {
			// Verify the ID was passed directly (not mangled by ParseRef)
			require.Equal(t, "9056219a5fb2", ref)
			return &image.InspectResponse{
				ID: "sha256:9056219a5fb2abc123def456",
				Config: &dockerspec.DockerOCIImageConfig{
					ImageConfig: ocispec.ImageConfig{
						Labels: map[string]string{

							LabelConfig:  `{"build":{"python_version":"3.11"}}`,
							LabelVersion: "0.10.0",
						},
					},
				},
			}, nil
		},
	}

	resolver := NewResolver(docker, &mockRegistry{})

	model, err := resolver.InspectByID(context.Background(), "9056219a5fb2")

	require.NoError(t, err)
	require.NotNil(t, model)
	require.Equal(t, ImageSourceLocal, model.Image.Source)
	require.Equal(t, "sha256:9056219a5fb2abc123def456", model.Image.Digest)
	require.Equal(t, "sha256:9056219a5fb2abc123def456", model.Image.Reference)
	require.Equal(t, "0.10.0", model.CogVersion)
	require.NotNil(t, model.Config)
	require.Equal(t, "3.11", model.Config.Build.PythonVersion)
}

func TestResolver_InspectByID_FullSHA(t *testing.T) {
	fullID := "sha256:9056219a5fb2abc123def456789"
	docker := &mockDocker{
		inspectFunc: func(ctx context.Context, ref string) (*image.InspectResponse, error) {
			require.Equal(t, fullID, ref)
			return &image.InspectResponse{
				ID: fullID,
				Config: &dockerspec.DockerOCIImageConfig{
					ImageConfig: ocispec.ImageConfig{
						Labels: map[string]string{

							LabelConfig:  `{"build":{"python_version":"3.11"}}`,
							LabelVersion: "0.10.0",
						},
					},
				},
			}, nil
		},
	}

	resolver := NewResolver(docker, &mockRegistry{})

	model, err := resolver.InspectByID(context.Background(), fullID)

	require.NoError(t, err)
	require.NotNil(t, model)
	require.Equal(t, fullID, model.Image.Digest)
}

func TestResolver_InspectByID_NotCogModel(t *testing.T) {
	docker := &mockDocker{
		inspectFunc: func(ctx context.Context, ref string) (*image.InspectResponse, error) {
			return &image.InspectResponse{
				ID: "sha256:abc123",
				Config: &dockerspec.DockerOCIImageConfig{
					ImageConfig: ocispec.ImageConfig{
						Labels: map[string]string{

							// No Cog labels
							"maintainer": "someone",
						},
					},
				},
			}, nil
		},
	}

	resolver := NewResolver(docker, &mockRegistry{})

	_, err := resolver.InspectByID(context.Background(), "abc123")

	require.Error(t, err)
	require.ErrorIs(t, err, ErrNotCogModel)
}

func TestResolver_InspectByID_NotFound(t *testing.T) {
	docker := &mockDocker{
		inspectFunc: func(ctx context.Context, ref string) (*image.InspectResponse, error) {
			return nil, errors.New("No such image: abc123")
		},
	}

	resolver := NewResolver(docker, &mockRegistry{})

	_, err := resolver.InspectByID(context.Background(), "abc123")

	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to load image by ID")
}

// =============================================================================
// Pull tests
// =============================================================================

func TestResolver_Pull_AlreadyLocal(t *testing.T) {
	pullCalled := false

	docker := &mockDocker{
		inspectFunc: func(ctx context.Context, ref string) (*image.InspectResponse, error) {
			return &image.InspectResponse{
				ID: "sha256:abc123",
				Config: &dockerspec.DockerOCIImageConfig{
					ImageConfig: ocispec.ImageConfig{
						Labels: map[string]string{

							LabelConfig:  `{"build":{"gpu":false}}`,
							LabelVersion: "0.10.0",
						},
					},
				},
			}, nil
		},
		pullFunc: func(ctx context.Context, ref string, force bool) (*image.InspectResponse, error) {
			pullCalled = true
			return nil, nil
		},
	}

	resolver := NewResolver(docker, &mockRegistry{})
	ref, err := ParseRef("my-image:latest")
	require.NoError(t, err)

	model, err := resolver.Pull(context.Background(), ref)

	require.NoError(t, err)
	require.False(t, pullCalled, "should not pull when image exists locally")
	require.NotNil(t, model)
	require.Equal(t, "0.10.0", model.CogVersion)
}

func TestResolver_Pull_NotLocal_PullsAndReturns(t *testing.T) {
	pullCalled := false
	inspectCalls := 0

	docker := &mockDocker{
		inspectFunc: func(ctx context.Context, ref string) (*image.InspectResponse, error) {
			inspectCalls++
			if inspectCalls == 1 {
				// First call: not found locally
				return nil, errors.New("No such image")
			}
			// After pull: found
			return &image.InspectResponse{
				ID: "sha256:abc123",
				Config: &dockerspec.DockerOCIImageConfig{
					ImageConfig: ocispec.ImageConfig{
						Labels: map[string]string{

							LabelConfig:  `{"build":{"gpu":true}}`,
							LabelVersion: "0.10.0",
						},
					},
				},
			}, nil
		},
		pullFunc: func(ctx context.Context, ref string, force bool) (*image.InspectResponse, error) {
			pullCalled = true
			return &image.InspectResponse{
				ID: "sha256:abc123",
				Config: &dockerspec.DockerOCIImageConfig{
					ImageConfig: ocispec.ImageConfig{
						Labels: map[string]string{

							LabelConfig:  `{"build":{"gpu":true}}`,
							LabelVersion: "0.10.0",
						},
					},
				},
			}, nil
		},
	}

	resolver := NewResolver(docker, &mockRegistry{})
	ref, err := ParseRef("r8.im/user/model:latest")
	require.NoError(t, err)

	model, err := resolver.Pull(context.Background(), ref)

	require.NoError(t, err)
	require.True(t, pullCalled, "should call Pull when image not local")
	require.NotNil(t, model)
	require.True(t, model.HasGPU())
}

func TestResolver_Pull_NotCogModel(t *testing.T) {
	inspectCalls := 0
	docker := &mockDocker{
		inspectFunc: func(ctx context.Context, ref string) (*image.InspectResponse, error) {
			inspectCalls++
			if inspectCalls == 1 {
				// First call: not found locally
				return nil, errors.New("No such image")
			}
			// After pull: found but not a Cog model
			return &image.InspectResponse{
				ID: "sha256:abc123",
				Config: &dockerspec.DockerOCIImageConfig{
					ImageConfig: ocispec.ImageConfig{
						Labels: map[string]string{

							// Not a Cog model
							"some.label": "value",
						},
					},
				},
			}, nil
		},
		pullFunc: func(ctx context.Context, ref string, force bool) (*image.InspectResponse, error) {
			return &image.InspectResponse{
				ID: "sha256:abc123",
				Config: &dockerspec.DockerOCIImageConfig{
					ImageConfig: ocispec.ImageConfig{
						Labels: map[string]string{

							"some.label": "value",
						},
					},
				},
			}, nil
		},
	}

	resolver := NewResolver(docker, &mockRegistry{})
	ref, err := ParseRef("not-cog:latest")
	require.NoError(t, err)

	_, err = resolver.Pull(context.Background(), ref)

	require.Error(t, err)
	require.ErrorIs(t, err, ErrNotCogModel)
}

func TestResolver_Pull_LocalOnly_NotFound(t *testing.T) {
	docker := &mockDocker{
		inspectFunc: func(ctx context.Context, ref string) (*image.InspectResponse, error) {
			return nil, errors.New("No such image")
		},
		pullFunc: func(ctx context.Context, ref string, force bool) (*image.InspectResponse, error) {
			t.Fatal("should not pull when LocalOnly")
			return nil, nil
		},
	}

	resolver := NewResolver(docker, &mockRegistry{})
	ref, err := ParseRef("my-image:latest")
	require.NoError(t, err)

	_, err = resolver.Pull(context.Background(), ref, LocalOnly())

	require.Error(t, err)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestResolver_Pull_PullFails(t *testing.T) {
	docker := &mockDocker{
		inspectFunc: func(ctx context.Context, ref string) (*image.InspectResponse, error) {
			return nil, errors.New("No such image")
		},
		pullFunc: func(ctx context.Context, ref string, force bool) (*image.InspectResponse, error) {
			return nil, errors.New("manifest unknown")
		},
	}

	resolver := NewResolver(docker, &mockRegistry{})
	ref, err := ParseRef("nonexistent:latest")
	require.NoError(t, err)

	_, err = resolver.Pull(context.Background(), ref)

	require.Error(t, err)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestResolver_Pull_LocalInspectRealError(t *testing.T) {
	docker := &mockDocker{
		inspectFunc: func(ctx context.Context, ref string) (*image.InspectResponse, error) {
			return nil, errors.New("connection refused")
		},
		pullFunc: func(ctx context.Context, ref string, force bool) (*image.InspectResponse, error) {
			t.Fatal("should not pull when local inspect has real error")
			return nil, nil
		},
	}

	resolver := NewResolver(docker, &mockRegistry{})
	ref, err := ParseRef("my-image:latest")
	require.NoError(t, err)

	_, err = resolver.Pull(context.Background(), ref)

	require.Error(t, err)
	require.Contains(t, err.Error(), "connection refused")
}

// =============================================================================
// Build tests
// =============================================================================

func TestResolver_Build_NoWeightsManifestWithoutWeights(t *testing.T) {
	validDigest := "sha256:a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"

	docker := &mockDocker{
		inspectFunc: func(ctx context.Context, ref string) (*image.InspectResponse, error) {
			return &image.InspectResponse{
				ID: validDigest,
				Config: &dockerspec.DockerOCIImageConfig{
					ImageConfig: ocispec.ImageConfig{
						Labels: map[string]string{

							LabelConfig:  `{"build":{"python_version":"3.11"}}`,
							LabelVersion: "0.10.0",
						},
					},
				},
			}, nil
		},
	}

	factory := &mockFactory{}
	resolver := NewResolver(docker, &mockRegistry{}).WithFactory(factory)

	src := &Source{
		Config:     &config.Config{Build: &config.Build{}},
		ProjectDir: t.TempDir(),
	}

	m, err := resolver.Build(context.Background(), src, BuildOptions{
		ImageName: "test-image",
	})

	require.NoError(t, err)
	require.False(t, m.IsBundle())
	require.Empty(t, m.WeightArtifacts())
}

func TestResolver_Build_PopulatesArtifacts(t *testing.T) {
	imageDigest := "sha256:a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"

	docker := &mockDocker{
		inspectFunc: func(ctx context.Context, ref string) (*image.InspectResponse, error) {
			return &image.InspectResponse{
				ID: imageDigest,
				Config: &dockerspec.DockerOCIImageConfig{
					ImageConfig: ocispec.ImageConfig{
						Labels: map[string]string{

							LabelConfig:  `{"build":{"python_version":"3.11"}}`,
							LabelVersion: "0.15.0",
						},
					},
				},
			}, nil
		},
	}

	factory := &mockFactory{
		buildFunc: func(ctx context.Context, src *Source, opts BuildOptions) (*ImageArtifact, error) {
			return &ImageArtifact{
				Reference: opts.ImageName,
				Digest:    imageDigest,
				Source:    ImageSourceBuild,
			}, nil
		},
	}
	resolver := NewResolver(docker, &mockRegistry{}).WithFactory(factory)

	src := &Source{
		Config:     &config.Config{Build: &config.Build{}},
		ProjectDir: t.TempDir(),
	}

	m, err := resolver.Build(context.Background(), src, BuildOptions{
		ImageName: "test-image:latest",
	})

	require.NoError(t, err)
	require.NotNil(t, m.Artifacts, "Build should populate Artifacts")
	require.Len(t, m.Artifacts, 1, "should have exactly one artifact (image)")

	// Verify it's an ImageArtifact with correct data
	imgArtifact := m.GetImageArtifact()
	require.NotNil(t, imgArtifact, "should contain an ImageArtifact")
	require.Equal(t, "model", imgArtifact.Name())
	require.Equal(t, ArtifactTypeImage, imgArtifact.Type())
	require.Equal(t, "test-image:latest", imgArtifact.Reference)
	require.Equal(t, imageDigest, imgArtifact.Descriptor().Digest.String())
}

func TestResolver_Build_PopulatesWeightArtifacts(t *testing.T) {
	imageDigest := "sha256:a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"

	docker := &mockDocker{
		inspectFunc: func(ctx context.Context, ref string) (*image.InspectResponse, error) {
			return &image.InspectResponse{
				ID: imageDigest,
				Config: &dockerspec.DockerOCIImageConfig{
					ImageConfig: ocispec.ImageConfig{
						Labels: map[string]string{

							LabelConfig:  `{"build":{"python_version":"3.11"}}`,
							LabelVersion: "0.15.0",
						},
					},
				},
			}, nil
		},
	}

	factory := &mockFactory{
		buildFunc: func(ctx context.Context, src *Source, opts BuildOptions) (*ImageArtifact, error) {
			return &ImageArtifact{
				Reference: opts.ImageName,
				Digest:    imageDigest,
				Source:    ImageSourceBuild,
			}, nil
		},
	}
	resolver := NewResolver(docker, &mockRegistry{}).WithFactory(factory)

	// Create a temp directory with a real weight file
	dir := t.TempDir()
	weightContent := []byte("test weight for resolver build")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "model.safetensors"), weightContent, 0o644))

	src := &Source{
		Config: &config.Config{
			Build: &config.Build{},
			Weights: []config.WeightSource{
				{Name: "my-model", Source: "model.safetensors", Target: "/srv/weights/model.safetensors"},
			},
		},
		ProjectDir: dir,
	}

	m, err := resolver.Build(context.Background(), src, BuildOptions{
		ImageName: "test-image:latest",
		OCIIndex:  true,
	})

	require.NoError(t, err)
	require.NotNil(t, m.Artifacts)

	// Should have 2 artifacts: 1 image + 1 weight
	require.Len(t, m.Artifacts, 2, "should have image + weight artifacts")

	// Verify image artifact
	imgArtifact := m.GetImageArtifact()
	require.NotNil(t, imgArtifact)
	require.Equal(t, "model", imgArtifact.Name())

	// Verify weight artifact
	weightArtifacts := m.WeightArtifacts()
	require.Len(t, weightArtifacts, 1)
	wa := weightArtifacts[0]
	require.Equal(t, "my-model", wa.Name())
	require.Equal(t, ArtifactTypeWeight, wa.Type())
	require.Equal(t, "/srv/weights/model.safetensors", wa.Target)
	require.Equal(t, filepath.Join(dir, "model.safetensors"), wa.FilePath)

	// Weight config should be populated
	require.Equal(t, "1.0", wa.Config.SchemaVersion)
	require.Equal(t, "my-model", wa.Config.Name)
	require.Equal(t, "/srv/weights/model.safetensors", wa.Config.Target)
	require.False(t, wa.Config.Created.IsZero())
}

func TestResolver_Build_WithWeightsLoadsManifest(t *testing.T) {
	imageDigest := "sha256:a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"

	docker := &mockDocker{
		inspectFunc: func(ctx context.Context, ref string) (*image.InspectResponse, error) {
			return &image.InspectResponse{
				ID: imageDigest,
				Config: &dockerspec.DockerOCIImageConfig{
					ImageConfig: ocispec.ImageConfig{
						Labels: map[string]string{

							LabelConfig:  `{"build":{"python_version":"3.11"}}`,
							LabelVersion: "0.15.0",
						},
					},
				},
			}, nil
		},
	}

	factory := &mockFactory{
		buildFunc: func(ctx context.Context, src *Source, opts BuildOptions) (*ImageArtifact, error) {
			return &ImageArtifact{
				Reference: opts.ImageName,
				Digest:    imageDigest,
				Source:    ImageSourceBuild,
			}, nil
		},
	}
	resolver := NewResolver(docker, &mockRegistry{}).WithFactory(factory)

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "model.bin"), []byte("test weights"), 0o644))

	src := &Source{
		Config: &config.Config{
			Build: &config.Build{},
			Weights: []config.WeightSource{
				{Name: "my-model", Source: "model.bin", Target: "/weights/model.bin"},
			},
		},
		ProjectDir: dir,
	}

	m, err := resolver.Build(context.Background(), src, BuildOptions{
		ImageName: "test-image:latest",
		OCIIndex:  true,
	})

	require.NoError(t, err)
	require.True(t, m.IsBundle())
	require.True(t, m.OCIIndex)

	// Should have 2 artifacts: image + weight
	require.Len(t, m.Artifacts, 2)
	require.NotNil(t, m.GetImageArtifact())
	require.Len(t, m.WeightArtifacts(), 1)

	// Weight artifacts should be populated
	require.Len(t, m.WeightArtifacts(), 1)
}

func TestIndexDetectionHelpers(t *testing.T) {
	t.Run("findWeightsManifest", func(t *testing.T) {
		manifests := []registry.PlatformManifest{
			{Digest: "sha256:image123", OS: "linux", Architecture: "amd64"},
			{
				Digest:       "sha256:weights456",
				OS:           PlatformUnknown,
				Architecture: PlatformUnknown,
				Annotations: map[string]string{
					AnnotationReferenceType: AnnotationValueWeights,
				},
			},
		}

		wm := findWeightsManifest(manifests)
		require.NotNil(t, wm)
		require.Equal(t, "sha256:weights456", wm.Digest)
	})

	t.Run("findWeightsManifest not found", func(t *testing.T) {
		manifests := []registry.PlatformManifest{
			{Digest: "sha256:image123", OS: "linux", Architecture: "amd64"},
		}

		wm := findWeightsManifest(manifests)
		require.Nil(t, wm)
	})

	t.Run("findImageManifest", func(t *testing.T) {
		manifests := []registry.PlatformManifest{
			{Digest: "sha256:image123", OS: "linux", Architecture: "amd64"},
			{Digest: "sha256:weights456", OS: PlatformUnknown, Architecture: PlatformUnknown},
		}

		platform := &registry.Platform{OS: "linux", Architecture: "amd64"}
		im := findImageManifest(manifests, platform)
		require.NotNil(t, im)
		require.Equal(t, "sha256:image123", im.Digest)
	})

	t.Run("findImageManifest skips unknown", func(t *testing.T) {
		manifests := []registry.PlatformManifest{
			{Digest: "sha256:weights456", OS: PlatformUnknown, Architecture: PlatformUnknown},
		}

		im := findImageManifest(manifests, nil)
		require.Nil(t, im)
	})

	t.Run("findImageManifest no platform filter", func(t *testing.T) {
		manifests := []registry.PlatformManifest{
			{Digest: "sha256:arm123", OS: "linux", Architecture: "arm64"},
			{Digest: "sha256:weights456", OS: PlatformUnknown, Architecture: PlatformUnknown},
		}

		im := findImageManifest(manifests, nil)
		require.NotNil(t, im)
		require.Equal(t, "sha256:arm123", im.Digest)
	})

	t.Run("findImageManifest platform mismatch", func(t *testing.T) {
		manifests := []registry.PlatformManifest{
			{Digest: "sha256:arm123", OS: "linux", Architecture: "arm64"},
			{Digest: "sha256:weights456", OS: PlatformUnknown, Architecture: PlatformUnknown},
		}

		platform := &registry.Platform{OS: "linux", Architecture: "amd64"}
		im := findImageManifest(manifests, platform)
		require.Nil(t, im)
	})

	t.Run("isOCIIndex with index", func(t *testing.T) {
		mr := &registry.ManifestResult{
			MediaType: "application/vnd.oci.image.index.v1+json",
		}
		require.True(t, isOCIIndex(mr))
	})

	t.Run("isOCIIndex with single manifest", func(t *testing.T) {
		mr := &registry.ManifestResult{
			MediaType: "application/vnd.oci.image.manifest.v1+json",
		}
		require.False(t, isOCIIndex(mr))
	})
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
