package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/util/console"
)

func newTestParser(t *testing.T) *kong.Kong {
	t.Helper()
	parser, err := newParser(t.Context(), &CLI{})
	require.NoError(t, err)
	return parser
}

type kongGlobalState struct {
	debug            bool
	noColor          bool
	profilingEnabled bool
	registry         string
	consoleColor     bool
	consoleLevel     console.Level
	noColorEnv       string
	hadNoColorEnv    bool
}

func snapshotKongGlobalState() kongGlobalState {
	noColorEnv, hadNoColorEnv := os.LookupEnv("NO_COLOR")
	return kongGlobalState{
		debug:            global.Debug,
		noColor:          global.NoColor,
		profilingEnabled: global.ProfilingEnabled,
		registry:         global.ReplicateRegistryHost,
		consoleColor:     console.ConsoleInstance.Color,
		consoleLevel:     console.ConsoleInstance.Level,
		noColorEnv:       noColorEnv,
		hadNoColorEnv:    hadNoColorEnv,
	}
}

func restoreKongGlobalState(t *testing.T, state kongGlobalState) {
	t.Helper()
	global.Debug = state.debug
	global.NoColor = state.noColor
	global.ProfilingEnabled = state.profilingEnabled
	global.ReplicateRegistryHost = state.registry
	console.SetColor(state.consoleColor)
	console.SetLevel(state.consoleLevel)
	if state.hadNoColorEnv {
		require.NoError(t, os.Setenv("NO_COLOR", state.noColorEnv))
	} else {
		require.NoError(t, os.Unsetenv("NO_COLOR"))
	}
}

func preserveKongGlobalState(t *testing.T) kongGlobalState {
	t.Helper()
	state := snapshotKongGlobalState()
	t.Cleanup(func() {
		restoreKongGlobalState(t, state)
	})
	return state
}

type testExitCode int

func newVersionTestParser(t *testing.T, stdout *bytes.Buffer) *kong.Kong {
	t.Helper()
	parser, err := newParser(t.Context(), &CLI{}, kong.Exit(func(code int) {
		panic(testExitCode(code))
	}))
	require.NoError(t, err)
	parser.Stdout = stdout
	parser.Stderr = stdout
	return parser
}

func TestKongRegistersAllTopLevelCommands(t *testing.T) {
	parser := newTestParser(t)
	commands := map[string]bool{}
	for _, node := range parser.Model.Children {
		commands[node.Name] = true
	}

	for _, name := range []string{
		"base-image",
		"build",
		"debug",
		"doctor",
		"exec",
		"init",
		"login",
		"predict",
		"push",
		"run",
		"serve",
		"train",
		"weights",
	} {
		require.Truef(t, commands[name], "missing command %q", name)
	}
}

func TestKongRegistersNestedCommands(t *testing.T) {
	preserveKongGlobalState(t)
	parser := newTestParser(t)

	for _, args := range [][]string{
		{"weights", "import", "--help"},
		{"weights", "pull", "--help"},
		{"weights", "status", "--help"},
		{"base-image", "dockerfile", "--help"},
		{"base-image", "build", "--help"},
	} {
		_, err := parser.Parse(args)
		require.NoErrorf(t, err, "parse %v", args)
	}
}

func TestKongRootHelpParses(t *testing.T) {
	preserveKongGlobalState(t)
	parser := newTestParser(t)
	var stdout bytes.Buffer
	parser.Stdout = &stdout
	parser.Stderr = &stdout

	kctx, err := parser.Parse([]string{"--help"})
	if err != nil {
		var parseErr *kong.ParseError
		require.True(t, errors.As(err, &parseErr), "expected ParseError, got %T", err)
		require.True(t, strings.HasPrefix(parseErr.Error(), "expected"), "expected command selection error, got %q", parseErr.Error())
		kctx = parseErr.Context
	}
	require.NoError(t, kctx.PrintUsage(false))

	help := stdout.String()
	require.Contains(t, help, "Usage: cog <command> [flags]")
	require.Contains(t, help, "build")
	require.Contains(t, help, "push")
	require.Contains(t, help, "weights")
	require.NotContains(t, help, "Usage: cog default")
}

func TestKongRootGlobalFlagsParse(t *testing.T) {
	state := preserveKongGlobalState(t)

	for _, args := range [][]string{
		{"--debug", "build", "--help"},
		{"--no-color", "build", "--help"},
		{"--profile", "build", "--help"},
		{"--registry", "example.com", "build", "--help"},
	} {
		restoreKongGlobalState(t, state)
		parser := newTestParser(t)
		_, err := parser.Parse(args)
		require.NoErrorf(t, err, "parse %v", args)
	}

	restoreKongGlobalState(t, state)
	var stdout bytes.Buffer
	versionParser := newVersionTestParser(t, &stdout)
	require.PanicsWithValue(t, testExitCode(0), func() {
		_, _ = versionParser.Parse([]string{"--version"})
	})
}

func TestKongHelpParsingDoesNotWriteUpdateState(t *testing.T) {
	preserveKongGlobalState(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("COG_NO_UPDATE_CHECK", "")

	for _, args := range [][]string{
		{"--help"},
		{"build", "--help"},
	} {
		parser := newTestParser(t)
		_, err := parser.Parse(args)
		if err != nil {
			var parseErr *kong.ParseError
			require.True(t, errors.As(err, &parseErr), "expected ParseError, got %T", err)
			require.True(t, strings.HasPrefix(parseErr.Error(), "expected"), "expected command selection error, got %q", parseErr.Error())
		}
		require.NoFileExists(t, filepath.Join(home, ".config", "cog", "update-state.json"), "parse %v", args)
	}
}

func TestKongNoColorAfterApplySetsGlobalNoColor(t *testing.T) {
	preserveKongGlobalState(t)
	require.NoError(t, os.Unsetenv("NO_COLOR"))
	global.NoColor = false

	globals := Globals{NoColor: true, Registry: global.ReplicateRegistryHost}
	require.NoError(t, globals.AfterApply())

	require.True(t, global.NoColor)
	require.Equal(t, "1", os.Getenv("NO_COLOR"))
}

func TestKongVersionExitsBeforeRootUsage(t *testing.T) {
	var stdout bytes.Buffer
	parser := newVersionTestParser(t, &stdout)

	require.PanicsWithValue(t, testExitCode(0), func() {
		_, _ = parser.Parse([]string{"--version"})
	})
	require.Equal(t, "cog version dev (built none)\n", stdout.String())
}

func TestKongCommandVersionExitsBeforeCommandRun(t *testing.T) {
	var stdout bytes.Buffer
	parser := newVersionTestParser(t, &stdout)

	require.PanicsWithValue(t, testExitCode(0), func() {
		_, _ = parser.Parse([]string{"build", "--version"})
	})
	require.Equal(t, "cog version dev (built none)\n", stdout.String())
}
