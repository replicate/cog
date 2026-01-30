package provider

import (
	"context"
	"errors"

	"github.com/replicate/cog/pkg/config"
)

// ErrUseDockerLogin is returned by Login() when provider doesn't support custom login
var ErrUseDockerLogin = errors.New("use docker login")

// Provider encapsulates registry-specific behavior
type Provider interface {
	// Name returns the provider identifier (e.g., "replicate", "generic")
	Name() string

	// MatchesRegistry returns true if this provider handles the given registry host
	MatchesRegistry(host string) bool

	// Login performs provider-specific authentication
	// Returns ErrUseDockerLogin if provider doesn't support custom login
	Login(ctx context.Context, registryHost string) error

	// PrePush is called before pushing (for validation, setup, analytics start)
	PrePush(ctx context.Context, image string, cfg *config.Config) error

	// PostPush is called after successful push (for registration, analytics end)
	PostPush(ctx context.Context, image string, cfg *config.Config, pushErr error) error
}
