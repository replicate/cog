package registry

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/registry_testhelpers"
)

func TestInspect(t *testing.T) {
	if testing.Short() {
		// TODO[md]: this is a hack to skip the test in GitHub Actions because
		// because macos runners don't have rootless docker. this should get added back
		// and be part of a normal integration suite we run on all target platforms
		t.Skip("skipping integration tests")
	}

	registry := registry_testhelpers.StartTestRegistry(t)

	t.Run("it returns an index for multi-platform images when a platform isn't provided", func(t *testing.T) {
		imageRef := registry.ImageRef("alpine:latest")

		client := NewRegistryClient()
		resp, err := client.Inspect(t.Context(), imageRef, nil)
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.True(t, resp.IsIndex(), "expected index")
		json.NewEncoder(os.Stdout).Encode(resp)
	})

	t.Run("it returns a single platform image when a platform is provided", func(t *testing.T) {
		imageRef := registry.ImageRef("alpine:latest")
		client := NewRegistryClient()
		resp, err := client.Inspect(t.Context(), imageRef, &Platform{OS: "linux", Architecture: "amd64"})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.False(t, resp.IsIndex(), "expected single platform image")
		assert.True(t, resp.IsSinglePlatform(), "expected single platform image")
		json.NewEncoder(os.Stdout).Encode(resp)
	})

	t.Run("when a repo does not exist", func(t *testing.T) {
		imageRef := registry.ImageRef("i-do-not-exist:latest")
		client := NewRegistryClient()
		resp, err := client.Inspect(t.Context(), imageRef, nil)
		assert.ErrorIs(t, err, NotFoundError, "expected not found error")
		assert.Nil(t, resp)
	})

	t.Run("when a repo with a slashdoes not exist", func(t *testing.T) {
		imageRef := registry.ImageRef("i-do-not-exist/with-a-slash:latest")
		client := NewRegistryClient()
		resp, err := client.Inspect(t.Context(), imageRef, nil)
		assert.ErrorIs(t, err, NotFoundError, "expected not found error")
		assert.Nil(t, resp)
	})

	t.Run("when the repo exists but the tag does not", func(t *testing.T) {
		imageRef := registry.ImageRef("alpine:not-found")
		client := NewRegistryClient()
		resp, err := client.Inspect(t.Context(), imageRef, nil)
		assert.ErrorIs(t, err, NotFoundError, "expected not found error")
		assert.Nil(t, resp)
	})

	t.Run("when the repo and tag exist but platform does not", func(t *testing.T) {
		imageRef := registry.ImageRef("alpine:latest")
		client := NewRegistryClient()
		resp, err := client.Inspect(t.Context(), imageRef, &Platform{OS: "windows", Architecture: "i386"})
		assert.ErrorContains(t, err, "platform not found")
		assert.Nil(t, resp)
	})
}

func TestIsRetryableError(t *testing.T) {
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
			name:     "io.EOF",
			err:      io.EOF,
			expected: true,
		},
		{
			name:     "io.ErrUnexpectedEOF",
			err:      io.ErrUnexpectedEOF,
			expected: true,
		},
		{
			name:     "syscall.EPIPE (broken pipe)",
			err:      syscall.EPIPE,
			expected: true,
		},
		{
			name:     "syscall.ECONNRESET",
			err:      syscall.ECONNRESET,
			expected: true,
		},
		{
			name:     "net.ErrClosed",
			err:      net.ErrClosed,
			expected: true,
		},
		{
			name:     "wrapped unexpected EOF",
			err:      errors.New("failed: unexpected EOF"),
			expected: true, // Should match via string pattern
		},
		{
			name:     "connection reset string",
			err:      errors.New("read tcp: connection reset by peer"),
			expected: true,
		},
		{
			name:     "timeout string",
			err:      errors.New("request timeout exceeded"),
			expected: true,
		},
		{
			name:     "generic error",
			err:      errors.New("something completely different"),
			expected: false,
		},
		{
			name:     "unauthorized error",
			err:      errors.New("unauthorized: authentication required"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isRetryableError(tt.err)
			assert.Equal(t, tt.expected, result, "isRetryableError(%v) = %v, want %v", tt.err, result, tt.expected)
		})
	}
}
