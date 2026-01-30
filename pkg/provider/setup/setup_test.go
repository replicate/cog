package setup

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/provider"
)

func TestInit(t *testing.T) {
	// Call Init multiple times - should be idempotent
	Init()
	Init()

	registry := provider.DefaultRegistry()

	// Replicate images should get the Replicate provider
	p := registry.ForImage("r8.im/user/model")
	require.NotNil(t, p)
	require.Equal(t, "replicate", p.Name())

	// Other images should get the Generic provider
	p = registry.ForImage("ghcr.io/owner/repo")
	require.NotNil(t, p)
	require.Equal(t, "generic", p.Name())

	// Docker Hub images should get Generic provider
	p = registry.ForImage("nginx")
	require.NotNil(t, p)
	require.Equal(t, "generic", p.Name())
}
