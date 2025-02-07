package docker

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/docker/dockertest"
	"github.com/replicate/cog/pkg/env"
	"github.com/replicate/cog/pkg/monobeam"
)

func TestPush(t *testing.T) {
	// Setup mock http server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte("Hello World"))
	}))
	defer server.Close()
	url, err := url.Parse(server.URL)
	require.NoError(t, err)
	t.Setenv(env.SchemeEnvVarName, url.Scheme)
	t.Setenv(monobeam.MonobeamHostEnvVarName, url.Host)

	// Create directories
	dir := t.TempDir()
	cogDir := filepath.Join(dir, ".cog")
	err = os.Mkdir(cogDir, 0o755)
	require.NoError(t, err)
	tmpDir := filepath.Join(cogDir, "tmp")
	err = os.Mkdir(tmpDir, 0o755)
	require.NoError(t, err)

	// Setup mock docker command
	command := dockertest.NewMockCommand()

	// Run fast push
	err = Push("test", true, dir, command)
	require.NoError(t, err)
}
