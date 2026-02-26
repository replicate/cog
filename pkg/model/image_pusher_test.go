package model

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/registry"
)

// ociMockClient implements registry.Client for testing ImagePusher.
type ociMockClient struct {
	mu              sync.Mutex
	writtenLayers   []v1.Hash
	pushedImages    []string
	writeLayerErr   error
	pushImageErr    error
	writeLayerCount int
}

func (m *ociMockClient) WriteLayer(_ context.Context, opts registry.WriteLayerOptions) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.writeLayerCount++
	if m.writeLayerErr != nil {
		return m.writeLayerErr
	}
	digest, err := opts.Layer.Digest()
	if err != nil {
		return err
	}
	m.writtenLayers = append(m.writtenLayers, digest)

	// Send progress if channel is provided
	if opts.ProgressCh != nil {
		size, _ := opts.Layer.Size()
		opts.ProgressCh <- v1.Update{Complete: size, Total: size}
	}
	return nil
}

func (m *ociMockClient) PushImage(_ context.Context, ref string, _ v1.Image) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pushImageErr != nil {
		return m.pushImageErr
	}
	m.pushedImages = append(m.pushedImages, ref)
	return nil
}

func (m *ociMockClient) Inspect(context.Context, string, *registry.Platform) (*registry.ManifestResult, error) {
	return nil, nil
}
func (m *ociMockClient) GetImage(context.Context, string, *registry.Platform) (v1.Image, error) {
	return nil, nil
}
func (m *ociMockClient) Exists(context.Context, string) (bool, error) { return false, nil }
func (m *ociMockClient) GetDescriptor(context.Context, string) (v1.Descriptor, error) {
	return v1.Descriptor{}, nil
}
func (m *ociMockClient) PushIndex(context.Context, string, v1.ImageIndex) error { return nil }

// testArtifact creates an *ImageArtifact for testing with the given reference string.
func testArtifact(ref string) *ImageArtifact {
	return &ImageArtifact{Reference: ref}
}

// fakeImageSaveFunc creates a fake ImageSave function that produces a Docker-format tar
// from the given v1.Image. This simulates Docker's ImageSave API.
func fakeImageSaveFunc(img v1.Image, tagStr string) func(context.Context, string) (io.ReadCloser, error) {
	return func(_ context.Context, _ string) (io.ReadCloser, error) {
		tag, err := name.NewTag(tagStr, name.Insecure)
		if err != nil {
			return nil, fmt.Errorf("parse tag: %w", err)
		}
		var buf bytes.Buffer
		refToImage := map[name.Tag]v1.Image{tag: img}
		if err := tarball.MultiWrite(refToImage, &buf); err != nil {
			return nil, fmt.Errorf("create test tar: %w", err)
		}
		return io.NopCloser(bytes.NewReader(buf.Bytes())), nil
	}
}

// =============================================================================
// ImagePusher.Push — OCI chunked push tests
// =============================================================================

