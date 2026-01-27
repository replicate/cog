package model

import (
	"context"
	"errors"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/registry"
)

// =============================================================================
// FromTag tests
// =============================================================================

func TestFromTag_ValidRef(t *testing.T) {
	ref, err := FromTag("my-image:latest")

	require.NoError(t, err)
	require.NotNil(t, ref)
	require.NotNil(t, ref.Parsed)
	require.Equal(t, "my-image:latest", ref.Parsed.Original)
}

func TestFromTag_ValidRefWithRegistry(t *testing.T) {
	ref, err := FromTag("r8.im/user/model:v1")

	require.NoError(t, err)
	require.NotNil(t, ref)
	require.Equal(t, "r8.im", ref.Parsed.Registry())
	require.Equal(t, "v1", ref.Parsed.Tag())
}

func TestFromTag_InvalidRef(t *testing.T) {
	ref, err := FromTag("INVALID::REF")

	require.Error(t, err)
	require.Nil(t, ref)
	require.Contains(t, err.Error(), "invalid image reference")
}

// =============================================================================
// FromLocal tests
// =============================================================================

func TestFromLocal_ValidRef(t *testing.T) {
	ref, err := FromLocal("my-image:latest")

	require.NoError(t, err)
	require.NotNil(t, ref)
	require.NotNil(t, ref.Parsed)
	require.Equal(t, "my-image:latest", ref.Parsed.Original)
}

func TestFromLocal_InvalidRef(t *testing.T) {
	ref, err := FromLocal("INVALID::REF")

	require.Error(t, err)
	require.Nil(t, ref)
	require.Contains(t, err.Error(), "invalid image reference")
}

// =============================================================================
// FromRemote tests
// =============================================================================

func TestFromRemote_ValidRef(t *testing.T) {
	ref, err := FromRemote("r8.im/user/model")

	require.NoError(t, err)
	require.NotNil(t, ref)
	require.NotNil(t, ref.Parsed)
	require.Equal(t, "r8.im", ref.Parsed.Registry())
}

func TestFromRemote_InvalidRef(t *testing.T) {
	ref, err := FromRemote("INVALID::REF")

	require.Error(t, err)
	require.Nil(t, ref)
	require.Contains(t, err.Error(), "invalid image reference")
}

// =============================================================================
// FromBuild tests
// =============================================================================

func TestFromBuild(t *testing.T) {
	src := &Source{
		Config:     &config.Config{Predict: "predict.py:Predictor"},
		ProjectDir: "/path/to/project",
	}
	opts := BuildOptions{
		ImageName: "my-built-image:latest",
		NoCache:   true,
	}

	ref := FromBuild(src, opts)

	require.NotNil(t, ref)
	require.Same(t, src, ref.Source)
	require.Equal(t, "my-built-image:latest", ref.Options.ImageName)
	require.True(t, ref.Options.NoCache)
}

func TestFromBuild_NilSource(t *testing.T) {
	// FromBuild should accept nil source - validation happens at resolve time
	ref := FromBuild(nil, BuildOptions{ImageName: "test"})

	require.NotNil(t, ref)
	require.Nil(t, ref.Source)
}

// =============================================================================
// TagRef.resolve tests
// =============================================================================

func TestTagRef_Resolve_Success(t *testing.T) {
	docker := &mockDocker{
		inspectFunc: func(ctx context.Context, ref string) (*image.InspectResponse, error) {
			return &image.InspectResponse{
				ID: "sha256:abc123",
				Config: &container.Config{
					Labels: map[string]string{
						LabelVersion: "0.10.0",
					},
				},
			}, nil
		},
	}

	resolver := NewResolver(docker, &mockRegistry{})
	ref, err := FromTag("my-image:latest")
	require.NoError(t, err)

	model, err := resolver.Resolve(context.Background(), ref)

	require.NoError(t, err)
	require.NotNil(t, model)
	require.Equal(t, "0.10.0", model.CogVersion)
}

func TestTagRef_Resolve_FallsBackToRemote(t *testing.T) {
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
			return &registry.ManifestResult{SchemaVersion: 2}, nil
		},
	}

	resolver := NewResolver(docker, reg)
	ref, err := FromTag("my-image:latest")
	require.NoError(t, err)

	model, err := resolver.Resolve(context.Background(), ref)

	require.NoError(t, err)
	require.NotNil(t, model)
	require.True(t, localCalled, "TagRef should try local first")
	require.True(t, remoteCalled, "TagRef should fall back to remote")
	require.Equal(t, ImageSourceRemote, model.Image.Source)
}

// =============================================================================
// LocalRef.resolve tests
// =============================================================================

