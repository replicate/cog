package model

import (
	"context"
	"errors"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/config"
)

func TestImageBuilder_HappyPath(t *testing.T) {
	// Setup mock factory that returns a built image
	factory := &mockFactory{
		buildFunc: func(_ context.Context, _ *Source, opts BuildOptions) (*ImageArtifact, error) {
			return &ImageArtifact{
				Reference: opts.ImageName,
				Digest:    "sha256:a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
				Source:    ImageSourceBuild,
			}, nil
		},
	}

	// Setup mock docker that returns inspect results with labels
	docker := &mockDocker{
		inspectFunc: func(_ context.Context, ref string) (*image.InspectResponse, error) {
			return &image.InspectResponse{
				ID: "sha256:a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
				Config: &container.Config{
					Labels: map[string]string{
						"org.cogmodel.cog_version": "0.15.0",
					},
				},
			}, nil
		},
	}

	src := NewSourceFromConfig(&config.Config{
		Image: "my-model:latest",
	}, "/project")

	ib := NewImageBuilder(factory, docker, src, BuildOptions{
		ImageName: "my-model:latest",
	})

	spec := NewImageSpec("model", "my-model:latest")
	artifact, err := ib.Build(context.Background(), spec)
	require.NoError(t, err)
	require.NotNil(t, artifact)

	// Type assertion
	ia, ok := artifact.(*ImageArtifact)
	require.True(t, ok, "expected *ImageArtifact, got %T", artifact)

	// Check artifact interface
	require.Equal(t, ArtifactTypeImage, ia.Type())
	require.Equal(t, "model", ia.Name())

	// Check descriptor has the digest
	desc := ia.Descriptor()
	require.Equal(t, "sha256:a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2", desc.Digest.String())

	// Check image-specific fields
	require.Equal(t, "my-model:latest", ia.Reference)
}

func TestImageBuilder_ErrorWrongSpecType(t *testing.T) {
	src := NewSourceFromConfig(&config.Config{}, "/project")
	ib := NewImageBuilder(&mockFactory{}, &mockDocker{}, src, BuildOptions{})

	// Pass a WeightSpec instead of ImageSpec
	weightSpec := NewWeightSpec("model", "model.bin", "/weights/model.bin")
	_, err := ib.Build(context.Background(), weightSpec)
	require.Error(t, err)
	require.Contains(t, err.Error(), "expected *ImageSpec")
}

func TestImageBuilder_ErrorFactoryBuildFails(t *testing.T) {
	factory := &mockFactory{
		buildFunc: func(_ context.Context, _ *Source, _ BuildOptions) (*ImageArtifact, error) {
			return nil, errors.New("docker build failed: out of disk")
		},
	}

	src := NewSourceFromConfig(&config.Config{}, "/project")
	ib := NewImageBuilder(factory, &mockDocker{}, src, BuildOptions{})

	spec := NewImageSpec("model", "test-image")
	_, err := ib.Build(context.Background(), spec)
	require.Error(t, err)
	require.Contains(t, err.Error(), "image build failed")
	require.Contains(t, err.Error(), "out of disk")
}

func TestImageBuilder_ErrorInspectFails(t *testing.T) {
	factory := &mockFactory{
		buildFunc: func(_ context.Context, _ *Source, opts BuildOptions) (*ImageArtifact, error) {
			return &ImageArtifact{
				Reference: opts.ImageName,
				Digest:    "sha256:a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
				Source:    ImageSourceBuild,
			}, nil
		},
	}

	docker := &mockDocker{
		inspectFunc: func(_ context.Context, _ string) (*image.InspectResponse, error) {
			return nil, errors.New("image not found")
		},
	}

	src := NewSourceFromConfig(&config.Config{}, "/project")
	ib := NewImageBuilder(factory, docker, src, BuildOptions{})

	spec := NewImageSpec("model", "test-image")
	_, err := ib.Build(context.Background(), spec)
	require.Error(t, err)
	require.Contains(t, err.Error(), "inspect built image")
}

func TestImageBuilder_ImplementsBuilderInterface(t *testing.T) {
	src := NewSourceFromConfig(&config.Config{}, "/project")
	// Compile-time check
	var _ Builder = NewImageBuilder(&mockFactory{}, &mockDocker{}, src, BuildOptions{})
}
