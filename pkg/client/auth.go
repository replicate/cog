package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/replicate/cog/pkg/model"
)

func (c *Client) GetDisplayTokenURL(address string) (url string, err error) {
	resp, err := http.Get(address + "/v1/auth/display-token-url")
	if err != nil {
		return "", fmt.Errorf("Failed to get login URL: %w", err)
	}
	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("Login page does not exist on %s. Is it the correct URL?", address)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Login returned status %d", resp.StatusCode)
	}

	body := &struct {
		URL string `json:"url"`
	}{}
	if err := json.NewDecoder(resp.Body).Decode(body); err != nil {
		return "", err
	}
	return body.URL, nil
}

func (c *Client) VerifyToken(address string, token string) (username string, err error) {
	resp, err := http.PostForm(address+"/v1/auth/verify-token", url.Values{
		"token": []string{token},
	})
	if err != nil {
		return "", fmt.Errorf("Failed to verify token: %w", err)
	}
	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("User does not exist")
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Failed to verify token, got status %d", resp.StatusCode)
	}
	body := &struct {
		Username string `json:"username"`
	}{}
	if err := json.NewDecoder(resp.Body).Decode(body); err != nil {
		return "", err
	}
	return body.Username, nil
}

func (c *Client) CheckRead(repo *model.Repo) error {
	url := newURL(repo, "v1/repos/%s/%s/check-read", repo.User, repo.Name)
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
