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

func TestGenerateTorchMinorVersionCompatibilityMatrix(t *testing.T) {
	matrix := []TorchCompatibility{{
		Torch:   "2.0.0",
		CUDA:    nil,
		Pythons: []string{"3.7", "3.8"},
	}, {
		Torch:   "2.0.0",
		CUDA:    stringp("12.0"),
		Pythons: []string{"3.7", "3.8"},
	}, {
		Torch:   "2.0.1",
		CUDA:    stringp("12.0"),
		Pythons: []string{"3.7", "3.8", "3.9"},
	}, {
		Torch:   "2.0.2",
		CUDA:    stringp("12.0"),
		Pythons: []string{"3.8", "3.9"},
	}, {
		Torch:   "2.1.0",
		CUDA:    stringp("12.2"),
		Pythons: []string{"3.8", "3.9"},
	}, {
		Torch:   "2.1.1",
		CUDA:    stringp("12.3"),
		Pythons: []string{"3.9", "3.10"},
	}}
	actual := generateTorchMinorVersionCompatibilityMatrix(matrix)

	expected := []TorchCompatibility{{
		Torch:   "2.1",
		CUDA:    stringp("12.3"),
		Pythons: []string{"3.9", "3.10"},
	}, {
		Torch:   "2.1",
		CUDA:    stringp("12.2"),
		Pythons: []string{"3.8", "3.9"},
	}, {
		Torch:   "2.0",
		CUDA:    stringp("12.0"),
		Pythons: []string{"3.8", "3.9"},
	}, {
		Torch:   "2.0",
		CUDA:    nil,
		Pythons: []string{"3.7", "3.8"},
	}}

	require.Equal(t, expected, actual)
}

func stringp(s string) *string {
	return &s
}
