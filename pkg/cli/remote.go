package cli

import (
	"fmt"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"net/http"

	"github.com/replicate/cog/pkg/settings"
)

func newRemoteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remote",
		Short: "Manage remote build server",
		RunE:  showRemote,
		Args:  cobra.NoArgs,
	}

	cmd.AddCommand(newSetRemoteCommand())

	return cmd
}

func newSetRemoteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set",
		Short: "Set remote build server address",
		RunE:  setRemote,
		Args:  cobra.ExactArgs(1),
	}

	return cmd
}

func showRemote(cmd *cobra.Command, args []string) error {
	userSettings, err := settings.LoadUserSettings()
	if err != nil {
		return err
	}
	fmt.Println(userSettings.Remote)
	return nil
}

func setRemote(cmd *cobra.Command, args []string) error {
	remote := args[0]
	resp, err := http.Get(remote + "/ping")
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Request to %s/ping failed with status %d", remote, resp.StatusCode)
	}

	userSettings, err := settings.LoadUserSettings()
	if err != nil {
		return err
	}
	userSettings.Remote = args[0]
	if err := userSettings.Save(); err != nil {
		return fmt.Errorf("Failed to save settings: %w", err)
	}

	log.Infof("Updated remote: %s", remote)
	return nil
}
