package docker

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/api"
	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker/dockertest"
	"github.com/replicate/cog/pkg/env"
	cogHttp "github.com/replicate/cog/pkg/http"
	"github.com/replicate/cog/pkg/procedure"
	"github.com/replicate/cog/pkg/web"
)

func TestPipelinePush(t *testing.T) {
	// Setup mock web server for cog.replicate.com (token exchange)
	webServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/token/user":
			// Mock token exchange response
			//nolint:gosec
			tokenResponse := `{
				"keys": {
					"cog": {
						"key": "test-api-token",
						"expires_at": "2024-12-31T23:59:59Z"
					}
				}
			}`
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(tokenResponse))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer webServer.Close()

	// Setup mock API server for api.replicate.com (version and release endpoints)
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models/user/test/versions":
			// Mock version creation response
			versionResponse := `{"id": "test-version-id"}`
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(versionResponse))
		case "/v1/models/user/test/releases":
			// Mock release creation response - empty body with 204 status
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer apiServer.Close()

	webURL, err := url.Parse(webServer.URL)
	require.NoError(t, err)
	apiURL, err := url.Parse(apiServer.URL)
	require.NoError(t, err)

	t.Setenv(env.SchemeEnvVarName, webURL.Scheme)
	t.Setenv(env.WebHostEnvVarName, webURL.Host)
	t.Setenv(env.APIHostEnvVarName, apiURL.Host)

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
	apiClient := api.NewClient(command, client, webClient)

	cfg := config.DefaultConfig()
	err = PipelinePush(t.Context(), "r8.im/user/test", dir, apiClient, client, cfg)
	require.NoError(t, err)
}

func TestPipelinePushFailWithExtraRequirements(t *testing.T) {
	t.Skip("Skipping for now, requirements.txt is always overwritten, and hopefully we replace that with support for custom requirements, if not this test comes back")
	// Setup mock web server for cog.replicate.com (token exchange)
	webServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/token/user":
			// Mock token exchange response
			//nolint:gosec
			tokenResponse := `{
				"keys": {
					"cog": {
						"key": "test-api-token",
						"expires_at": "2024-12-31T23:59:59Z"
					}
				}
			}`
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(tokenResponse))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer webServer.Close()

	// Setup mock API server for api.replicate.com (version and release endpoints)
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models/user/test/versions":
			// Mock version creation response
			versionResponse := `{"id": "test-version-id"}`
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(versionResponse))
		case "/v1/models/user/test/releases":
			// Mock release creation response - empty body with 204 status
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer apiServer.Close()

	webURL, err := url.Parse(webServer.URL)
	require.NoError(t, err)
	apiURL, err := url.Parse(apiServer.URL)
	require.NoError(t, err)

	t.Setenv(env.SchemeEnvVarName, webURL.Scheme)
	t.Setenv(env.WebHostEnvVarName, webURL.Host)
	t.Setenv(env.APIHostEnvVarName, apiURL.Host)

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
	apiClient := api.NewClient(command, client, webClient)

	cfg := config.DefaultConfig()
	requirementsPath := filepath.Join(dir, "requirements.txt")
	handle, err = os.Create(requirementsPath)
	require.NoError(t, err)
	handle.WriteString("mycustompackage==1.0.0")
	handle.Close()
	cfg.Build.PythonRequirements = filepath.Base(requirementsPath)
	err = PipelinePush(t.Context(), "r8.im/user/test", dir, apiClient, client, cfg)
	require.Error(t, err)
}

