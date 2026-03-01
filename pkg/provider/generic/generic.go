package generic

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/provider"
	"github.com/replicate/cog/pkg/util/console"
)

// GenericProvider works with any OCI-compliant registry
type GenericProvider struct{}

// New creates a new GenericProvider
func New() *GenericProvider {
	return &GenericProvider{}
}

func (p *GenericProvider) Name() string {
	return "generic"
}

func (p *GenericProvider) MatchesRegistry(host string) bool {
	return true // Fallback - matches everything
}

func (p *GenericProvider) Login(ctx context.Context, opts provider.LoginOptions) error {
	console.Infof("Logging in to %s", opts.Host)
	console.Info("")

	// TODO: support non-interactive login with token stdin for generic registries
	// Prompt for username
	fmt.Print("Username: ")
	reader := bufio.NewReader(os.Stdin)
	username, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read username: %w", err)
	}
	username = strings.TrimSpace(username)

	if username == "" {
		return fmt.Errorf("username cannot be empty")
	}

	// Prompt for password (hidden input)
	fmt.Print("Password: ")
	passwordBytes, err := term.ReadPassword(int(os.Stdin.Fd())) //nolint:gosec // G115: Fd() fits in int on all supported platforms
	if err != nil {
		return fmt.Errorf("failed to read password: %w", err)
	}
	fmt.Println() // newline after hidden input
	password := string(passwordBytes)

	if password == "" {
		return fmt.Errorf("password cannot be empty")
	}

	// Save credentials using Docker's credential system
	if err := docker.SaveLoginToken(ctx, opts.Host, username, password); err != nil {
		return fmt.Errorf("failed to save credentials: %w", err)
	}

	console.Infof("Login succeeded for %s", opts.Host)
	return nil
}

func (p *GenericProvider) PostPush(ctx context.Context, opts provider.PushOptions, pushErr error) error {
	// No special post-push handling for generic registries
	// Just show a simple success message if push succeeded
	if pushErr == nil {
		console.Infof("Image '%s' pushed", opts.Image)
	}
	return nil
}

// Verify interface compliance at compile time
var _ provider.Provider = (*GenericProvider)(nil)
