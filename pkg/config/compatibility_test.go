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

func TestCudasFromTorchWithCUVersionModifier(t *testing.T) {
	cudas, err := cudasFromTorch("2.0.1+cu118")
	require.GreaterOrEqual(t, len(cudas), 1)
	require.Equal(t, cudas[0], "11.8")
	require.Nil(t, err)
}
