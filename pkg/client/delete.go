package client

import (
	"fmt"
	"net/http"

	"github.com/replicate/cog/pkg/model"
)

func (c *Client) DeleteVersion(mod *model.Model, id string) error {
	url := newURL(mod, "v1/models/%s/%s/versions/%s", mod.User, mod.Name, id)
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
		return fmt.Errorf("Version not found: %s", id)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Server returned status %d", resp.StatusCode)
	}
	return nil
}