func TestLocalRef_Resolve_Success(t *testing.T) {
	docker := &mockDocker{
		inspectFunc: func(ctx context.Context, ref string) (*image.InspectResponse, error) {
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

	resolver := NewResolver(docker, &mockRegistry{})
	ref, err := FromLocal("my-image:latest")
	require.NoError(t, err)

	model, err := resolver.Resolve(context.Background(), ref)

	require.NoError(t, err)
	require.NotNil(t, model)
	require.Equal(t, ImageSourceLocal, model.Image.Source)
	require.Equal(t, "0.9.0", model.CogVersion)
}

func TestLocalRef_Resolve_NotFound(t *testing.T) {
	docker := &mockDocker{
		inspectFunc: func(ctx context.Context, ref string) (*image.InspectResponse, error) {
			return nil, errors.New("No such image: my-image:latest")
		},
	}
	reg := &mockRegistry{
		inspectFunc: func(ctx context.Context, ref string, platform *registry.Platform) (*registry.ManifestResult, error) {
			t.Fatal("LocalRef should not fall back to remote")
			return nil, nil
		},
	}

	resolver := NewResolver(docker, reg)
	ref, err := FromLocal("my-image:latest")
	require.NoError(t, err)

	_, err = resolver.Resolve(context.Background(), ref)

	require.Error(t, err)
	require.Contains(t, err.Error(), "not found locally")
}

// =============================================================================
// RemoteRef.resolve tests
// =============================================================================

func TestRemoteRef_Resolve_Success(t *testing.T) {
	docker := &mockDocker{
		inspectFunc: func(ctx context.Context, ref string) (*image.InspectResponse, error) {
			t.Fatal("RemoteRef should not check local docker")
			return nil, nil
		},
	}
	reg := &mockRegistry{
		inspectFunc: func(ctx context.Context, ref string, platform *registry.Platform) (*registry.ManifestResult, error) {
			return &registry.ManifestResult{SchemaVersion: 2}, nil
		},
	}

	resolver := NewResolver(docker, reg)
	ref, err := FromRemote("r8.im/user/model")
	require.NoError(t, err)

	model, err := resolver.Resolve(context.Background(), ref)

	require.NoError(t, err)
	require.NotNil(t, model)
	require.Equal(t, ImageSourceRemote, model.Image.Source)
}

func TestRemoteRef_Resolve_NotFound(t *testing.T) {
	docker := &mockDocker{}
	reg := &mockRegistry{
		inspectFunc: func(ctx context.Context, ref string, platform *registry.Platform) (*registry.ManifestResult, error) {
			return nil, errors.New("manifest unknown")
		},
	}

	resolver := NewResolver(docker, reg)
	ref, err := FromRemote("r8.im/user/model")
	require.NoError(t, err)

	_, err = resolver.Resolve(context.Background(), ref)

	require.Error(t, err)
	require.Contains(t, err.Error(), "not found in registry")
}

// =============================================================================
// BuildRef.resolve tests
// =============================================================================

func TestBuildRef_Resolve_Success(t *testing.T) {
	buildCalled := false
	docker := &mockDocker{
		inspectFunc: func(ctx context.Context, ref string) (*image.InspectResponse, error) {
			return &image.InspectResponse{
				ID: "sha256:built123",
				Config: &container.Config{
					Labels: map[string]string{
						LabelVersion: "0.11.0",
						LabelConfig:  `{"build":{"gpu":true}}`,
					},
				},
			}, nil
		},
	}

	factory := &mockFactory{
		name: "test",
		buildFunc: func(ctx context.Context, src *Source, opts BuildOptions) (*Image, error) {
			buildCalled = true
			require.Equal(t, "my-built-image", opts.ImageName)
			return &Image{Reference: opts.ImageName, Source: ImageSourceBuild}, nil
		},
	}

	resolver := NewResolver(docker, &mockRegistry{}).WithFactory(factory)

	src := &Source{
		Config:     &config.Config{Predict: "predict.py:Predictor"},
		ProjectDir: "/tmp/test",
	}
	ref := FromBuild(src, BuildOptions{ImageName: "my-built-image"})

	model, err := resolver.Resolve(context.Background(), ref)

	require.NoError(t, err)
	require.NotNil(t, model)
	require.True(t, buildCalled, "BuildRef should call factory.Build")
	require.Equal(t, "0.11.0", model.CogVersion)
}

func TestBuildRef_Resolve_BuildError(t *testing.T) {
	factory := &mockFactory{
		name: "test",
		buildFunc: func(ctx context.Context, src *Source, opts BuildOptions) (*Image, error) {
			return nil, errors.New("build failed: missing dependencies")
		},
	}

	resolver := NewResolver(&mockDocker{}, &mockRegistry{}).WithFactory(factory)

	src := &Source{
		Config:     &config.Config{},
		ProjectDir: "/tmp/test",
	}
	ref := FromBuild(src, BuildOptions{ImageName: "my-image"})

	_, err := resolver.Resolve(context.Background(), ref)

	require.Error(t, err)
	require.Contains(t, err.Error(), "build failed")
}

// =============================================================================
// Resolver.Resolve dispatch tests
// =============================================================================

func TestResolver_Resolve_DispatchesCorrectly(t *testing.T) {
	// This test verifies that Resolver.Resolve correctly dispatches to each Ref type
	tests := []struct {
		name        string
		ref         Ref
		expectLocal bool
	}{
		{
			name: "TagRef dispatches to Load (default behavior)",
			ref: func() Ref {
				r, _ := FromTag("my-image:latest")
				return r
			}(),
			expectLocal: true, // TagRef tries local first by default
		},
		{
			name: "LocalRef dispatches to local only",
			ref: func() Ref {
				r, _ := FromLocal("my-image:latest")
				return r
			}(),
			expectLocal: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			localCalled := false
			docker := &mockDocker{
				inspectFunc: func(ctx context.Context, ref string) (*image.InspectResponse, error) {
					localCalled = true
					return &image.InspectResponse{
						ID:     "sha256:test",
						Config: &container.Config{Labels: map[string]string{}},
					}, nil
				},
			}

			resolver := NewResolver(docker, &mockRegistry{})
			_, err := resolver.Resolve(context.Background(), tt.ref)

			require.NoError(t, err)
			require.Equal(t, tt.expectLocal, localCalled)
		})
	}
}

// =============================================================================
// Ref interface compile-time checks
// =============================================================================

// Compile-time check that all types implement Ref interface
var (
	_ Ref = (*TagRef)(nil)
	_ Ref = (*LocalRef)(nil)
	_ Ref = (*RemoteRef)(nil)
	_ Ref = (*BuildRef)(nil)
)
