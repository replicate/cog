package internal

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// userAgent identifies compatgen to the sites it scrapes. pytorch.org returns
// 403 to Go's default "Go-http-client/1.1" user agent.
const userAgent = "cog-compatgen (+https://github.com/replicate/cog)"

var httpClient = &http.Client{Timeout: 60 * time.Second}

// fetchURL returns the body of url, or an error if the response isn't 200 OK.
func fetchURL(url string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := httpClient.Do(req) //nolint:gosec // G704: URLs are hardcoded or come from those upstream responses, not user input
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s: %s", url, resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func split2(s string, sep string) (string, string) {
	parts := strings.SplitN(s, sep, 2)
	return parts[0], parts[1]
}
