package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mitchellh/go-homedir"
	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/client"
	"github.com/replicate/cog/pkg/util/files"
)

var downloadOutputDir string

func newDownloadCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "download <id>",
		Short: "Download a version",
		RunE:  download,
		Args:  cobra.ExactArgs(1),
	}
	addRepoFlag(cmd)
	cmd.Flags().StringVarP(&downloadOutputDir, "output-dir", "o", "", "Output directory")
	cmd.MarkFlagRequired("output-dir")

	return cmd
}

func download(cmd *cobra.Command, args []string) (err error) {
	repo, err := getRepo()
	if err != nil {
		return err
	}
	id := args[0]

	downloadOutputDir, err = homedir.Expand(downloadOutputDir)
	if err != nil {
		return err
	}
	downloadOutputDir, err = filepath.Abs(downloadOutputDir)
	if err != nil {
		return err
	}

	// TODO(andreas): allow to checkout to existing directories, with warning prompt
	exists, err := files.Exists(downloadOutputDir)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("%s already exists", downloadOutputDir)
	}

	if err := os.MkdirAll(downloadOutputDir, 0755); err != nil {
		return fmt.Errorf("Failed to create %s: %w", downloadOutputDir, err)
	}

	cli := client.NewClient()
	if err := cli.DownloadVersion(repo, id, downloadOutputDir); err != nil {
		return err
	}

	fmt.Printf("Downloaded files from version %s into %s\n", id, downloadOutputDir)
	return nil
}
