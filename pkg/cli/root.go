package cli

import (
	"fmt"
	"regexp"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"os"

	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/settings"
)

var repoFlag string

var repoRegex = regexp.MustCompile("^(?:([^/]*)/)(?:([-_a-zA-Z0-9]+)/)([-_a-zA-Z0-9]+)$")

func NewRootCommand() (*cobra.Command, error) {
	rootCmd := cobra.Command{
		Use:     "cog",
		Short:   "",
		Version: fmt.Sprintf("%s (built %s)", global.Version, global.BuildTime),
		// This stops errors being printed because we print them in cmd/keepsake/main.go
	}
	setPersistentFlags(&rootCmd)

	rootCmd.AddCommand(
		newBuildCommand(),
		newDebugCommand(),
		newInferCommand(),
		newServerCommand(),
		newShowCommand(),
		newRepoCommand(),
		newDownloadCommand(),
	)

	log.SetLevel(log.DebugLevel)

	return &rootCmd, nil
}

func setPersistentFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().BoolVarP(&global.Verbose, "verbose", "v", false, "Verbose output")

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
		return nil, fmt.Errorf("Either you must run cog repo set <repo> in the current directory, or specify --repo/-r")
	}
}

func parseRepo(repoString string) (*model.Repo, error) {
	matches := repoRegex.FindStringSubmatch(repoString)
	if len(matches) == 0 {
		return nil, fmt.Errorf("Repo '%s' doesn't match <host>/<user>/<name>", repoString)
	}
	return &model.Repo{
		Host: matches[1],
		User: matches[2],
		Name: matches[3],
	}, nil
}
