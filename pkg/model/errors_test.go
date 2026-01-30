package model

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSentinelErrors(t *testing.T) {
	// Test that sentinel errors can be wrapped and unwrapped
	t.Run("ErrNotCogModel can be wrapped and detected", func(t *testing.T) {
		wrapped := fmt.Errorf("failed to inspect image: %w", ErrNotCogModel)
		require.True(t, errors.Is(wrapped, ErrNotCogModel))
	})

	t.Run("ErrNotFound can be wrapped and detected", func(t *testing.T) {
		wrapped := fmt.Errorf("image my-image:latest: %w", ErrNotFound)
		require.True(t, errors.Is(wrapped, ErrNotFound))
	})

	t.Run("errors are distinct", func(t *testing.T) {
		require.False(t, errors.Is(ErrNotCogModel, ErrNotFound))
		require.False(t, errors.Is(ErrNotFound, ErrNotCogModel))
	})
}
