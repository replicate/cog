package wheels

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReadCogWheel(t *testing.T) {
	filename, data := ReadCogWheel()
	require.Equal(t, "cog.whl", filename)
	require.Greater(t, len(data), 10000)
}

func TestReadCogletWheel(t *testing.T) {
	filename, data := ReadCogletWheel()
	require.Equal(t, "coglet.whl", filename)
	require.Greater(t, len(data), 1000000)
}
