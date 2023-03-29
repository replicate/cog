package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/update"
	"github.com/replicate/cog/pkg/util/console"
)

var projectDirFlag string

func NewRootCommand() (*cobra.Command, error) {
	rootCmd := cobra.Command{
		Use:   "cog",
		Short: "Cog: Containers for machine learning",
		Long: `Containers for machine learning.

To get started, take a look at the documentation:
https://github.com/replicate/cog`,
		Example: `   To run a command inside a Docker environment defined with Cog:
      $ cog run echo hello world`,
		Version: fmt.Sprintf("%s (built %s)", global.Version, global.BuildTime),
		// This stops errors being printed because we print them in cmd/cog/cog.go
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if global.Debug {
				console.SetLevel(console.DebugLevel)
			}
			cmd.SilenceUsage = true
			if err := update.DisplayAndCheckForRelease(); err != nil {
				console.Debugf("%s", err)
			}
		},
		SilenceErrors: true,
	}
	setPersistentFlags(&rootCmd)

	rootCmd.AddCommand(
		newBuildCommand(),
		newDebugCommand(),
		newInitCommand(),
		newLoginCommand(),
		newPredictCommand(),
		newPushCommand(),
		newRunCommand(),
		newTrainCommand(),
	)

	return &rootCmd, nil
}

func setPersistentFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().BoolVar(&global.Debug, "debug", false, "Show debugging output")
	cmd.PersistentFlags().BoolVar(&global.ProfilingEnabled, "profile", false, "Enable profiling")
	cmd.PersistentFlags().Bool("version", false, "Show version of Cog")
	_ = cmd.PersistentFlags().MarkHidden("profile")
}
