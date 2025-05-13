package docker

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/testcontainers/testcontainers-go"

	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/docker/dockertest"
	"github.com/replicate/cog/pkg/registry_testhelpers"
)

func TestDockerClient(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping docker client tests in short mode")
	}

	suite := &DockerClientSuite{
		dockerHelper: dockertest.NewHelperClient(t),
		dockerClient: NewDockerCommand(),
	}

	t.Run("ImageInspect", suite.runImageInspectTests)
	t.Run("Pull", suite.runPullTests)
	t.Run("ContainerStop", suite.runContainerStopTests)
}

type DockerClientSuite struct {
	dockerHelper *dockertest.HelperClient
	dockerClient command.Command
}

func (s *DockerClientSuite) assertImageExists(t *testing.T, imageRef string) {
	inspect, err := s.dockerClient.Inspect(t.Context(), imageRef)
	assert.NoError(t, err, "Failed to inspect image %q", imageRef)
	assert.NotNil(t, inspect, "Image should exist")
}

func (s *DockerClientSuite) assertNoImageExists(t *testing.T, imageRef string) {
	inspect, err := s.dockerClient.Inspect(t.Context(), imageRef)
	assert.ErrorIs(t, err, &command.NotFoundError{}, "Image should not exist")
	assert.Nil(t, inspect, "Image should not exist")
}

func (s *DockerClientSuite) runImageInspectTests(t *testing.T) {
	t.Run("ExistingLocalImage", func(t *testing.T) {
		t.Parallel()

		image := "docker.io/library/busybox:latest"

		s.dockerHelper.MustPullImage(t, image)

		expectedImage := s.dockerHelper.InspectImage(t, image)
		resp, err := s.dockerClient.Inspect(t.Context(), image)
		require.NoError(t, err, "Failed to inspect image %q", image)
		assert.Equal(t, expectedImage.ID, resp.ID)
	})

	t.Run("MissingLocalImage", func(t *testing.T) {
		t.Parallel()

		image := "not-a-valid-image"
		_, err := s.dockerClient.Inspect(t.Context(), image)
		assert.ErrorIs(t, err, &command.NotFoundError{})
		assert.ErrorContains(t, err, "image not found")
	})
}

func (s *DockerClientSuite) runPullTests(t *testing.T) {
	testRegistry := registry_testhelpers.StartTestRegistry(t)

	// TODO[md]: add tests for the following permutations:
	// - remote reference exists/not exists
	// - local reference exists/not exists
	// - force pull true/false

	t.Run("RemoteImageExists", func(t *testing.T) {
		imageRef := testRegistry.ImageRefForTest(t, "")

		s.dockerHelper.LoadImageFixture(t, "alpine", imageRef)
		s.dockerHelper.MustPushImage(t, imageRef)
		s.dockerHelper.MustDeleteImage(t, imageRef)

		s.assertNoImageExists(t, imageRef)

		resp, err := s.dockerClient.Pull(t.Context(), imageRef, false)
		require.NoError(t, err, "Failed to pull image %q", imageRef)
		s.dockerHelper.CleanupImage(t, imageRef)

		s.assertImageExists(t, imageRef)
		expectedResp := s.dockerHelper.InspectImage(t, imageRef)
		// TODO[md]: we should check that the responsees are actually equal beyond the IDs. but atm
		// the CLI and api are slightly different. The CLI leaves the descriptor field nil while the
		// API response is populated. These should be identical on the new client, so we can change to EqualValues
		assert.Equal(t, expectedResp.ID, resp.ID, "inspect response should match expected")
	})

	t.Run("RemoteReferenceNotFound", func(t *testing.T) {
		imageRef := testRegistry.ImageRefForTest(t, "")

		s.assertNoImageExists(t, imageRef)

		resp, err := s.dockerClient.Pull(t.Context(), imageRef, false)
		// TODO[md]: this might not be the right check. we probably want to wrap the error from the registry
		// so we handle other failure cases, like failed auth, unknown tag, and unknown repo
		require.Error(t, err, "Failed to pull image %q", imageRef)
		assert.ErrorIs(t, err, &command.NotFoundError{Object: "manifest", Ref: imageRef})
		assert.Nil(t, resp, "inspect response should be nil")
	})

	t.Run("InvalidAuth", func(t *testing.T) {
		t.Skip("skip auth tests until we're using the docker engine since we can't set auth on the host without side effects")
		imageRef := testRegistry.ImageRefForTest(t, "")

		s.assertNoImageExists(t, imageRef)

		resp, err := s.dockerClient.Pull(t.Context(), imageRef, false)
		// TODO[md]: this might not be the right check. we probably want to wrap the error from the registry
		// so we handle other failure cases, like failed auth, unknown tag, and unknown repo
		require.Error(t, err, "Failed to pull image %q", imageRef)
		assert.ErrorContains(t, err, "failed to resolve reference")
		assert.Nil(t, resp, "inspect response should be nil")
	})
}

func (s *DockerClientSuite) runContainerStopTests(t *testing.T) {
	t.Run("ContainerExistsAndIsRunning", func(t *testing.T) {
		t.Parallel()

		container, err := testcontainers.Run(
			t.Context(),
			"docker.io/library/busybox:latest",
			testcontainers.WithCmd("sleep", "5000"),
		)
		defer testcontainers.CleanupContainer(t, container)
		require.NoError(t, err, "Failed to run container")

		err = s.dockerClient.ContainerStop(t.Context(), container.ID)
		require.NoError(t, err, "Failed to stop container %q", container.ID)

		state, err := container.State(t.Context())
		require.NoError(t, err, "Failed to get container state")
		assert.Equal(t, state.Running, false)
	})

	t.Run("ContainerExistsAndIsNotRunning", func(t *testing.T) {
		t.Parallel()

		container, err := testcontainers.GenericContainer(t.Context(),
			testcontainers.GenericContainerRequest{
				ContainerRequest: testcontainers.ContainerRequest{
					Image: "docker.io/library/busybox:latest",
					Cmd:   []string{"sleep", "5000"},
				},
				Started: false,
			},
		)
		defer testcontainers.CleanupContainer(t, container)
		containerID := container.GetContainerID()
		require.NoError(t, err, "Failed to create container")

		err = s.dockerClient.ContainerStop(t.Context(), containerID)
		require.NoError(t, err, "Failed to stop container %q", containerID)

		state, err := container.State(t.Context())
		require.NoError(t, err, "Failed to get container state")
		assert.Equal(t, state.Running, false)
	})

	t.Run("ContainerDoesNotExist", func(t *testing.T) {
		t.Parallel()

		err := s.dockerClient.ContainerStop(t.Context(), "containerid-that-does-not-exist")
		require.ErrorIs(t, err, &command.NotFoundError{})
		require.ErrorContains(t, err, "container not found")
	})
}
