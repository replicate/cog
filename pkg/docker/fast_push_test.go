package docker

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFastPush(t *testing.T) {
	// Setup mock http server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte("Hello World"))
	}))
	defer server.Close()
	url, err := url.Parse(server.URL)
	require.NoError(t, err)
	err = os.Setenv(SCHEME_ENV, url.Scheme)
	require.NoError(t, err)
	err = os.Setenv(HOST_ENV, url.Host)
	require.NoError(t, err)

	// Create directories
	dir := t.TempDir()
	cogDir := filepath.Join(dir, ".cog")
	err = os.Mkdir(cogDir, 0o755)
	require.NoError(t, err)
	tmpDir := filepath.Join(cogDir, "tmp")
	err = os.Mkdir(tmpDir, 0o755)
	require.NoError(t, err)

	// Setup mock command
	command := NewMockCommand()

	// Run fast push
	err = FastPush("test", dir, command)
	require.NoError(t, err)

	// Cleanup env
	err = os.Unsetenv(SCHEME_ENV)
	require.NoError(t, err)
	err = os.Unsetenv(HOST_ENV)
	require.NoError(t, err)
}
