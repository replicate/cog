package generic

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/provider"
)

func TestGenericProvider_Name(t *testing.T) {
	p := New()
	require.Equal(t, "generic", p.Name())
}

func TestGenericProvider_MatchesRegistry(t *testing.T) {
	p := New()
	// Generic provider matches everything (it's the fallback)
	require.True(t, p.MatchesRegistry("ghcr.io"))
	require.True(t, p.MatchesRegistry("docker.io"))
	require.True(t, p.MatchesRegistry("ecr.aws"))
	require.True(t, p.MatchesRegistry("anything.example.com"))
}

func TestGenericProvider_Login(t *testing.T) {
	// Login() prompts for username/password interactively and saves credentials
	// via Docker's credential system. This cannot be easily tested without mocking
	// stdin and the docker credential helpers.
	//
	// The Login method:
	// 1. Prompts for username (from stdin)
	// 2. Prompts for password (hidden input via terminal)
	// 3. Calls docker.SaveLoginToken() to store credentials
	//
	// For integration testing, use manual testing with 'cog login --registry <host>'
	t.Skip("Login requires interactive input - test manually")
}

func TestGenericProvider_PostPush(t *testing.T) {
	p := New()

	// On success, PostPush is intentionally silent — the CLI prints
	// a structured ref tree (see pkg/cli/push.go:printPushResult) as
	// the success indicator. Asserting "silent" via stderr capture
	// adds plumbing we don't have elsewhere in this package; rely on
	// printPushResult's own tests for the success-output contract.
	t.Run("success", func(t *testing.T) {
		opts := provider.PushOptions{
			Image: "ghcr.io/org/model",
		}
		err := p.PostPush(context.Background(), opts, nil)
		require.NoError(t, err)
	})

	t.Run("with error", func(t *testing.T) {
		opts := provider.PushOptions{
			Image: "ghcr.io/org/model",
		}
		pushErr := errors.New("push failed")
		err := p.PostPush(context.Background(), opts, pushErr)
		require.NoError(t, err) // PostPush itself doesn't error
	})
}
