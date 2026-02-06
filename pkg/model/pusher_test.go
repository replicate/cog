package model

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// BundlePusher tests
// =============================================================================

func TestBundlePusher_Push(t *testing.T) {
	t.Run("returns error when no image artifact in model", func(t *testing.T) {
		docker := &mockDocker{}
		reg := &mockRegistry{}
		pusher := NewBundlePusher(docker, reg)
		m := &Model{
			Image:     nil,
			Artifacts: []Artifact{}, // no image artifact
		}

		err := pusher.Push(context.Background(), m, PushOptions{})

		require.Error(t, err)
		require.Contains(t, err.Error(), "no image artifact")
	})

	t.Run("pushes image-only model as single-entry index", func(t *testing.T) {
		docker := &mockDocker{
			pushFunc: func(ctx context.Context, ref string) error { return nil },
		}

		imgDesc := v1.Descriptor{
			MediaType: types.OCIManifestSchema1,
			Size:      1234,
			Digest:    v1.Hash{Algorithm: "sha256", Hex: "imgonly"},
		}

		reg := &mockRegistry{
			getDescriptorFunc: func(ctx context.Context, ref string) (v1.Descriptor, error) {
				return imgDesc, nil
			},
			pushIndexFunc: func(ctx context.Context, ref string, idx v1.ImageIndex) error {
				// Verify index has exactly 1 entry (image only, no weights)
				idxManifest, err := idx.IndexManifest()
				require.NoError(t, err)
				require.Len(t, idxManifest.Manifests, 1)
				require.Equal(t, imgDesc.Digest, idxManifest.Manifests[0].Digest)
				require.Equal(t, "linux", idxManifest.Manifests[0].Platform.OS)
				return nil
			},
		}

		pusher := NewBundlePusher(docker, reg)
		m := &Model{
			Image: &ImageArtifact{Reference: "r8.im/user/model:latest"},
			Artifacts: []Artifact{
				&ImageArtifact{name: "model", Reference: "r8.im/user/model:latest"},
				// no weight artifacts — image-only model
			},
		}

		err := pusher.Push(context.Background(), m, PushOptions{})
		require.NoError(t, err)
	})

	t.Run("full push flow succeeds with single weight", func(t *testing.T) {
		// Create temp weight file
		dir := t.TempDir()
		weightPath := filepath.Join(dir, "model.safetensors")
		require.NoError(t, os.WriteFile(weightPath, []byte("fake weight data"), 0o644))

		// Track call sequence (mutex-protected for goroutine safety)
		var mu sync.Mutex
		var callOrder []string
		track := func(entry string) {
			mu.Lock()
			callOrder = append(callOrder, entry)
			mu.Unlock()
		}

		docker := &mockDocker{
			pushFunc: func(ctx context.Context, ref string) error {
				track("docker:push:" + ref)
				return nil
			},
		}

		imgDesc := v1.Descriptor{
			MediaType: types.OCIManifestSchema1,
			Size:      1234,
			Digest:    v1.Hash{Algorithm: "sha256", Hex: "imgdigest"},
		}

		reg := &mockRegistry{
			getDescriptorFunc: func(ctx context.Context, ref string) (v1.Descriptor, error) {
				track("registry:getDescriptor:" + ref)
				return imgDesc, nil
			},
			pushImageFunc: func(ctx context.Context, ref string, img v1.Image) error {
				track("registry:pushImage:" + ref)
				return nil
			},
			pushIndexFunc: func(ctx context.Context, ref string, idx v1.ImageIndex) error {
				track("registry:pushIndex:" + ref)

				// Verify the index structure
				idxManifest, err := idx.IndexManifest()
				require.NoError(t, err)
				require.Len(t, idxManifest.Manifests, 2) // image + 1 weight

				// First manifest: image with platform
				require.Equal(t, imgDesc.Digest, idxManifest.Manifests[0].Digest)
				require.Equal(t, "linux", idxManifest.Manifests[0].Platform.OS)
				require.Equal(t, "amd64", idxManifest.Manifests[0].Platform.Architecture)

				// Second manifest: weight with annotations
				require.Equal(t, PlatformUnknown, idxManifest.Manifests[1].Platform.OS)
				require.Equal(t, AnnotationValueWeights, idxManifest.Manifests[1].Annotations[AnnotationReferenceType])
				require.Equal(t, imgDesc.Digest.String(), idxManifest.Manifests[1].Annotations[AnnotationReferenceDigest])

				return nil
			},
		}

		pusher := NewBundlePusher(docker, reg)
		m := &Model{
			Image: &ImageArtifact{Reference: "r8.im/user/model:latest"},
			Artifacts: []Artifact{
				&ImageArtifact{name: "model", Reference: "r8.im/user/model:latest"},
				NewWeightArtifact("model-v1", v1.Descriptor{}, weightPath, "/weights/model.safetensors", WeightConfig{
					SchemaVersion: "1.0",
					CogVersion:    "0.15.0",
					Name:          "model-v1",
					Target:        "/weights/model.safetensors",
					Created:       time.Now().UTC(),
				}),
			},
		}

		err := pusher.Push(context.Background(), m, PushOptions{
			Platform: &Platform{OS: "linux", Architecture: "amd64"},
		})

		require.NoError(t, err)

		// Verify the call sequence:
		// 1. Push image via docker
		// 2. Get image descriptor from registry (lightweight HEAD)
		// 3. Push weight via registry
		// 4. Push OCI index to registry
		require.Len(t, callOrder, 4)
		require.Equal(t, "docker:push:r8.im/user/model:latest", callOrder[0])
		require.Equal(t, "registry:getDescriptor:r8.im/user/model:latest", callOrder[1])
		require.Equal(t, "registry:pushImage:r8.im/user/model", callOrder[2]) // repo, no tag
		require.Equal(t, "registry:pushIndex:r8.im/user/model:latest", callOrder[3])
	})

	t.Run("uses default platform when not specified", func(t *testing.T) {
		dir := t.TempDir()
		weightPath := filepath.Join(dir, "model.bin")
		require.NoError(t, os.WriteFile(weightPath, []byte("test"), 0o644))

		docker := &mockDocker{
			pushFunc: func(ctx context.Context, ref string) error { return nil },
		}

		reg := &mockRegistry{
			getDescriptorFunc: func(ctx context.Context, ref string) (v1.Descriptor, error) {
				return v1.Descriptor{
					MediaType: types.OCIManifestSchema1,
					Size:      100,
					Digest:    v1.Hash{Algorithm: "sha256", Hex: "abc"},
				}, nil
			},
			pushImageFunc: func(ctx context.Context, ref string, img v1.Image) error { return nil },
			pushIndexFunc: func(ctx context.Context, ref string, idx v1.ImageIndex) error {
				idxManifest, _ := idx.IndexManifest()
				// Default platform should be linux/amd64
				require.Equal(t, "linux", idxManifest.Manifests[0].Platform.OS)
				require.Equal(t, "amd64", idxManifest.Manifests[0].Platform.Architecture)
				return nil
			},
		}

		pusher := NewBundlePusher(docker, reg)
		m := &Model{
			Image: &ImageArtifact{Reference: "r8.im/user/model:latest"},
			Artifacts: []Artifact{
				&ImageArtifact{name: "model", Reference: "r8.im/user/model:latest"},
				NewWeightArtifact("w1", v1.Descriptor{}, weightPath, "/weights/model.bin", WeightConfig{
					SchemaVersion: "1.0", CogVersion: "0.15.0", Name: "w1",
					Target: "/weights/model.bin", Created: time.Now().UTC(),
				}),
			},
		}

		err := pusher.Push(context.Background(), m, PushOptions{})
		require.NoError(t, err)
	})

	t.Run("returns error when image push fails", func(t *testing.T) {
		dir := t.TempDir()
		weightPath := filepath.Join(dir, "model.bin")
		require.NoError(t, os.WriteFile(weightPath, []byte("test"), 0o644))

		docker := &mockDocker{
			pushFunc: func(ctx context.Context, ref string) error {
				return errors.New("unauthorized: authentication required")
			},
		}
		reg := &mockRegistry{}

		pusher := NewBundlePusher(docker, reg)
		m := &Model{
			Image: &ImageArtifact{Reference: "r8.im/user/model:latest"},
			Artifacts: []Artifact{
				&ImageArtifact{name: "model", Reference: "r8.im/user/model:latest"},
				NewWeightArtifact("w1", v1.Descriptor{}, weightPath, "/weights/model.bin", WeightConfig{
					SchemaVersion: "1.0", CogVersion: "0.15.0", Name: "w1",
					Target: "/weights/model.bin", Created: time.Now().UTC(),
				}),
			},
		}

		err := pusher.Push(context.Background(), m, PushOptions{})

		require.Error(t, err)
		require.Contains(t, err.Error(), "push image")
		require.Contains(t, err.Error(), "unauthorized")
	})

	t.Run("returns error when get descriptor fails", func(t *testing.T) {
		dir := t.TempDir()
		weightPath := filepath.Join(dir, "model.bin")
		require.NoError(t, os.WriteFile(weightPath, []byte("test"), 0o644))

		docker := &mockDocker{
			pushFunc: func(ctx context.Context, ref string) error { return nil },
		}
		reg := &mockRegistry{
			getDescriptorFunc: func(ctx context.Context, ref string) (v1.Descriptor, error) {
				return v1.Descriptor{}, errors.New("manifest not found")
			},
		}

		pusher := NewBundlePusher(docker, reg)
		m := &Model{
			Image: &ImageArtifact{Reference: "r8.im/user/model:latest"},
			Artifacts: []Artifact{
				&ImageArtifact{name: "model", Reference: "r8.im/user/model:latest"},
				NewWeightArtifact("w1", v1.Descriptor{}, weightPath, "/weights/model.bin", WeightConfig{
					SchemaVersion: "1.0", CogVersion: "0.15.0", Name: "w1",
					Target: "/weights/model.bin", Created: time.Now().UTC(),
				}),
			},
		}

		err := pusher.Push(context.Background(), m, PushOptions{})

		require.Error(t, err)
		require.Contains(t, err.Error(), "get image descriptor")
	})

	t.Run("returns error when weight push fails", func(t *testing.T) {
		dir := t.TempDir()
		weightPath := filepath.Join(dir, "model.bin")
		require.NoError(t, os.WriteFile(weightPath, []byte("test"), 0o644))

		docker := &mockDocker{
			pushFunc: func(ctx context.Context, ref string) error { return nil },
		}
		reg := &mockRegistry{
			getDescriptorFunc: func(ctx context.Context, ref string) (v1.Descriptor, error) {
				return v1.Descriptor{
					MediaType: types.OCIManifestSchema1,
					Size:      100,
					Digest:    v1.Hash{Algorithm: "sha256", Hex: "abc"},
				}, nil
			},
			pushImageFunc: func(ctx context.Context, ref string, img v1.Image) error {
				return errors.New("weight push failed: quota exceeded")
			},
		}

		pusher := NewBundlePusher(docker, reg)
		m := &Model{
			Image: &ImageArtifact{Reference: "r8.im/user/model:latest"},
			Artifacts: []Artifact{
				&ImageArtifact{name: "model", Reference: "r8.im/user/model:latest"},
				NewWeightArtifact("w1", v1.Descriptor{}, weightPath, "/weights/model.bin", WeightConfig{
					SchemaVersion: "1.0", CogVersion: "0.15.0", Name: "w1",
					Target: "/weights/model.bin", Created: time.Now().UTC(),
				}),
			},
		}

		err := pusher.Push(context.Background(), m, PushOptions{})

		require.Error(t, err)
		require.Contains(t, err.Error(), "push weight")
		require.Contains(t, err.Error(), "w1")
	})

	t.Run("returns error when index push fails", func(t *testing.T) {
		dir := t.TempDir()
		weightPath := filepath.Join(dir, "model.bin")
		require.NoError(t, os.WriteFile(weightPath, []byte("test"), 0o644))

		docker := &mockDocker{
			pushFunc: func(ctx context.Context, ref string) error { return nil },
		}
		reg := &mockRegistry{
			getDescriptorFunc: func(ctx context.Context, ref string) (v1.Descriptor, error) {
				return v1.Descriptor{
					MediaType: types.OCIManifestSchema1,
					Size:      100,
					Digest:    v1.Hash{Algorithm: "sha256", Hex: "abc"},
				}, nil
			},
			pushImageFunc: func(ctx context.Context, ref string, img v1.Image) error { return nil },
			pushIndexFunc: func(ctx context.Context, ref string, idx v1.ImageIndex) error {
				return errors.New("index push failed")
			},
		}

		pusher := NewBundlePusher(docker, reg)
		m := &Model{
			Image: &ImageArtifact{Reference: "r8.im/user/model:latest"},
			Artifacts: []Artifact{
				&ImageArtifact{name: "model", Reference: "r8.im/user/model:latest"},
				NewWeightArtifact("w1", v1.Descriptor{}, weightPath, "/weights/model.bin", WeightConfig{
					SchemaVersion: "1.0", CogVersion: "0.15.0", Name: "w1",
					Target: "/weights/model.bin", Created: time.Now().UTC(),
				}),
			},
		}

		err := pusher.Push(context.Background(), m, PushOptions{})

		require.Error(t, err)
		require.Contains(t, err.Error(), "push OCI index")
	})

	t.Run("pushes multiple weights concurrently", func(t *testing.T) {
		dir := t.TempDir()
		weight1Path := filepath.Join(dir, "model1.bin")
		weight2Path := filepath.Join(dir, "model2.bin")
		require.NoError(t, os.WriteFile(weight1Path, []byte("weight 1 data"), 0o644))
		require.NoError(t, os.WriteFile(weight2Path, []byte("weight 2 data"), 0o644))

		docker := &mockDocker{
			pushFunc: func(ctx context.Context, ref string) error { return nil },
		}

		// Use atomic counter — safe for concurrent access from goroutines
		var pushedWeightCount atomic.Int32
		reg := &mockRegistry{
			getDescriptorFunc: func(ctx context.Context, ref string) (v1.Descriptor, error) {
				return v1.Descriptor{
					MediaType: types.OCIManifestSchema1,
					Size:      100,
					Digest:    v1.Hash{Algorithm: "sha256", Hex: "abc"},
				}, nil
			},
			pushImageFunc: func(ctx context.Context, ref string, img v1.Image) error {
				pushedWeightCount.Add(1)
				return nil
			},
			pushIndexFunc: func(ctx context.Context, ref string, idx v1.ImageIndex) error {
				idxManifest, _ := idx.IndexManifest()
				require.Len(t, idxManifest.Manifests, 3) // 1 image + 2 weights
				return nil
			},
		}

		pusher := NewBundlePusher(docker, reg)
		m := &Model{
			Image: &ImageArtifact{Reference: "r8.im/user/model:latest"},
			Artifacts: []Artifact{
				&ImageArtifact{name: "model", Reference: "r8.im/user/model:latest"},
				NewWeightArtifact("w1", v1.Descriptor{}, weight1Path, "/weights/model1.bin", WeightConfig{
					SchemaVersion: "1.0", CogVersion: "0.15.0", Name: "w1",
					Target: "/weights/model1.bin", Created: time.Now().UTC(),
				}),
				NewWeightArtifact("w2", v1.Descriptor{}, weight2Path, "/weights/model2.bin", WeightConfig{
					SchemaVersion: "1.0", CogVersion: "0.15.0", Name: "w2",
					Target: "/weights/model2.bin", Created: time.Now().UTC(),
				}),
			},
		}

		err := pusher.Push(context.Background(), m, PushOptions{})

		require.NoError(t, err)
		require.Equal(t, int32(2), pushedWeightCount.Load()) // both weights pushed
	})
}

