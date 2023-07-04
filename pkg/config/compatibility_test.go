package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLatestCuDNNForCUDA(t *testing.T) {
	actual, err := latestCuDNNForCUDA("11.8")
	require.NoError(t, err)
	require.Equal(t, "8", actual)
}

func TestResolveMinorToPatch(t *testing.T) {
	cuda, err := resolveMinorToPatch("11.3")
	require.NoError(t, err)
	require.Equal(t, "11.3.1", cuda)
	_, err = resolveMinorToPatch("1214348324.432879432")
	require.Error(t, err)
}
