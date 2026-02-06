package model

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/registry"
)

// =============================================================================
// BundlePusher tests
// =============================================================================

func TestBundlePusher_Push(t *testing.T) {
	t.Run("returns error when image is nil", func(t *testing.T) {
		docker := &mockDocker{}
		reg := &mockRegistry{}
		pusher := NewBundlePusher(docker, reg)
		m := &Model{
			Image:       nil,
			ImageFormat: FormatBundle,
		}

		err := pusher.Push(context.Background(), m, PushOptions{})

		require.Error(t, err)
		require.Contains(t, err.Error(), "model has no image reference")
	})

	t.Run("returns error when weights manifest is nil", func(t *testing.T) {
		docker := &mockDocker{}
		reg := &mockRegistry{}
		pusher := NewBundlePusher(docker, reg)
		m := &Model{
			Image:           &ImageArtifact{Reference: "r8.im/user/model:latest"},
			ImageFormat:     FormatBundle,
			WeightsManifest: nil,
		}

		err := pusher.Push(context.Background(), m, PushOptions{})

		require.Error(t, err)
		require.Contains(t, err.Error(), "bundle format requires WeightsManifest")
	})

	t.Run("returns error when file paths not provided", func(t *testing.T) {
		docker := &mockDocker{}
		reg := &mockRegistry{}
		pusher := NewBundlePusher(docker, reg)
		m := &Model{
			Image:       &ImageArtifact{Reference: "r8.im/user/model:latest"},
			ImageFormat: FormatBundle,
			WeightsManifest: &WeightsManifest{
				Files: []WeightFile{{Name: "my-weights-v1", Dest: "/weights/weights.bin"}},
			},
		}

		err := pusher.Push(context.Background(), m, PushOptions{FilePaths: nil})

		require.Error(t, err)
		require.Contains(t, err.Error(), "bundle push requires FilePaths")
	})

	t.Run("full push flow succeeds", func(t *testing.T) {
		// Setup: Create temp dir with weights file
		dir := t.TempDir()
		weightsDir := filepath.Join(dir, "weights")
		require.NoError(t, os.MkdirAll(weightsDir, 0o755))
		modelPath := filepath.Join(weightsDir, "model.bin")
		require.NoError(t, os.WriteFile(modelPath, []byte("test weights"), 0o644))

		// Track calls to verify correct sequence
		var pushOrder []string

		docker := &mockDocker{
			pushFunc: func(ctx context.Context, ref string) error {
				pushOrder = append(pushOrder, "docker:"+ref)
				return nil
			},
		}

		// Create a test image to return from GetImage
		testImg := empty.Image
		testImg, _ = mutate.Config(testImg, v1.Config{})

		reg := &mockRegistry{
			getImageFunc: func(ctx context.Context, ref string, platform *registry.Platform) (v1.Image, error) {
				pushOrder = append(pushOrder, "registry:getImage:"+ref)
				return testImg, nil
			},
			pushIndexFunc: func(ctx context.Context, ref string, idx v1.ImageIndex) error {
				pushOrder = append(pushOrder, "registry:pushIndex:"+ref)
				return nil
			},
		}

		pusher := NewBundlePusher(docker, reg)
		m := &Model{
			Image:       &ImageArtifact{Reference: "r8.im/user/model:latest"},
			ImageFormat: FormatBundle,
			WeightsManifest: &WeightsManifest{
				ArtifactType: MediaTypeWeightArtifact,
				Created:      time.Now().UTC(),
				Files: []WeightFile{
					{
						Name: "my-model-v1",
						Dest: "/cache/model.bin",
					},
				},
			},
		}

		err := pusher.Push(context.Background(), m, PushOptions{
			FilePaths: map[string]string{
				"my-model-v1": modelPath,
			},
			Platform: &Platform{OS: "linux", Architecture: "amd64"},
		})

		require.NoError(t, err)
		// Verify the push sequence:
		// 1. Push model image via docker
		// 2. Fetch image from registry to get v1.Image
		// 3. Push OCI index to registry
		require.Len(t, pushOrder, 3)
		require.Equal(t, "docker:r8.im/user/model:latest", pushOrder[0])
		require.Equal(t, "registry:getImage:r8.im/user/model:latest", pushOrder[1])
		require.Equal(t, "registry:pushIndex:r8.im/user/model:latest", pushOrder[2])
	})

	t.Run("returns error when docker push fails", func(t *testing.T) {
		dir := t.TempDir()
		weightsDir := filepath.Join(dir, "weights")
		require.NoError(t, os.MkdirAll(weightsDir, 0o755))
		modelPath := filepath.Join(weightsDir, "model.bin")
		require.NoError(t, os.WriteFile(modelPath, []byte("test"), 0o644))

		docker := &mockDocker{
			pushFunc: func(ctx context.Context, ref string) error {
				return errors.New("push failed: unauthorized")
			},
		}
		reg := &mockRegistry{}

		pusher := NewBundlePusher(docker, reg)
		m := &Model{
			Image:       &ImageArtifact{Reference: "r8.im/user/model:latest"},
			ImageFormat: FormatBundle,
			WeightsManifest: &WeightsManifest{
				Files: []WeightFile{
					{Name: "my-model-v1", Dest: "/cache/model.bin"},
				},
			},
		}

		err := pusher.Push(context.Background(), m, PushOptions{
			FilePaths: map[string]string{"my-model-v1": modelPath},
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "push model image")
		require.Contains(t, err.Error(), "unauthorized")
	})

	t.Run("returns error when registry get image fails", func(t *testing.T) {
		dir := t.TempDir()
		weightsDir := filepath.Join(dir, "weights")
		require.NoError(t, os.MkdirAll(weightsDir, 0o755))
		modelPath := filepath.Join(weightsDir, "model.bin")
		require.NoError(t, os.WriteFile(modelPath, []byte("test"), 0o644))

		docker := &mockDocker{
			pushFunc: func(ctx context.Context, ref string) error {
				return nil
			},
		}
		reg := &mockRegistry{
			getImageFunc: func(ctx context.Context, ref string, platform *registry.Platform) (v1.Image, error) {
				return nil, errors.New("manifest not found")
			},
		}

		pusher := NewBundlePusher(docker, reg)
		m := &Model{
			Image:       &ImageArtifact{Reference: "r8.im/user/model:latest"},
			ImageFormat: FormatBundle,
			WeightsManifest: &WeightsManifest{
				Files: []WeightFile{
					{Name: "my-model-v1", Dest: "/cache/model.bin"},
				},
			},
		}

		err := pusher.Push(context.Background(), m, PushOptions{
			FilePaths: map[string]string{"my-model-v1": modelPath},
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "fetch pushed image")
	})

	t.Run("returns error when index push fails", func(t *testing.T) {
		dir := t.TempDir()
		weightsDir := filepath.Join(dir, "weights")
		require.NoError(t, os.MkdirAll(weightsDir, 0o755))
		modelPath := filepath.Join(weightsDir, "model.bin")
		require.NoError(t, os.WriteFile(modelPath, []byte("test"), 0o644))

		testImg := empty.Image
		testImg, _ = mutate.Config(testImg, v1.Config{})

		docker := &mockDocker{
			pushFunc: func(ctx context.Context, ref string) error {
				return nil
			},
		}
		reg := &mockRegistry{
			getImageFunc: func(ctx context.Context, ref string, platform *registry.Platform) (v1.Image, error) {
				return testImg, nil
			},
			pushIndexFunc: func(ctx context.Context, ref string, idx v1.ImageIndex) error {
				return errors.New("index push failed")
			},
		}

		pusher := NewBundlePusher(docker, reg)
		m := &Model{
			Image:       &ImageArtifact{Reference: "r8.im/user/model:latest"},
			ImageFormat: FormatBundle,
			WeightsManifest: &WeightsManifest{
				Files: []WeightFile{
					{Name: "my-model-v1", Dest: "/cache/model.bin"},
				},
			},
		}

		err := pusher.Push(context.Background(), m, PushOptions{
			FilePaths: map[string]string{"my-model-v1": modelPath},
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "push OCI index")
	})
}

// =============================================================================
// Resolver.Push tests
// =============================================================================

func TestResolver_Push_SelectsCorrectPusher(t *testing.T) {
	t.Run("uses ImagePusher for standalone format", func(t *testing.T) {
		var pushedRef string
		docker := &mockDocker{
			pushFunc: func(ctx context.Context, ref string) error {
				pushedRef = ref
				return nil
			},
		}
		reg := &mockRegistry{}
		resolver := NewResolver(docker, reg)

		m := &Model{
			Image:       &ImageArtifact{Reference: "r8.im/user/model:latest"},
			ImageFormat: FormatStandalone,
		}

		err := resolver.Push(context.Background(), m, PushOptions{})

		require.NoError(t, err)
		require.Equal(t, "r8.im/user/model:latest", pushedRef)
	})

	t.Run("uses ImagePusher for empty format (default)", func(t *testing.T) {
		var pushedRef string
		docker := &mockDocker{
			pushFunc: func(ctx context.Context, ref string) error {
				pushedRef = ref
				return nil
			},
		}
		reg := &mockRegistry{}
		resolver := NewResolver(docker, reg)

		m := &Model{
			Image:       &ImageArtifact{Reference: "r8.im/user/model:latest"},
			ImageFormat: "", // empty = default to standalone
		}

		err := resolver.Push(context.Background(), m, PushOptions{})

		require.NoError(t, err)
		require.Equal(t, "r8.im/user/model:latest", pushedRef)
	})

	t.Run("uses BundlePusher for bundle format", func(t *testing.T) {
		// BundlePusher requires WeightsManifest, so we expect an error
		// if we don't provide it (this tests that it's selected)
		docker := &mockDocker{}
		reg := &mockRegistry{}
		resolver := NewResolver(docker, reg)

		m := &Model{
			Image:           &ImageArtifact{Reference: "r8.im/user/model:latest"},
			ImageFormat:     FormatBundle,
			WeightsManifest: nil, // intentionally nil to trigger error
		}

		err := resolver.Push(context.Background(), m, PushOptions{})

		require.Error(t, err)
		require.Contains(t, err.Error(), "bundle format requires WeightsManifest")
	})
}

// =============================================================================
// PushOptions tests
// =============================================================================

func TestPushOptions(t *testing.T) {
	t.Run("ProjectDir field", func(t *testing.T) {
		opts := PushOptions{
			ProjectDir: "/path/to/project",
		}
		require.Equal(t, "/path/to/project", opts.ProjectDir)
	})

	t.Run("Platform field", func(t *testing.T) {
		opts := PushOptions{
			Platform: &Platform{OS: "linux", Architecture: "arm64"},
		}
		require.Equal(t, "linux", opts.Platform.OS)
		require.Equal(t, "arm64", opts.Platform.Architecture)
	})
}
