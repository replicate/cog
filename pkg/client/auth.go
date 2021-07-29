package client

import (
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/replicate/cog/pkg/model"
)

func (c *Client) CheckRead(mod *model.Model) error {
	url := newURL(mod, "v1/models/%s/%s/check-read", mod.User, mod.Name)
	req, err := c.newRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("Failed to read response body: %w", err)
	}
	body := string(bodyBytes)
	if resp.StatusCode != http.StatusOK {
		return errors.New(body)
	}
	return nil
}
