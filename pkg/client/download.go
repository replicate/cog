package client

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"

	"github.com/mholt/archiver/v3"
	"github.com/schollz/progressbar/v3"

	"github.com/replicate/cog/pkg/model"
)

func (c *Client) DownloadVersion(mod *model.Model, id string, outputDir string) error {
	url := newURL(mod, "v1/models/%s/%s/versions/%s.zip", mod.User, mod.Name, id)
	req, err := c.newRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("Failed to create HTTP request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("Failed to perform HTTP request: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("Version does not exist: %s", id)
	}
	if resp.StatusCode != http.StatusOK {
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("Error downloading version data (status %d): %s", resp.StatusCode, err)
		}
		return fmt.Errorf("Error downloading version data (status %d): %s", resp.StatusCode, body)
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