func TestImagePusher_Push(t *testing.T) {
	t.Setenv("COG_PUSH_OCI", "1")

	t.Run("pushes layers config and manifest via OCI path", func(t *testing.T) {
		img, err := random.Image(1024, 2) // 2 layers of 1KB
		require.NoError(t, err)

		mock := &ociMockClient{}
		tag := "example.com/test/repo:v1"
		docker := &mockDocker{imageSaveFunc: fakeImageSaveFunc(img, tag)}
		pusher := newImagePusher(docker, mock)

		err = pusher.Push(context.Background(), testArtifact(tag))
		require.NoError(t, err)

		// Should have pushed 2 layers + 1 config blob = 3 WriteLayer calls
		assert.Equal(t, 3, mock.writeLayerCount)

		// Should have pushed the manifest
		require.Len(t, mock.pushedImages, 1)
		assert.Equal(t, tag, mock.pushedImages[0])
	})

	t.Run("reports progress via callback", func(t *testing.T) {
		img, err := random.Image(1024, 1)
		require.NoError(t, err)

		mock := &ociMockClient{}
		tag := "example.com/test/repo:v1"
		docker := &mockDocker{imageSaveFunc: fakeImageSaveFunc(img, tag)}
		pusher := newImagePusher(docker, mock)

		var mu sync.Mutex
		var progressUpdates []PushProgress
		opts := ImagePushOptions{
			ProgressFn: func(p PushProgress) {
				mu.Lock()
				defer mu.Unlock()
				progressUpdates = append(progressUpdates, p)
			},
		}

		err = pusher.Push(context.Background(), testArtifact(tag), opts)
		require.NoError(t, err)

		mu.Lock()
		defer mu.Unlock()
		assert.NotEmpty(t, progressUpdates)
		for _, p := range progressUpdates {
			assert.NotEmpty(t, p.LayerDigest)
			assert.True(t, p.Complete > 0)
			assert.True(t, p.Total > 0)
		}
	})

	t.Run("falls back to docker when WriteLayer fails", func(t *testing.T) {
		img, err := random.Image(1024, 1)
		require.NoError(t, err)

		var dockerPushed bool
		mock := &ociMockClient{writeLayerErr: errors.New("upload failed")}
		tag := "example.com/test/repo:v1"
		docker := &mockDocker{
			imageSaveFunc: fakeImageSaveFunc(img, tag),
			pushFunc: func(_ context.Context, _ string) error {
				dockerPushed = true
				return nil
			},
		}
		pusher := newImagePusher(docker, mock)

		err = pusher.Push(context.Background(), testArtifact(tag))
		require.NoError(t, err)
		assert.True(t, dockerPushed)
	})

	t.Run("falls back to docker when PushImage fails", func(t *testing.T) {
		img, err := random.Image(1024, 1)
		require.NoError(t, err)

		var dockerPushed bool
		mock := &ociMockClient{pushImageErr: errors.New("manifest push failed")}
		tag := "example.com/test/repo:v1"
		docker := &mockDocker{
			imageSaveFunc: fakeImageSaveFunc(img, tag),
			pushFunc: func(_ context.Context, _ string) error {
				dockerPushed = true
				return nil
			},
		}
		pusher := newImagePusher(docker, mock)

		err = pusher.Push(context.Background(), testArtifact(tag))
		require.NoError(t, err)
		assert.True(t, dockerPushed)
	})

	t.Run("falls back to docker when ImageSave fails", func(t *testing.T) {
		mock := &ociMockClient{}

		var dockerPushed bool
		docker := &mockDocker{
			imageSaveFunc: func(_ context.Context, _ string) (io.ReadCloser, error) {
				return nil, errors.New("docker daemon unavailable")
			},
			pushFunc: func(_ context.Context, _ string) error {
				dockerPushed = true
				return nil
			},
		}
		pusher := newImagePusher(docker, mock)

		err := pusher.Push(context.Background(), testArtifact("example.com/test/repo:v1"))
		require.NoError(t, err)
		assert.True(t, dockerPushed)
	})

	t.Run("handles empty image with no layers", func(t *testing.T) {
		img := empty.Image
		img, err := mutate.Config(img, v1.Config{})
		require.NoError(t, err)

		mock := &ociMockClient{}
		tag := "example.com/test/repo:empty"
		docker := &mockDocker{imageSaveFunc: fakeImageSaveFunc(img, tag)}
		pusher := newImagePusher(docker, mock)

		err = pusher.Push(context.Background(), testArtifact(tag))
		require.NoError(t, err)

		// Only config blob should be written (no layers)
		assert.Equal(t, 1, mock.writeLayerCount)
		require.Len(t, mock.pushedImages, 1)
	})
}

// =============================================================================
// ImagePusher.Push with artifact tests
// =============================================================================

