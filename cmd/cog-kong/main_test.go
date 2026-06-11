package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/cli"
	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/util/console"
)

func newTestParser(t *testing.T) *kong.Kong {
	t.Helper()
	// Override Exit so Kong's built-in --help flag (which exits the process
	// after printing help, before validation) doesn't kill the test process.
	parser, err := newParser(t.Context(), &CLI{}, kong.Exit(func(code int) {
		panic(testExitCode(code))
	}))
	require.NoError(t, err)
	parser.Stdout = io.Discard
	parser.Stderr = io.Discard
	return parser
}

// parseForTest parses args, tolerating Kong's built-in --help short-circuit,
// which prints help and calls Exit(0) (translated to a testExitCode(0) panic by
// newTestParser). It returns the parse error for non-help cases.
func parseForTest(t *testing.T, parser *kong.Kong, args []string) (err error) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			code, ok := r.(testExitCode)
			require.Truef(t, ok, "unexpected panic parsing %v: %v", args, r)
			require.Equalf(t, testExitCode(0), code, "non-zero exit parsing %v", args)
		}
	}()
	_, err = parser.Parse(args)
	return err
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
		require.NoErrorf(t, parseForTest(t, parser, args), "parse %v", args)
	}
}

func TestKongRootHelpParses(t *testing.T) {
	preserveKongGlobalState(t)
	// Override Exit so the built-in --help flag doesn't terminate the process,
	// and capture stdout to assert on the rendered help output.
	var stdout bytes.Buffer
	parser, err := newParser(t.Context(), &CLI{}, kong.Exit(func(code int) {
		panic(testExitCode(code))
	}))
	require.NoError(t, err)
	parser.Stdout = &stdout
	parser.Stderr = &stdout

	require.PanicsWithValue(t, testExitCode(0), func() {
		_, _ = parser.Parse([]string{"--help"})
	})

	help := stdout.String()
	require.Contains(t, help, "Usage: cog <command> [flags]")
	require.Contains(t, help, "build")
	require.Contains(t, help, "push")
	require.NotContains(t, help, "Usage: cog default")

	// Commands hidden in Cobra must also be hidden in the Kong model so they
	// don't appear in the root help output.
	hiddenByName := map[string]bool{}
	for _, node := range parser.Model.Children {
		if node.Hidden {
			hiddenByName[node.Name] = true
		}
	}
	for _, name := range []string{"predict", "train", "weights", "debug"} {
		require.Truef(t, hiddenByName[name], "command %q should be hidden", name)
	}
	// Visible commands (notably run, the non-hidden twin of predict) must NOT be
	// hidden — guards against accidentally hiding the wrong prediction command.
	for _, name := range []string{"run", "build", "push", "serve", "exec", "init", "login", "doctor", "base-image"} {
		require.Falsef(t, hiddenByName[name], "command %q should be visible", name)
	}
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
		require.NoErrorf(t, parseForTest(t, parser, args), "parse %v", args)
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
		_ = parseForTest(t, parser, args)
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

func TestKongRuntimeCommandFlagsParse(t *testing.T) {
	parser := newTestParser(t)

	for _, args := range [][]string{
		{"serve", "--port", "5000", "--upload-url", "https://example.com/upload", "--gpus", "all", "--help"},
		{"exec", "--gpus", "all", "--publish", "8888", "--env", "A=B", "python", "-c", "print(1)"},
	} {
		require.NoErrorf(t, parseForTest(t, parser, args), "parse %v", args)
	}
}

func TestKongSimpleCommandFlagsParse(t *testing.T) {
	parser := newTestParser(t)

	for _, args := range [][]string{
		{"init", "--help"},
		{"login", "--token-stdin", "--help"},
		{"doctor", "--fix", "--file", "custom.yaml", "--help"},
		{"debug", "--image-name", "myimage", "--help"},
	} {
		require.NoErrorf(t, parseForTest(t, parser, args), "parse %v", args)
	}
}

func TestKongPredictionCommandFlagsParse(t *testing.T) {
	parser := newTestParser(t)

	for _, args := range [][]string{
		{"predict", "example/image", "--input", "prompt=cat", "--help"},
		{"run", "example/image", "--input", "prompt=cat", "--output", "out.png", "--help"},
		{"run", "--json", "@inputs.json", "--gpus", "all", "--help"},
		{"train", "example/image", "--input", "dataset=@data.json", "--help"},
	} {
		require.NoErrorf(t, parseForTest(t, parser, args), "parse %v", args)
	}
}

func TestKongWeightsCommandFlagsParse(t *testing.T) {
	parser := newTestParser(t)

	for _, args := range [][]string{
		{"weights", "import", "--dry-run", "--verbose", "model.safetensors", "--help"},
		{"weights", "pull", "--verbose", "weights-name", "--help"},
		{"weights", "status", "--json", "--verbose", "--help"},
	} {
		require.NoErrorf(t, parseForTest(t, parser, args), "parse %v", args)
	}
}

func TestKongBaseImageCommandFlagsParse(t *testing.T) {
	parser := newTestParser(t)

	for _, args := range [][]string{
		{"base-image", "dockerfile", "--cuda", "12.4", "--python", "3.12", "--torch", "2.5.0", "--no-cache", "--progress", "plain", "--help"},
		{"base-image", "build", "--cuda", "12.4", "--python", "3.12", "--torch", "2.5.0", "--help"},
		{"base-image", "generate-matrix", "--cuda", "12.4", "--python", "3.12", "--help"},
	} {
		require.NoErrorf(t, parseForTest(t, parser, args), "parse %v", args)
	}
}

// TestKongBuildOnlyFlagsParity asserts that --timestamp and
// --skip-schema-validation are build-only (not on push), matching Cobra, and
// that build maps them through to the shared options.
func TestKongBuildOnlyFlagsParity(t *testing.T) {
	preserveKongGlobalState(t)

	// build accepts the build-only flags and maps them to options.
	var buildCLI CLI
	buildParser, err := newParser(t.Context(), &buildCLI)
	require.NoError(t, err)
	_, err = buildParser.Parse([]string{"build", "--timestamp", "42", "--skip-schema-validation", "--tag", "img:latest"})
	require.NoError(t, err)
	opts := buildCLI.Build.options()
	require.Equal(t, int64(42), opts.Timestamp)
	require.True(t, opts.SkipSchemaValidation)

	// push does NOT expose either flag (parity with Cobra push).
	for _, flag := range []string{"--timestamp", "--skip-schema-validation"} {
		pushParser := newTestParser(t)
		_, err := pushParser.Parse([]string{"push", flag, "x", "img:latest"})
		require.Errorf(t, err, "push should reject %s", flag)
		require.Containsf(t, err.Error(), "unknown flag", "push should reject %s as unknown", flag)
	}

	// push still defaults Timestamp to -1 (timestamp rewriting disabled), not 0.
	require.Equal(t, int64(-1), (&BuildFlags{}).Options().Timestamp)
}

// TestKongEnvFlagParity asserts --env is available on exec/predict/run/train but
// NOT on serve, matching the Cobra CLI (Cobra serve has no --env).
func TestKongEnvFlagParity(t *testing.T) {
	// serve has no --env.
	serveParser := newTestParser(t)
	_, err := serveParser.Parse([]string{"serve", "--env", "A=B"})
	require.Error(t, err, "serve should reject --env")
	require.Contains(t, err.Error(), "unknown flag")

	// exec/predict/run/train accept --env and thread it through.
	var execCLI CLI
	execParser, err := newParser(t.Context(), &execCLI)
	require.NoError(t, err)
	_, err = execParser.Parse([]string{"exec", "--env", "A=B", "echo", "hi"})
	require.NoError(t, err)
	require.Equal(t, []string{"A=B"}, execCLI.Exec.Env)

	var runCLI CLI
	runParser, err := newParser(t.Context(), &runCLI)
	require.NoError(t, err)
	_, err = runParser.Parse([]string{"run", "--env", "A=B", "img"})
	require.NoError(t, err)
	require.Equal(t, []string{"A=B"}, runCLI.RunCommand.options("run").Env)
}

// TestKongExecRequiresCommand asserts the zero-arg exec error matches Cobra's
// MinimumNArgs(1) message.
func TestKongExecRequiresCommand(t *testing.T) {
	cmd := &ExecCmd{}
	err := cmd.Validate()
	require.Error(t, err)
	require.Equal(t, "requires at least 1 arg(s), only received 0", err.Error())
}

// TestKongCommandCoverageMatchesCobraSurface derives the expected command set
// and hidden status directly from the real Cobra command tree
// (cli.NewRootCommand and cli.NewBaseImageRootCommand) and asserts the Kong
// model matches it node-by-node. Deriving from the actual Cobra surface (rather
// than a hand-maintained list) means any future Cobra command/hidden-flag change
// that the Kong CLI doesn't mirror will fail this test.
func TestKongCommandCoverageMatchesCobraSurface(t *testing.T) {
	parser := newTestParser(t)

	// Kong top-level command name -> hidden.
	kongCmds := map[string]bool{}
	kongChildren := map[string]*kong.Node{}
	for _, node := range parser.Model.Children {
		if node.Type != kong.CommandNode {
			continue
		}
		kongCmds[node.Name] = node.Hidden
		kongChildren[node.Name] = node
	}

	// Expected top-level surface from the real Cobra root command.
	root, err := cli.NewRootCommand()
	require.NoError(t, err)
	expected := map[string]bool{}
	for _, c := range root.Commands() {
		expected[c.Name()] = c.Hidden
	}
	// Cobra ships base-image as a separate binary; Kong folds it under
	// `cog base-image`, so add it to the expected top-level surface.
	expected["base-image"] = false

	require.Len(t, kongCmds, len(expected), "top-level command count mismatch: kong=%v expected=%v", keys(kongCmds), keys(expected))
	for name, hidden := range expected {
		gotHidden, ok := kongCmds[name]
		require.Truef(t, ok, "Kong is missing command %q present in Cobra", name)
		require.Equalf(t, hidden, gotHidden, "hidden mismatch for command %q", name)
	}

	// base-image subcommands must match the Cobra base-image binary.
	bi, err := cli.NewBaseImageRootCommand()
	require.NoError(t, err)
	expectedBase := map[string]bool{}
	for _, c := range bi.Commands() {
		expectedBase[c.Name()] = c.Hidden
	}
	kongBase := map[string]bool{}
	require.NotNil(t, kongChildren["base-image"], "Kong missing base-image group")
	for _, node := range kongChildren["base-image"].Children {
		if node.Type != kong.CommandNode {
			continue
		}
		kongBase[node.Name] = node.Hidden
	}
	require.Equal(t, expectedBase, kongBase, "base-image subcommand surface mismatch")
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
