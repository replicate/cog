package model

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/stretchr/testify/require"
)

const testImageRef = "r8.im/user/model:latest"

// testBundleModel builds a Model with a fixed image ref and optional weights.
func testBundleModel(weights ...Weight) *Model {
	return &Model{
		Image: &ImageArtifact{Reference: testImageRef},
		Artifacts: []Artifact{
			&ImageArtifact{name: "model", Reference: testImageRef},
		},
		Weights: weights,
	}
}

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

		err := pusher.Push(context.Background(), testBundleModel(), PushOptions{})
		require.NoError(t, err)
	})

	t.Run("full push flow succeeds with single weight", func(t *testing.T) {
		w := Weight{
			Name:      "model-v1",
			Target:    "/src/weights/model-v1",
			Digest:    "sha256:weightdigest123",
			SetDigest: "sha256:setdigestabc",
			Size:      4096,
		}

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
			Digest:    v1.Hash{Algorithm: "sha256", Hex: "imgdigestabc1234567"},
		}

		weightDesc := v1.Descriptor{
			MediaType: types.OCIManifestSchema1,
			Size:      500,
			Digest:    v1.Hash{Algorithm: "sha256", Hex: "weightdigest123"},
		}

		reg := &mockRegistry{
			getDescriptorFunc: func(ctx context.Context, ref string) (v1.Descriptor, error) {
				track("registry:getDescriptor:" + ref)
				expectedWeightTag := WeightTag(w.Name, w.SetDigest)
				if ref == "r8.im/user/model:"+expectedWeightTag {
					return weightDesc, nil
				}
				return imgDesc, nil
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
				require.NotEmpty(t, idxManifest.Manifests[1].Annotations[AnnotationV1WeightName])
				require.NotEmpty(t, idxManifest.Manifests[1].Annotations[AnnotationV1WeightSetDigest])

				return nil
			},
		}

		pusher := NewBundlePusher(docker, reg)

		err := pusher.Push(context.Background(), testBundleModel(w), PushOptions{
			Platform: &Platform{OS: "linux", Architecture: "amd64"},
		})

		require.NoError(t, err)

		// Verify call sequence. Weight verification happens first,
		// then docker push, then image HEAD, then index push.
		require.Len(t, callOrder, 4)
		expectedTag := WeightTag(w.Name, w.SetDigest)
		require.Equal(t, "registry:getDescriptor:r8.im/user/model:"+expectedTag, callOrder[0],
			"weight verification must happen before image push")
		require.Equal(t, "docker:push:r8.im/user/model:latest", callOrder[1])
		require.Equal(t, "registry:getDescriptor:r8.im/user/model:latest", callOrder[2])
		require.Equal(t, "registry:pushIndex:r8.im/user/model:latest", callOrder[3])
	})

	t.Run("uses default platform when not specified", func(t *testing.T) {
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
				idxManifest, _ := idx.IndexManifest()
				// Default platform should be linux/amd64
				require.Equal(t, "linux", idxManifest.Manifests[0].Platform.OS)
				require.Equal(t, "amd64", idxManifest.Manifests[0].Platform.Architecture)
				return nil
			},
		}

		pusher := NewBundlePusher(docker, reg)

		err := pusher.Push(context.Background(), testBundleModel(
			Weight{Name: "w1", Target: "/src/weights/w1", SetDigest: "sha256:abc"},
		), PushOptions{})
		require.NoError(t, err)
	})

	t.Run("returns error when image push fails", func(t *testing.T) {
		docker := &mockDocker{
			pushFunc: func(ctx context.Context, ref string) error {
				return errors.New("unauthorized: authentication required")
			},
		}
		reg := &mockRegistry{
			getDescriptorFunc: func(ctx context.Context, ref string) (v1.Descriptor, error) {
				// Weight verification succeeds; image push will fail.
				return v1.Descriptor{
					MediaType: types.OCIManifestSchema1,
					Size:      100,
					Digest:    v1.Hash{Algorithm: "sha256", Hex: "abc"},
				}, nil
			},
		}

		pusher := NewBundlePusher(docker, reg)
		w1 := Weight{Name: "w1", Target: "/src/weights/w1", SetDigest: "sha256:abc"}

		err := pusher.Push(context.Background(), testBundleModel(w1), PushOptions{})

		require.Error(t, err)
		require.Contains(t, err.Error(), "push image")
		require.Contains(t, err.Error(), "unauthorized")
	})

	t.Run("returns error when get descriptor fails", func(t *testing.T) {
		docker := &mockDocker{
			pushFunc: func(ctx context.Context, ref string) error { return nil },
		}
		reg := &mockRegistry{
			getDescriptorFunc: func(ctx context.Context, ref string) (v1.Descriptor, error) {
				return v1.Descriptor{}, errors.New("manifest not found")
			},
		}

		pusher := NewBundlePusher(docker, reg)
		w1 := Weight{Name: "w1", Target: "/src/weights/w1", SetDigest: "sha256:abc"}

		err := pusher.Push(context.Background(), testBundleModel(w1), PushOptions{})

		require.Error(t, err)
		require.Contains(t, err.Error(), "manifest not found")
	})

	t.Run("returns error when weight manifest not in registry", func(t *testing.T) {
		docker := &mockDocker{
			pushFunc: func(ctx context.Context, ref string) error {
				t.Fatal("docker push should not be called when weight verification fails")
				return nil
			},
		}
		reg := &mockRegistry{
			getDescriptorFunc: func(ctx context.Context, ref string) (v1.Descriptor, error) {
				// Weight HEAD fails — verification happens before image push.
				return v1.Descriptor{}, errors.New("manifest unknown")
			},
		}

		pusher := NewBundlePusher(docker, reg)
		w1 := Weight{Name: "w1", Target: "/src/weights/w1", SetDigest: "sha256:abc"}

		err := pusher.Push(context.Background(), testBundleModel(w1), PushOptions{})

		require.Error(t, err)
		require.Contains(t, err.Error(), "w1")
		require.Contains(t, err.Error(), "not found in registry")
		require.Contains(t, err.Error(), "cog weights import")
		require.Contains(t, err.Error(), "manifest unknown")
	})

	t.Run("returns error when index push fails", func(t *testing.T) {
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
				return errors.New("index push failed")
			},
		}

		pusher := NewBundlePusher(docker, reg)
		w1 := Weight{Name: "w1", Target: "/src/weights/w1", SetDigest: "sha256:abc"}

		err := pusher.Push(context.Background(), testBundleModel(w1), PushOptions{})

		require.Error(t, err)
		require.Contains(t, err.Error(), "push OCI index")
	})

	t.Run("verifies multiple weights concurrently", func(t *testing.T) {
		docker := &mockDocker{
			pushFunc: func(ctx context.Context, ref string) error { return nil },
		}

		// Use atomic counter — safe for concurrent access from goroutines
		var headCheckCount atomic.Int32
		reg := &mockRegistry{
			getDescriptorFunc: func(ctx context.Context, ref string) (v1.Descriptor, error) {
				// Count HEAD checks for weight tags (not the image tag)
				if ref != "r8.im/user/model:latest" {
					headCheckCount.Add(1)
				}
				return v1.Descriptor{
					MediaType: types.OCIManifestSchema1,
					Size:      100,
					Digest:    v1.Hash{Algorithm: "sha256", Hex: "abc"},
				}, nil
			},
			pushIndexFunc: func(ctx context.Context, ref string, idx v1.ImageIndex) error {
				idxManifest, _ := idx.IndexManifest()
				require.Len(t, idxManifest.Manifests, 3) // 1 image + 2 weights
				return nil
			},
		}

		pusher := NewBundlePusher(docker, reg)

		err := pusher.Push(context.Background(), testBundleModel(
			Weight{Name: "w1", Target: "/src/weights/w1", SetDigest: "sha256:set1"},
			Weight{Name: "w2", Target: "/src/weights/w2", SetDigest: "sha256:set2"},
		), PushOptions{})

		require.NoError(t, err)
		require.Equal(t, int32(2), headCheckCount.Load()) // both weights HEAD-checked
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

		err := resolver.Push(context.Background(), testBundleModel(), PushOptions{})
		require.NoError(t, err)
		require.True(t, dockerPushed, "standalone should use docker push")
	})

	t.Run("no weights uses docker push", func(t *testing.T) {
		var dockerPushed bool
		docker := &mockDocker{
			pushFunc: func(ctx context.Context, ref string) error {
				dockerPushed = true
				return nil
			},
		}
		reg := &mockRegistry{}
		resolver := NewResolver(docker, reg)

		err := resolver.Push(context.Background(), testBundleModel(), PushOptions{})
		require.NoError(t, err)
		require.True(t, dockerPushed, "model without weights should use docker push")
	})

	t.Run("bundle with weights produces an OCI index", func(t *testing.T) {
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

		err := resolver.Push(context.Background(), testBundleModel(
			Weight{Name: "w1", Target: "/src/weights/w1", SetDigest: "sha256:abc"},
		), PushOptions{})
		require.NoError(t, err)
		require.True(t, indexPushed, "bundle with weights should push an OCI index")
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
		require.Contains(t, err.Error(), "no image artifact")
	})

	t.Run("bundle returns error when no image artifact", func(t *testing.T) {
		docker := &mockDocker{}
		reg := &mockRegistry{}
		resolver := NewResolver(docker, reg)

		m := &Model{
			Weights: []Weight{
				{Name: "w1", Target: "/src/weights/w1", SetDigest: "sha256:abc"},
			},
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