func TestImagePusher_PushArtifact(t *testing.T) {
	t.Run("pushes artifact by reference", func(t *testing.T) {
		var dockerPushed string
		docker := &mockDocker{
			pushFunc: func(_ context.Context, ref string) error {
				dockerPushed = ref
				return nil
			},
		}

		// No registry — will use Docker push directly
		pusher := newImagePusher(docker, nil)
		artifact := &ImageArtifact{Reference: "r8.im/user/model:latest"}

		err := pusher.Push(context.Background(), artifact)

		require.NoError(t, err)
		require.Equal(t, "r8.im/user/model:latest", dockerPushed)
	})

	t.Run("returns error for nil artifact", func(t *testing.T) {
		pusher := newImagePusher(&mockDocker{}, nil)

		err := pusher.Push(context.Background(), nil)

		require.Error(t, err)
		require.Contains(t, err.Error(), "nil")
	})

	t.Run("returns error for empty reference", func(t *testing.T) {
		pusher := newImagePusher(&mockDocker{}, nil)

		err := pusher.Push(context.Background(), &ImageArtifact{Reference: ""})

		require.Error(t, err)
		require.Contains(t, err.Error(), "no reference")
	})

	t.Run("propagates docker push error", func(t *testing.T) {
		docker := &mockDocker{
			pushFunc: func(_ context.Context, _ string) error {
				return errors.New("unauthorized: authentication required")
			},
		}

		pusher := newImagePusher(docker, nil)
		artifact := &ImageArtifact{Reference: "r8.im/user/model:latest"}

		err := pusher.Push(context.Background(), artifact)

		require.Error(t, err)
		require.Contains(t, err.Error(), "unauthorized")
	})
}

// =============================================================================
// Docker fallback behavior tests
// =============================================================================

func TestImagePusher_Fallback(t *testing.T) {
	t.Setenv("COG_PUSH_OCI", "1")

	t.Run("uses OCI push when it succeeds", func(t *testing.T) {
		img, err := random.Image(512, 1)
		require.NoError(t, err)

		mock := &ociMockClient{}
		tag := "example.com/test/repo:v1"
		docker := &mockDocker{
			imageSaveFunc: fakeImageSaveFunc(img, tag),
			pushFunc: func(_ context.Context, _ string) error {
				t.Fatal("docker push should not be called when OCI succeeds")
				return nil
			},
		}

		pusher := newImagePusher(docker, mock)

		err = pusher.Push(context.Background(), testArtifact(tag))
		require.NoError(t, err)
	})

	t.Run("falls back to docker on OCI error", func(t *testing.T) {
		var dockerPushed bool
		mock := &ociMockClient{writeLayerErr: errors.New("connection reset")}
		tag := "example.com/test/repo:v1"

		img, err := random.Image(512, 1)
		require.NoError(t, err)

		docker := &mockDocker{
			imageSaveFunc: fakeImageSaveFunc(img, tag),
			pushFunc: func(_ context.Context, _ string) error {
				dockerPushed = true
				return nil
			},
		}

		pusher := newImagePusher(docker, mock)

		err = pusher.Push(context.Background(), testArtifact(tag))
		require.NoError(t, err)
		assert.True(t, dockerPushed)
	})

	t.Run("does not fall back on context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // cancel immediately

		mock := &ociMockClient{}
		tag := "example.com/test/repo:v1"
		docker := &mockDocker{
			imageSaveFunc: func(ctx context.Context, _ string) (io.ReadCloser, error) {
				return nil, ctx.Err()
			},
			pushFunc: func(_ context.Context, _ string) error {
				t.Fatal("docker push should not be called on context cancellation")
				return nil
			},
		}

		pusher := newImagePusher(docker, mock)

		err := pusher.Push(ctx, testArtifact(tag))
		require.Error(t, err)
	})

	t.Run("does not fall back on 401 unauthorized", func(t *testing.T) {
		mock := &ociMockClient{writeLayerErr: &transport.Error{StatusCode: 401}}
		tag := "example.com/test/repo:v1"

		img, err := random.Image(512, 1)
		require.NoError(t, err)

		docker := &mockDocker{
			imageSaveFunc: fakeImageSaveFunc(img, tag),
			pushFunc: func(_ context.Context, _ string) error {
				t.Fatal("docker push should not be called on auth error")
				return nil
			},
		}

		pusher := newImagePusher(docker, mock)

		err = pusher.Push(context.Background(), testArtifact(tag))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "OCI chunked push")
	})

	t.Run("does not fall back on 403 forbidden", func(t *testing.T) {
		mock := &ociMockClient{writeLayerErr: &transport.Error{StatusCode: 403}}
		tag := "example.com/test/repo:v1"

		img, err := random.Image(512, 1)
		require.NoError(t, err)

		docker := &mockDocker{
			imageSaveFunc: fakeImageSaveFunc(img, tag),
			pushFunc: func(_ context.Context, _ string) error {
				t.Fatal("docker push should not be called on auth error")
				return nil
			},
		}

		pusher := newImagePusher(docker, mock)

		err = pusher.Push(context.Background(), testArtifact(tag))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "OCI chunked push")
	})

	t.Run("uses docker directly when registry is nil", func(t *testing.T) {
		var dockerPushed bool
		docker := &mockDocker{
			pushFunc: func(_ context.Context, _ string) error {
				dockerPushed = true
				return nil
			},
		}

		pusher := newImagePusher(docker, nil)

		err := pusher.Push(context.Background(), testArtifact("example.com/test/repo:v1"))
		require.NoError(t, err)
		assert.True(t, dockerPushed)
	})
}