func TestPipelinePushSuccessWithBetaPatch(t *testing.T) {
	// Setup mock web server for cog.replicate.com (token exchange)
	webServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/token/user":
			// Mock token exchange response
			//nolint:gosec
			tokenResponse := `{
				"keys": {
					"cog": {
						"key": "test-api-token",
						"expires_at": "2024-12-31T23:59:59Z"
					}
				}
			}`
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(tokenResponse))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer webServer.Close()

	// Setup mock API server for api.replicate.com (version and release endpoints)
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models/user/test/versions":
			// Mock version creation response
			versionResponse := `{"id": "test-version-id"}`
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(versionResponse))
		case "/v1/models/user/test/releases":
			// Mock release creation response - empty body with 204 status
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer apiServer.Close()

	cdnServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/requirements.txt":
			// Mock requirements.txt response
			requirementsResponse := "mycustompackage==1.1.0b2"
			w.Header().Add(procedure.EtagHeader, "a")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(requirementsResponse))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer cdnServer.Close()

	webURL, err := url.Parse(webServer.URL)
	require.NoError(t, err)
	apiURL, err := url.Parse(apiServer.URL)
	require.NoError(t, err)
	cdnURL, err := url.Parse(cdnServer.URL)
	require.NoError(t, err)

	t.Setenv(env.SchemeEnvVarName, webURL.Scheme)
	t.Setenv(env.WebHostEnvVarName, webURL.Host)
	t.Setenv(env.APIHostEnvVarName, apiURL.Host)
	t.Setenv(env.PipelinesRuntimeHostEnvVarName, cdnURL.Host)

	dir := t.TempDir()

	// Create mock predict
	predictPyPath := filepath.Join(dir, "predict.py")
	handle, err := os.Create(predictPyPath)
	require.NoError(t, err)
	handle.WriteString("import cog")
	handle.Close()
	dockertest.MockCogConfig = "{\"build\":{\"python_version\":\"3.12\",\"python_packages\":[\"torch==2.5.0\",\"mycustompackage==1.1.0b2\"],\"system_packages\":[\"git\"]},\"image\":\"test\",\"predict\":\"" + predictPyPath + ":Predictor\"}"

	// Setup mock command
	command := dockertest.NewMockCommand()
	client, err := cogHttp.ProvideHTTPClient(t.Context(), command)
	require.NoError(t, err)
	webClient := web.NewClient(command, client)
	apiClient := api.NewClient(command, client, webClient)

	cfg := config.DefaultConfig()
	requirementsPath := filepath.Join(dir, "requirements.txt")
	handle, err = os.Create(requirementsPath)
	require.NoError(t, err)
	handle.WriteString("mycustompackage==1.1.0b2")
	handle.Close()
	cfg.Build.PythonRequirements = filepath.Base(requirementsPath)
	err = PipelinePush(t.Context(), "r8.im/user/test", dir, apiClient, client, cfg)
	require.NoError(t, err)
}

func TestPipelinePushSuccessWithAlphaPatch(t *testing.T) {
	// Setup mock web server for cog.replicate.com (token exchange)
	webServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/token/user":
			// Mock token exchange response
			//nolint:gosec
			tokenResponse := `{
				"keys": {
					"cog": {
						"key": "test-api-token",
						"expires_at": "2024-12-31T23:59:59Z"
					}
				}
			}`
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(tokenResponse))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer webServer.Close()

	// Setup mock API server for api.replicate.com (version and release endpoints)
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models/user/test/versions":
			// Mock version creation response
			versionResponse := `{"id": "test-version-id"}`
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(versionResponse))
		case "/v1/models/user/test/releases":
			// Mock release creation response - empty body with 204 status
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer apiServer.Close()

	cdnServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/requirements.txt":
			// Mock requirements.txt response
			requirementsResponse := "mycustompackage==1.1.0b2"
			w.Header().Add(procedure.EtagHeader, "a")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(requirementsResponse))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer cdnServer.Close()

	webURL, err := url.Parse(webServer.URL)
	require.NoError(t, err)
	apiURL, err := url.Parse(apiServer.URL)
	require.NoError(t, err)
	cdnURL, err := url.Parse(cdnServer.URL)
	require.NoError(t, err)

	t.Setenv(env.SchemeEnvVarName, webURL.Scheme)
	t.Setenv(env.WebHostEnvVarName, webURL.Host)
	t.Setenv(env.APIHostEnvVarName, apiURL.Host)
	t.Setenv(env.PipelinesRuntimeHostEnvVarName, cdnURL.Host)

	dir := t.TempDir()

	// Create mock predict
	predictPyPath := filepath.Join(dir, "predict.py")
	handle, err := os.Create(predictPyPath)
	require.NoError(t, err)
	handle.WriteString("import cog")
	handle.Close()
	dockertest.MockCogConfig = "{\"build\":{\"python_version\":\"3.12\",\"python_packages\":[\"torch==2.5.0\",\"mycustompackage>=1.0\"],\"system_packages\":[\"git\"]},\"image\":\"test\",\"predict\":\"" + predictPyPath + ":Predictor\"}"

	// Setup mock command
	command := dockertest.NewMockCommand()
	client, err := cogHttp.ProvideHTTPClient(t.Context(), command)
	require.NoError(t, err)
	webClient := web.NewClient(command, client)
	apiClient := api.NewClient(command, client, webClient)

	cfg := config.DefaultConfig()
	requirementsPath := filepath.Join(dir, "requirements.txt")
	handle, err = os.Create(requirementsPath)
	require.NoError(t, err)
	handle.WriteString("mycustompackage>=1.0")
	handle.Close()
	cfg.Build.PythonRequirements = filepath.Base(requirementsPath)
	err = PipelinePush(t.Context(), "r8.im/user/test", dir, apiClient, client, cfg)
	require.NoError(t, err)
}

