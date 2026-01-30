package generic

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/config"
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
	err := p.PrePush(context.Background(), "ghcr.io/org/model", &config.Config{})
	require.NoError(t, err)
}

func TestGenericProvider_PostPush(t *testing.T) {
	p := New()
	err := p.PostPush(context.Background(), "ghcr.io/org/model", &config.Config{}, nil)
	require.NoError(t, err)
}
