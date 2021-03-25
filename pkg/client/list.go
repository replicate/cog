package client

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/replicate/cog/pkg/model"
)

func (c *Client) ListPackages(repo *model.Repo) ([]*model.Model, error) {
	url := fmt.Sprintf("http://%s/v1/repos/%s/%s/packages/", repo.Host, repo.User, repo.Name)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
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