// =============================================================================
// shouldFallbackToDocker tests
// =============================================================================

func TestShouldFallbackToDocker(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"context canceled", context.Canceled, false},
		{"context deadline", context.DeadlineExceeded, false},
		{"401 unauthorized", &transport.Error{StatusCode: 401}, false},
		{"403 forbidden", &transport.Error{StatusCode: 403}, false},
		{"wrapped 401", fmt.Errorf("push failed: %w", &transport.Error{StatusCode: 401}), false},
		{"500 server error", &transport.Error{StatusCode: 500}, true},
		{"network error", errors.New("connection refused"), true},
		{"unknown error", errors.New("something unexpected"), true},
		{"export error", errors.New("export OCI layout: daemon error"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, shouldFallbackToDocker(tt.err))
		})
	}
}

// =============================================================================
// sanitizeError tests
// =============================================================================

func TestSanitizeError(t *testing.T) {
	t.Run("strips HTML body from transport error", func(t *testing.T) {
		htmlBody := `<html><head><title>413 Request Entity Too Large</title></head><body><center><h1>413 Request Entity Too Large</h1></center><hr><center>cloudflare</center></body></html>`
		transportErr := &transport.Error{
			StatusCode: 413,
			Errors:     nil,
		}
		// The rawBody field is unexported, so we test via the wrapped error behavior.
		// A transport.Error with no Errors slice and status 413 produces a message
		// that includes the raw body — sanitizeError should replace it.
		_ = htmlBody // illustrates the problem scenario

		result := sanitizeError(transportErr)
		assert.Equal(t, "HTTP 413 Request Entity Too Large", result.Error())
	})

	t.Run("strips body from 502 transport error", func(t *testing.T) {
		transportErr := &transport.Error{
			StatusCode: 502,
		}

		result := sanitizeError(transportErr)
		assert.Equal(t, "HTTP 502 Bad Gateway", result.Error())
	})

	t.Run("passes through non-transport errors unchanged", func(t *testing.T) {
		err := errors.New("connection refused")
		result := sanitizeError(err)
		assert.Equal(t, "connection refused", result.Error())
	})

	t.Run("passes through wrapped transport errors", func(t *testing.T) {
		transportErr := &transport.Error{
			StatusCode: 413,
		}
		wrapped := fmt.Errorf("pushing layer: %w", transportErr)

		result := sanitizeError(wrapped)
		assert.Equal(t, "HTTP 413 Request Entity Too Large", result.Error())
	})
}

// =============================================================================
// OnFallback callback tests
// =============================================================================

