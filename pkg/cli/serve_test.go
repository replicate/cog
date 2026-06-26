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
