package cli

import (
	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/client"
	"github.com/replicate/cog/pkg/logger"
	"github.com/replicate/cog/pkg/util/console"
)

var buildNoFollow bool

func newBuildCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "build",
		Short: "Manage image builds",
		Args:  cobra.NoArgs,
	}

	cmd.AddCommand(newBuildLogCommand())

	return cmd
}

func newBuildLogCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "log <BUILD_ID>",
		Short: "Display build logs",
		RunE:  showBuildLogs,
		Args:  cobra.ExactArgs(1),
	}

	addModelFlag(cmd)

	cmd.Flags().BoolVarP(&buildNoFollow, "no-follow", "", false, "Exit immediately instead of waiting until output finishes")
	// TODO(andreas): tail

	return cmd
}

func showBuildLogs(cmd *cobra.Command, args []string) error {
	buildID := args[0]

	model, err := getModel()
	if err != nil {
		return err
	}

	c := client.NewClient()
	// FIXME(bfirsh): why isn't this a logger.Logger?
	logChan, err := c.GetBuildLogs(model, buildID, !buildNoFollow)
	if err != nil {
		return err
	}
	for entry := range logChan {
		outputLogEntry(entry, "")
	}

	return nil
}

func outputLogEntry(entry *client.LogEntry, prefix string) {
	switch entry.Level {
	case logger.LevelFatal:
		console.Fatal(prefix + entry.Line)
	case logger.LevelError:
		console.Error(prefix + entry.Line)
	case logger.LevelWarn:
		console.Warn(prefix + entry.Line)
	case logger.LevelStatus: // TODO(andreas): handle status differently or remove
		console.Info(prefix + entry.Line)
	case logger.LevelInfo:
		console.Info(prefix + entry.Line)
	case logger.LevelDebug:
		console.Debug(prefix + entry.Line)
	}
}