func TestImagePusher_OnFallback(t *testing.T) {
	t.Setenv("COG_PUSH_OCI", "1")

	t.Run("calls OnFallback before docker push on OCI failure", func(t *testing.T) {
		var callOrder []string

		mock := &ociMockClient{writeLayerErr: errors.New("connection reset")}
		tag := "example.com/test/repo:v1"

		img, err := random.Image(512, 1)
		require.NoError(t, err)

		docker := &mockDocker{
			imageSaveFunc: fakeImageSaveFunc(img, tag),
			pushFunc: func(_ context.Context, _ string) error {
				callOrder = append(callOrder, "docker-push")
				return nil
			},
		}

		pusher := newImagePusher(docker, mock)

		err = pusher.Push(context.Background(), testArtifact(tag), ImagePushOptions{
			OnFallback: func() {
				callOrder = append(callOrder, "on-fallback")
			},
		})
		require.NoError(t, err)
		assert.Equal(t, []string{"on-fallback", "docker-push"}, callOrder)
	})

	t.Run("does not call OnFallback when OCI push succeeds", func(t *testing.T) {
		mock := &ociMockClient{}
		tag := "example.com/test/repo:v1"

		img, err := random.Image(512, 1)
		require.NoError(t, err)

		docker := &mockDocker{
			imageSaveFunc: fakeImageSaveFunc(img, tag),
		}

		pusher := newImagePusher(docker, mock)

		var fallbackCalled bool
		err = pusher.Push(context.Background(), testArtifact(tag), ImagePushOptions{
			OnFallback: func() {
				fallbackCalled = true
			},
		})
		require.NoError(t, err)
		assert.False(t, fallbackCalled)
	})
}

// =============================================================================
// configBlobLayer tests
// =============================================================================

func TestConfigBlobLayer(t *testing.T) {
	data := []byte(`{"architecture":"amd64","os":"linux"}`)
	digest := v1.Hash{Algorithm: "sha256", Hex: "abc123"}

	layer := &configBlobLayer{data: data, digest: digest}

	t.Run("Digest", func(t *testing.T) {
		d, err := layer.Digest()
		require.NoError(t, err)
		assert.Equal(t, digest, d)
	})

	t.Run("DiffID equals Digest for uncompressed config", func(t *testing.T) {
		d, err := layer.DiffID()
		require.NoError(t, err)
		assert.Equal(t, digest, d)
	})

	t.Run("Size", func(t *testing.T) {
		size, err := layer.Size()
		require.NoError(t, err)
		assert.Equal(t, int64(len(data)), size)
	})

	t.Run("MediaType", func(t *testing.T) {
		mt, err := layer.MediaType()
		require.NoError(t, err)
		assert.Equal(t, types.OCIConfigJSON, mt)
	})

	t.Run("Compressed returns data", func(t *testing.T) {
		rc, err := layer.Compressed()
		require.NoError(t, err)
		defer rc.Close()
		got, err := io.ReadAll(rc)
		require.NoError(t, err)
		assert.Equal(t, data, got)
	})

	t.Run("Uncompressed returns data", func(t *testing.T) {
		rc, err := layer.Uncompressed()
		require.NoError(t, err)
		defer rc.Close()
		got, err := io.ReadAll(rc)
		require.NoError(t, err)
		assert.Equal(t, data, got)
	})
}

// =============================================================================
// GetPushConcurrency tests
// =============================================================================

func TestGetPushConcurrency(t *testing.T) {
	t.Run("returns default when env not set", func(t *testing.T) {
		t.Setenv("COG_PUSH_CONCURRENCY", "")
		assert.Equal(t, DefaultPushConcurrency, GetPushConcurrency())
	})

	t.Run("returns env var value", func(t *testing.T) {
		t.Setenv("COG_PUSH_CONCURRENCY", "8")
		assert.Equal(t, 8, GetPushConcurrency())
	})

	t.Run("returns default for invalid value", func(t *testing.T) {
		t.Setenv("COG_PUSH_CONCURRENCY", "not-a-number")
		assert.Equal(t, DefaultPushConcurrency, GetPushConcurrency())
	})

	t.Run("returns default for zero", func(t *testing.T) {
		t.Setenv("COG_PUSH_CONCURRENCY", "0")
		assert.Equal(t, DefaultPushConcurrency, GetPushConcurrency())
	})

	t.Run("returns default for negative", func(t *testing.T) {
		t.Setenv("COG_PUSH_CONCURRENCY", "-1")
		assert.Equal(t, DefaultPushConcurrency, GetPushConcurrency())
	})
}
