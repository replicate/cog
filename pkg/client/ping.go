package client

import (
	"fmt"
	"net/http"

	"github.com/replicate/cog/pkg/model"
)

func (c *Client) Ping(repo *model.Repo) error {
	url, err := c.getURL(repo, "ping")
	if err != nil {
		return err
	}
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Request to %s failed with status %d", url, resp.StatusCode)
	}
	return nil
}
