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

//go:embed init-templates/.dockerignore
var dockerignoreContent []byte

//go:embed init-templates/cog.yaml
var cogYamlContent []byte

//go:embed init-templates/predict.py
var predictPyContent []byte

//go:embed init-templates/.github/workflows/push.yaml
var actionsWorkflowContent []byte

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

	fileContentMap := map[string][]byte{
		"cog.yaml":                    cogYamlContent,
		"predict.py":                  predictPyContent,
		".dockerignore":               dockerignoreContent,
		".github/workflows/push.yaml": actionsWorkflowContent,
	}

	for filename, content := range fileContentMap {
		filePath := path.Join(cwd, filename)
		fileExists, err := files.Exists(filePath)
		if err != nil {
			return err
		}

		if fileExists {
			return fmt.Errorf("Found an existing %s.\nExiting without overwriting (to be on the safe side!)", filename)
		}

		dirPath := path.Dir(filePath)
		err = os.MkdirAll(dirPath, os.ModePerm)
		if err != nil {
			return fmt.Errorf("Error creating directory %s: %w", dirPath, err)
		}

		err = os.WriteFile(filePath, content, 0o644)
		if err != nil {
			return fmt.Errorf("Error writing %s: %w", filePath, err)
		}
		console.Infof("✅ Created %s", filePath)
	}

	console.Infof("\nDone! For next steps, check out the docs at https://cog.run/getting-started")

	return nil
}
