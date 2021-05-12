package cli

import (
	"fmt"
	"os"
	"path"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/console"

	"github.com/replicate/cog/pkg/client"
	"github.com/replicate/cog/pkg/global"
)

func newPushCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "push",
		Short: "Push Cog model",
		RunE:  pushModel,
		Args:  cobra.NoArgs,
	}
	addRepoFlag(cmd)
	addProjectDirFlag(cmd)

	return cmd
}

func pushModel(cmd *cobra.Command, args []string) error {
	repo, err := getRepo()
	if err != nil {
		return err
	}

	projectDir, err := getProjectDir()
	if err != nil {
		return err
	}

	if _, err := os.Stat(path.Join(projectDir, global.ConfigFilename)); os.IsNotExist(err) {
		return fmt.Errorf("%s does not exist in %s. Are you in the right directory?", global.ConfigFilename, projectDir)
	}

	console.Infof("Uploading %s to %s", projectDir, repo)

	cli := client.NewClient()
	mod, err := cli.UploadModel(repo, projectDir)
	if err != nil {
		return err
	}

	fmt.Printf("Successfully uploaded model version %s\n", mod.ID)
	return nil
}