// =============================================================================
// Resolver.Push tests
// =============================================================================

func TestResolver_Push(t *testing.T) {
	t.Run("default uses docker push", func(t *testing.T) {
		var dockerPushed bool
		docker := &mockDocker{
			pushFunc: func(ctx context.Context, ref string) error {
				dockerPushed = true
				return nil
			},
		}
		reg := &mockRegistry{}
		resolver := NewResolver(docker, reg)

		m := &Model{
			Image: &ImageArtifact{Reference: "r8.im/user/model:latest"},
			Artifacts: []Artifact{
				&ImageArtifact{name: "model", Reference: "r8.im/user/model:latest"},
			},
		}

		err := resolver.Push(context.Background(), m, PushOptions{})
		require.NoError(t, err)
		require.True(t, dockerPushed, "standalone should use docker push")
	})

	t.Run("OCIIndex false uses docker push", func(t *testing.T) {
		var dockerPushed bool
		docker := &mockDocker{
			pushFunc: func(ctx context.Context, ref string) error {
				dockerPushed = true
				return nil
			},
		}
		reg := &mockRegistry{}
		resolver := NewResolver(docker, reg)

		m := &Model{
			// OCIIndex not set (false by default)
			Image: &ImageArtifact{Reference: "r8.im/user/model:latest"},
			Artifacts: []Artifact{
				&ImageArtifact{name: "model", Reference: "r8.im/user/model:latest"},
			},
		}

		err := resolver.Push(context.Background(), m, PushOptions{})
		require.NoError(t, err)
		require.True(t, dockerPushed, "default format should use docker push")
	})

	t.Run("OCIIndex true produces an OCI index", func(t *testing.T) {
		var indexPushed bool
		docker := &mockDocker{
			pushFunc: func(ctx context.Context, ref string) error { return nil },
		}
		reg := &mockRegistry{
			getDescriptorFunc: func(ctx context.Context, ref string) (v1.Descriptor, error) {
				return v1.Descriptor{
					MediaType: types.OCIManifestSchema1,
					Size:      100,
					Digest:    v1.Hash{Algorithm: "sha256", Hex: "abc"},
				}, nil
			},
			pushIndexFunc: func(ctx context.Context, ref string, idx v1.ImageIndex) error {
				indexPushed = true
				return nil
			},
		}
		resolver := NewResolver(docker, reg)

		m := &Model{
			OCIIndex: true,
			Image:    &ImageArtifact{Reference: "r8.im/user/model:latest"},
			Artifacts: []Artifact{
				&ImageArtifact{name: "model", Reference: "r8.im/user/model:latest"},
			},
		}

		err := resolver.Push(context.Background(), m, PushOptions{})
		require.NoError(t, err)
		require.True(t, indexPushed, "OCIIndex=true should push an OCI index")
	})

	t.Run("standalone returns error when image nil", func(t *testing.T) {
		docker := &mockDocker{}
		reg := &mockRegistry{}
		resolver := NewResolver(docker, reg)

		m := &Model{
			Image:     nil,
			Artifacts: []Artifact{},
		}

		err := resolver.Push(context.Background(), m, PushOptions{})
		require.Error(t, err)
		require.Contains(t, err.Error(), "artifact is nil")
	})

	t.Run("OCIIndex true returns error when no image artifact", func(t *testing.T) {
		docker := &mockDocker{}
		reg := &mockRegistry{}
		resolver := NewResolver(docker, reg)

		m := &Model{
			OCIIndex:  true,
			Image:     nil,
			Artifacts: []Artifact{},
		}

		err := resolver.Push(context.Background(), m, PushOptions{})
		require.Error(t, err)
		require.Contains(t, err.Error(), "no image artifact")
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

// =============================================================================
// repoFromReference tests
// =============================================================================

func TestRepoFromReference(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"r8.im/user/model:latest", "r8.im/user/model"},
		{"r8.im/user/model:v1.0", "r8.im/user/model"},
		{"r8.im/user/model@sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", "r8.im/user/model"},
		{"r8.im/user/model", "r8.im/user/model"},
		{"registry.example.com/org/model:tag", "registry.example.com/org/model"},
		{"localhost:5000/model:latest", "localhost:5000/model"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := repoFromReference(tt.input)
			require.Equal(t, tt.want, got)
		})
	}
}
