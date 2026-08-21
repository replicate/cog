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

func TestCUDAVersionFromIndexURL(t *testing.T) {
	for _, tt := range []struct {
		url  string
		want string
		ok   bool
	}{
		{"https://download.pytorch.org/whl/cu128/", "12.8", true},
		{"https://download.pytorch.org/whl/cu118", "11.8", true},
		{"https://download.pytorch.org/whl/cu92/", "9.2", true},
		{"https://download.pytorch.org/whl/cpu/", "", false},
		{"https://download.pytorch.org/whl/rocm6.2/", "", false},
		{"", "", false},
	} {
		t.Run(tt.url, func(t *testing.T) {
			got, ok := cudaVersionFromIndexURL(tt.url)
			require.Equal(t, tt.ok, ok)
			require.Equal(t, tt.want, got)
		})
	}
}
