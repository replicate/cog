package cli

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunCommandDispatchMode(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want runDispatchMode
	}{
		{name: "no args predicts local project", args: nil, want: runDispatchPredict},
		{name: "one arg predicts image", args: []string{"r8.im/acme/model"}, want: runDispatchPredict},
		{name: "one likely command forwards to exec", args: []string{"python"}, want: runDispatchExec},
		{name: "run flag before likely command forwards to exec", args: []string{"--gpus", "all", "python"}, want: runDispatchExec},
		{name: "one image plus input flag predicts image", args: []string{"r8.im/acme/model", "-i", "prompt=hello"}, want: runDispatchPredict},
		{name: "two args forwards to exec", args: []string{"python", "script.py"}, want: runDispatchExec},
		{name: "command flag forwards to exec", args: []string{"python", "-m", "http.server"}, want: runDispatchExec},
		{name: "command args before input-like flag forwards to exec", args: []string{"python", "script.py", "-i", "input.txt"}, want: runDispatchExec},
		{name: "run flag before command forwards to exec", args: []string{"--gpus", "all", "python", "script.py"}, want: runDispatchExec},
		{name: "run flag before image predicts image", args: []string{"--gpus", "all", "r8.im/acme/model"}, want: runDispatchPredict},
		{name: "config file before image predicts image", args: []string{"--file", "custom.yaml", "r8.im/acme/model"}, want: runDispatchPredict},
		{name: "cuda base flag before image predicts image", args: []string{"--use-cuda-base-image", "false", "r8.im/acme/model"}, want: runDispatchPredict},
		{name: "publish before command forwards to exec", args: []string{"-p", "8888", "jupyter", "notebook"}, want: runDispatchExec},
		{name: "publish without command forwards to exec for arg validation", args: []string{"-p", "8888"}, want: runDispatchExec},
		{name: "help alone is prediction help", args: []string{"--help"}, want: runDispatchPredict},
		{name: "command help forwards to exec", args: []string{"python", "script.py", "--help"}, want: runDispatchExec},
		{name: "single command help forwards to exec", args: []string{"python", "--help"}, want: runDispatchExec},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, runDispatchModeForArgs(tt.args))
		})
	}
}

func TestRunCommandDispatchModeAfterFlagParsing(t *testing.T) {
	cmd := newRunCommand()
	require.True(t, cmd.DisableFlagParsing)
	require.Equal(t, runDispatchPredict, runDispatchModeForArgs([]string{"r8.im/acme/model", "-i", "prompt=hello"}))
}

func TestCommandVisibilityForRunPredictAndExec(t *testing.T) {
	root, err := NewRootCommand()
	require.NoError(t, err)

	runCmd, _, err := root.Find([]string{"run"})
	require.NoError(t, err)
	require.Equal(t, "run [image]", runCmd.Use)
	require.False(t, runCmd.Hidden)
	require.Contains(t, runCmd.Example, "cog run -i prompt")
	publishFlag := runCmd.Flags().Lookup("publish")
	require.NotNil(t, publishFlag)
	require.True(t, publishFlag.Hidden)

	predictCmd, _, err := root.Find([]string{"predict"})
	require.NoError(t, err)
	require.True(t, predictCmd.Hidden)
	require.Contains(t, predictCmd.Short, "deprecated")

	execCmd, _, err := root.Find([]string{"exec"})
	require.NoError(t, err)
	require.NotContains(t, execCmd.Aliases, "run")
}
