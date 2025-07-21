package cogpack_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"

	"github.com/replicate/cog/pkg/cogpack"
	"github.com/replicate/cog/pkg/cogpack/builder"
	"github.com/replicate/cog/pkg/cogpack/project"
	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/docker/dockertest"
	"github.com/replicate/cog/pkg/util"
)

func requireIntegrationSuite(t *testing.T) {
	if os.Getenv("COGPACK_INTEGRATION") == "" {
		t.Skip("set COGPACK_INTEGRATION=1 to run integration tests")
	}
}

func TestPythonStack_SourceCodeIsCopied(t *testing.T) {
	requireIntegrationSuite(t)

	imageName := buildImageFromFixture(t, "minimal-source")
	fmt.Println("imageName", imageName)
	container := startContainer(t, imageName)
	defer testcontainers.CleanupContainer(t, container)
	assertFileExists(t, container, "/src/predict.py")
}

func TestPythonStack_EnvPropagation(t *testing.T) {
	requireIntegrationSuite(t)

	// baseImage := testcontainers.Run(t.Context())

}

func TestCogpackIntegration(t *testing.T) {
	if os.Getenv("COGPACK_INTEGRATION") == "" {
		t.Skip("set COGPACK_INTEGRATION=1 to run integration tests")
	}

	// Set COGPACK=1 to ensure proper Docker client is used
	t.Setenv("COGPACK", "1")
}

func testDependencies(t *testing.T) {
	// Build image from minimal-dependencies fixture
	imageName := buildImageFromFixture(t, "minimal-dependencies")

	// Start container with sleep to keep it running
	container := startContainer(t, imageName)

	// Assert requests dependency is installed
	assertPythonImport(t, container, "requests")

	// Verify specific version
	assertCommandOutput(t, container,
		[]string{"python", "-c", "import requests; print(requests.__version__)"},
		"2.31.0")

	// Check that venv directory exists
	assertFileExists(t, container, "/venv/lib/python3.11/site-packages/requests")

	// Verify the predictor can import and use requests
	assertCommandSucceeds(t, container, []string{"python", "-c", "from predict import Predictor; import requests"})
}

func OpenFixture(t *testing.T, fixtureName string) *fixture {
	t.Helper()

	dir := t.TempDir()
	sourcefs, err := os.OpenRoot(filepath.Join("testdata", "fixtures", fixtureName))
	require.NoError(t, err)

	err = os.CopyFS(dir, sourcefs.FS())
	require.NoError(t, err)

	fixturefs, err := os.OpenRoot(dir)
	require.NoError(t, err)

	t.Cleanup(func() {
		sourcefs.Close()
	})
	return &fixture{t: t, dir: dir, rootfs: fixturefs}
}

type fixture struct {
	t      *testing.T
	dir    string
	rootfs *os.Root
}

func (f *fixture) ReadConfig() (*config.Config, error) {
	f.t.Helper()

	configPath := filepath.Join(f.dir, "cog.yaml")
	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}
	return config.FromYAML(configBytes)
}

func (f *fixture) MustReadConfig() *config.Config {
	f.t.Helper()
	cfg, err := f.ReadConfig()
	require.NoError(f.t, err)
	return cfg
}

func (f *fixture) SourceInfo() (*project.SourceInfo, error) {
	f.t.Helper()

	return cogpack.NewSourceInfo(f.dir, f.MustReadConfig())
}

func (f *fixture) MustSourceInfo() *project.SourceInfo {
	f.t.Helper()
	src, err := f.SourceInfo()
	require.NoError(f.t, err)
	return src
}

// Helper function to build a cogpack image from a fixture directory
func buildImageFromFixture(t *testing.T, fixtureName string) string {
	t.Helper()

	modelFixture := OpenFixture(t, fixtureName)

	// Generate plan
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	planResult, err := cogpack.GeneratePlan(ctx, modelFixture.MustSourceInfo())
	require.NoError(t, err, "Failed to generate plan for %s", fixtureName)

	util.JSONPrettyPrint(planResult)

	// Create Docker command and builder - uses environment to choose client
	dockerClient := dockertest.NewHelperClient(t)
	dockerCmd, err := docker.NewClient(ctx)
	require.NoError(t, err, "Failed to create Docker client")

	builderInstance := builder.NewBuildKitBuilder(dockerCmd)

	// Build the image
	imageName := fmt.Sprintf("cogpack-test:%s-%d", fixtureName, time.Now().Unix())

	buildConfig := &builder.BuildConfig{
		Tag: imageName,
	}

	finalImageName, _, err := builderInstance.Build(ctx, planResult.Plan, buildConfig)
	require.NoError(t, err, "Failed to build image for %s", fixtureName)

	// Clean up image when test completes
	dockerClient.CleanupImage(t, finalImageName)

	return finalImageName
}

// Helper function to start a container with sleep infinity
func startContainer(t *testing.T, imageName string) testcontainers.Container {
	t.Helper()

	container, err := testcontainers.Run(
		t.Context(),
		imageName,
		testcontainers.WithCmd("sleep", "5000"),
	)

	require.NoError(t, err, "Failed to start container")
	// defer testcontainers.CleanupContainer(t, container)

	return container
}

// Assertion helper functions
func assertFileExists(t *testing.T, container testcontainers.Container, path string) {
	t.Helper()

	exitCode, _, err := container.Exec(t.Context(), []string{"test", "-f", path})
	require.NoError(t, err, "Failed to check if file exists: %s", path)
	assert.Equal(t, 0, exitCode, "File does not exist: %s", path)
}

func assertFileContent(t *testing.T, container testcontainers.Container, path, expectedContent string) {
	t.Helper()

	exitCode, reader, err := container.Exec(t.Context(), []string{"cat", path})
	require.NoError(t, err, "Failed to read file: %s", path)
	require.Equal(t, 0, exitCode, "Failed to cat file: %s", path)

	content, err := readAllFromReader(reader)
	require.NoError(t, err, "Failed to read container output")

	assert.Equal(t, expectedContent, strings.TrimSpace(content), "File content mismatch for %s", path)
}

func assertCommandSucceeds(t *testing.T, container testcontainers.Container, cmd []string) {
	t.Helper()

	exitCode, _, err := container.Exec(t.Context(), cmd)
	require.NoError(t, err, "Failed to execute command: %v", cmd)
	assert.Equal(t, 0, exitCode, "Command failed: %v", cmd)
}

func assertCommandOutput(t *testing.T, container testcontainers.Container, cmd []string, expectedOutput string) {
	t.Helper()

	exitCode, reader, err := container.Exec(t.Context(), cmd)
	require.NoError(t, err, "Failed to execute command: %v", cmd)
	require.Equal(t, 0, exitCode, "Command failed: %v", cmd)

	output, err := readAllFromReader(reader)
	require.NoError(t, err, "Failed to read command output")

	assert.Equal(t, expectedOutput, strings.TrimSpace(output), "Command output mismatch for: %v", cmd)
}

func assertPythonImport(t *testing.T, container testcontainers.Container, module string) {
	t.Helper()
	assertCommandSucceeds(t, container, []string{"python", "-c", fmt.Sprintf("import %s", module)})
}

// Helper to read all content from a reader (similar to io.ReadAll)
func readAllFromReader(reader io.Reader) (string, error) {
	if reader == nil {
		return "", nil
	}

	content, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}

	return string(content), nil
}
