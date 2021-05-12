package client

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/replicate/cog/pkg/model"
)

func (c *Client) ListVersions(repo *model.Repo) ([]*model.Version, error) {
	url := newURL(repo, "v1/repos/%s/%s/versions/", repo.User, repo.Name)
	req, err := c.newRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("Repository not found: %s", repo.String())
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
