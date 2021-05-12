package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/client"
	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/settings"
	"github.com/replicate/cog/pkg/util/console"
	"github.com/replicate/cog/pkg/util/files"
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
	if projectSettings.Repo == nil {
		return fmt.Errorf("Repository not set. Run 'cog repo set <repo>' to set it for this directory.")
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
	exists, err := files.Exists(filepath.Join(cwd, global.ConfigFilename))
	if !exists {
		console.Warnf("%s does not exist in %s. Are you in the right directory?", global.ConfigFilename, cwd)
	}

	cli := client.NewClient()
	if err := cli.CheckRead(repo); err != nil {
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
