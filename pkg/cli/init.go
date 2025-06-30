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

//go:embed init-templates/requirements.txt
var requirementsTxtContent []byte

var (
	skipExisting      bool
	overwriteExisting bool
)

func newInitCommand() *cobra.Command {
	var cmd = &cobra.Command{
		Use:        "init",
		SuggestFor: []string{"new", "start"},
		Short:      "Configure your project for use with Cog",
		RunE:       initCommand,
		Args:       cobra.MaximumNArgs(0),
	}

	cmd.Flags().BoolVarP(&skipExisting, "skip-existing", "s", false, "Skip all existing files without prompting")
	cmd.Flags().BoolVarP(&overwriteExisting, "overwrite-existing", "o", false, "Overwrite all existing files without prompting")

	return cmd
}

func initCommand(cmd *cobra.Command, args []string) error {
	console.Infof("\nSetting up the current directory for use with Cog...\n")

	if skipExisting && overwriteExisting {
		return fmt.Errorf("Cannot specify both --skip-existing and --overwrite-existing")
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	fileContentMap := map[string][]byte{
		"cog.yaml":                    cogYamlContent,
		"predict.py":                  predictPyContent,
		".dockerignore":               dockerignoreContent,
		".github/workflows/push.yaml": actionsWorkflowContent,
		"requirements.txt":            requirementsTxtContent,
	}

	var globalChoice string

	for filename, content := range fileContentMap {
		filePath := path.Join(cwd, filename)
		fileExists, err := files.Exists(filePath)
		if err != nil {
			return err
		}

		shouldWrite := true
		if fileExists {
			if skipExisting {
				console.Infof("Skipped existing %s", filename)
				continue
			}
			if overwriteExisting {
				console.Infof("Overwriting existing %s", filename)
			} else {
				// Check if we have a global choice from previous prompts
				if globalChoice == "S" {
					console.Infof("Skipped existing %s", filename)
					continue
				}
				// Prompt for this specific file
				choice, err := console.Interactive{
					Prompt:  fmt.Sprintf("File %s already exists. What would you like to do?", filename),
					Options: []string{"skip", "skip-all", "quit"},
					Default: "skip",
				}.Read()
				if err != nil {
					return err
				}

				switch choice {
				case "skip":
					console.Infof("Skipped existing %s", filename)
					continue
				case "skip-all":
					globalChoice = "S"
					console.Infof("Skipped existing %s (and will skip all remaining)", filename)
					continue
				case "quit":
					console.Infof("Exiting without making changes")
					return nil
				}
			}
		}

		if shouldWrite {
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
	}

	console.Infof("\nDone! For next steps, check out the docs at https://cog.run/getting-started")

	return nil
}
