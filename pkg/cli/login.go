package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/util/console"
)

type VerifyResponse struct {
	Username string `json:"username"`
}

func newLoginCommand() *cobra.Command {
	var cmd = &cobra.Command{
		Use:        "login [REGISTRY_HOST]",
		SuggestFor: []string{"auth", "authenticate", "authorize"},
		Short:      "Authorize the replicate CLI to a Cog server",
		RunE:       login,
		Args:       cobra.MaximumNArgs(1),
	}

	return cmd
}

func login(cmd *cobra.Command, args []string) error {
	registryHost := global.DefaultRegistryHost
	if len(args) == 1 {
		registryHost = args[0]
	}
	url, err := getDisplayTokenURL(registryHost)
	if err != nil {
		return err
	}
	fmt.Println("Please visit " + url + " in a web browser")
	fmt.Println("and copy the authorization token.")
	maybeOpenBrowser(url)

	fmt.Print("\nPaste the token here: ")
	token, err := bufio.NewReader(os.Stdin).ReadString('\n')
	token = strings.TrimSpace(token)
	if err != nil {
		return err
	}

	username, err := verifyToken(registryHost, token)
	if err != nil {
		return err
	}

	if err := docker.SaveLoginToken(registryHost, username, token); err != nil {
		return err
	}

	console.Infof("Successfully authenticated as %s", username)

	return nil
}

func getDisplayTokenURL(registryHost string) (string, error) {
	resp, err := http.Get(addressWithScheme(registryHost) + "/cog/v1/display-token-url")
	if err != nil {
		return "", fmt.Errorf("Failed to log in to %s: %w\nDoes this registry support Cog authentication?", registryHost, err)
	}
	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("%s does not support Cog authentication\nPlease log in using `docker login`", registryHost)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s returned HTTP status %d", registryHost, resp.StatusCode)
	}
	body := &struct {
		URL string `json:"url"`
	}{}
	if err := json.NewDecoder(resp.Body).Decode(body); err != nil {
		return "", err
	}
	return body.URL, nil
}

func addressWithScheme(address string) string {
	if strings.Contains(address, "://") {
		return address
	}
	return "https://" + address
}

func maybeOpenBrowser(url string) {
	switch runtime.GOOS {
	case "linux":
		_ = exec.Command("xdg-open", url).Start()
	case "windows":
		_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		_ = exec.Command("open", url).Start()
	}
}

func verifyToken(registryHost string, token string) (username string, err error) {
	resp, err := http.PostForm(addressWithScheme(registryHost)+"/cog/v1/verify-token", url.Values{
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
