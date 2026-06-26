//nolint:staticcheck // container.NetworkSettingsBase deprecated but Ports field moving to NetworkSettings in docker v29
package docker

import (
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/go-connections/nat"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/docker/dockertest"
)

func TestGetHostPortForContainer(t *testing.T) {
	runningState := &container.State{Status: "running", Running: true}

	inspect := func(bindings []nat.PortBinding) *container.InspectResponse {
		return &container.InspectResponse{
			ContainerJSONBase: &container.ContainerJSONBase{State: runningState},
			NetworkSettings: &container.NetworkSettings{
				NetworkSettingsBase: container.NetworkSettingsBase{
					Ports: nat.PortMap{nat.Port("5678/tcp"): bindings},
				},
			},
		}
	}

	inspectDifferentPort := func(bindings []nat.PortBinding) *container.InspectResponse {
		return &container.InspectResponse{
			ContainerJSONBase: &container.ContainerJSONBase{State: runningState},
			NetworkSettings: &container.NetworkSettings{
				NetworkSettingsBase: container.NetworkSettingsBase{
					Ports: nat.PortMap{nat.Port("1234/tcp"): bindings},
				},
			},
		}
	}

	inspectNoNetwork := func() *container.InspectResponse {
		return &container.InspectResponse{
			ContainerJSONBase: &container.ContainerJSONBase{State: runningState},
		}
	}

	inspectNotRunning := func() *container.InspectResponse {
		return &container.InspectResponse{
			ContainerJSONBase: &container.ContainerJSONBase{
				State: &container.State{Status: "dead", Dead: true},
			},
		}
	}

	tests := []struct {
		name          string
		hostIP        string
		inspect       *container.InspectResponse
		wantPort      int
		wantErrString string
	}{
		{
			name:     "matching localhost binding",
			hostIP:   command.DefaultHostIP,
			inspect:  inspect([]nat.PortBinding{{HostIP: command.DefaultHostIP, HostPort: "12345"}}),
			wantPort: 12345,
		},
		{
			name:     "empty hostIP defaults to localhost",
			hostIP:   "",
			inspect:  inspect([]nat.PortBinding{{HostIP: command.DefaultHostIP, HostPort: "12345"}}),
			wantPort: 12345,
		},
		{
			name:     "all interfaces",
			hostIP:   "0.0.0.0",
			inspect:  inspect([]nat.PortBinding{{HostIP: "0.0.0.0", HostPort: "12345"}}),
			wantPort: 12345,
		},
		{
			name:     "custom IP",
			hostIP:   "192.168.1.1",
			inspect:  inspect([]nat.PortBinding{{HostIP: "192.168.1.1", HostPort: "12345"}}),
			wantPort: 12345,
		},
		{
			name:     "IPv6 localhost",
			hostIP:   "::1",
			inspect:  inspect([]nat.PortBinding{{HostIP: "::1", HostPort: "12345"}}),
			wantPort: 12345,
		},
		{
			name:     "fallback to single binding when HostIP differs",
			hostIP:   command.DefaultHostIP,
			inspect:  inspect([]nat.PortBinding{{HostIP: "0.0.0.0", HostPort: "12345"}}),
			wantPort: 12345,
		},
		{
			name:     "fallback to single binding when HostIP is empty",
			hostIP:   command.DefaultHostIP,
			inspect:  inspect([]nat.PortBinding{{HostIP: "", HostPort: "12345"}}),
			wantPort: 12345,
		},
		{
			name:          "error when single binding has non-matching specific IP",
			hostIP:        command.DefaultHostIP,
			inspect:       inspect([]nat.PortBinding{{HostIP: "192.168.1.1", HostPort: "12345"}}),
			wantErrString: "does not have a port bound to " + command.DefaultHostIP,
		},
		{
			name:     "select matching binding from multiple",
			hostIP:   command.DefaultHostIP,
			inspect:  inspect([]nat.PortBinding{{HostIP: command.DefaultHostIP, HostPort: "12345"}, {HostIP: command.DefaultHostIP, HostPort: "54321"}}),
			wantPort: 12345,
		},
		{
			name:          "error when no matching binding and multiple bindings",
			hostIP:        command.DefaultHostIP,
			inspect:       inspect([]nat.PortBinding{{HostIP: "0.0.0.0", HostPort: "12345"}, {HostIP: "192.168.1.1", HostPort: "54321"}}),
			wantErrString: "does not have a port bound to " + command.DefaultHostIP,
		},
		{
			name:          "error when target port not exposed",
			hostIP:        command.DefaultHostIP,
			inspect:       inspectDifferentPort([]nat.PortBinding{{HostIP: command.DefaultHostIP, HostPort: "12345"}}),
			wantErrString: "does not have a port bound to " + command.DefaultHostIP,
		},
		{
			name:          "error when network settings missing",
			hostIP:        command.DefaultHostIP,
			inspect:       inspectNoNetwork(),
			wantErrString: "does not have expected network configuration",
		},
		{
			name:          "error when container not running",
			hostIP:        command.DefaultHostIP,
			inspect:       inspectNotRunning(),
			wantErrString: "is not running",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testClient := dockertest.NewMockCommand2(t)
			testClient.EXPECT().ContainerInspect(t.Context(), "container123").Return(tt.inspect, nil)

			hostPort, err := GetHostPortForContainer(t.Context(), testClient, "container123", 5678, tt.hostIP)
			if tt.wantErrString != "" {
				require.ErrorContains(t, err, tt.wantErrString)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantPort, hostPort)
		})
	}
}
