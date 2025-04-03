package dockerfile

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker/dockertest"
)

func TestGeneratorFactory(t *testing.T) {
	dir := t.TempDir()
	build := config.Build{
		PythonPackages: []string{"torch==2.5.1"},
	}
	config := config.Config{
		Build: &build,
	}
	command := dockertest.NewMockCommand()
	generator, err := NewGenerator(&config, dir, true, command, true)
	require.NoError(t, err)
	require.Equal(t, generator.Name(), FAST_GENERATOR_NAME)
}

func TestGeneratorFactoryStandardGenerator(t *testing.T) {
	dir := t.TempDir()
	build := config.Build{
		PythonPackages: []string{"torch==2.5.1"},
	}
	config := config.Config{
		Build: &build,
	}
	command := dockertest.NewMockCommand()
	generator, err := NewGenerator(&config, dir, false, command, true)
	require.NoError(t, err)
	require.Equal(t, generator.Name(), STANDARD_GENERATOR_NAME)
}
