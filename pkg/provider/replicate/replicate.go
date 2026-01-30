package replicate

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"golang.org/x/term"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/provider"
	"github.com/replicate/cog/pkg/util/console"
)

// ReplicateProvider handles Replicate's r8.im registry
type ReplicateProvider struct{}

// New creates a new ReplicateProvider
func New() *ReplicateProvider {
	return &ReplicateProvider{}
}

func (p *ReplicateProvider) Name() string {
	return "replicate"
}

func (p *ReplicateProvider) MatchesRegistry(host string) bool {
	return host == global.DefaultReplicateRegistryHost ||
		host == global.ReplicateRegistryHost
}

// Login performs Replicate-specific authentication via browser token flow
func (p *ReplicateProvider) Login(ctx context.Context, registryHost string) error {
	return p.LoginWithOptions(ctx, registryHost, false, nil)
}

// LoginWithOptions performs Replicate-specific authentication with configurable options
func (p *ReplicateProvider) LoginWithOptions(ctx context.Context, registryHost string, tokenStdin bool, stdin *os.File) error {
	var token string
	var err error

	if tokenStdin {
		token, err = readTokenFromStdin()
		if err != nil {
			return err
		}
	} else {
		token, err = readTokenInteractively(registryHost, stdin)
		if err != nil {
			return err
		}
	}
	token = strings.TrimSpace(token)

	if err := checkTokenFormat(token); err != nil {
		return err
	}

	username, err := verifyToken(registryHost, token)
	if err != nil {
		return err
	}

	if err := docker.SaveLoginToken(ctx, registryHost, username, token); err != nil {
		return err
	}

	console.Infof("You've successfully authenticated as %s! You can now use the '%s' registry.", username, registryHost)

	return nil
}

func (p *ReplicateProvider) PrePush(ctx context.Context, image string, cfg *config.Config) error {
	// Future: Add Replicate-specific pre-push validation here
	// For now, this is a no-op - the existing validation is in docker.Push
	return nil
}

func (p *ReplicateProvider) PostPush(ctx context.Context, image string, cfg *config.Config, pushErr error) error {
	// Future: Move coglog analytics and Replicate model registration here
	// For now, this is a no-op - analytics are still in push.go
	return nil
}

// readTokenFromStdin reads the authentication token from stdin
func readTokenFromStdin() (string, error) {
	tokenBytes, err := os.ReadFile("/dev/stdin")
	if err != nil {
		return "", fmt.Errorf("failed to read token from stdin: %w", err)
	}
	return string(tokenBytes), nil
}

// readTokenInteractively guides user through browser-based token flow
func readTokenInteractively(registryHost string, stdin *os.File) (string, error) {
	tokenURL, err := getDisplayTokenURL(registryHost)
	if err != nil {
		return "", err
	}

	console.Infof("This command will authenticate Docker with Replicate's '%s' Docker registry. You will need a Replicate account.", registryHost)
	console.Info("")
	console.Info("Hit enter to get started. A browser will open with an authentication token that you need to paste here.")

	inputReader := os.Stdin
	inputFd := int(os.Stdin.Fd())
	if stdin != nil {
		inputReader = stdin
		inputFd = int(stdin.Fd())
	}

	reader := bufio.NewReader(inputReader)
	if _, err := reader.ReadString('\n'); err != nil {
		return "", err
	}

	console.Info("If it didn't open automatically, open this URL in a web browser:")
	console.Info(tokenURL)
	maybeOpenBrowser(tokenURL)

	console.Info("")
	console.Info("Once you've signed in, copy the token from that web page, paste it here, then hit enter:")

	fmt.Print("CLI auth token: ")
	// Read the token securely, masking the input
	tokenBytes, err := term.ReadPassword(inputFd)
	if err != nil {
		return "", fmt.Errorf("failed to read token: %w", err)
	}

	// Print a newline after the hidden input
	fmt.Println()

	return string(tokenBytes), nil
}

// getDisplayTokenURL fetches the token URL from Replicate's API
func getDisplayTokenURL(registryHost string) (string, error) {
	resp, err := http.Get(addressWithScheme(registryHost) + "/cog/v1/display-token-url")
	if err != nil {
		return "", fmt.Errorf("failed to log in to %s: %w", registryHost, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("%s is not the Replicate registry\nPlease log in using 'docker login'", registryHost)
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

// addressWithScheme ensures the address has an https:// scheme
func addressWithScheme(address string) string {
	if strings.Contains(address, "://") {
		return address
	}
	return "https://" + address
}

// maybeOpenBrowser attempts to open the URL in the default browser
func maybeOpenBrowser(urlToOpen string) {
	switch runtime.GOOS {
	case "linux":
		_ = exec.Command("xdg-open", urlToOpen).Start()
	case "windows":
		_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", urlToOpen).Start()
	case "darwin":
		_ = exec.Command("open", urlToOpen).Start()
	}
}

// checkTokenFormat validates the token isn't an API token
func checkTokenFormat(token string) error {
	if strings.HasPrefix(token, "r8_") {
		return fmt.Errorf("that looks like a Replicate API token, not a CLI auth token. Please fetch a token from https://replicate.com/auth/token to log in")
	}
	return nil
}

// verifyToken validates the token with Replicate and returns the username
func verifyToken(registryHost string, token string) (username string, err error) {
	if token == "" {
		return "", fmt.Errorf("token is empty")
	}

	resp, err := http.PostForm(addressWithScheme(registryHost)+"/cog/v1/verify-token", url.Values{
		"token": []string{token},
	})
	if err != nil {
		return "", fmt.Errorf("failed to verify token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("user does not exist")
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to verify token, got status %d", resp.StatusCode)
	}

	body := &struct {
		Username string `json:"username"`
	}{}
	if err := json.NewDecoder(resp.Body).Decode(body); err != nil {
		return "", err
	}
	return body.Username, nil
}

// Verify interface compliance at compile time
var _ provider.Provider = (*ReplicateProvider)(nil)
