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
	"sync"
	"time"

	"golang.org/x/term"

	"github.com/replicate/cog/pkg/coglog"
	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/provider"
	"github.com/replicate/cog/pkg/util/console"
)

// ReplicateProvider handles Replicate's r8.im registry
type ReplicateProvider struct {
	// Analytics state (protected by mutex for thread safety)
	mu        sync.Mutex
	logClient *coglog.Client
	logCtx    coglog.PushLogContext
	started   time.Time
}

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

// Login performs login to the registry with options
func (p *ReplicateProvider) Login(ctx context.Context, opts provider.LoginOptions) error {

	var (
		token string
		err   error
	)

	if opts.TokenStdin {
		token, err = readTokenFromStdin()
		if err != nil {
			return err
		}
	} else {
		token, err = readTokenInteractively(opts.Host)
		if err != nil {
			return err
		}
	}

	token = strings.TrimSpace(token)

	if err := checkTokenFormat(token); err != nil {
		return err
	}

	username, err := verifyToken(opts.Host, token)
	if err != nil {
		return err
	}

	if err := docker.SaveLoginToken(ctx, opts.Host, username, token); err != nil {
		return err
	}

	console.Infof("You've successfully authenticated as %s! You can now use the '%s' registry.", username, opts.Host)
	return nil
}

func (p *ReplicateProvider) PrePush(ctx context.Context, opts provider.PushOptions) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// All features are supported for Replicate - no validation errors

	// Start analytics
	if opts.HTTPClient != nil {
		p.logClient = coglog.NewClient(opts.HTTPClient)
		p.logCtx = p.logClient.StartPush(opts.LocalImage)
		p.logCtx.Fast = opts.FastPush
		p.logCtx.CogRuntime = false
		if opts.Config != nil && opts.Config.Build.CogRuntime != nil {
			p.logCtx.CogRuntime = *opts.Config.Build.CogRuntime
		}
		p.started = time.Now()
	}

	if opts.FastPush {
		console.Info("Fast push enabled.")
	}

	return nil
}

func (p *ReplicateProvider) PostPush(ctx context.Context, opts provider.PushOptions, pushErr error) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// End analytics
	if p.logClient != nil {
		p.logClient.EndPush(ctx, pushErr, p.logCtx)
	}

	if pushErr != nil {
		// Return Replicate-specific error message for repository not found errors
		if command.IsNotFoundError(pushErr) {
			return fmt.Errorf("Unable to find existing Replicate model for %s. "+
				"Go to replicate.com and create a new model before pushing."+
				"\n\n"+
				"If the model already exists, you may be getting this error "+
				"because you're not logged in as owner of the model. "+
				"This can happen if you did `sudo cog login` instead of `cog login` "+
				"or `sudo cog push` instead of `cog push`, "+
				"which causes Docker to use the wrong Docker credentials.",
				opts.Image)
		}
		return pushErr
	}

	// Success - show Replicate model URL
	console.Infof("Image '%s' pushed", opts.Image)
	replicatePage := fmt.Sprintf("https://%s", strings.Replace(opts.Image, global.ReplicateRegistryHost, global.ReplicateWebsiteHost, 1))
	console.Infof("\nRun your model on Replicate:\n    %s", replicatePage)

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
func readTokenInteractively(registryHost string) (string, error) {
	tokenURL, err := getDisplayTokenURL(registryHost)
	if err != nil {
		return "", err
	}

	console.Infof("This command will authenticate Docker with Replicate's '%s' Docker registry. You will need a Replicate account.", registryHost)
	console.Info("")
	console.Info("Hit enter to get started. A browser will open with an authentication token that you need to paste here.")

	inputReader := os.Stdin
	inputFd := int(os.Stdin.Fd())

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
