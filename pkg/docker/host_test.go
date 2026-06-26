package docker

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsLocalDockerHost(t *testing.T) {
	tests := []struct {
		name string
		host string
		want bool
	}{
		{"empty", "", true},
		{"unix socket", "unix:///var/run/docker.sock", true},
		{"named pipe", "npipe:////./pipe/docker_engine", true},
		{"tcp localhost", "tcp://localhost:2375", false},
		{"tcp loopback IP", "tcp://127.0.0.1:2375", false},
		{"tcp IPv6 loopback", "tcp://[::1]:2375", false},
		{"tcp remote", "tcp://192.168.1.1:2375", false},
		{"ssh", "ssh://user@host", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, isLocalDockerHost(tt.host))
		})
	}
}

func TestIsRemoteDockerHost(t *testing.T) {
	t.Setenv("DOCKER_HOST", "tcp://192.168.1.1:2375")

	isRemote, host, err := IsRemoteDockerHost()
	require.NoError(t, err)
	require.True(t, isRemote)
	require.Equal(t, "tcp://192.168.1.1:2375", host)
}

func TestIsRemoteDockerHost_Local(t *testing.T) {
	t.Setenv("DOCKER_HOST", "unix:///var/run/docker.sock")

	isRemote, host, err := IsRemoteDockerHost()
	require.NoError(t, err)
	require.False(t, isRemote)
	require.Equal(t, "unix:///var/run/docker.sock", host)
}
