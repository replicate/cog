package client

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/server"
)

func (c *Client) GetVersion(mod *model.Model, id string) (*server.APIVersion, error) {
	url := newURL(mod, "v1/models/%s/%s/versions/%s", mod.User, mod.Name, id)
	req, err := c.newRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("Version not found: %s:%s", mod.String(), id)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Server returned status %d: %s", resp.StatusCode, body)
	}

	version := &server.APIVersion{}

	if err := json.NewDecoder(resp.Body).Decode(&version); err != nil {
		return nil, err
	}
	return version, nil
}
