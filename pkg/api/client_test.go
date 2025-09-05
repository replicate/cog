package api

import (
	"archive/tar"
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/docker/dockertest"
	"github.com/replicate/cog/pkg/env"
	"github.com/replicate/cog/pkg/web"
)

func TestPostPipeline(t *testing.T) {
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
	webClient := web.NewClient(command, http.DefaultClient)

	client := NewClient(command, http.DefaultClient, webClient)
	err = client.PostNewPipeline(t.Context(), "r8.im/user/test", new(bytes.Buffer))
	require.NoError(t, err)
}

func TestPullSource(t *testing.T) {
	// Create file to pull
	dir := t.TempDir()
	predictPyPath := filepath.Join(dir, "predict.py")
	handle, err := os.Create(predictPyPath)
	require.NoError(t, err)
	handle.WriteString("import cog")
	err = handle.Close()
	require.NoError(t, err)
	info, err := os.Stat(predictPyPath)
	require.NoError(t, err)

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

	// Setup mock API server for api.replicate.com (model and source endpoints)
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models/user/test":
			// Mock model response
			versionResponse := `{"latest_version": {"id": "test-version-id"}}`
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(versionResponse))
		case "/v1/models/user/test/versions/test-version-id/source":
			// Mock source pull endpoint
			var buf bytes.Buffer
			tw := tar.NewWriter(&buf)
			header, err := tar.FileInfoHeader(info, info.Name())
			require.NoError(t, err)
			header.Name = "predict.py"
			err = tw.WriteHeader(header)
			require.NoError(t, err)
			file, err := os.Open(predictPyPath)
			require.NoError(t, err)
			defer file.Close()
			_, err = io.Copy(tw, file)
			require.NoError(t, err)
			err = tw.Close()
			require.NoError(t, err)
			w.WriteHeader(http.StatusOK)
			w.Write(buf.Bytes())
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

	// Setup mock command
	command := dockertest.NewMockCommand()
	webClient := web.NewClient(command, http.DefaultClient)

	client := NewClient(command, http.DefaultClient, webClient)
	err = client.PullSource(t.Context(), "r8.im/user/test", func(header *tar.Header, tr *tar.Reader) error {
		return nil
	})
	require.NoError(t, err)
}

func TestPullDraftSource(t *testing.T) {
	// Create file to pull
	dir := t.TempDir()
	predictPyPath := filepath.Join(dir, "predict.py")
	handle, err := os.Create(predictPyPath)
	require.NoError(t, err)
	handle.WriteString("import cog")
	err = handle.Close()
	require.NoError(t, err)
	info, err := os.Stat(predictPyPath)
	require.NoError(t, err)

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

	// Setup mock API server for api.replicate.com (model and source endpoints)
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/drafts/digest/source":
			// Mock draft source pull endpoint
			var buf bytes.Buffer
			tw := tar.NewWriter(&buf)
			header, err := tar.FileInfoHeader(info, info.Name())
			require.NoError(t, err)
			header.Name = "predict.py"
			err = tw.WriteHeader(header)
			require.NoError(t, err)
			file, err := os.Open(predictPyPath)
			require.NoError(t, err)
			defer file.Close()
			_, err = io.Copy(tw, file)
			require.NoError(t, err)
			err = tw.Close()
			require.NoError(t, err)
			w.WriteHeader(http.StatusOK)
			w.Write(buf.Bytes())
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

	// Setup mock command
	command := dockertest.NewMockCommand()
	webClient := web.NewClient(command, http.DefaultClient)

	client := NewClient(command, http.DefaultClient, webClient)
	err = client.PullSource(t.Context(), "draft:user/digest", func(header *tar.Header, tr *tar.Reader) error {
		return nil
	})
	require.NoError(t, err)
}

func TestPullSourceWithTag(t *testing.T) {
	// Create file to pull
	dir := t.TempDir()
	predictPyPath := filepath.Join(dir, "predict.py")
	handle, err := os.Create(predictPyPath)
	require.NoError(t, err)
	handle.WriteString("import cog")
	err = handle.Close()
	require.NoError(t, err)
	info, err := os.Stat(predictPyPath)
	require.NoError(t, err)

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

	// Setup mock API server for api.replicate.com (model and source endpoints)
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models/user/test/versions/12435/source":
			// Mock source pull endpoint
			var buf bytes.Buffer
			tw := tar.NewWriter(&buf)
			header, err := tar.FileInfoHeader(info, info.Name())
			require.NoError(t, err)
			header.Name = "predict.py"
			err = tw.WriteHeader(header)
			require.NoError(t, err)
			file, err := os.Open(predictPyPath)
			require.NoError(t, err)
			defer file.Close()
			_, err = io.Copy(tw, file)
			require.NoError(t, err)
			err = tw.Close()
			require.NoError(t, err)
			w.WriteHeader(http.StatusOK)
			w.Write(buf.Bytes())
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

	// Setup mock command
	command := dockertest.NewMockCommand()
	webClient := web.NewClient(command, http.DefaultClient)

	client := NewClient(command, http.DefaultClient, webClient)
	err = client.PullSource(t.Context(), "r8.im/user/test:12435", func(header *tar.Header, tr *tar.Reader) error {
		return nil
	})
	require.NoError(t, err)
}

func TestPostPipelineFails(t *testing.T) {

	type testCase struct {
		name      string
		body      string
		wantError string
	}

	for _, tt := range []testCase{
		{
			name:      "model already has versions",
			body:      "{\"detail\": \"The following errors occurred:\\n- This endpoint does not support models that have versions published with `cog push`.\",\"errors\":[{\"detail\":\"This endpoint does not support models that have versions published with `cog push`.\",\"pointer\": \"/\"}],\"status\":400,\"title\":\"Validation failed\"}",
			wantError: "This endpoint does not support models that have versions published with `cog push`.",
		},
		{
			name:      "model uses procedures",
			body:      "{\"detail\": \"The following errors occurred:\\n- You cannot use this mechanism to push versions of a model that uses pipelines.\",\"errors\":[{\"detail\":\"You cannot use this mechanism to push versions of a model that uses pipelines.\",\"pointer\": \"/\"}],\"status\":400,\"title\":\"Validation failed\"}",
			wantError: "You cannot use this mechanism to push versions of a model that uses pipelines.",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
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
					w.WriteHeader(http.StatusBadRequest)
					w.Write([]byte(tt.body))
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
			webClient := web.NewClient(command, http.DefaultClient)

			client := NewClient(command, http.DefaultClient, webClient)
			err = client.PostNewPipeline(t.Context(), "r8.im/user/test", new(bytes.Buffer))
			require.EqualError(t, err, tt.wantError)
		})
	}

}
