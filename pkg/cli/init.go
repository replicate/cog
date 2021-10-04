package cli

import (
	_ "embed"
	"fmt"
	"io/ioutil"
	"os"
	"path"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/util/console"
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
		RunE:       initialize,
		Args:       cobra.MaximumNArgs(0),
	}

	return cmd
}

func initialize(cmd *cobra.Command, args []string) error {
	console.Infof("Setting up the current directory for use with Cog...")

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("Unable to get current working directory: %w", err)
	}

	// Create cog.yaml
	cogYamlPath := path.Join(cwd, "cog.yaml")
	err = ioutil.WriteFile(cogYamlPath, []byte(cogYamlContent), 0644)
	if err != nil {
		return fmt.Errorf("Error writing %s: %w", cogYamlPath, err)
	}
	console.Infof("Wrote %s", cogYamlPath)

	// Create predict.py
	predictPyPath := path.Join(cwd, "predict.py")
	err = ioutil.WriteFile(predictPyPath, []byte(predictPyContent), 0644)
	if err != nil {
		return fmt.Errorf("Error writing %s: %w", predictPyPath, err)
	}
	console.Infof("Wrote %s", predictPyPath)

	return nil
}
