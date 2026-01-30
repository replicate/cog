package generic

import (
	"context"
	"fmt"

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

func (p *GenericProvider) Login(ctx context.Context, registryHost string) error {
	// Return ErrUseDockerLogin - actual push auth uses Docker's credential system
	// (pkg/docker/credentials.go) which already handles any registry
	return provider.ErrUseDockerLogin
}

func (p *GenericProvider) PrePush(ctx context.Context, opts provider.PushOptions) error {
	// Validate options - some features are not supported for generic registries
	if opts.LocalImage {
		return fmt.Errorf("local image push (--local-image) is not supported for this registry; it only works with Replicate's registry (r8.im)")
	}

	if opts.FastPush {
		console.Warnf("Fast push (--x-fast) is not supported for this registry. Falling back to standard push.")
		// Note: We warn but don't error - the caller should set FastPush=false
	}

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
