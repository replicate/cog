package internal

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFetchURLSendsUserAgent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, userAgent, r.UserAgent())
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	body, err := fetchURL(server.URL)
	require.NoError(t, err)
	require.Equal(t, "ok", body)
}

func TestFetchURLFailsOnNonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("blocked"))
	}))
	defer server.Close()

	_, err := fetchURL(server.URL)
	require.ErrorContains(t, err, "403 Forbidden")
}
