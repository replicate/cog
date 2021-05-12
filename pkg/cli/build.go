package cli

import (
	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/client"
	"github.com/replicate/cog/pkg/console"
	"github.com/replicate/cog/pkg/logger"
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

	addRepoFlag(cmd)

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

	repo, err := getRepo()
	if err != nil {
		return err
	}

	c := client.NewClient()
	logChan, err := c.GetBuildLogs(repo, buildID, follow)
	if err != nil {
		return err
	}
	for entry := range logChan {
		switch entry.Level {
		case logger.LevelFatal:
			console.Fatal(entry.Line)
		case logger.LevelError:
			console.Error(entry.Line)
		case logger.LevelWarn:
			console.Warn(entry.Line)
		case logger.LevelStatus: // TODO(andreas): handle status differently or remove
			console.Info(entry.Line)
		case logger.LevelInfo:
			console.Info(entry.Line)
		case logger.LevelDebug:
			console.Debug(entry.Line)
		}
	}

	return nil
}
