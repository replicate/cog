package docker

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/docker/command"
)

func TestDockerPush(t *testing.T) {
	t.Setenv(DockerCommandEnvVarName, "echo")

	command := NewDockerCommand()
	err := command.Push(t.Context(), "test")
	require.NoError(t, err)
}

func TestDockerArgs(t *testing.T) {
	tests := []struct {
		name     string
		options  command.ImageBuildOptions
		expected []string
	}{
		{
			name: "basic build",
			options: command.ImageBuildOptions{
				ImageName: "some-image",
			},
			expected: []string{
				"buildx",
				"build",
				"--provenance", "false",
				"--platform", "linux/amd64",
				"--builder", "docker-container",
				"--load",
				"--cache-to", "type=inline",
				"--file", "-",
				"--tag", "some-image",
				".",
			},
		},
		{
			name: "with secrets",
			options: command.ImageBuildOptions{
				ImageName: "some-image",
				Secrets:   []string{"id=secret1", "id=secret2"},
			},
			expected: []string{
				"buildx",
				"build",
				"--provenance", "false",
				"--platform", "linux/amd64",
				"--builder", "docker-container",
				"--load",
				"--secret", "id=secret1",
				"--secret", "id=secret2",
				"--cache-to", "type=inline",
				"--file", "-",
				"--tag", "some-image",
				".",
			},
		},
		{
			name: "with no cache",
			options: command.ImageBuildOptions{
				ImageName: "some-image",
				NoCache:   true,
			},
			expected: []string{
				"buildx",
				"build",
				"--provenance", "false",
				"--platform", "linux/amd64",
				"--builder", "docker-container",
				"--load",
				"--no-cache",
				"--cache-to", "type=inline",
				"--file", "-",
				"--tag", "some-image",
				".",
			},
		},
		{
			name: "with labels",
			options: command.ImageBuildOptions{
				ImageName: "some-image",
				Labels: map[string]string{
					"org.opencontainers.image.version": "1.0.0",
					"org.opencontainers.image.source":  "https://github.com/org/repo",
				},
			},
			expected: []string{
				"buildx",
				"build",
				"--provenance", "false",
				"--platform", "linux/amd64",
				"--builder", "docker-container",
				"--load",
				"--label", "org.opencontainers.image.source=https://github.com/org/repo",
				"--label", "org.opencontainers.image.version=1.0.0",
				"--cache-to", "type=inline",
				"--file", "-",
				"--tag", "some-image",
				".",
			},
		},
		{
			name: "with epoch timestamp",
			options: command.ImageBuildOptions{
				ImageName: "some-image",
				Epoch:     ptr(int64(1374391507024)),
			},
			expected: []string{
				"buildx",
				"build",
				"--provenance", "false",
				"--platform", "linux/amd64",
				"--builder", "docker-container",
				"--load",
				"--build-arg", "SOURCE_DATE_EPOCH=1374391507024",
				"--output", "type=docker,rewrite-timestamp=true",
				"--cache-to", "type=inline",
				"--file", "-",
				"--tag", "some-image",
				".",
			},
		},
		{
			name: "with build contexts",
			options: command.ImageBuildOptions{
				ImageName: "some-image",
				BuildContexts: map[string]string{
					"context1": "/path/to/context1",
					"context2": "/path/to/context2",
				},
			},
			expected: []string{
				"buildx",
				"build",
				"--provenance", "false",
				"--platform", "linux/amd64",
				"--builder", "docker-container",
				"--load",
				"--cache-to", "type=inline",
				"--build-context", "context1=/path/to/context1",
				"--build-context", "context2=/path/to/context2",
				"--file", "-",
				"--tag", "some-image",
				".",
			},
		},
		{
			name: "with progress output",
			options: command.ImageBuildOptions{
				ImageName:      "some-image",
				ProgressOutput: "plain",
			},
			expected: []string{
				"buildx",
				"build",
				"--provenance", "false",
				"--platform", "linux/amd64",
				"--builder", "docker-container",
				"--load",
				"--cache-to", "type=inline",
				"--progress", "plain",
				"--file", "-",
				"--tag", "some-image",
				".",
			},
		},
		{
			name: "with custom context dir",
			options: command.ImageBuildOptions{
				ImageName:  "some-image",
				ContextDir: "/custom/context",
			},
			expected: []string{
				"buildx",
				"build",
				"--provenance", "false",
				"--platform", "linux/amd64",
				"--builder", "docker-container",
				"--load",
				"--cache-to", "type=inline",
				"--file", "-",
				"--tag", "some-image",
				"/custom/context",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewDockerCommand()
			actual := c.imageBuildArgs(tt.options)
			require.Equal(t, tt.expected, actual)
		})
	}
}

// ptr returns a pointer to the given value
func ptr[T any](v T) *T {
	return &v
}
