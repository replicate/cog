package docker

import (
	"fmt"
	"math/rand"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/go-connections/nat"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/testcontainers/testcontainers-go"
	testregistry "github.com/testcontainers/testcontainers-go/modules/registry"

	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/docker/dockertest"
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

func (s *DockerClientSuite) assertContainerIsStopped(t *testing.T, containerID string) {
	inspect := s.dockerHelper.InspectContainer(t, containerID)
	assert.False(t, inspect.State.Running, "Container should be stopped")
}

func (s *DockerClientSuite) assertContainerExists(t *testing.T, containerID string) {
	_, err := s.dockerHelper.Client.ContainerInspect(t.Context(), containerID)
	require.NoErrorf(t, err, "Container %q should exist", containerID)
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

// pickFreePort returns a TCP port in [min,max] that’s free *right now*.
// There’s still a small race between closing the listener and Docker grabbing
// the port, but it’s good enough for test code.
func pickFreePort(min, max int) (int, error) {
	if min < 1024 { //|| max > 9999 || min > max {
		return 0, fmt.Errorf("invalid port range")
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	for tries := 0; tries < 20; tries++ { // avoid infinite loops
		p := rng.Intn(max-min+1) + min
		l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
		if err == nil {
			l.Close()
			return p, nil // looks free
		}
	}
	return 0, fmt.Errorf("could not find free port in range %d-%d", min, max)
}

func makeHtpasswd(user, pass string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}

	line := user + ":" + string(hash) + "\n"
	return line, nil
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
	fmt.Println("runPullTests")
	// htpasswd, err := makeHtpasswd("user", "pass")
	// require.NoError(t, err, "Failed to make htpasswd")
	// port, err := pickFreePort(10000, 60000)
	// require.NoError(t, err, "Failed to pick free port")
	registryContainer, err := testregistry.Run(
		t.Context(),
		"registry:2",
		// testregistry.WithHtpasswd(htpasswd),
		testcontainers.WithHostConfigModifier(func(hostConfig *container.HostConfig) {
			port, err := pickFreePort(1024, 9999)
			require.NoError(t, err, "Failed to pick free port")
			hostConfig.PortBindings = map[nat.Port][]nat.PortBinding{
				nat.Port("5000/tcp"): {{HostIP: "0.0.0.0", HostPort: strconv.Itoa(port)}},
			}
		}),
	)
	defer testcontainers.CleanupContainer(t, registryContainer)
	require.NoError(t, err, "Failed to start registry container")

	// restore, err := testregistry.SetDockerAuthConfig(registryContainer.RegistryName, "user", "pass")
	// require.NoError(t, err, "Failed to set docker auth config")
	// defer restore()

	t.Run("RemoteImageExists", func(t *testing.T) {
		imageRef := dockertest.ImageRefWithRegistry(t, registryContainer.RegistryName, "")

		s.dockerHelper.LoadImageFixture(t, "alpine", imageRef)
		s.dockerHelper.MustPushImage(t, imageRef)
		s.dockerHelper.MustDeleteImage(t, imageRef)

		s.assertNoImageExists(t, imageRef)

		err = s.dockerClient.Pull(t.Context(), imageRef)
		require.NoError(t, err, "Failed to pull image %q", imageRef)
		s.dockerHelper.CleanupImage(t, imageRef)

		s.assertImageExists(t, imageRef)
	})

	t.Run("RemoteReferenceNotFound", func(t *testing.T) {
		imageRef := dockertest.ImageRefWithRegistry(t, registryContainer.RegistryName, "")

		s.assertNoImageExists(t, imageRef)

		err := s.dockerClient.Pull(t.Context(), imageRef)
		// TODO[md]: this might not be the right check. we probably want to wrap the error from the registry
		// so we handle other failure cases, like failed auth, unknown tag, and unknown repo
		require.Error(t, err, "Failed to pull image %q", imageRef)
		assert.ErrorContains(t, err, "failed to resolve reference")
	})

	t.Run("InvalidAuth", func(t *testing.T) {
		t.Skip("skip auth tests until we're using the docker engine since we can't set auth on the host without side effects")
		imageRef := dockertest.ImageRefWithRegistry(t, registryContainer.RegistryName, "")

		s.assertNoImageExists(t, imageRef)

		err = s.dockerClient.Pull(t.Context(), imageRef)
		// TODO[md]: this might not be the right check. we probably want to wrap the error from the registry
		// so we handle other failure cases, like failed auth, unknown tag, and unknown repo
		require.Error(t, err, "Failed to pull image %q", imageRef)
		assert.ErrorContains(t, err, "failed to resolve reference")
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

		container.IsRunning()

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
