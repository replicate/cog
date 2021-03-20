package cli

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/mholt/archiver/v3"
	"github.com/mitchellh/go-homedir"
	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/files"
)

var downloadOutputDir string

func newDownloadCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "download <id>",
		Short: "Download a model package",
		RunE:  downloadPackage,
		Args:  cobra.ExactArgs(1),
	}

	cmd.Flags().StringVarP(&downloadOutputDir, "output-dir", "o", "", "Output directory")
	cmd.MarkFlagRequired("output-dir")

	return cmd
}

func downloadPackage(cmd *cobra.Command, args []string) (err error) {
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
	exists, err := files.FileExists(downloadOutputDir)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("%s already exists", downloadOutputDir)
	}

	if err := os.MkdirAll(downloadOutputDir, 0755); err != nil {
		return fmt.Errorf("Failed to create %s: %w", downloadOutputDir, err)
	}

	req, err := http.NewRequest("GET", remoteHost()+"/v1/packages/"+id+".zip", nil)
	if err != nil {
		return fmt.Errorf("Failed to create HTTP request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("Failed to perform HTTP request: %w", err)
	}

	bar := progressbar.DefaultBytes(
		resp.ContentLength,
		"Downloading",
	)
	buff := bytes.NewBuffer([]byte{})
	size, err := io.Copy(io.MultiWriter(buff, bar), resp.Body)
	if err != nil {
		return err
	}
	reader := bytes.NewReader(buff.Bytes())

	// TODO(andreas): handle missing ID
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Package zip endpoint returned status %d", resp.StatusCode)
	}

	zip := archiver.NewZip()
	if err := zip.ReaderUnarchive(reader, size, downloadOutputDir); err != nil {
		return fmt.Errorf("Failed to unzip into %s: %w", downloadOutputDir, err)
	}

	fmt.Printf("Downloaded package %s into %s\n", id, downloadOutputDir)
	return nil
}
