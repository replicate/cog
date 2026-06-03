package cli

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/provider"
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

	tokenStdin, err := cmd.Flags().GetBool("token-stdin")
	if err != nil {
		return err
	}

	return RunLogin(ctx, provider.DefaultRegistry(), LoginOptions{
		TokenStdin: tokenStdin,
		// Use global registry host (can be set via --registry flag or COG_REGISTRY_HOST env var)
		Host: global.ReplicateRegistryHost,
	})
}

// LoginOptions holds the parser-independent options for the login command.
type LoginOptions struct {
	TokenStdin bool
	Host       string
}

// RunLogin logs in to the container registry for opts.Host. It is shared by
// both the Cobra and Kong login commands.
func RunLogin(ctx context.Context, providerReg *provider.Registry, opts LoginOptions) error {
	// Look up the provider for this registry
	p := providerReg.ForHost(opts.Host)
	if p == nil {
		// This shouldn't happen since GenericProvider matches everything
		console.Warnf("No provider found for registry '%s'.", opts.Host)
		console.Infof("Please use 'docker login %s' to authenticate.", opts.Host)
		return nil
	}

	return p.Login(ctx, provider.LoginOptions{
		TokenStdin: opts.TokenStdin,
		Host:       opts.Host,
	})
}
