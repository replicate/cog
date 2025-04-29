package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/docker/dockertest"
)

func TestClientDecoratesUserAgent(t *testing.T) {
	// Setup mock http server
	seenUserAgent := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, r.Header.Get(UserAgentHeader), UserAgent())
		seenUserAgent = true
	}))
	defer server.Close()

	command := dockertest.NewMockCommand()
	client, err := ProvideHTTPClient(t.Context(), command)
	require.NoError(t, err)

	_, err = client.Get(server.URL)
	require.NoError(t, err)

	require.True(t, seenUserAgent)
}
