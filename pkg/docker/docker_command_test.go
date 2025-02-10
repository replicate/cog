package docker

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDockerPush(t *testing.T) {
	t.Setenv(DockerCommandEnvVarName, "echo")

	command := NewDockerCommand()
	err := command.Push("test")
	require.NoError(t, err)
}
