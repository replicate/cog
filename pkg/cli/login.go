package cli

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/client"
	"github.com/replicate/cog/pkg/console"
	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/settings"
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

	c := client.NewClient()
	url, err := c.GetDisplayTokenURL(address)
	if err != nil {
		return err
	}
	if url == "" {
		return fmt.Errorf("This server does not support authentication")
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

	username, err := c.VerifyToken(address, token)
	if err != nil {
		return err
	}

	err = settings.SaveAuthToken(address, username, token)
	if err != nil {
	return err
	}

	console.Infof("Successfully authenticated as %s", username)

	return nil
}

func maybeOpenBrowser(url string) {
	switch runtime.GOOS {
	case "linux":
		exec.Command("xdg-open", url).Start()
	case "windows":
		exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		exec.Command("open", url).Start()
	}
}
