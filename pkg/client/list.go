package client

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/replicate/cog/pkg/model"
)

func (c *Client) ListModels(repo *model.Repo) ([]*model.Model, error) {
	url := newURL(repo, "v1/repos/%s/%s/models/", repo.User, repo.Name)
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

	models := []*model.Model{}
	if err := json.NewDecoder(resp.Body).Decode(&models); err != nil {
		return nil, fmt.Errorf("Failed to decode response: %w", err)
	}

	return models, nil
}
