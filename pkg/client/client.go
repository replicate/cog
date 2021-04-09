package client

import (
	"fmt"
	"os"

	"github.com/replicate/cog/pkg/model"
)

type Client struct {
}

func NewClient() *Client {
	return &Client{}
}

func (c *Client) getURL(repo *model.Repo, path string, args ...interface{}) (string, error) {
	if len(args) > 0 {
		path = fmt.Sprintf(path, args...)
	}
	var host string
	if repo.Host != "" {
		host = repo.Host
	} else {
		host = os.Getenv("COG_SERVER")
		if host == "" {
			return "", fmt.Errorf("Repo is missing host and COG_SERVER is not set")
		}
	}
	return fmt.Sprintf("http://%s/%s", host, path), nil
}
