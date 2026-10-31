package docker

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The live e2e subtests below connect to a real Docker daemon reached over
// SSH. They are skipped unless both COG_SSH_E2E=1 and COG_SSH_E2E_HOST=<url>
// are set, so this file is portable across contributors' local setups.
//
// Example:
//
//	COG_SSH_E2E=1 COG_SSH_E2E_HOST=ssh://user@host go test -run TestNewClientSSHHost ./pkg/docker/

func TestNewClientSSHHost(t *testing.T) {
	t.Run("invalid SSH URL surfaces validation error", func(t *testing.T) {
		// connhelper rejects ssh URLs that carry a plain-text password.
		_, err := NewClient(context.Background(), WithHost("ssh://user:badpass@127.0.0.1"))
		require.Error(t, err, "ssh URL with password should fail NewClient")
		assert.ErrorContains(t, err, "invalid docker host")
		assert.ErrorContains(t, err, "password is not supported")

		// connhelper rejects ssh URLs with query parameters.
		_, err = NewClient(context.Background(), WithHost("ssh://user@127.0.0.1?foo=bar"))
		require.Error(t, err, "ssh URL with query should fail NewClient")
		assert.ErrorContains(t, err, "invalid docker host")
		assert.ErrorContains(t, err, "query parameters are not allowed")
	})

	if os.Getenv("COG_SSH_E2E") == "" {
		t.Skip("set COG_SSH_E2E=1 and COG_SSH_E2E_HOST=<ssh-url> to run live e2e subtests")
	}
	host := os.Getenv("COG_SSH_E2E_HOST")
	if host == "" {
		t.Skip("COG_SSH_E2E_HOST must point at an ssh:// URL reachable from this machine")
	}

	t.Run("happy path", func(t *testing.T) {
		c, err := NewClient(context.Background(), WithHost(host))
		require.NoError(t, err)
		require.NotNil(t, c)

		// Pull exercises the full HTTP-over-SSH path: dialer opens the
		// ssh session, the helper.Dialer pipes the HTTP request through
		// `docker system dial-stdio` on the remote end.
		_, err = c.Pull(context.Background(), "alpine:latest", true)
		require.NoError(t, err, "Pull over SSH should succeed")
	})

	// Regression: prior code layered WithTLSClientConfigFromEnv before the
	// SSH dialer, so DOCKER_CERT_PATH caused TLS to be applied to the plain
	// docker dial-stdio stream.
	t.Run("ignores DOCKER_CERT_PATH", func(t *testing.T) {
		t.Setenv("DOCKER_TLS_VERIFY", "1")
		t.Setenv("DOCKER_CERT_PATH", "/nonexistent/cert.pem")

		c, err := NewClient(context.Background(), WithHost(host))
		require.NoError(t, err, "SSH should ignore DOCKER_CERT_PATH")
		_, err = c.Pull(context.Background(), "alpine:latest", true)
		require.NoError(t, err)
	})

	// Regression: prior code used the default transport's proxy resolver,
	// which tried to HTTP-CONNECT the dummy ssh host before opening SSH.
	t.Run("ignores HTTP_PROXY", func(t *testing.T) {
		t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
		t.Setenv("http_proxy", "http://127.0.0.1:1")
		t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
		t.Setenv("https_proxy", "http://127.0.0.1:1")
		t.Setenv("NO_PROXY", "")
		t.Setenv("no_proxy", "")

		c, err := NewClient(context.Background(), WithHost(host))
		require.NoError(t, err, "SSH should ignore HTTP_PROXY")
		_, err = c.Pull(context.Background(), "alpine:latest", true)
		require.NoError(t, err)
	})
}
