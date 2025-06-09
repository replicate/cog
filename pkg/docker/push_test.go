package docker

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker/dockertest"
	"github.com/replicate/cog/pkg/dockercontext"
	"github.com/replicate/cog/pkg/env"
	"github.com/replicate/cog/pkg/global"
	cogHttp "github.com/replicate/cog/pkg/http"
	"github.com/replicate/cog/pkg/web"
	"github.com/replicate/cog/pkg/weights"
)

func TestPush(t *testing.T) {
	// Setup mock http server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/models/username/modelname/versions":
			output := "{\"version\":\"user/test:53c740f17ce88a61c3da5b0c20e48fd48e2da537c3a1276dec63ab11fbad6bcb\"}"
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(output))
		case "/api/models/file-challenge":
			w.WriteHeader(http.StatusOK)
			body, _ := json.Marshal(web.FileChallenge{
				Salt:   "a",
				Start:  0,
				End:    1,
				Digest: "",
				ID:     "a",
			})
			w.Write(body)
		default:
			w.WriteHeader(http.StatusConflict)
			w.Write([]byte("Hello World"))
		}
	}))
	defer server.Close()
	url, err := url.Parse(server.URL)
	require.NoError(t, err)
	t.Setenv(env.SchemeEnvVarName, url.Scheme)
	t.Setenv(env.MonobeamHostEnvVarName, url.Host)
	t.Setenv(env.WebHostEnvVarName, url.Host)

	// Create directories
	dir := t.TempDir()
	cogDir := filepath.Join(dir, global.CogBuildArtifactsFolder)
	err = os.Mkdir(cogDir, 0o755)
	require.NoError(t, err)
	err = os.Mkdir(filepath.Join(dir, TarballsDir), 0o755)
	require.NoError(t, err)
	for _, d := range []string{dockercontext.AptBuildDir} {
		tmpDir := filepath.Join(cogDir, "tmp", d)
		err = os.MkdirAll(tmpDir, 0o755)
		require.NoError(t, err)
	}

	// Create mock predict
	predictPyPath := filepath.Join(dir, "predict.py")
	handle, err := os.Create(predictPyPath)
	require.NoError(t, err)
	handle.WriteString("import cog")
	dockertest.MockCogConfig = "{\"build\":{\"python_version\":\"3.12\",\"python_packages\":[\"torch==2.5.0\",\"beautifulsoup4==4.12.3\"],\"system_packages\":[\"git\"]},\"image\":\"test\",\"predict\":\"" + predictPyPath + ":Predictor\"}"

	// Setup mock docker command
	command := dockertest.NewMockCommand()
	client, err := cogHttp.ProvideHTTPClient(t.Context(), command)
	require.NoError(t, err)

	// Run fast push
	cfg := config.DefaultConfig()
	err = Push(t.Context(), "r8.im/username/modelname", true, dir, command, BuildInfo{}, client, cfg)
	require.NoError(t, err)
}

func TestPushWithWeight(t *testing.T) {
	// Setup mock http server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/models/username/modelname/versions":
			output := "{\"version\":\"user/test:53c740f17ce88a61c3da5b0c20e48fd48e2da537c3a1276dec63ab11fbad6bcb\"}"
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(output))
		case "/api/models/file-challenge":
			w.WriteHeader(http.StatusOK)
			body, _ := json.Marshal(web.FileChallenge{
				Salt:   "a",
				Start:  0,
				End:    1,
				Digest: "",
				ID:     "a",
			})
			w.Write(body)
		default:
			w.WriteHeader(http.StatusConflict)
			w.Write([]byte("Hello World"))
		}
	}))
	defer server.Close()
	url, err := url.Parse(server.URL)
	require.NoError(t, err)
	t.Setenv(env.SchemeEnvVarName, url.Scheme)
	t.Setenv(env.MonobeamHostEnvVarName, url.Host)
	t.Setenv(env.WebHostEnvVarName, url.Host)

	// Create directories
	dir := t.TempDir()
	cogDir := filepath.Join(dir, global.CogBuildArtifactsFolder)
	err = os.Mkdir(cogDir, 0o755)
	require.NoError(t, err)
	err = os.Mkdir(filepath.Join(dir, TarballsDir), 0o755)
	require.NoError(t, err)
	for _, d := range []string{dockercontext.AptBuildDir} {
		tmpDir := filepath.Join(cogDir, "tmp", d)
		err = os.MkdirAll(tmpDir, 0o755)
		require.NoError(t, err)
	}

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
	client, err := cogHttp.ProvideHTTPClient(t.Context(), command)
	require.NoError(t, err)

	// Run fast push
	cfg := config.DefaultConfig()
	err = Push(t.Context(), "r8.im/username/modelname", true, dir, command, BuildInfo{}, client, cfg)
	require.NoError(t, err)
}
