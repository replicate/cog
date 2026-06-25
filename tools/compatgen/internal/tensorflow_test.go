package internal

import (
	"cmp"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/config"
)

func TestTensorFlowCompatibilitySortingIsDeterministic(t *testing.T) {
	compats := []config.TFCompatibility{
		{TF: "2.20.0", CUDA: "12.5", CuDNN: "9.3"},
		{TF: "2.21.0", CUDA: "12.5", CuDNN: "9.3"},
		{TF: "2.15.0", CUDA: "12.2", CuDNN: "8.9"},
		{TF: "2.15.0", CUDA: "12.3", CuDNN: "8.9"},
		{TF: "2.20.0", CUDA: "12.5", CuDNN: "9.2"},
	}

	// Apply the same sort logic used in FetchTensorFlowCompatibilityMatrix
	slices.SortFunc(compats, func(a, b config.TFCompatibility) int {
		return cmp.Or(
			cmp.Compare(a.TF, b.TF),
			cmp.Compare(a.CUDA, b.CUDA),
			cmp.Compare(a.CuDNN, b.CuDNN),
		)
	})

	require.Equal(t, []config.TFCompatibility{
		{TF: "2.15.0", CUDA: "12.2", CuDNN: "8.9"},
		{TF: "2.15.0", CUDA: "12.3", CuDNN: "8.9"},
		{TF: "2.20.0", CUDA: "12.5", CuDNN: "9.2"},
		{TF: "2.20.0", CUDA: "12.5", CuDNN: "9.3"},
		{TF: "2.21.0", CUDA: "12.5", CuDNN: "9.3"},
	}, compats)
}
