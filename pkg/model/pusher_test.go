package model

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/stretchr/testify/require"
)

const (
	testImageRef = "r8.im/user/model:latest"
	testRepo     = "r8.im/user/model"
	testModelTag = "20260512T120000Z"
)

// testCogImageRef and testModelRef are the registry destinations
// BundlePusher should produce for a Model built from testBundleModel.
// Derived from ImageTag/ResolvedRef.String so the fixtures track the
// helpers; tag_test.go covers ImageTag's formatting independently.
var (
	testCogImageRef = testRepo + ":" + ImageTag(testModelTag)
	testModelRef    = testRepo + ":" + testModelTag
)

// Valid 64-char hex digests for use in test fixtures. v1.NewHash
// rejects non-hex strings, so the verifyWeights digest-equality check
// requires real-looking digests.
const (
	testW1Digest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	testW2Digest = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
)

// testBundleModel builds a Model with a deterministic Ref so tests
// can assert on exact destination refs. The same *ImageArtifact
// instance is shared between Model.Image and Model.Artifacts[0] —
// production builds (see modelFromImage) maintain this invariant
// and Push relies on it.
func testBundleModel(weights ...Weight) *Model {
	img := &ImageArtifact{name: "model", Reference: testImageRef}
	return &Model{
		Format: FormatBundle,
		Ref: &ResolvedRef{
			Registry: "r8.im",
			Repo:     "user/model",
			Tag:      testModelTag,
		},
		Image:     img,
		Artifacts: []Artifact{img},
		Weights:   weights,
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

		_, err := pusher.Push(context.Background(), m, PushOptions{})

		require.Error(t, err)
		require.Contains(t, err.Error(), "no image artifact")
	})

	t.Run("returns error when Model.Ref is missing", func(t *testing.T) {
		docker := &mockDocker{}
		reg := &mockRegistry{}
		pusher := NewBundlePusher(docker, reg)
		m := &Model{
			Format: FormatBundle,
			Image:  &ImageArtifact{Reference: testImageRef},
			Artifacts: []Artifact{
				&ImageArtifact{name: "model", Reference: testImageRef},
			},
			// Ref deliberately nil.
		}

		_, err := pusher.Push(context.Background(), m, PushOptions{})

		require.Error(t, err)
		require.Contains(t, err.Error(), "Model.Ref")
	})

	t.Run("returns error when Model.Ref is digest-pinned", func(t *testing.T) {
		docker := &mockDocker{}
		reg := &mockRegistry{}
		pusher := NewBundlePusher(docker, reg)
		m := testBundleModel()
		m.Ref = &ResolvedRef{
			Registry: "r8.im",
			Repo:     "user/model",
			Digest:   testW1Digest, // digest-pinned, no Tag
		}

		_, err := pusher.Push(context.Background(), m, PushOptions{})

		require.Error(t, err)
		require.Contains(t, err.Error(), "digest-pinned")
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

		_, err := pusher.Push(context.Background(), testBundleModel(), PushOptions{})
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
			tagFunc: func(ctx context.Context, source, target string) error {
				track("docker:tag:" + source + "->" + target)
				return nil
			},
			removeFunc: func(ctx context.Context, ref string) error {
				track("docker:remove:" + ref)
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

		weightRef := testRepo + "@" + w.Digest

		// Captured from pushIndexFunc; the index descriptor is
		// derived locally from idx.Digest() so the test asserts
		// against whatever the production code computes.
		var pushedIndexDigest string

		reg := &mockRegistry{
			getDescriptorFunc: func(ctx context.Context, ref string) (v1.Descriptor, error) {
				track("registry:getDescriptor:" + ref)
				switch ref {
				case weightRef:
					return weightDesc, nil
				case testCogImageRef:
					return imgDesc, nil
				}
				return v1.Descriptor{}, fmt.Errorf("unexpected descriptor lookup: %s", ref)
			},
			pushIndexFunc: func(ctx context.Context, ref string, idx v1.ImageIndex) error {
				track("registry:pushIndex:" + ref)

				idxManifest, err := idx.IndexManifest()
				require.NoError(t, err)
				require.Len(t, idxManifest.Manifests, 2) // image + 1 weight

				require.Equal(t, imgDesc.Digest, idxManifest.Manifests[0].Digest)
				require.Equal(t, "linux", idxManifest.Manifests[0].Platform.OS)
				require.Equal(t, "amd64", idxManifest.Manifests[0].Platform.Architecture)

				require.Equal(t, PlatformUnknown, idxManifest.Manifests[1].Platform.OS)
				require.NotEmpty(t, idxManifest.Manifests[1].Annotations[AnnotationV1WeightName])
				require.NotEmpty(t, idxManifest.Manifests[1].Annotations[AnnotationV1WeightSetDigest])

				digest, err := idx.Digest()
				require.NoError(t, err)
				pushedIndexDigest = digest.String()

				return nil
			},
		}

		pusher := NewBundlePusher(docker, reg)

		pushed, err := pusher.Push(context.Background(), testBundleModel(w), PushOptions{
			Platform: &Platform{OS: "linux", Architecture: "amd64"},
		})

		require.NoError(t, err)
		require.NotNil(t, pushed)

		// Verify call sequence: weight verified first (HEAD by digest,
		// before anything mutates the registry), then local re-tag,
		// then docker push, then image HEAD, then index push, then
		// the deferred local-tag cleanup. The index descriptor is
		// computed locally from the v1.ImageIndex bytes — no HEAD.
		require.Equal(t,
			[]string{
				"registry:getDescriptor:" + weightRef,
				"docker:tag:" + testImageRef + "->" + testCogImageRef,
				"docker:push:" + testCogImageRef,
				"registry:getDescriptor:" + testCogImageRef,
				"registry:pushIndex:" + testModelRef,
				"docker:remove:" + testCogImageRef,
			},
			callOrder,
		)
		// Negative: the local image tag must never reach the registry
		// directly — push and HEAD always target the cog-image tag.
		require.NotContains(t, callOrder, "docker:push:"+testImageRef)
		require.NotContains(t, callOrder, "registry:getDescriptor:"+testImageRef)
		require.NotContains(t, callOrder, "registry:pushIndex:"+testImageRef)

		// Enriched return values.
		require.NotNil(t, pushed.Ref)
		require.NotEmpty(t, pushedIndexDigest, "pushIndexFunc should have captured the index digest")
		require.Equal(t, pushedIndexDigest, pushed.Ref.Digest)
		require.Equal(t, testModelTag, pushed.Ref.Tag)
		require.Equal(t, testRepo+"@"+pushedIndexDigest, pushed.Ref.String())

		require.NotNil(t, pushed.Image)
		require.Equal(t, testRepo+"@"+imgDesc.Digest.String(), pushed.Image.Reference)
		require.Equal(t, imgDesc.Digest.String(), pushed.Image.Digest)

		require.Len(t, pushed.Weights, 1)
		require.Equal(t, weightRef, pushed.Weights[0].Reference)
		require.Equal(t, WeightTag(w.Name, w.SetDigest), pushed.Weights[0].Tag)

		// Invariant: Model.Image and Model.Artifacts[0] are the same
		// instance after enrichment, matching the production-build
		// invariant on Model.
		require.Len(t, pushed.Artifacts, 1)
		require.Same(t, pushed.Image, pushed.Artifacts[0],
			"enriched Image and Artifacts[0] should be the same instance")
	})

	t.Run("skips tag+remove when local image already at cog-image ref", func(t *testing.T) {
		// Defends against deleting the underlying image when the
		// build-path tag and the cog-image tag coincide.
		var tagCalled, removeCalled bool
		docker := &mockDocker{
			pushFunc: func(ctx context.Context, ref string) error { return nil },
			tagFunc: func(ctx context.Context, source, target string) error {
				tagCalled = true
				return nil
			},
			removeFunc: func(ctx context.Context, ref string) error {
				removeCalled = true
				return nil
			},
		}
		reg := &mockRegistry{
			getDescriptorFunc: func(ctx context.Context, ref string) (v1.Descriptor, error) {
				return descriptorFromRef(ref), nil
			},
			pushIndexFunc: func(ctx context.Context, ref string, idx v1.ImageIndex) error { return nil },
		}

		m := testBundleModel()
		// Force the local image to already live at the cog-image ref.
		img := &ImageArtifact{name: "model", Reference: testCogImageRef}
		m.Image = img
		m.Artifacts = []Artifact{img}

		pusher := NewBundlePusher(docker, reg)
		_, err := pusher.Push(context.Background(), m, PushOptions{})
		require.NoError(t, err)
		require.False(t, tagCalled, "Tag should not be called when source == destination")
		require.False(t, removeCalled, "RemoveImage should not be called when no tag was added")
	})

	t.Run("uses default platform when not specified", func(t *testing.T) {
		docker := &mockDocker{
			pushFunc: func(ctx context.Context, ref string) error { return nil },
		}

		reg := &mockRegistry{
			getDescriptorFunc: func(ctx context.Context, ref string) (v1.Descriptor, error) {
				return descriptorFromRef(ref), nil
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

		_, err := pusher.Push(context.Background(), testBundleModel(
			Weight{Name: "w1", Target: "/src/weights/w1", Digest: testW1Digest, SetDigest: "sha256:abc"},
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
				return descriptorFromRef(ref), nil
			},
		}

		pusher := NewBundlePusher(docker, reg)
		w1 := Weight{Name: "w1", Target: "/src/weights/w1", Digest: testW1Digest, SetDigest: "sha256:abc"}

		_, err := pusher.Push(context.Background(), testBundleModel(w1), PushOptions{})

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
		w1 := Weight{Name: "w1", Target: "/src/weights/w1", Digest: testW1Digest, SetDigest: "sha256:abc"}

		_, err := pusher.Push(context.Background(), testBundleModel(w1), PushOptions{})

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
		w1 := Weight{Name: "w1", Target: "/src/weights/w1", Digest: testW1Digest, SetDigest: "sha256:abc"}

		_, err := pusher.Push(context.Background(), testBundleModel(w1), PushOptions{})

		require.Error(t, err)
		require.Contains(t, err.Error(), "w1")
		require.Contains(t, err.Error(), "not found in registry")
		require.Contains(t, err.Error(), "cog weights import")
		require.Contains(t, err.Error(), "manifest unknown")
	})

	t.Run("returns error when registry returns mismatched digest", func(t *testing.T) {
		// A non-content-addressed proxy could substitute manifests;
		// verifyWeights cross-checks the returned digest.
		docker := &mockDocker{
			pushFunc: func(ctx context.Context, ref string) error {
				t.Fatal("docker push should not run when registry returns mismatched digest")
				return nil
			},
		}
		// Return a different (but valid) digest than the one
		// requested.
		mismatchedDigest := "sha256:9999999999999999999999999999999999999999999999999999999999999999"
		reg := &mockRegistry{
			getDescriptorFunc: func(ctx context.Context, ref string) (v1.Descriptor, error) {
				hash, _ := v1.NewHash(mismatchedDigest)
				return v1.Descriptor{
					MediaType: types.OCIManifestSchema1,
					Size:      100,
					Digest:    hash,
				}, nil
			},
		}

		pusher := NewBundlePusher(docker, reg)
		w1 := Weight{Name: "w1", Target: "/src/weights/w1", Digest: testW1Digest, SetDigest: "sha256:abc"}

		_, err := pusher.Push(context.Background(), testBundleModel(w1), PushOptions{})

		require.Error(t, err)
		require.Contains(t, err.Error(), "registry returned digest")
	})

	t.Run("returns error when weight has no digest", func(t *testing.T) {
		docker := &mockDocker{
			pushFunc: func(ctx context.Context, ref string) error {
				t.Fatal("docker push should not run when weight has no digest")
				return nil
			},
		}
		reg := &mockRegistry{}

		pusher := NewBundlePusher(docker, reg)
		w1 := Weight{Name: "w1", Target: "/src/weights/w1", SetDigest: "sha256:abc"}

		_, err := pusher.Push(context.Background(), testBundleModel(w1), PushOptions{})

		require.Error(t, err)
		require.Contains(t, err.Error(), "missing manifest digest")
		require.Contains(t, err.Error(), "cog weights import")
	})

	t.Run("returns error when index push fails", func(t *testing.T) {
		docker := &mockDocker{
			pushFunc: func(ctx context.Context, ref string) error { return nil },
		}
		reg := &mockRegistry{
			getDescriptorFunc: func(ctx context.Context, ref string) (v1.Descriptor, error) {
				return descriptorFromRef(ref), nil
			},
			pushIndexFunc: func(ctx context.Context, ref string, idx v1.ImageIndex) error {
				return errors.New("index push failed")
			},
		}

		pusher := NewBundlePusher(docker, reg)
		w1 := Weight{Name: "w1", Target: "/src/weights/w1", Digest: testW1Digest, SetDigest: "sha256:abc"}

		_, err := pusher.Push(context.Background(), testBundleModel(w1), PushOptions{})

		require.Error(t, err)
		require.Contains(t, err.Error(), "push OCI index")
	})

	t.Run("verifies multiple weights concurrently", func(t *testing.T) {
		docker := &mockDocker{
			pushFunc: func(ctx context.Context, ref string) error { return nil },
		}

		var headCheckCount atomic.Int32
		reg := &mockRegistry{
			getDescriptorFunc: func(ctx context.Context, ref string) (v1.Descriptor, error) {
				// Count weight HEADs only (those are by repo@digest).
				if strings.Contains(ref, "@") {
					headCheckCount.Add(1)
				}
				return descriptorFromRef(ref), nil
			},
			pushIndexFunc: func(ctx context.Context, ref string, idx v1.ImageIndex) error {
				idxManifest, _ := idx.IndexManifest()
				require.Len(t, idxManifest.Manifests, 3) // 1 image + 2 weights
				return nil
			},
		}

		pusher := NewBundlePusher(docker, reg)

		_, err := pusher.Push(context.Background(), testBundleModel(
			Weight{Name: "w1", Target: "/src/weights/w1", Digest: testW1Digest, SetDigest: "sha256:set1"},
			Weight{Name: "w2", Target: "/src/weights/w2", Digest: testW2Digest, SetDigest: "sha256:set2"},
		), PushOptions{})

		require.NoError(t, err)
		require.Equal(t, int32(2), headCheckCount.Load()) // both weights HEAD-checked
	})

}

// descriptorFromRef returns a Descriptor whose Digest matches the
// digest in a "repo@sha256:..." reference, mirroring registry
// content-addressed behavior. Tag-form refs get a fixed digest.
func descriptorFromRef(ref string) v1.Descriptor {
	if _, digest, ok := strings.Cut(ref, "@"); ok {
		hash, _ := v1.NewHash(digest)
		return v1.Descriptor{
			MediaType: types.OCIManifestSchema1,
			Size:      100,
			Digest:    hash,
		}
	}
	return v1.Descriptor{
		MediaType: types.OCIManifestSchema1,
		Size:      100,
		Digest:    v1.Hash{Algorithm: "sha256", Hex: "imagedigest"},
	}
}

// =============================================================================
// Resolver.Push tests
// =============================================================================

func TestResolver_Push(t *testing.T) {
	t.Run("FormatImage uses docker push", func(t *testing.T) {
		var dockerPushed bool
		docker := &mockDocker{
			pushFunc: func(ctx context.Context, ref string) error {
				dockerPushed = true
				return nil
			},
		}
		imgDesc := v1.Descriptor{
			MediaType: types.OCIManifestSchema1,
			Size:      1234,
			Digest:    v1.Hash{Algorithm: "sha256", Hex: "imagedigestformatimage"},
		}
		reg := &mockRegistry{
			getDescriptorFunc: func(ctx context.Context, ref string) (v1.Descriptor, error) {
				return imgDesc, nil
			},
		}
		resolver := NewResolver(docker, reg)

		// Share the same *ImageArtifact between Model.Image and
		// Model.Artifacts so the post-push replacement matches
		// production builds.
		img := &ImageArtifact{name: "model", Reference: testImageRef}
		m := &Model{
			Format:    FormatImage,
			Image:     img,
			Artifacts: []Artifact{img},
		}

		pushed, err := resolver.Push(context.Background(), m, PushOptions{})
		require.NoError(t, err)
		require.True(t, dockerPushed, "FormatImage should use docker push")
		require.NotNil(t, pushed)
		require.NotNil(t, pushed.Image)
		require.Equal(t, testRepo+"@"+imgDesc.Digest.String(), pushed.Image.Reference,
			"FormatImage push should enrich Image.Reference to repo@digest")
		// Invariant: Image and the first ImageArtifact in Artifacts
		// are the same instance after enrichment.
		require.Len(t, pushed.Artifacts, 1)
		require.Same(t, pushed.Image, pushed.Artifacts[0],
			"Image and Artifacts[0] should be the same enriched instance")
	})

	t.Run("FormatImage falls back to unenriched Model when HEAD fails", func(t *testing.T) {
		// A successful docker push followed by a HEAD failure should
		// not fail the operation; Push returns the input Model
		// unchanged so the caller keeps their original references.
		// Legacy registries that don't support HEAD on tags hit this
		// path.
		var dockerPushed bool
		docker := &mockDocker{
			pushFunc: func(ctx context.Context, ref string) error {
				dockerPushed = true
				return nil
			},
		}
		reg := &mockRegistry{
			getDescriptorFunc: func(ctx context.Context, ref string) (v1.Descriptor, error) {
				return v1.Descriptor{}, errors.New("HEAD unsupported")
			},
		}
		resolver := NewResolver(docker, reg)

		img := &ImageArtifact{name: "model", Reference: testImageRef}
		m := &Model{
			Format:    FormatImage,
			Image:     img,
			Artifacts: []Artifact{img},
		}

		pushed, err := resolver.Push(context.Background(), m, PushOptions{})
		require.NoError(t, err)
		require.True(t, dockerPushed)
		require.Same(t, m, pushed,
			"on HEAD failure, Push should return the input Model unchanged")
	})

	t.Run("FormatBundle with no weights produces a single-entry index", func(t *testing.T) {
		// Behavioral change: a FormatBundle model with zero weights is
		// still pushed as an OCI index (containing only the image
		// manifest), not as a legacy single image.
		var indexPushed bool
		docker := &mockDocker{
			pushFunc: func(ctx context.Context, ref string) error { return nil },
		}
		reg := &mockRegistry{
			getDescriptorFunc: func(ctx context.Context, ref string) (v1.Descriptor, error) {
				return descriptorFromRef(ref), nil
			},
			pushIndexFunc: func(ctx context.Context, ref string, idx v1.ImageIndex) error {
				indexPushed = true
				return nil
			},
		}
		resolver := NewResolver(docker, reg)

		_, err := resolver.Push(context.Background(), testBundleModel(), PushOptions{})
		require.NoError(t, err)
		require.True(t, indexPushed, "FormatBundle should push an OCI index even with no weights")
	})

	t.Run("bundle with weights produces an OCI index", func(t *testing.T) {
		var indexPushed bool
		docker := &mockDocker{
			pushFunc: func(ctx context.Context, ref string) error { return nil },
		}
		reg := &mockRegistry{
			getDescriptorFunc: func(ctx context.Context, ref string) (v1.Descriptor, error) {
				return descriptorFromRef(ref), nil
			},
			pushIndexFunc: func(ctx context.Context, ref string, idx v1.ImageIndex) error {
				indexPushed = true
				return nil
			},
		}
		resolver := NewResolver(docker, reg)

		_, err := resolver.Push(context.Background(), testBundleModel(
			Weight{Name: "w1", Target: "/src/weights/w1", Digest: testW1Digest, SetDigest: "sha256:abc"},
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

		_, err := resolver.Push(context.Background(), m, PushOptions{})
		require.Error(t, err)
		require.Contains(t, err.Error(), "no image artifact")
	})

	t.Run("bundle returns error when no image artifact", func(t *testing.T) {
		docker := &mockDocker{}
		reg := &mockRegistry{}
		resolver := NewResolver(docker, reg)

		m := &Model{
			Format: FormatBundle,
			Weights: []Weight{
				{Name: "w1", Target: "/src/weights/w1", Digest: testW1Digest, SetDigest: "sha256:abc"},
			},
		}

		_, err := resolver.Push(context.Background(), m, PushOptions{})
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
