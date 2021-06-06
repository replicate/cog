package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/client"
	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/logger"
	"github.com/replicate/cog/pkg/util/terminal"
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

	ui := terminal.ConsoleUI(context.Background())
	defer ui.Close()

	logChan, err := c.GetBuildLogs(model, buildID, !buildNoFollow)
	if err != nil {
		return err
	}

	if global.Verbose {
		for entry := range logChan {
			fmt.Println(entry.Line)
		}
	} else {
		logWriter := logger.NewTerminalLogger(ui)
		pipeLogChanToLogger(logChan, logWriter)
	}

	return nil
}

// FIXME(bfirsh):
func pipeLogChanToLogger(logChan chan *client.LogEntry, logWriter logger.Logger) {
	for entry := range logChan {
		switch entry.Level {
		case logger.LevelFatal:
			logWriter.WriteError(fmt.Errorf(entry.Line))
		case logger.LevelError:
			logWriter.WriteError(fmt.Errorf(entry.Line))
		case logger.LevelWarn:
			logWriter.WriteError(fmt.Errorf(entry.Line))
		case logger.LevelStatus:
			logWriter.Info(entry.Line)
		case logger.LevelInfo:
			logWriter.Info(entry.Line)
		case logger.LevelDebug:
			logWriter.Debug(entry.Line)
		}
	}

}
