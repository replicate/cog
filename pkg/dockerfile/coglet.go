package dockerfile

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// GitHubRelease represents a GitHub release response
type GitHubRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

// GetLatestCogletWheelURL fetches the latest coglet wheel URL from GitHub releases
func GetLatestCogletWheelURL(ctx context.Context) (string, error) {
	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// GitHub API URL for latest release
	apiURL := "https://api.github.com/repos/replicate/cog-runtime/releases/latest"

	// Create request with context
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Add headers
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "cog-cli")

	// Make the request
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch release data: %w", err)
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	// Parse JSON response
	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", fmt.Errorf("failed to parse release data: %w", err)
	}

	// Find coglet wheel in assets
	for _, asset := range release.Assets {
		if strings.HasSuffix(asset.Name, ".whl") && strings.Contains(asset.Name, "coglet") {
			return asset.BrowserDownloadURL, nil
		}
	}

	return "", fmt.Errorf("no coglet wheel found in latest release %s", release.TagName)
}
