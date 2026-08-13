package cli

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/docker/command"
)

func TestDisplayHostForServe(t *testing.T) {
	tests := []struct {
		name string
		host string
		want string
	}{
		{"default localhost", command.DefaultHostIP, "localhost"},
		{"all interfaces", "0.0.0.0", "0.0.0.0"},
		{"custom IP", "192.168.1.1", "192.168.1.1"},
		{"IPv6 localhost", "::1", "localhost"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, displayHostForServe(tt.host))
		})
	}
}

func TestFormatServeURL(t *testing.T) {
	tests := []struct {
		name string
		host string
		port int
		want string
	}{
		{"default localhost", command.DefaultHostIP, 8393, "http://localhost:8393"},
		{"IPv6 localhost", "::1", 8393, "http://localhost:8393"},
		{"all interfaces shows localhost too", "0.0.0.0", 8393, "http://0.0.0.0:8393 (http://localhost:8393)"},
		{"custom IPv4", "192.168.1.1", 5000, "http://192.168.1.1:5000"},
		{"custom IPv6 is bracketed", "fe80::1", 5000, "http://[fe80::1]:5000"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, formatServeURL(tt.host, tt.port))
		})
	}
}

func TestValidateServePorts(t *testing.T) {
	tests := []struct {
		name           string
		serveHost      string
		port           int
		playgroundPort int
		playgroundHost string
		wantErr        bool
	}{
		{"distinct ports", command.DefaultHostIP, 8393, 9000, "127.0.0.1", false},
		{"playground picks free port", command.DefaultHostIP, 8393, 0, "127.0.0.1", false},
		{"same port and host", command.DefaultHostIP, 8393, 8393, "127.0.0.1", true},
		{"same port different host", "0.0.0.0", 8393, 8393, "127.0.0.1", true},
		{"same port serve loopback playground all", command.DefaultHostIP, 9000, 9000, "0.0.0.0", true},
		{"same port different specific host", "127.0.0.1", 8393, 8393, "192.168.1.1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateServePorts(tt.serveHost, tt.port, tt.playgroundPort, tt.playgroundHost)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateBoundServePorts(t *testing.T) {
	tests := []struct {
		name           string
		serveHost      string
		port           int
		playgroundPort int
		playgroundHost string
		wantErr        bool
	}{
		{"resolved port differs", command.DefaultHostIP, 8393, 9000, embeddedPlaygroundHost, false},
		{"resolved port matches", command.DefaultHostIP, 8393, 8393, embeddedPlaygroundHost, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateBoundServePorts(tt.serveHost, tt.port, tt.playgroundPort, tt.playgroundHost)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestPlaygroundTargetURL(t *testing.T) {
	tests := []struct {
		name string
		host string
		port int
		want string
	}{
		{"default loopback", command.DefaultHostIP, 8393, "http://127.0.0.1:8393"},
		{"custom IP", "192.168.1.1", 5000, "http://192.168.1.1:5000"},
		{"wildcard falls back to loopback", "0.0.0.0", 8393, "http://127.0.0.1:8393"},
		{"ipv6 wildcard falls back to loopback", "::", 8393, "http://127.0.0.1:8393"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, playgroundTargetURL(tt.host, tt.port))
		})
	}
}

func TestHostsOverlap(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
		want bool
	}{
		{"identical specific", "127.0.0.1", "127.0.0.1", true},
		{"wildcard and specific", "0.0.0.0", "127.0.0.1", true},
		{"specific and wildcard", "127.0.0.1", "0.0.0.0", true},
		{"ipv6 wildcard", "::", "192.168.1.1", true},
		{"two different specific", "127.0.0.1", "192.168.1.1", false},
		{"wildcard and wildcard", "0.0.0.0", "::", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, hostsOverlap(tt.a, tt.b))
		})
	}
}
