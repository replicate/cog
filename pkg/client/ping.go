package client

import (
	"fmt"
	"net/http"
)

func (c *Client) Ping(host string) error {
	url := fmt.Sprintf("http://%s/ping", host)
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Request to %s failed with status %d", url, resp.StatusCode)
	}
	return nil
}
