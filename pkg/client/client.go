package client

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/replicate/cog/pkg/model"
)

type Client struct {
	url string
}

func NewClient(url string) *Client {
	return &Client{url: url}
}

// The URL says "package" but the code says "Model", sob
func (c *Client) GetPackage(id string) (*model.Model, error) {
	resp, err := http.Get(c.url + "/v1/packages/" + id)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := ioutil.ReadAll(resp.Body)
		return nil, fmt.Errorf("Server returned status %d: %s", resp.StatusCode, body)
	}
	model := &model.Model{}
	if err := json.NewDecoder(resp.Body).Decode(model); err != nil {
		return nil, err
	}
	return model, nil
}
