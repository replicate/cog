package docker

import (
	"testing"

	"github.com/docker/go-connections/nat"
	"github.com/stretchr/testify/assert"

	"github.com/replicate/cog/pkg/docker/command"
)

func TestExposedPortsFromRunOptions(t *testing.T) {
	tests := []struct {
		name  string
		ports []command.Port
		want  nat.PortSet
	}{
		{
			name:  "empty",
			ports: nil,
			want:  nat.PortSet{},
		},
		{
			name: "single port",
			ports: []command.Port{
				{HostPort: 8080, ContainerPort: 5000, HostIP: command.DefaultHostIP},
			},
			want: nat.PortSet{
				nat.Port("5000/tcp"): {},
			},
		},
		{
			name: "multiple ports",
			ports: []command.Port{
				{HostPort: 8080, ContainerPort: 5000},
				{HostPort: 8888, ContainerPort: 8888, HostIP: "0.0.0.0"},
			},
			want: nat.PortSet{
				nat.Port("5000/tcp"): {},
				nat.Port("8888/tcp"): {},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, exposedPortsFromRunOptions(tt.ports))
		})
	}
}

func TestPortBindingsFromRunOptions(t *testing.T) {
	tests := []struct {
		name  string
		ports []command.Port
		want  nat.PortMap
	}{
		{
			name:  "empty",
			ports: nil,
			want:  nat.PortMap{},
		},
		{
			name: "default host IP",
			ports: []command.Port{
				{HostPort: 8080, ContainerPort: 5000},
			},
			want: nat.PortMap{
				nat.Port("5000/tcp"): {
					{HostIP: command.DefaultHostIP, HostPort: "8080"},
				},
			},
		},
		{
			name: "explicit host IP",
			ports: []command.Port{
				{HostPort: 8080, ContainerPort: 5000, HostIP: "0.0.0.0"},
			},
			want: nat.PortMap{
				nat.Port("5000/tcp"): {
					{HostIP: "0.0.0.0", HostPort: "8080"},
				},
			},
		},
		{
			name: "IPv6 host IP",
			ports: []command.Port{
				{HostPort: 8080, ContainerPort: 5000, HostIP: "::1"},
			},
			want: nat.PortMap{
				nat.Port("5000/tcp"): {
					{HostIP: "::1", HostPort: "8080"},
				},
			},
		},
		{
			name: "multiple ports",
			ports: []command.Port{
				{HostPort: 8080, ContainerPort: 5000},
				{HostPort: 8888, ContainerPort: 8888, HostIP: "0.0.0.0"},
			},
			want: nat.PortMap{
				nat.Port("5000/tcp"): {
					{HostIP: command.DefaultHostIP, HostPort: "8080"},
				},
				nat.Port("8888/tcp"): {
					{HostIP: "0.0.0.0", HostPort: "8888"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, portBindingsFromRunOptions(tt.ports))
		})
	}
}
