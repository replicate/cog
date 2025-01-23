package docker

import (
	"context"
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
	t.Setenv(schemeEnv, url.Scheme)
	t.Setenv(hostEnv, url.Host)

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
	err = FastPush("test", dir, command, context.Background())
	require.NoError(t, err)
}
