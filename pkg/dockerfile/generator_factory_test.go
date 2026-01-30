package dockerfile

import (
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker/dockertest"
	"github.com/replicate/cog/pkg/registry/registrytest"
	"github.com/replicate/cog/pkg/util/console"
)

func TestGeneratorFactory(t *testing.T) {
	dir := t.TempDir()
	build := config.Build{
		PythonPackages: []string{"torch==2.5.1"},
	}
	cfg := config.Config{
		Build: &build,
	}
	command := dockertest.NewMockCommand()
	client := registrytest.NewMockRegistryClient()

	previousColor := console.ConsoleInstance.Color
	previousLevel := console.ConsoleInstance.Level
	console.SetColor(false)
	console.SetLevel(console.InfoLevel)
	defer func() {
		console.SetColor(previousColor)
		console.SetLevel(previousLevel)
	}()

	previousStderr := os.Stderr
	stderrReader, stderrWriter, err := os.Pipe()
	require.NoError(t, err)
	os.Stderr = stderrWriter
	defer func() {
		os.Stderr = previousStderr
		_ = stderrReader.Close()
		_ = stderrWriter.Close()
	}()

	generator, err := NewGenerator(&cfg, dir, true, command, true, client, true)
	require.NoError(t, err)
	require.Equal(t, STANDARD_GENERATOR_NAME, generator.Name())
	require.NotNil(t, cfg.Build.CogRuntime)
	require.True(t, *cfg.Build.CogRuntime)

	require.NoError(t, stderrWriter.Close())
	stderrOutput, err := io.ReadAll(stderrReader)
	require.NoError(t, err)
	require.Contains(t, string(stderrOutput), "experimental fast features, --x-fast / fast: true, are no longer supported. Use of flags/directives will error in a future release; falling back to standard build and generator.")
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
	client := registrytest.NewMockRegistryClient()
	generator, err := NewGenerator(&config, dir, false, command, true, client, true)
	require.NoError(t, err)
	require.Equal(t, generator.Name(), STANDARD_GENERATOR_NAME)
}
