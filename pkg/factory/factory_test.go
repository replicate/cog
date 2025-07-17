//go:build ignore

package factory

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/util/console"
)

func TestTorchVersion(t *testing.T) {
	console.SetLevel(console.DebugLevel)

	cfg := &config.Config{
		Build: &config.Build{
			GPU: true,
			// PythonVersion: "3.10",
			PythonPackages: []string{"torch==2.6.0"},
		},
	}

	err := cfg.ValidateAndComplete(".")
	require.NoError(t, err)

	assert.Equal(t, "11.8||12.1", cfg.Build.CUDA)

	// ver, err := cfg.CudaVersionConstraint()
	// require.NoError(t, err)
	// require.Equal(t, "11.8||12.1", ver)

	// ver, ok := cfg.TorchVersion()
	// // require.NoError(t, err)
	// fmt.Println(ver, ok)
	// require.Equal(t, "2.0.0", ver)
}
