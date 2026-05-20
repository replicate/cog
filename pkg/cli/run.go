package cli

import "github.com/spf13/cobra"

func newRunCommand() *cobra.Command {
	return newPredictionCommand("run", false)
}
