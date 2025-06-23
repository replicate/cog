package procedure

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
)

func TestValidate(t *testing.T) {
	cdnServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/requirements.txt":
			// Mock requirements.txt response
			requirementsResponse := "mycustompackage==1.1.0b2"
			w.Header().Add(EtagHeader, "a")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(requirementsResponse))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer cdnServer.Close()

	cdnURL, err := url.Parse(cdnServer.URL)
	require.NoError(t, err)

	t.Setenv(env.SchemeEnvVarName, cdnURL.Scheme)
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

	cfg := config.DefaultConfig()
	requirementsPath := filepath.Join(dir, "requirements.txt")
	handle, err = os.Create(requirementsPath)
	require.NoError(t, err)
	handle.WriteString("mycustompackage>=1.0")
	handle.Close()
	cfg.Build.PythonRequirements = filepath.Base(requirementsPath)
	err = Validate(dir, client, cfg, false)
	require.NoError(t, err)
}

func TestValidateBadPythonVersion(t *testing.T) {
	cdnServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/requirements.txt":
			// Mock requirements.txt response
			requirementsResponse := "mycustompackage==1.1.0b2"
			w.Header().Add(EtagHeader, "a")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(requirementsResponse))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer cdnServer.Close()

	cdnURL, err := url.Parse(cdnServer.URL)
	require.NoError(t, err)

	t.Setenv(env.SchemeEnvVarName, cdnURL.Scheme)
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

	cfg := config.DefaultConfig()
	cfg.Build.PythonVersion = "3.10"
	err = Validate(dir, client, cfg, false)
	require.Error(t, err)
}

func TestValidateBadSystemPackage(t *testing.T) {
	cdnServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/requirements.txt":
			// Mock requirements.txt response
			requirementsResponse := "mycustompackage==1.1.0b2"
			w.Header().Add(EtagHeader, "a")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(requirementsResponse))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer cdnServer.Close()

	cdnURL, err := url.Parse(cdnServer.URL)
	require.NoError(t, err)

	t.Setenv(env.SchemeEnvVarName, cdnURL.Scheme)
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

	cfg := config.DefaultConfig()
	cfg.Build.SystemPackages = []string{"badpackage"}
	err = Validate(dir, client, cfg, false)
	require.Error(t, err)
}

func TestValidateRunCommands(t *testing.T) {
	cdnServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/requirements.txt":
			// Mock requirements.txt response
			requirementsResponse := "mycustompackage==1.1.0b2"
			w.Header().Add(EtagHeader, "a")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(requirementsResponse))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer cdnServer.Close()

	cdnURL, err := url.Parse(cdnServer.URL)
	require.NoError(t, err)

	t.Setenv(env.SchemeEnvVarName, cdnURL.Scheme)
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

	cfg := config.DefaultConfig()
	cfg.Build.Run = append(cfg.Build.Run, config.RunItem{Command: "ls -lh"})
	err = Validate(dir, client, cfg, false)
	require.Error(t, err)
}

func TestValidateGPU(t *testing.T) {
	cdnServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/requirements.txt":
			// Mock requirements.txt response
			requirementsResponse := "mycustompackage==1.1.0b2"
			w.Header().Add(EtagHeader, "a")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(requirementsResponse))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer cdnServer.Close()

	cdnURL, err := url.Parse(cdnServer.URL)
	require.NoError(t, err)

	t.Setenv(env.SchemeEnvVarName, cdnURL.Scheme)
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

	cfg := config.DefaultConfig()
	cfg.Build.GPU = true
	err = Validate(dir, client, cfg, false)
	require.Error(t, err)
}

func TestValidateCUDA(t *testing.T) {
	cdnServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/requirements.txt":
			// Mock requirements.txt response
			requirementsResponse := "mycustompackage==1.1.0b2"
			w.Header().Add(EtagHeader, "a")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(requirementsResponse))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer cdnServer.Close()

	cdnURL, err := url.Parse(cdnServer.URL)
	require.NoError(t, err)

	t.Setenv(env.SchemeEnvVarName, cdnURL.Scheme)
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

	cfg := config.DefaultConfig()
	cfg.Build.CUDA = "12.1"
	err = Validate(dir, client, cfg, false)
	require.Error(t, err)
}

func TestValidateConcurrency(t *testing.T) {
	cdnServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/requirements.txt":
			// Mock requirements.txt response
			requirementsResponse := "mycustompackage==1.1.0b2"
			w.Header().Add(EtagHeader, "a")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(requirementsResponse))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer cdnServer.Close()

	cdnURL, err := url.Parse(cdnServer.URL)
	require.NoError(t, err)

	t.Setenv(env.SchemeEnvVarName, cdnURL.Scheme)
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

	cfg := config.DefaultConfig()
	cfg.Concurrency = &config.Concurrency{Max: 12}
	err = Validate(dir, client, cfg, false)
	require.Error(t, err)
}
