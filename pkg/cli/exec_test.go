package cli

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/docker/command"
)

func TestParsePublishFlags(t *testing.T) {
	tests := []struct {
		name      string
		values    []string
		wantPorts []command.Port
		wantErr   string
	}{
		{
			name:      "empty",
			values:    []string{},
			wantPorts: []command.Port{},
		},
		{
			name:   "port only",
			values: []string{"8000"},
			wantPorts: []command.Port{
				{HostPort: 8000, ContainerPort: 8000, HostIP: command.DefaultHostIP},
			},
		},
		{
			name:   "host:port",
			values: []string{"0.0.0.0:8000"},
			wantPorts: []command.Port{
				{HostPort: 8000, ContainerPort: 8000, HostIP: "0.0.0.0"},
			},
		},
		{
			name:   "IPv6 host:port",
			values: []string{"::1:8000"},
			wantPorts: []command.Port{
				{HostPort: 8000, ContainerPort: 8000, HostIP: "::1"},
			},
		},
		{
			name:   "multiple ports",
			values: []string{"8000", "0.0.0.0:8888"},
			wantPorts: []command.Port{
				{HostPort: 8000, ContainerPort: 8000, HostIP: command.DefaultHostIP},
				{HostPort: 8888, ContainerPort: 8888, HostIP: "0.0.0.0"},
			},
		},
		{
			name:   "bracketed IPv6 host:port",
			values: []string{"[::1]:8000"},
			wantPorts: []command.Port{
				{HostPort: 8000, ContainerPort: 8000, HostIP: "::1"},
			},
		},
		{
			name:   "bracketed IPv6 with zone",
			values: []string{"[::1%lo0]:8000"},
			wantPorts: []command.Port{
				{HostPort: 8000, ContainerPort: 8000, HostIP: "::1%lo0"},
			},
		},
		{
			name:    "invalid port",
			values:  []string{"not-a-port"},
			wantErr: "invalid port",
		},
		{
			name:    "empty value",
			values:  []string{""},
			wantErr: "cannot be empty",
		},
		{
			name:    "empty host",
			values:  []string{":8000"},
			wantErr: "host cannot be empty",
		},
		{
			name:    "empty port",
			values:  []string{"0.0.0.0:"},
			wantErr: "invalid port",
		},
		{
			name:    "port out of range",
			values:  []string{"99999"},
			wantErr: "between 1 and 65535",
		},
		{
			name:    "missing closing bracket",
			values:  []string{"[::1:8000"},
			wantErr: "missing closing bracket",
		},
		{
			name:    "port after bracket without colon",
			values:  []string{"[::1]8000"},
			wantErr: "expected ':' after ']'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ports, err := parsePublishFlags(tt.values)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantPorts, ports)
		})
	}
}
