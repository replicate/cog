package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLatestCuDNNForCUDA(t *testing.T) {
	actual := latestCuDNNForCUDA("10.2")
	require.Equal(t, "8", actual)
}
