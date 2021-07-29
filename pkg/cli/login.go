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
	"github.com/replicate/cog/pkg/settings"
	"github.com/replicate/cog/pkg/util/console"
)

type VerifyResponse struct {
	Username string `json:"username"`
}

func newLoginCommand() *cobra.Command {
	var cmd = &cobra.Command{
		Use:        "login [COG_SERVER_ADDRESS]",
		SuggestFor: []string{"auth", "authenticate", "authorize"},
		Short:      "Authorize the replicate CLI to a Cog server",
		RunE:       login,
		Args:       cobra.MaximumNArgs(1),
	}

	return cmd
}

func login(cmd *cobra.Command, args []string) error {
	address := global.CogServerAddress
	if len(args) == 1 {
		address = args[0]
	}

	url := address + "/api/auth/display-token"
	fmt.Println("Please visit " + url + " in a web browser")
	fmt.Println("and copy the authorization token.")
	maybeOpenBrowser(url)

	fmt.Print("\nPaste the token here: ")
	token, err := bufio.NewReader(os.Stdin).ReadString('\n')
	token = strings.TrimSpace(token)
	if err != nil {
		return err
	}

	username, registryHost, err := verifyToken(address, token)
	if err != nil {
		return err
	}

	if err := settings.SaveAuthToken(address, username, token, registryHost); err != nil {
		return err
	}

	if err := docker.SetCogCredentialHelperForHost(registryHost); err != nil {
		return err
	}

	console.Infof("Successfully authenticated as %s", username)

	return nil
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

func verifyToken(address string, token string) (username string, registryHost string, err error) {
	resp, err := http.PostForm(address+"/api/auth/verify-token", url.Values{
		"token": []string{token},
	})
	if err != nil {
		return "", "", fmt.Errorf("Failed to verify token: %w", err)
	}
	if resp.StatusCode == http.StatusNotFound {
		return "", "", fmt.Errorf("User does not exist")
	}
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("Failed to verify token, got status %d", resp.StatusCode)
	}
	body := &struct {
		Username     string `json:"username"`
		RegistryHost string `json:"registry_host"`
	}{}
	if err := json.NewDecoder(resp.Body).Decode(body); err != nil {
		return "", "", err
	}
	return body.Username, body.RegistryHost, nil
}
