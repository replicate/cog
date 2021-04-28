package client

import (
	"fmt"
	"net/http"

	"github.com/replicate/cog/pkg/model"
)

func (c *Client) Ping(repo *model.Repo) error {
	url := newURL(repo, "ping")
	req, err := c.newRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Request to %s failed with status %d", url, resp.StatusCode)
	}
	return nil
}
