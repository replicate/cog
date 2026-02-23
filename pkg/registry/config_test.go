package registry

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetChunkSize(t *testing.T) {
	t.Run("returns default when env not set", func(t *testing.T) {
		t.Setenv(envPushChunkSize, "")
		assert.Equal(t, int64(DefaultChunkSize), getChunkSize())
	})

	t.Run("returns env var value", func(t *testing.T) {
		t.Setenv(envPushChunkSize, "134217728") // 128 MB
		assert.Equal(t, int64(134217728), getChunkSize())
	})

	t.Run("returns default for invalid value", func(t *testing.T) {
		t.Setenv(envPushChunkSize, "xyz")
		assert.Equal(t, int64(DefaultChunkSize), getChunkSize())
	})

	t.Run("returns default for zero", func(t *testing.T) {
		t.Setenv(envPushChunkSize, "0")
		assert.Equal(t, int64(DefaultChunkSize), getChunkSize())
	})

	t.Run("returns default for negative", func(t *testing.T) {
		t.Setenv(envPushChunkSize, "-100")
		assert.Equal(t, int64(DefaultChunkSize), getChunkSize())
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
