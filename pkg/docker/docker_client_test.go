package docker

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/docker/dockertest"
	"github.com/replicate/cog/pkg/registry_testhelpers"
)

func TestDockerClient(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping docker client tests in short mode")
	}

	client := NewDockerCommand()
	runDockerClientTests(t, client)
}

func TestDockerAPIClient(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping docker client tests in short mode")
	}

	apiClient, err := NewAPIClient(t.Context())
	require.NoError(t, err, "Failed to create docker api client")
	runDockerClientTests(t, apiClient)
}

func runDockerClientTests(t *testing.T, dockerClient command.Command) {
	dockerHelper := dockertest.NewHelperClient(t)
	testRegistry := registry_testhelpers.StartTestRegistry(t)

	dockerHelper.CleanupImages(t)

	t.Run("ImageInspect", func(t *testing.T) {
		t.Parallel()

		t.Run("ExistingLocalImage", func(t *testing.T) {
			t.Parallel()

			imageRef := imageRefForTest(t)
			dockerHelper.LoadImageFixture(t, "alpine", imageRef)

			expectedImage := dockerHelper.InspectImage(t, imageRef)
			resp, err := dockerClient.Inspect(t.Context(), imageRef)
			require.NoError(t, err, "Failed to inspect image %q", imageRef)
			assert.Equal(t, expectedImage.ID, resp.ID)
		})

		t.Run("MissingLocalImage", func(t *testing.T) {
			t.Parallel()

			image := "not-a-valid-image"
			_, err := dockerClient.Inspect(t.Context(), image)
			assert.ErrorIs(t, err, &command.NotFoundError{})
			assert.ErrorContains(t, err, "image not found")
		})
	})

	t.Run("Pull", func(t *testing.T) {
		t.Parallel()

		// TODO[md]: add tests for the following permutations:
		// - remote reference exists/not exists
		// - local reference exists/not exists
		// - force pull true/false

		t.Run("RemoteImageExists", func(t *testing.T) {
			t.Parallel()
			repo := testRegistry.CloneRepoForTest(t, "alpine")
			imageRef := repo + ":latest"

			assertNoImageExists(t, dockerClient, imageRef)

			resp, err := dockerClient.Pull(t.Context(), imageRef, false)
			require.NoError(t, err, "Failed to pull image %q", imageRef)
			dockerHelper.CleanupImage(t, imageRef)

			assertImageExists(t, dockerClient, imageRef)
			expectedResp := dockerHelper.InspectImage(t, imageRef)
			// TODO[md]: we should check that the responsees are actually equal beyond the IDs. but atm
			// the CLI and api are slightly different. The CLI leaves the descriptor field nil while the
			// API response is populated. These should be identical on the new client, so we can change to EqualValues
			assert.Equal(t, expectedResp.ID, resp.ID, "inspect response should match expected")
		})

		t.Run("RemoteReferenceNotFound", func(t *testing.T) {
			t.Parallel()
			imageRef := testRegistry.ImageRefForTest(t, "")

			assertNoImageExists(t, dockerClient, imageRef)

			resp, err := dockerClient.Pull(t.Context(), imageRef, false)
			// TODO[md]: this might not be the right check. we probably want to wrap the error from the registry
			// so we handle other failure cases, like failed auth, unknown tag, and unknown repo
			require.Error(t, err, "Failed to pull image %q", imageRef)
			assert.ErrorIs(t, err, &command.NotFoundError{Object: "manifest", Ref: imageRef})
			assert.Nil(t, resp, "inspect response should be nil")
		})

		t.Run("InvalidAuth", func(t *testing.T) {
			t.Skip("skip auth tests until we're using the docker engine since we can't set auth on the host without side effects")
			imageRef := testRegistry.ImageRefForTest(t, "")

			assertNoImageExists(t, dockerClient, imageRef)

			resp, err := dockerClient.Pull(t.Context(), imageRef, false)
			// TODO[md]: this might not be the right check. we probably want to wrap the error from the registry
			// so we handle other failure cases, like failed auth, unknown tag, and unknown repo
			require.Error(t, err, "Failed to pull image %q", imageRef)
			assert.ErrorContains(t, err, "failed to resolve reference")
			assert.Nil(t, resp, "inspect response should be nil")
		})
	})

	t.Run("ContainerStop", func(t *testing.T) {
		t.Parallel()

		t.Run("ContainerExistsAndIsRunning", func(t *testing.T) {
			t.Parallel()

			container, err := testcontainers.Run(
				t.Context(),
				testRegistry.ImageRef("alpine:latest"),
				testcontainers.WithCmd("sleep", "5000"),
			)
			defer dockerHelper.CleanupImages(t)
			defer testcontainers.CleanupContainer(t, container)
			require.NoError(t, err, "Failed to run container")

			err = dockerClient.ContainerStop(t.Context(), container.ID)
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
						Image: testRegistry.ImageRef("alpine:latest"),
						Cmd:   []string{"sleep", "5000"},
					},
					Started: false,
				},
			)
			defer testcontainers.CleanupContainer(t, container)
			containerID := container.GetContainerID()
			require.NoError(t, err, "Failed to create container")

			err = dockerClient.ContainerStop(t.Context(), containerID)
			require.NoError(t, err, "Failed to stop container %q", containerID)

			state, err := container.State(t.Context())
			require.NoError(t, err, "Failed to get container state")
			assert.Equal(t, state.Running, false)
		})

		t.Run("ContainerDoesNotExist", func(t *testing.T) {
			t.Parallel()

			err := dockerClient.ContainerStop(t.Context(), "containerid-that-does-not-exist")
			require.ErrorIs(t, err, &command.NotFoundError{})
			require.ErrorContains(t, err, "container not found")
		})
	})

	t.Run("ContainerInspect", func(t *testing.T) {
		t.Parallel()

		t.Run("ContainerExists", func(t *testing.T) {
			t.Parallel()

			container, err := testcontainers.Run(
				t.Context(),
				testRegistry.ImageRef("alpine:latest"),
				testcontainers.WithCmd("sleep", "5000"),
			)
			defer testcontainers.CleanupContainer(t, container)
			require.NoError(t, err, "Failed to run container")

			expected, err := container.Inspect(t.Context())
			require.NoError(t, err, "Failed to inspect container for expected response")

			resp, err := dockerClient.ContainerInspect(t.Context(), container.ID)
			require.NoError(t, err, "Failed to inspect container")
			require.Equal(t, expected, resp)
		})

		t.Run("ContainerDoesNotExist", func(t *testing.T) {
			t.Parallel()

			_, err := dockerClient.ContainerInspect(t.Context(), "containerid-that-does-not-exist")
			require.ErrorIs(t, err, &command.NotFoundError{})
		})
	})

	t.Run("ContainerLogs", func(t *testing.T) {
		t.Parallel()

		t.Run("ContainerExistsAndIsRunning", func(t *testing.T) {
			t.Parallel()

			container, err := testcontainers.Run(
				t.Context(),
				testRegistry.ImageRef("alpine:latest"),
				// print "line $i" N times then exit, where $i is the line number
				testcontainers.WithCmd("sh", "-c", "for i in $(seq 1 5); do echo \"line $i\"; sleep 1; done"),
			)
			require.NoError(t, err, "Failed to run container")
			defer testcontainers.CleanupContainer(t, container)

			var buf bytes.Buffer
			err = dockerClient.ContainerLogs(t.Context(), container.ID, &buf)
			require.NoError(t, err, "Failed to get container logs")

			assert.Equal(t, "[stdout] line 1\n[stdout] line 2\n[stdout] line 3\n[stdout] line 4\n[stdout] line 5\n", buf.String())
		})

		t.Run("ContainerAlreadyStopped", func(t *testing.T) {
			t.Parallel()

			container, err := testcontainers.Run(
				t.Context(),
				testRegistry.ImageRef("alpine:latest"),
				testcontainers.WithCmd("sh", "-c", "for i in $(seq 1 3); do echo \"line $i\"; sleep 0.1; done"),
				testcontainers.WithWaitStrategy(wait.ForExit()),
			)
			require.NoError(t, err, "Failed to run container")
			defer testcontainers.CleanupContainer(t, container)

			state, err := container.State(t.Context())
			require.NoError(t, err, "Failed to get container state")
			assert.Equal(t, state.Running, false)

			var buf bytes.Buffer
			err = dockerClient.ContainerLogs(t.Context(), container.ID, &buf)
			require.NoError(t, err, "Failed to get container logs")

			assert.Equal(t, "[stdout] line 1\n[stdout] line 2\n[stdout] line 3\n", buf.String())
		})

		t.Run("ContainerDoesNotExist", func(t *testing.T) {
			t.Parallel()

			err := dockerClient.ContainerLogs(t.Context(), "containerid-that-does-not-exist", &bytes.Buffer{})
			require.ErrorIs(t, err, &command.NotFoundError{})
		})
	})
}

func imageRefForTest(t *testing.T) string {
	return fmt.Sprintf("%s:test-%d", strings.ToLower(t.Name()), time.Now().Unix())
}

func assertImageExists(t *testing.T, dockerClient command.Command, imageRef string) {
	inspect, err := dockerClient.Inspect(t.Context(), imageRef)
	assert.NoError(t, err, "Failed to inspect image %q", imageRef)
	assert.NotNil(t, inspect, "Image should exist")
}

func assertNoImageExists(t *testing.T, dockerClient command.Command, imageRef string) {
	inspect, err := dockerClient.Inspect(t.Context(), imageRef)
	assert.ErrorIs(t, err, &command.NotFoundError{}, "Image should not exist")
	assert.Nil(t, inspect, "Image should not exist")
}
