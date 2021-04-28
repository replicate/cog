package client

import (
	"fmt"
	"net/http"

	"github.com/replicate/cog/pkg/model"
)

func (c *Client) DeleteModel(repo *model.Repo, id string) error {
	url := newURL(repo, "v1/repos/%s/%s/models/%s", repo.User, repo.Name, id)
	req, err := c.newRequest(http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("Model not found: %s", id)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Server returned status %d", resp.StatusCode)
	}
	return nil
}
