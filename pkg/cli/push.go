package cli

import (
	"fmt"
	"os"
	"path"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/client"
	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/util/console"
)

func newPushCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "push",
		Short: "Push version",
		RunE:  push,
		Args:  cobra.NoArgs,
	}
	addModelFlag(cmd)
	addProjectDirFlag(cmd)

	return cmd
}

func push(cmd *cobra.Command, args []string) error {
	model, err := getModel()
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

	console.Infof("Uploading %s to %s", projectDir, model)

	cli := client.NewClient()
	version, err := cli.UploadVersion(model, projectDir)
	if err != nil {
		return err
	}

	fmt.Printf("Successfully uploaded version %s\n", version.ID)
	return nil
}
