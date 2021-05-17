package cli

import (
	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/client"
	"github.com/replicate/cog/pkg/logger"
	"github.com/replicate/cog/pkg/util/console"
)

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

	cmd.Flags().BoolP("follow", "f", false, "Follow streaming logs")
	// TODO(andreas): tail

	return cmd
}

func showBuildLogs(cmd *cobra.Command, args []string) error {
	buildID := args[0]
	follow, err := cmd.Flags().GetBool("follow")
	if err != nil {
		return err
	}

	model, err := getModel()
	if err != nil {
		return err
	}

	c := client.NewClient()
	logChan, err := c.GetBuildLogs(model, buildID, follow)
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
