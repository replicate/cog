package cli

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/util/console"
)

type runDispatchMode int

const (
	runDispatchPredict runDispatchMode = iota
	runDispatchExec
)

func runDispatchModeForArgs(args []string) runDispatchMode {
	remaining, hasUnknownFlag := runArgsAfterPredictionFlags(args)
	if hasUnknownFlag {
		return runDispatchExec
	}
	if len(remaining) == 1 && isLikelyRunCommand(remaining[0]) {
		return runDispatchExec
	}
	if len(remaining) <= 1 {
		return runDispatchPredict
	}
	return runDispatchExec
}

func newRunCommand() *cobra.Command {
	cmd := newPredictionCommand("run", false)
	cmd.DisableFlagParsing = true
	cmd.Args = cobra.ArbitraryArgs
	cmd.RunE = cmdRun
	cmd.PreRunE = checkMutuallyExclusiveFlags
	cmd.Flags().StringArrayVarP(&execPorts, "publish", "p", []string{}, "Publish a container's port to the host, e.g. -p 8000")
	_ = cmd.Flags().MarkHidden("publish")
	return cmd
}

func cmdRun(cmd *cobra.Command, args []string) error {
	mode := runDispatchModeForArgs(args)
	if mode == runDispatchPredict && runArgsContainHelp(args) {
		return cmd.Help()
	}
	if mode == runDispatchExec {
		cmd.Flags().SetInterspersed(false)
		if err := cmd.Flags().Parse(args); err != nil {
			return err
		}
		if err := checkMutuallyExclusiveFlags(cmd, cmd.Flags().Args()); err != nil {
			return err
		}
		if len(cmd.Flags().Args()) == 0 {
			return cobra.MinimumNArgs(1)(cmd, cmd.Flags().Args())
		}
		console.Warn(`"cog run <command>" is deprecated, use "cog exec <command>"`)
		return execCmd(cmd, cmd.Flags().Args())
	}
	cmd.Flags().SetInterspersed(true)
	if err := cmd.Flags().Parse(args); err != nil {
		return err
	}
	if err := checkMutuallyExclusiveFlags(cmd, cmd.Flags().Args()); err != nil {
		return err
	}
	return cmdPredict(cmd, cmd.Flags().Args())
}

func isLikelyRunCommand(arg string) bool {
	switch arg {
	case "bash", "sh", "zsh", "python", "python3", "ipython", "jupyter", "pip", "pip3", "uv":
		return true
	default:
		return false
	}
}

func runArgsContainHelp(args []string) bool {
	for _, arg := range args {
		if arg == "--help" || arg == "-h" {
			return true
		}
	}
	return false
}

func runArgsAfterPredictionFlags(args []string) ([]string, bool) {
	remaining := []string{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") {
			remaining = append(remaining, arg)
			continue
		}
		if (arg == "--help" || arg == "-h") && len(remaining) > 0 {
			return remaining, true
		}
		if !isRunPredictionFlag(arg) {
			return remaining, true
		}
		if runPredictionFlagTakesValue(arg) && !strings.Contains(arg, "=") && i+1 < len(args) {
			i++
		}
	}
	return remaining, false
}

func isRunPredictionFlag(arg string) bool {
	name := strings.TrimLeft(arg, "-")
	if before, _, ok := strings.Cut(name, "="); ok {
		name = before
	}
	switch name {
	case "h", "help", "i", "input", "o", "output", "e", "env", "use-replicate-token", "json",
		"use-cuda-base-image", "use-cog-base-image", "progress", "dockerfile", "gpus",
		"setup-timeout", "f", "file":
		return true
	default:
		return false
	}
}

func runPredictionFlagTakesValue(arg string) bool {
	name := strings.TrimLeft(arg, "-")
	if before, _, ok := strings.Cut(name, "="); ok {
		name = before
	}
	switch name {
	case "i", "input", "o", "output", "e", "env", "json", "progress", "dockerfile",
		"gpus", "setup-timeout", "f", "file", "use-cuda-base-image":
		return true
	default:
		return false
	}
}
