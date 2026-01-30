package generic

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/provider"
)

func TestGenericProvider_Name(t *testing.T) {
	p := New()
	require.Equal(t, "generic", p.Name())
}

func TestGenericProvider_MatchesRegistry(t *testing.T) {
	p := New()
	// Generic provider matches everything (it's the fallback)
	require.True(t, p.MatchesRegistry("ghcr.io"))
	require.True(t, p.MatchesRegistry("docker.io"))
	require.True(t, p.MatchesRegistry("ecr.aws"))
	require.True(t, p.MatchesRegistry("anything.example.com"))
}

func TestGenericProvider_Login(t *testing.T) {
	p := New()
	err := p.Login(context.Background(), "ghcr.io")
	// Should return ErrUseDockerLogin - actual auth uses Docker's credential system
	require.True(t, errors.Is(err, provider.ErrUseDockerLogin))
}

func TestGenericProvider_PrePush(t *testing.T) {
	p := New()

	t.Run("basic push succeeds", func(t *testing.T) {
		opts := provider.PushOptions{
			Image: "ghcr.io/org/model",
		}
		err := p.PrePush(context.Background(), opts)
		require.NoError(t, err)
	})

	t.Run("local image push fails", func(t *testing.T) {
		opts := provider.PushOptions{
			Image:      "ghcr.io/org/model",
			LocalImage: true,
		}
		err := p.PrePush(context.Background(), opts)
		require.Error(t, err)
		require.Contains(t, err.Error(), "local image push")
	})

	t.Run("fast push warns but succeeds", func(t *testing.T) {
		opts := provider.PushOptions{
			Image:    "ghcr.io/org/model",
			FastPush: true,
		}
		// FastPush warns but doesn't error
		err := p.PrePush(context.Background(), opts)
		require.NoError(t, err)
	})
}

func TestGenericProvider_PostPush(t *testing.T) {
	p := New()

	t.Run("success", func(t *testing.T) {
		opts := provider.PushOptions{
			Image: "ghcr.io/org/model",
		}
		err := p.PostPush(context.Background(), opts, nil)
		require.NoError(t, err)
	})

	t.Run("with error", func(t *testing.T) {
		opts := provider.PushOptions{
			Image: "ghcr.io/org/model",
		}
		pushErr := errors.New("push failed")
		err := p.PostPush(context.Background(), opts, pushErr)
		require.NoError(t, err) // PostPush itself doesn't error
	})
}
