package cli

import (
	"fmt"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/settings"
)

func NewRootCommand() (*cobra.Command, error) {
	rootCmd := cobra.Command{
		Use:     "cog",
		Short:   "",
		Version: fmt.Sprintf("%s (built %s)", global.Version, global.BuildTime),
		// This stops errors being printed because we print them in cmd/keepsake/main.go
	}
	setPersistentFlags(&rootCmd)

	rootCmd.AddCommand(
		newBuildCommand(),
		newDebugCommand(),
		newInferCommand(),
		newServerCommand(),
		newListCommand(),
		newShowCommand(),
		newDeleteCommand(),
		newRemoteCommand(),
		newDownloadCommand(),
	)

	log.SetLevel(log.DebugLevel)

	return &rootCmd, nil
}

func setPersistentFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().BoolVarP(&global.Verbose, "verbose", "v", false, "Verbose output")

}

func remoteHost() string {
	userSettings, err := settings.LoadUserSettings()
	if err != nil {
		log.Fatalf("Failed to load user settings: %w", err)
	}
	return userSettings.Remote
}
