package cli

import (
	"github.com/spf13/cobra"

	"github.com/replicate/modelserver/pkg/global"
)

func NewRootCommand() (*cobra.Command, error) {
	rootCmd := cobra.Command{
		Use:   "modelserver",
		Short: "The Replicate model server",
		Version: global.Version,
		// This stops errors being printed because we print them in cmd/keepsake/main.go
	}
	setPersistentFlags(&rootCmd)

	rootCmd.AddCommand(
		newServerCommand(),
	)

	return &rootCmd, nil
}

func setPersistentFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().BoolVarP(&global.Verbose, "verbose", "v", false, "Verbose output")

}
