package cli

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"regexp"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/settings"
	"github.com/replicate/cog/pkg/util/console"
	"github.com/replicate/cog/pkg/util/files"
)

var modelFlag string
var projectDirFlag string

var modelRegex = regexp.MustCompile("^(?:(https?://[^/]*)/)?(?:([-_a-zA-Z0-9]+)/)([-_a-zA-Z0-9]+)$")

func NewRootCommand() (*cobra.Command, error) {
	rootCmd := cobra.Command{
		Use:     "cog",
		Short:   "",
		Version: fmt.Sprintf("%s (built %s)", global.Version, global.BuildTime),
		// This stops errors being printed because we print them in cmd/keepsake/main.go
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if global.Verbose {
				console.SetLevel(console.DebugLevel)
			}
			cmd.SilenceUsage = true
		},
		SilenceErrors: true,
	}
	setPersistentFlags(&rootCmd)

	rootCmd.AddCommand(
		newBuildCommand(),
		newPushCommand(),
		newTestCommand(),
		newDebugCommand(),
		newPredictCommand(),
		newRunCommand(),
		newServerCommand(),
		newShowCommand(),
		newModelCommand(),
		newDownloadCommand(),
		newListCommand(),
		newDeleteCommand(),
		newLoginCommand(),
	)

	return &rootCmd, nil
}

func setPersistentFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().BoolVarP(&global.Verbose, "verbose", "v", false, "Verbose output")
	cmd.PersistentFlags().BoolVar(&global.ProfilingEnabled, "profile", false, "Enable profiling")
	_ = cmd.PersistentFlags().MarkHidden("profile")
}

func addModelFlag(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&modelFlag, "model", "m", "", "Model URL, e.g. https://cog.hooli.corp/hotdog-detector/hotdog-detector")
}

func getModel() (*model.Model, error) {
	if modelFlag != "" {
		model, err := parseModel(modelFlag)
		if err != nil {
			return nil, err
		}
		return model, nil
	} else {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		projectSettings, err := settings.LoadProjectSettings(cwd)
		if err != nil {
			return nil, err
		}
		if projectSettings.Model != nil {
			return projectSettings.Model, nil
		}
		return nil, fmt.Errorf("No model specified. You need to either run `cog model set <url>` to set a model for the current directory, or pass --model <url> to the command")
	}
}

func parseModel(modelString string) (*model.Model, error) {
	matches := modelRegex.FindStringSubmatch(modelString)
	if len(matches) == 0 {
		return nil, fmt.Errorf("Model '%s' doesn't match [http[s]://<host>/]<user>/<name>", modelString)
	}
	return &model.Model{
		Host: matches[1],
		User: matches[2],
		Name: matches[3],
	}, nil
}

func addProjectDirFlag(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&projectDirFlag, "project-dir", "D", "", "Project directory, defaults to current working directory")
}

func getConfig() (*model.Config, string, error) {
	projectDir, err := os.Getwd()
	if err != nil {
		return nil, "", err
	}

	configPath := path.Join(projectDir, global.ConfigFilename)

	exists, err := files.Exists(configPath)
	if err != nil {
		return nil, "", err
	}
	if !exists {
		return nil, "", fmt.Errorf("%s does not exist in %s. Are you in the right directory?", global.ConfigFilename, projectDir)
	}

	contents, err := ioutil.ReadFile(configPath)
	if err != nil {
		return nil, "", err
	}

	config, err := model.ConfigFromYAML(contents)
	if err != nil {
		return nil, "", err
	}
	err = config.ValidateAndCompleteConfig()
	return config, projectDir, err
}
