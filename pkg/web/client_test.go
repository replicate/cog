package web

import (
	"context"
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
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()
	url, err := url.Parse(server.URL)
	require.NoError(t, err)
	t.Setenv(env.SchemeEnvVarName, url.Scheme)
	t.Setenv(WebHostEnvVarName, url.Host)

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
	ctx := context.Background()
	err = client.PostNewVersion(ctx, "r8.im/user/test", []File{}, []File{})
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
	version, err := client.versionFromManifest("r8.im/user/test", []File{}, []File{})
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

	config := RuntimeConfig{
		Files: []File{
			File{
				Path:   path,
				Digest: "abc",
				Size:   22,
			},
		},
		Weights: []File{
			File{
				Path:   path,
				Digest: "def",
				Size:   22,
			},
		},
	}

	query := FileChallengeQuery{
		ID: "abcdef",
		Challenges: []FileChallenge{
			{
				Digest: "abc",
				Start:  0,
				End:    6,
				Salt:   "go\n",
			},
			{
				Digest: "def",
				Start:  16,
				End:    22,
				Salt:   "go\n",
			},
		},
	}

	response, err := doFileChallenges(query, config)
	require.NoError(t, err)
	assert.Equal(t, response.ID, query.ID)
	assert.ElementsMatch(t, response.Challenges, []FileChallengeAnswer{
		{
			Digest: "abc",
			Hash:   "43d250d92b5dbb47f75208de8e9a9a321d23e85eed0dc3d5dfa83bc3cc5aa68c",
		},
		{
			Digest: "def",
			Hash:   "43d250d92b5dbb47f75208de8e9a9a321d23e85eed0dc3d5dfa83bc3cc5aa68c",
		},
	})
}
