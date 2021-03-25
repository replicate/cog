package client

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/replicate/cog/pkg/model"
)

// The URL says "package" but the code says "Model", sob
func (c *Client) GetPackage(repo *model.Repo, id string) (*model.Model, error) {
	url := fmt.Sprintf("http://%s/v1/repos/%s/%s/packages/%s", repo.Host, repo.User, repo.Name, id)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Server returned status %d: %s", resp.StatusCode, body)
	}
	model := &model.Model{}
	if err := json.NewDecoder(resp.Body).Decode(model); err != nil {
		return nil, err
	}
	return model, nil
}
