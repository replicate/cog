package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker/dockertest"
	"github.com/replicate/cog/pkg/env"
)

func TestPostNewVersion(t *testing.T) {
	// Setup mock http server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		output := "{\"version\":\"user/test:53c740f17ce88a61c3da5b0c20e48fd48e2da537c3a1276dec63ab11fbad6bcb\"}"
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(output))
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

	client := NewClient(command, http.DefaultClient)
	err = client.PostNewVersion(t.Context(), "r8.im/user/test", []File{}, []File{}, nil)
	require.NoError(t, err)
}

func TestVersionFromManifest(t *testing.T) {
	// Setup mock command
	command := dockertest.NewMockCommand()

	// Create mock predict
	dir := t.TempDir()
	predictPyPath := filepath.Join(dir, "predict.py")
	handle, err := os.Create(predictPyPath)
	require.NoError(t, err)
	handle.WriteString("import cog")
	dockertest.MockCogConfig = "{\"build\":{\"python_version\":\"3.12\",\"python_packages\":[\"torch==2.5.0\",\"beautifulsoup4==4.12.3\"],\"system_packages\":[\"git\"]},\"image\":\"test\",\"predict\":\"" + predictPyPath + ":Predictor\"}"
	dockertest.MockOpenAPISchema = "{\"test\": true}"

	client := NewClient(command, http.DefaultClient)
	version, err := client.versionFromManifest(t.Context(), "r8.im/user/test", []File{}, []File{}, nil)
	require.NoError(t, err)

	var openAPISchema map[string]any
	err = json.Unmarshal([]byte(dockertest.MockOpenAPISchema), &openAPISchema)
	require.NoError(t, err)

	var cogConfig config.Config
	err = json.Unmarshal([]byte(dockertest.MockCogConfig), &cogConfig)
	require.NoError(t, err)

	require.Equal(t, openAPISchema, version.OpenAPISchema)
	require.Equal(t, cogConfig, version.CogConfig)
}

func TestVersionURLErrorWithoutR8IMPrefix(t *testing.T) {
	_, err := newVersionURL("docker.com/thing/thing")
	require.Error(t, err)
}

func TestVersionURLErrorWithout3Components(t *testing.T) {
	_, err := newVersionURL("username/test")
	require.Error(t, err)
}

func TestDoFileChallenge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.tmp")
	d1 := []byte("hello\nreplicate\nhello\n")
	err := os.WriteFile(path, d1, 0o644)
	require.NoError(t, err)

	path2 := filepath.Join(dir, "test2.tmp")
	d2 := []byte("hello\nreplicate\nhello\n")
	err = os.WriteFile(path2, d2, 0o644)
	require.NoError(t, err)

	files := []File{
		{
			Path:   path,
			Digest: "abc",
			Size:   22,
		},
	}
	weights := []File{
		{
			Path:   path,
			Digest: "def",
			Size:   22,
		},
	}

	abcChallenge := FileChallenge{
		ID:     "abc",
		Digest: "abc",
		Start:  0,
		End:    6,
		Salt:   "go\n",
	}

	defChallenge := FileChallenge{
		ID:     "def",
		Digest: "def",
		Start:  16,
		End:    22,
		Salt:   "go\n",
	}

	// Setup mock http server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		var challengeRequest FileChallengeRequest
		// Ignore errors - make sure the test is set up correctly
		json.NewDecoder(r.Body).Decode(&challengeRequest)
		if challengeRequest.Digest == "abc" {
			body, _ := json.Marshal(abcChallenge)
			w.Write(body)
		} else {
			body, _ := json.Marshal(defChallenge)
			w.Write(body)
		}
	}))
	defer server.Close()
	url, err := url.Parse(server.URL)
	require.NoError(t, err)
	t.Setenv(env.SchemeEnvVarName, url.Scheme)
	t.Setenv(env.WebHostEnvVarName, url.Host)

	// Setup mock command
	command := dockertest.NewMockCommand()
	client := NewClient(command, http.DefaultClient)
	response, err := client.InitiateAndDoFileChallenge(t.Context(), weights, files)
	require.NoError(t, err)
	assert.ElementsMatch(t, response, []FileChallengeAnswer{
		{
			ChallengeID: "abc",
			Digest:      "abc",
			Hash:        "43d250d92b5dbb47f75208de8e9a9a321d23e85eed0dc3d5dfa83bc3cc5aa68c",
		},
		{
			ChallengeID: "def",
			Digest:      "def",
			Hash:        "43d250d92b5dbb47f75208de8e9a9a321d23e85eed0dc3d5dfa83bc3cc5aa68c",
		},
	})
}

func TestFetchToken(t *testing.T) {
	// Setup mock http server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	defer server.Close()
	url, err := url.Parse(server.URL)
	require.NoError(t, err)
	t.Setenv(env.SchemeEnvVarName, url.Scheme)
	t.Setenv(env.WebHostEnvVarName, url.Host)

	// Setup mock command
	command := dockertest.NewMockCommand()

	client := NewClient(command, http.DefaultClient)
	token, err := client.FetchAPIToken(t.Context(), "user")
	require.NoError(t, err)
	require.Equal(t, "test-api-token", token)
}
