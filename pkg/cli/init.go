package cli

import (
	// blank import for embeds
	_ "embed"
	"fmt"
	"os"
	"path"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/util/console"
	"github.com/replicate/cog/pkg/util/files"
)

//go:embed init-templates/cog.yaml
var cogYamlContent []byte

//go:embed init-templates/predict.py
var predictPyContent []byte

func newInitCommand() *cobra.Command {
	var cmd = &cobra.Command{
		Use:        "init",
		SuggestFor: []string{"new", "start"},
		Short:      "Configure your project for use with Cog",
		RunE: func(cmd *cobra.Command, args []string) error {
			return initCommand(args)
		},
		Args: cobra.MaximumNArgs(0),
	}

	return cmd
}

func initCommand(args []string) error {
	console.Infof("\nSetting up the current directory for use with Cog...\n")

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	// cog.yaml
	cogYamlPath := path.Join(cwd, "cog.yaml")

	cogYamlPathExists, err := files.Exists(cogYamlPath)
	if err != nil {
		return err
	}

	if cogYamlPathExists {
		return fmt.Errorf("Found an existing cog.yaml.\nExiting without overwriting (to be on the safe side!)")
	}

	err = os.WriteFile(cogYamlPath, cogYamlContent, 0o644)
	if err != nil {
		return fmt.Errorf("Error writing %s: %w", cogYamlPath, err)
	}
	console.Infof("✅ Created %s", cogYamlPath)

	// predict.py
	predictPyPath := path.Join(cwd, "predict.py")

	predictPyPathExists, err := files.Exists(predictPyPath)
	if err != nil {
		return err
	}

	if predictPyPathExists {
		return fmt.Errorf("Found an existing predict.py.\nExiting without overwriting (to be on the safe side!)")
	}

	err = os.WriteFile(predictPyPath, predictPyContent, 0o644)
	if err != nil {
		return fmt.Errorf("Error writing %s: %w", predictPyPath, err)
	}
	console.Infof("✅ Created %s", predictPyPath)

	console.Infof("\nDone! For next steps, check out the docs at https://cog.run/docs/getting-started")

	return nil
}