func TestPipelinePushSuccessWithURLInstallPath(t *testing.T) {
	// Setup mock web server for cog.replicate.com (token exchange)
	webServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/token/user":
			// Mock token exchange response
			//nolint:gosec
			tokenResponse := `{
				"keys": {
					"cog": {
						"key": "test-api-token",
						"expires_at": "2024-12-31T23:59:59Z"
					}
				}
			}`
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(tokenResponse))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer webServer.Close()

	// Setup mock API server for api.replicate.com (version and release endpoints)
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models/user/test/versions":
			// Mock version creation response
			versionResponse := `{"id": "test-version-id"}`
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(versionResponse))
		case "/v1/models/user/test/releases":
			// Mock release creation response - empty body with 204 status
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer apiServer.Close()

	cdnServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/requirements.txt":
			// Mock requirements.txt response
			requirementsResponse := "mycustompackage==1.1.0b2\ncoglet @ https://github.com/replicate/cog-runtime/releases/download/v0.1.0-alpha29/coglet-0.1.0a29-py3-none-any.whl"
			w.Header().Add(procedure.EtagHeader, "a")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(requirementsResponse))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer cdnServer.Close()

	webURL, err := url.Parse(webServer.URL)
	require.NoError(t, err)
	apiURL, err := url.Parse(apiServer.URL)
	require.NoError(t, err)
	cdnURL, err := url.Parse(cdnServer.URL)
	require.NoError(t, err)

	t.Setenv(env.SchemeEnvVarName, webURL.Scheme)
	t.Setenv(env.WebHostEnvVarName, webURL.Host)
	t.Setenv(env.APIHostEnvVarName, apiURL.Host)
	t.Setenv(env.PipelinesRuntimeHostEnvVarName, cdnURL.Host)

	dir := t.TempDir()

	// Create mock predict
	predictPyPath := filepath.Join(dir, "predict.py")
	handle, err := os.Create(predictPyPath)
	require.NoError(t, err)
	handle.WriteString("import cog")
	handle.Close()
	dockertest.MockCogConfig = "{\"build\":{\"python_version\":\"3.12\",\"python_packages\":[\"torch==2.5.0\",\"mycustompackage>=1.0\"],\"system_packages\":[\"git\"]},\"image\":\"test\",\"predict\":\"" + predictPyPath + ":Predictor\"}"

	// Setup mock command
	command := dockertest.NewMockCommand()
	client, err := cogHttp.ProvideHTTPClient(t.Context(), command)
	require.NoError(t, err)
	webClient := web.NewClient(command, client)
	apiClient := api.NewClient(command, client, webClient)

	cfg := config.DefaultConfig()
	requirementsPath := filepath.Join(dir, "requirements.txt")
	handle, err = os.Create(requirementsPath)
	require.NoError(t, err)
	handle.WriteString("mycustompackage>=1.0")
	handle.Close()
	cfg.Build.PythonRequirements = filepath.Base(requirementsPath)
	err = PipelinePush(t.Context(), "r8.im/user/test", dir, apiClient, client, cfg)
	require.NoError(t, err)
}
