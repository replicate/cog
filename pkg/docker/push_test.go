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
	"github.com/replicate/cog/pkg/web"
	"github.com/replicate/cog/pkg/weights"
)

func TestPush(t *testing.T) {
	// Setup mock http server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/models/username/modelname/versions" {
			w.WriteHeader(http.StatusCreated)
		} else {
			w.WriteHeader(http.StatusConflict)
			w.Write([]byte("Hello World"))
		}
	}))
	defer server.Close()
	url, err := url.Parse(server.URL)
	require.NoError(t, err)
	t.Setenv(env.SchemeEnvVarName, url.Scheme)
	t.Setenv(monobeam.MonobeamHostEnvVarName, url.Host)
	t.Setenv(web.WebHostEnvVarName, url.Host)

	// Create directories
	dir := t.TempDir()
	cogDir := filepath.Join(dir, ".cog")
	err = os.Mkdir(cogDir, 0o755)
	require.NoError(t, err)
	tmpDir := filepath.Join(cogDir, "tmp")
	err = os.Mkdir(tmpDir, 0o755)
	require.NoError(t, err)

	// Create mock predict
	predictPyPath := filepath.Join(dir, "predict.py")
	handle, err := os.Create(predictPyPath)
	require.NoError(t, err)
	handle.WriteString("import cog")
	dockertest.MockCogConfig = "{\"build\":{\"python_version\":\"3.12\",\"python_packages\":[\"torch==2.5.0\",\"beautifulsoup4==4.12.3\"],\"system_packages\":[\"git\"]},\"image\":\"test\",\"predict\":\"" + predictPyPath + ":Predictor\"}"

	// Setup mock docker command
	command := dockertest.NewMockCommand()

	// Run fast push
	err = Push("r8.im/username/modelname", true, dir, command, BuildInfo{})
	require.NoError(t, err)
}

func TestPushWithWeight(t *testing.T) {
	// Setup mock http server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/models/username/modelname/versions" {
			w.WriteHeader(http.StatusCreated)
		} else {
			w.WriteHeader(http.StatusConflict)
			w.Write([]byte("Hello World"))
		}
	}))
	defer server.Close()
	url, err := url.Parse(server.URL)
	require.NoError(t, err)
	t.Setenv(env.SchemeEnvVarName, url.Scheme)
	t.Setenv(monobeam.MonobeamHostEnvVarName, url.Host)
	t.Setenv(web.WebHostEnvVarName, url.Host)

	// Create directories
	dir := t.TempDir()
	cogDir := filepath.Join(dir, ".cog")
	err = os.Mkdir(cogDir, 0o755)
	require.NoError(t, err)
	tmpDir := filepath.Join(cogDir, "tmp")
	err = os.Mkdir(tmpDir, 0o755)
	require.NoError(t, err)

	// Create mock predict
	predictPyPath := filepath.Join(dir, "predict.py")
	handle, err := os.Create(predictPyPath)
	require.NoError(t, err)
	handle.WriteString("import cog")
	dockertest.MockCogConfig = "{\"build\":{\"python_version\":\"3.12\",\"python_packages\":[\"torch==2.5.0\",\"beautifulsoup4==4.12.3\"],\"system_packages\":[\"git\"]},\"image\":\"test\",\"predict\":\"" + predictPyPath + ":Predictor\"}"

	// Create mock weight
	data := make([]byte, 1024)
	for i := 0; i < len(data); i++ {
		data[i] = byte(i % 256)
	}
	file, err := os.Create(filepath.Join(dir, "test_weight"))
	require.NoError(t, err)
	defer file.Close()
	for i := 0; i <= ((weights.WEIGHT_FILE_SIZE_INCLUSION+1)/1024)+1; i++ {
		_, err := file.Write(data)
		require.NoError(t, err)
	}

	// Setup mock docker command
	command := dockertest.NewMockCommand()

	// Run fast push
	err = Push("r8.im/username/modelname", true, dir, command, BuildInfo{})
	require.NoError(t, err)
}
