package cli

import (
	"fmt"
	"os"
	"path/filepath"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/client"
	"github.com/replicate/cog/pkg/files"
	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/settings"
)

func newRepoCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repo",
		Short: "Manage remote repository",
		RunE:  showRepo,
		Args:  cobra.NoArgs,
	}

	cmd.AddCommand(newSetRepoCommand())

	return cmd
}

func newSetRepoCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set",
		Short: "Set remote repository",
		RunE:  setRepo,
		Args:  cobra.ExactArgs(1),
	}

	return cmd
}

func showRepo(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	projectSettings, err := settings.LoadProjectSettings(cwd)
	if err != nil {
		return err
	}
	fmt.Println(projectSettings.Repo.String())
	return nil
}

func setRepo(cmd *cobra.Command, args []string) error {
	repo, err := parseRepo(args[0])
	if err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	exists, err := files.FileExists(filepath.Join(cwd, global.ConfigFilename))
	if !exists {
		log.Warnf("%s does not exist in %s. Are you in the right directory?", global.ConfigFilename, cwd)
	}

	cli := client.NewClient()
	if err := cli.Ping(repo.Host); err != nil {
		return err
	}

	userSettings, err := settings.LoadProjectSettings(cwd)
	if err != nil {
		return err
	}
	userSettings.Repo = repo
	if err := userSettings.Save(); err != nil {
		return fmt.Errorf("Failed to save settings: %w", err)
	}

	fmt.Printf("Updated repo: %s\n", repo)
	return nil
}
