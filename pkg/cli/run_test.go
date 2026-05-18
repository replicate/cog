package cli

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCommandVisibilityForRunPredictAndExec(t *testing.T) {
	root, err := NewRootCommand()
	require.NoError(t, err)

	runCmd, _, err := root.Find([]string{"run"})
	require.NoError(t, err)
	require.Equal(t, "run [image]", runCmd.Use)
	require.False(t, runCmd.Hidden)
	require.Contains(t, runCmd.Example, "cog run -i prompt")
	require.Nil(t, runCmd.Flags().Lookup("publish"))

	predictCmd, _, err := root.Find([]string{"predict"})
	require.NoError(t, err)
	require.True(t, predictCmd.Hidden)
	require.Contains(t, predictCmd.Short, "deprecated")

	execCmd, _, err := root.Find([]string{"exec"})
	require.NoError(t, err)
	require.NotContains(t, execCmd.Aliases, "run")
}
