package cli

import (
	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/util/console"
	"github.com/spf13/cobra"
)

func newValidateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "validate",
		Hidden: true,
		RunE:   validateCogYamlFile,
	}

	validate := &cobra.Command{
		Use:    "validate",
		Short:  "Validates cog.yaml file in the current directory",
		Hidden: true,
	}

	cmd.AddCommand(validate)

	return cmd
}

func validateCogYamlFile(cmd *cobra.Command, args []string) error {
	config, _, err := config.GetConfig(projectDirFlag)
	if err != nil {
		return err
	}
	err = config.ValidateAndCompleteConfig()
	if err != nil {
		return err
	}
	console.Output("Valid cog.yaml file")
	return nil
}
