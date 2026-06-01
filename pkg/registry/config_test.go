package registry

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetDefaultChunkSize(t *testing.T) {
	t.Run("returns default when env not set", func(t *testing.T) {
		t.Setenv(envPushDefaultChunkSize, "")
		assert.Equal(t, int64(DefaultChunkSize), getDefaultChunkSize())
	})

	t.Run("returns env var value", func(t *testing.T) {
		t.Setenv(envPushDefaultChunkSize, "134217728") // 128 MB
		assert.Equal(t, int64(134217728), getDefaultChunkSize())
	})

	t.Run("returns default for invalid value", func(t *testing.T) {
		t.Setenv(envPushDefaultChunkSize, "xyz")
		assert.Equal(t, int64(DefaultChunkSize), getDefaultChunkSize())
	})

	t.Run("returns default for zero", func(t *testing.T) {
		t.Setenv(envPushDefaultChunkSize, "0")
		assert.Equal(t, int64(DefaultChunkSize), getDefaultChunkSize())
	})

	t.Run("returns default for negative", func(t *testing.T) {
		t.Setenv(envPushDefaultChunkSize, "-100")
		assert.Equal(t, int64(DefaultChunkSize), getDefaultChunkSize())
	})
}

func TestEffectiveChunkSize(t *testing.T) {
	t.Run("uses client default when server provides no limits", func(t *testing.T) {
		t.Setenv(envPushDefaultChunkSize, "")
		s := uploadSession{}
		assert.Equal(t, int64(DefaultChunkSize), s.effectiveChunkSize())
	})

	t.Run("uses env var default when server provides no limits", func(t *testing.T) {
		t.Setenv(envPushDefaultChunkSize, "50000000") // 50 MB
		s := uploadSession{}
		assert.Equal(t, int64(50000000), s.effectiveChunkSize())
	})

	t.Run("server max takes precedence over client default", func(t *testing.T) {
		t.Setenv(envPushDefaultChunkSize, "")
		serverMax := int64(90 * 1024 * 1024)
		s := uploadSession{ChunkMaxBytes: serverMax}
		expected := serverMax - chunkSizeMargin
		assert.Equal(t, expected, s.effectiveChunkSize())
	})

	t.Run("server max takes precedence even when larger than client default", func(t *testing.T) {
		t.Setenv(envPushDefaultChunkSize, "")
		// Server max of 200 MB -- server still dictates, not the client default
		serverMax := int64(200 * 1024 * 1024)
		s := uploadSession{ChunkMaxBytes: serverMax}
		expected := serverMax - chunkSizeMargin
		assert.Equal(t, expected, s.effectiveChunkSize())
	})

	t.Run("server max takes precedence over env var", func(t *testing.T) {
		t.Setenv(envPushDefaultChunkSize, "50000000") // 50 MB -- ignored when server provides max
		serverMax := int64(100 * 1000 * 1000)         // 100 MB
		s := uploadSession{ChunkMaxBytes: serverMax}
		expected := serverMax - chunkSizeMargin
		assert.Equal(t, expected, s.effectiveChunkSize())
	})

	t.Run("handles very small server max gracefully", func(t *testing.T) {
		t.Setenv(envPushDefaultChunkSize, "")
		s := uploadSession{ChunkMaxBytes: 1000} // 1000 bytes, smaller than margin
		// Margin is bigger than max, so we use the max directly
		assert.Equal(t, int64(1000), s.effectiveChunkSize())
	})

	t.Run("server min does not raise chunk size when already above it", func(t *testing.T) {
		t.Setenv(envPushDefaultChunkSize, "")
		// Server says min=5MiB max=90MiB; max-margin is well above min, so min has no effect
		serverMax := int64(90 * 1024 * 1024)
		s := uploadSession{ChunkMinBytes: 5 * 1024 * 1024, ChunkMaxBytes: serverMax}
		expected := serverMax - chunkSizeMargin
		assert.Equal(t, expected, s.effectiveChunkSize())
	})

	t.Run("server min clamps up a too-small client default", func(t *testing.T) {
		t.Setenv(envPushDefaultChunkSize, "1000") // 1 KB, below server min
		serverMin := int64(5 * 1024 * 1024)       // 5 MiB
		s := uploadSession{ChunkMinBytes: serverMin}
		assert.Equal(t, serverMin, s.effectiveChunkSize())
	})

	t.Run("server min clamps up when max minus margin falls below min", func(t *testing.T) {
		t.Setenv(envPushDefaultChunkSize, "")
		// Contrived: max is just above min, so max-margin < min. Min should win.
		serverMin := int64(5 * 1024 * 1024)
		serverMax := serverMin + chunkSizeMargin/2 // max - margin < min
		s := uploadSession{ChunkMinBytes: serverMin, ChunkMaxBytes: serverMax}
		assert.Equal(t, serverMin, s.effectiveChunkSize())
	})
}

func TestGetMultipartThreshold(t *testing.T) {
	t.Run("returns default when env not set", func(t *testing.T) {
		t.Setenv(envMultipartThreshold, "")
		assert.Equal(t, int64(DefaultMultipartThreshold), getMultipartThreshold())
	})

	t.Run("returns env var value", func(t *testing.T) {
		t.Setenv(envMultipartThreshold, "104857600") // 100 MB
		assert.Equal(t, int64(104857600), getMultipartThreshold())
	})

	t.Run("returns default for invalid value", func(t *testing.T) {
		t.Setenv(envMultipartThreshold, "abc")
		assert.Equal(t, int64(DefaultMultipartThreshold), getMultipartThreshold())
	})

	t.Run("returns default for zero", func(t *testing.T) {
		t.Setenv(envMultipartThreshold, "0")
		assert.Equal(t, int64(DefaultMultipartThreshold), getMultipartThreshold())
	})

	t.Run("returns default for negative", func(t *testing.T) {
		t.Setenv(envMultipartThreshold, "-50")
		assert.Equal(t, int64(DefaultMultipartThreshold), getMultipartThreshold())
	})
}
