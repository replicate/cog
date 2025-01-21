package docker

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/dockerfile"
)

func TestLoadLoginToken(t *testing.T) {
	token, err := LoadLoginToken(dockerfile.BaseImageRegistry)
	require.NoError(t, err)
	require.NotEqual(t, token, "")
}
