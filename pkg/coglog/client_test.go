package coglog

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/env"
)

func TestLogBuild(t *testing.T) {
	// Setup mock http server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	url, err := url.Parse(server.URL)
	require.NoError(t, err)
	t.Setenv(env.SchemeEnvVarName, url.Scheme)
	t.Setenv(CoglogHostEnvVarName, url.Host)

	client := NewClient(http.DefaultClient)
	logContext := client.StartBuild(false)
	success := client.EndBuild(t.Context(), nil, logContext)
	require.True(t, success)
}

func TestLogBuildDisabled(t *testing.T) {
	t.Setenv(CoglogDisableEnvVarName, "true")
	client := NewClient(http.DefaultClient)
	logContext := client.StartBuild(false)
	success := client.EndBuild(t.Context(), nil, logContext)
	require.False(t, success)
}

func TestLogPush(t *testing.T) {
	// Setup mock http server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	url, err := url.Parse(server.URL)
	require.NoError(t, err)
	t.Setenv(env.SchemeEnvVarName, url.Scheme)
	t.Setenv(CoglogHostEnvVarName, url.Host)

	client := NewClient(http.DefaultClient)
	logContext := client.StartPush(false)
	success := client.EndPush(t.Context(), nil, logContext)
	require.True(t, success)
}
