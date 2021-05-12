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

func newModelCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "model",
		Short: "Manage model used in this directory",
		RunE:  modelShow,
		Args:  cobra.NoArgs,
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "set",
		Short: "Set model used in this directory",
		RunE:  modelSet,
		Args:  cobra.ExactArgs(1),
	})

	return cmd
}

func modelShow(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	projectSettings, err := settings.LoadProjectSettings(cwd)
	if err != nil {
		return err
	}
	if projectSettings.Model == nil {
		return fmt.Errorf("Model not set. Run 'cog model set <model>' to set it for this directory.")
	}
	fmt.Println(projectSettings.Model.String())
	return nil
}

func modelSet(cmd *cobra.Command, args []string) error {
	model, err := parseModel(args[0])
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
	if err := cli.CheckRead(model); err != nil {
		return err
	}

	userSettings, err := settings.LoadProjectSettings(cwd)
	if err != nil {
		return err
	}
	userSettings.Model = model
	if err := userSettings.Save(); err != nil {
		return fmt.Errorf("Failed to save settings: %w", err)
	}

	fmt.Printf("Updated model: %s\n", model)
	return nil
}
