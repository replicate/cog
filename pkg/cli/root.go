package cli

import (
	"fmt"
	"os"
	"regexp"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/util/console"
	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/settings"
)

var repoFlag string
var projectDirFlag string

var repoRegex = regexp.MustCompile("^(?:(https?://[^/]*)/)?(?:([-_a-zA-Z0-9]+)/)([-_a-zA-Z0-9]+)$")

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
		newInferCommand(),
		newServerCommand(),
		newShowCommand(),
		newRepoCommand(),
		newDownloadCommand(),
		newListCommand(),
		newBenchmarkCommand(),
		newDeleteCommand(),
		newLoginCommand(),
	)

	return &rootCmd, nil
}

func setPersistentFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().BoolVarP(&global.Verbose, "verbose", "v", false, "Verbose output")
	cmd.PersistentFlags().BoolVar(&global.ProfilingEnabled, "profile", false, "Enable profiling")
	cmd.PersistentFlags().MarkHidden("profile")
}

func addRepoFlag(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&repoFlag, "repo", "r", "", "Remote repository, e.g. andreas/jazz-composer (for replicate.ai), or replicate.my-company.net/andreas/jazz-composer")
}

func getRepo() (*model.Repo, error) {
	if repoFlag != "" {
		repo, err := parseRepo(repoFlag)
		if err != nil {
			return nil, err
		}
		return repo, nil
	} else {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		projectSettings, err := settings.LoadProjectSettings(cwd)
		if projectSettings.Repo != nil {
			return projectSettings.Repo, nil
		}
		return nil, fmt.Errorf("No repository specified. You need to either run `cog repo set <repo>` to set a repo for the current directory, or pass --repo <repository> to the command")
	}
}

func parseRepo(repoString string) (*model.Repo, error) {
	matches := repoRegex.FindStringSubmatch(repoString)
	if len(matches) == 0 {
		return nil, fmt.Errorf("Repo '%s' doesn't match [http[s]://<host>/]<user>/<name>", repoString)
	}
	return &model.Repo{
		Host: matches[1],
		User: matches[2],
		Name: matches[3],
	}, nil
}

func addProjectDirFlag(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&projectDirFlag, "project-dir", "D", "", "Projet directory, defaults to current working directory")
}

func getProjectDir() (string, error) {
	if projectDirFlag == "" {
		return os.Getwd()
	}
	return projectDirFlag, nil
}
