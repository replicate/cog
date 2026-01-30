package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/provider"
	"github.com/replicate/cog/pkg/provider/replicate"
	"github.com/replicate/cog/pkg/provider/setup"
	"github.com/replicate/cog/pkg/util/console"
)

func newLoginCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:        "login",
		SuggestFor: []string{"auth", "authenticate", "authorize"},
		Short:      "Log in to a container registry",
		Long: `Log in to a container registry.

For Replicate's registry (r8.im), this command handles authentication
through Replicate's token-based flow.

For other registries, this command prompts for username and password,
then stores credentials using Docker's credential system.`,
		RunE: login,
		Args: cobra.MaximumNArgs(0),
	}

	cmd.Flags().Bool("token-stdin", false, "Pass login token on stdin instead of opening a browser. You can find your Replicate login token at https://replicate.com/auth/token")

	return cmd
}

func login(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// Initialize the provider registry
	setup.Init()

	// Use global registry host (can be set via --registry flag or COG_REGISTRY_HOST env var)
	registryHost := global.ReplicateRegistryHost

	tokenStdin, err := cmd.Flags().GetBool("token-stdin")
	if err != nil {
		return err
	}

	// Look up the provider for this registry
	p := provider.DefaultRegistry().ForHost(registryHost)
	if p == nil {
		// This shouldn't happen since GenericProvider matches everything
		console.Warnf("No provider found for registry '%s'.", registryHost)
		console.Infof("Please use 'docker login %s' to authenticate.", registryHost)
		return nil
	}

	// Check if this is the Replicate provider which supports LoginWithOptions
	if rp, ok := p.(*replicate.ReplicateProvider); ok {
		return rp.LoginWithOptions(ctx, registryHost, tokenStdin, nil)
	}

	// For other providers, use regular Login
	return p.Login(ctx, registryHost)
}

// LoginToRegistry performs login for a specific registry host with options
// This is exported for use by other commands that may need to trigger login
func LoginToRegistry(ctx context.Context, registryHost string, tokenStdin bool) error {
	setup.Init()

	p := provider.DefaultRegistry().ForHost(registryHost)
	if p == nil {
		return fmt.Errorf("no provider found for registry '%s'", registryHost)
	}

	// Check if this is the Replicate provider which supports LoginWithOptions
	if rp, ok := p.(*replicate.ReplicateProvider); ok {
		return rp.LoginWithOptions(ctx, registryHost, tokenStdin, nil)
	}

	// For other providers, use regular Login
	return p.Login(ctx, registryHost)
}
