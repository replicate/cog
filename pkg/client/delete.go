package client

import (
	"fmt"
	"net/http"

	"github.com/replicate/cog/pkg/model"
)

// The URL says "package" but the code says "Model", sob
func (c *Client) DeletePackage(repo *model.Repo, id string) error {
	url := fmt.Sprintf("http://%s/v1/repos/%s/%s/packages/%s", repo.Host, repo.User, repo.Name, id)
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Server returned status %d", resp.StatusCode)
	}
	return nil
}
