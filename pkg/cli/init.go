package cli

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/util/console"
)

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

var cogYamlContent = `# Configuration for Cog 🤖 🐍 ⚙️
# Reference: https://github.com/replicate/cog/blob/main/docs/yaml.md

predict: "predict.py:Predictor"
build:
  python_version: "3.8"`

var predictPyContent = `# Prediction interface for Cog 🤖 🐍 ⚙️
# Reference: https://github.com/replicate/cog/blob/main/docs/python.md
	
import cog

class Predictor(cog.Predictor):
    def setup(self):
      # this function is only run once for setup
      # use it to load model weights, do extra setup, etc

    @cog.input("image", type=cog.Path, help="Grayscale input image")
    def predict(self, image):
        # ... pre-processing ...
        output = self.model(processed_image)
        # ... post-processing ...
        return processed_output`

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
