package docker

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker/dockertest"
	"github.com/replicate/cog/pkg/env"
	cogHttp "github.com/replicate/cog/pkg/http"
	"github.com/replicate/cog/pkg/web"
)

func TestPipelinePush(t *testing.T) {
	// Setup mock http server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		output := "{\"version\":\"user/test:53c740f17ce88a61c3da5b0c20e48fd48e2da537c3a1276dec63ab11fbad6bcb\"}"
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(output))
		w.Header().Add(EtagHeader, "a")
	}))
	defer server.Close()
	url, err := url.Parse(server.URL)
	require.NoError(t, err)
	t.Setenv(env.SchemeEnvVarName, url.Scheme)
	t.Setenv(env.WebHostEnvVarName, url.Host)

	dir := t.TempDir()

	// Create mock predict
	predictPyPath := filepath.Join(dir, "predict.py")
	handle, err := os.Create(predictPyPath)
	require.NoError(t, err)
	handle.WriteString("import cog")
	dockertest.MockCogConfig = "{\"build\":{\"python_version\":\"3.12\",\"python_packages\":[\"torch==2.5.0\",\"beautifulsoup4==4.12.3\"],\"system_packages\":[\"git\"]},\"image\":\"test\",\"predict\":\"" + predictPyPath + ":Predictor\"}"

	// Setup mock command
	command := dockertest.NewMockCommand()
	client, err := cogHttp.ProvideHTTPClient(t.Context(), command)
	require.NoError(t, err)
	webClient := web.NewClient(command, client)

	cfg := config.DefaultConfig()
	err = PipelinePush(t.Context(), "r8.im/username/modelname", dir, webClient, client, cfg)
	require.NoError(t, err)
}

func TestPipelinePushFailWithExtraRequirements(t *testing.T) {
	// Setup mock http server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(EtagHeader, "a")
		output := "apipelinepackage==1.0.0"
		w.Write([]byte(output))
	}))
	defer server.Close()
	url, err := url.Parse(server.URL)
	require.NoError(t, err)
	t.Setenv(env.SchemeEnvVarName, url.Scheme)
	t.Setenv(env.PipelinesRuntimeHostEnvVarName, url.Host)

	dir := t.TempDir()

	// Create mock predict
	predictPyPath := filepath.Join(dir, "predict.py")
	handle, err := os.Create(predictPyPath)
	require.NoError(t, err)
	handle.WriteString("import cog")
	handle.Close()
	dockertest.MockCogConfig = "{\"build\":{\"python_version\":\"3.12\",\"python_packages\":[\"torch==2.5.0\",\"beautifulsoup4==4.12.3\"],\"system_packages\":[\"git\"]},\"image\":\"test\",\"predict\":\"" + predictPyPath + ":Predictor\"}"

	// Setup mock command
	command := dockertest.NewMockCommand()
	client, err := cogHttp.ProvideHTTPClient(t.Context(), command)
	require.NoError(t, err)
	webClient := web.NewClient(command, client)

	cfg := config.DefaultConfig()
	requirementsPath := filepath.Join(dir, "requirements.txt")
	handle, err = os.Create(requirementsPath)
	require.NoError(t, err)
	handle.WriteString("mycustompackage==1.0.0")
	handle.Close()
	cfg.Build.PythonRequirements = filepath.Base(requirementsPath)
	err = PipelinePush(t.Context(), "r8.im/username/modelname", dir, webClient, client, cfg)
	require.Error(t, err)
}
