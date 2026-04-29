package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/update"
	"github.com/replicate/cog/pkg/util/console"
)

func NewRootCommand() (*cobra.Command, error) {
	rootCmd := cobra.Command{
		Use:   "cog",
		Short: "Cog: Containers for machine learning",
		Long: `Containers for machine learning.

To get started, take a look at the documentation:
https://github.com/replicate/cog`,
		Example: `   To execute a command inside a Docker environment defined with Cog:
      $ cog exec echo hello world`,
		Version: fmt.Sprintf("%s (built %s)", global.Version, global.BuildTime),
		// This stops errors being printed because we print them in cmd/cog/cog.go
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if global.Debug {
				console.SetLevel(console.DebugLevel)
			}
			if global.NoColor || !console.ShouldUseColor() {
				console.SetColor(false)
			}
			if global.NoColor {
				os.Setenv("NO_COLOR", "1") //nolint:errcheck,gosec // best-effort
			}
			cmd.SilenceUsage = true
			if err := update.DisplayAndCheckForRelease(cmd.Context()); err != nil {
				console.Debugf("%s", err)
			}
		},
		SilenceErrors: true,
	}
	setPersistentFlags(&rootCmd)

	rootCmd.AddCommand(
		newBuildCommand(),
		newDebugCommand(),
		newDoctorCommand(),
		newInitCommand(),
		newLoginCommand(),
		newPredictCommand(),
		newPushCommand(),
		newExecCommand(),
		newServeCommand(),
		newTrainCommand(),
		newWeightsCommand(),
	)

	return &rootCmd, nil
}

func setPersistentFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().BoolVar(&global.Debug, "debug", false, "Show debugging output")
	cmd.PersistentFlags().BoolVar(&global.NoColor, "no-color", false, "Disable colored output")
	cmd.PersistentFlags().BoolVar(&global.ProfilingEnabled, "profile", false, "Enable profiling")
	cmd.PersistentFlags().Bool("version", false, "Show version of Cog")
	cmd.PersistentFlags().StringVar(&global.ReplicateRegistryHost, "registry", global.ReplicateRegistryHost, "Registry host")
	_ = cmd.PersistentFlags().MarkHidden("profile")
	_ = cmd.PersistentFlags().MarkHidden("registry")
}
