package client

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/replicate/cog/pkg/model"
)

func (c *Client) ListVersions(mod *model.Model) ([]*model.Version, error) {
	url := newURL(mod, "v1/models/%s/%s/versions/", mod.User, mod.Name)
	req, err := c.newRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("Model not found: %s", mod.String())
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("List endpoint returned status %d", resp.StatusCode)
	}

	versions := []*model.Version{}
	if err := json.NewDecoder(resp.Body).Decode(&versions); err != nil {
		return nil, fmt.Errorf("Failed to decode response: %w", err)
	}

	return versions, nil
}
