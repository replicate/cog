package client

import (
	"bytes"
	"fmt"
	"io"
	"net/http"

	"github.com/mholt/archiver/v3"
	"github.com/schollz/progressbar/v3"

	"github.com/replicate/cog/pkg/model"
)

func (c *Client) DownloadModel(repo *model.Repo, id string, outputDir string) error {
	url, err := c.getURL(repo, "v1/repos/%s/%s/models/%s.zip", repo.User, repo.Name, id)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("Failed to create HTTP request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("Failed to perform HTTP request: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("Model ID doesn't exist: %s", id)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Model zip endpoint returned status %d", resp.StatusCode)
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
	zip := archiver.NewZip()
	if err := zip.ReaderUnarchive(reader, size, outputDir); err != nil {
		return fmt.Errorf("Failed to unzip into %s: %w", outputDir, err)
	}
	return nil
}
