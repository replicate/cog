package dockerfile

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/config"
)

func TestGeneratorFactory(t *testing.T) {
	dir := t.TempDir()
	build := config.Build{
		PythonPackages: []string{"torch==2.5.1"},
	}
	config := config.Config{
		Build: &build,
	}
	generator, err := NewGenerator(&config, dir, true)
	require.NoError(t, err)
	require.Equal(t, generator.Name(), FAST_GENERATOR_NAME)
}
