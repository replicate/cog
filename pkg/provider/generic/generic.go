package generic

import (
	"context"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/provider"
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

func (p *GenericProvider) PrePush(ctx context.Context, image string, cfg *config.Config) error {
	return nil
}

func (p *GenericProvider) PostPush(ctx context.Context, image string, cfg *config.Config, pushErr error) error {
	return nil
}

// Verify interface compliance at compile time
var _ provider.Provider = (*GenericProvider)(nil)
