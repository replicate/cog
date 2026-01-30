package provider

import (
	"context"
	"net/http"

	"github.com/replicate/cog/pkg/config"
)

// PushOptions contains all options for a push operation
type PushOptions struct {
	Image      string
	Config     *config.Config
	ProjectDir string

	// Feature flags
	LocalImage bool
	FastPush   bool

	// For analytics
	BuildID string

	// HTTP client for API calls (may be nil for generic provider)
	HTTPClient *http.Client
}

type LoginOptions struct {
	TokenStdin bool
	Host       string
}

// Provider encapsulates registry-specific behavior
type Provider interface {
	// Name returns the provider identifier (e.g., "replicate", "generic")
	Name() string

	// MatchesRegistry returns true if this provider handles the given registry host
	MatchesRegistry(host string) bool

	// Login performs provider-specific authentication
	Login(ctx context.Context, opts LoginOptions) error

	// PrePush is called before pushing
	// - Validates push options (returns error if unsupported features are used)
	// - Starts analytics/telemetry
	// - Any other pre-push setup
	PrePush(ctx context.Context, opts PushOptions) error

	// PostPush is called after push attempt (success or failure)
	// - Ends analytics/telemetry
	// - Shows success message (e.g., Replicate model URL)
	// - pushErr is nil on success, contains the push error on failure
	PostPush(ctx context.Context, opts PushOptions, pushErr error) error
}
