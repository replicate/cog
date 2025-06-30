package docker

import (
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/go-connections/nat"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/docker/dockertest"
)

func TestGetHostPortForContainer(t *testing.T) {
	t.Run("WithExposedPort", func(t *testing.T) {
		testClient := dockertest.NewMockCommand2(t)
		testClient.EXPECT().ContainerInspect(t.Context(), "container123").Return(&container.InspectResponse{
			ContainerJSONBase: &container.ContainerJSONBase{
				State: &container.State{
					Status:  "running",
					Running: true,
				},
			},
			NetworkSettings: &container.NetworkSettings{
				NetworkSettingsBase: container.NetworkSettingsBase{
					Ports: nat.PortMap{
						nat.Port("5678/tcp"): []nat.PortBinding{
							{
								HostIP:   "0.0.0.0",
								HostPort: "12345",
							},
						},
					},
				},
			},
		}, nil)

		hostPort, err := GetHostPortForContainer(t.Context(), testClient, "container123", 5678)
		require.NoError(t, err)
		require.Equal(t, 12345, hostPort)
	})

	t.Run("WithMultipleExposedPorts", func(t *testing.T) {
		testClient := dockertest.NewMockCommand2(t)
		testClient.EXPECT().ContainerInspect(t.Context(), "container123").Return(&container.InspectResponse{
			ContainerJSONBase: &container.ContainerJSONBase{
				State: &container.State{
					Status:  "running",
					Running: true,
				},
			},
			NetworkSettings: &container.NetworkSettings{
				NetworkSettingsBase: container.NetworkSettingsBase{
					Ports: nat.PortMap{
						nat.Port("5678/tcp"): []nat.PortBinding{
							{
								HostIP:   "0.0.0.0",
								HostPort: "12345",
							},
							{
								HostIP:   "0.0.0.0",
								HostPort: "54321",
							},
						},
					},
				},
			},
		}, nil)

		hostPort, err := GetHostPortForContainer(t.Context(), testClient, "container123", 5678)
		require.NoError(t, err)
		require.Equal(t, 12345, hostPort)
	})

	t.Run("WithExposedPortOnDifferentAddress", func(t *testing.T) {
		testClient := dockertest.NewMockCommand2(t)
		testClient.EXPECT().ContainerInspect(t.Context(), "container123").Return(&container.InspectResponse{
			ContainerJSONBase: &container.ContainerJSONBase{
				State: &container.State{
					Status:  "running",
					Running: true,
				},
			},
			NetworkSettings: &container.NetworkSettings{
				NetworkSettingsBase: container.NetworkSettingsBase{
					Ports: nat.PortMap{
						nat.Port("5678/tcp"): []nat.PortBinding{
							{
								HostIP:   "127.0.0.1",
								HostPort: "12345",
							},
						},
					},
				},
			},
		}, nil)

		_, err := GetHostPortForContainer(t.Context(), testClient, "container123", 5678)
		require.ErrorContains(t, err, "does not have a port bound to 0.0.0.0")
	})

	t.Run("WithDifferentPortExposed", func(t *testing.T) {
		testClient := dockertest.NewMockCommand2(t)
		testClient.EXPECT().ContainerInspect(t.Context(), "container123").Return(&container.InspectResponse{
			ContainerJSONBase: &container.ContainerJSONBase{
				State: &container.State{
					Status:  "running",
					Running: true,
				},
			},
			NetworkSettings: &container.NetworkSettings{
				NetworkSettingsBase: container.NetworkSettingsBase{
					Ports: nat.PortMap{
						nat.Port("1234/tcp"): []nat.PortBinding{
							{
								HostIP:   "0.0.0.0",
								HostPort: "12345",
							},
						},
					},
				},
			},
		}, nil)

		_, err := GetHostPortForContainer(t.Context(), testClient, "container123", 5678)
		require.ErrorContains(t, err, "does not have a port bound to 0.0.0.0")
	})

	t.Run("WithNoExposedPort", func(t *testing.T) {
		testClient := dockertest.NewMockCommand2(t)
		testClient.EXPECT().ContainerInspect(t.Context(), "container123").Return(&container.InspectResponse{
			ContainerJSONBase: &container.ContainerJSONBase{
				State: &container.State{
					Status:  "running",
					Running: true,
				},
			},
		}, nil)

		_, err := GetHostPortForContainer(t.Context(), testClient, "container123", 5678)
		require.ErrorContains(t, err, "does not have expected network configuration")
	})

	t.Run("ContainerNotRunning", func(t *testing.T) {
		testClient := dockertest.NewMockCommand2(t)
		testClient.EXPECT().ContainerInspect(t.Context(), "container123").Return(&container.InspectResponse{
			ContainerJSONBase: &container.ContainerJSONBase{
				State: &container.State{
					Status: "dead",
					Dead:   true,
				},
			},
		}, nil)

		_, err := GetHostPortForContainer(t.Context(), testClient, "container123", 5678)
		require.ErrorContains(t, err, "is not running")
	})
}
