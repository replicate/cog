package docker

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/docker/dockertest"
)

func TestStandardPush(t *testing.T) {
	command := dockertest.NewMockCommand()
	dockertest.PushError = nil
	err := StandardPush(t.Context(), "test", command)
	require.NoError(t, err)
}

func TestStandardPushWithFullDockerCommand(t *testing.T) {
	t.Setenv(DockerCommandEnvVarName, "echo")
	command, err := NewClient(t.Context())
	require.NoError(t, err)
	err = StandardPush(t.Context(), "test", command)
	require.NoError(t, err)
}
