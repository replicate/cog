package monobeam

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/vbauerster/mpb/v8"

	"github.com/replicate/cog/pkg/docker/dockertest"
	"github.com/replicate/cog/pkg/env"
	r8HTTP "github.com/replicate/cog/pkg/http"
	"github.com/replicate/cog/pkg/weights"
)

func TestUploadFile(t *testing.T) {
	// Setup mock http server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, r8HTTP.UserAgent(), r.Header.Get(r8HTTP.UserAgentHeader))
		w.WriteHeader(http.StatusConflict)
	}))
	defer server.Close()
	url, err := url.Parse(server.URL)
	require.NoError(t, err)
	t.Setenv(env.SchemeEnvVarName, url.Scheme)
	t.Setenv(MonobeamHostEnvVarName, url.Host)

	dir := t.TempDir()

	// Create mock weight
	data := make([]byte, 1024)
	for i := 0; i < len(data); i++ {
		data[i] = byte(i % 256)
	}
	weightPath := filepath.Join(dir, "test_weight")
	file, err := os.Create(weightPath)
	require.NoError(t, err)
	defer file.Close()
	for i := 0; i <= ((weights.WEIGHT_FILE_SIZE_INCLUSION+1)/1024)+1; i++ {
		_, err := file.Write(data)
		require.NoError(t, err)
	}

	// Setup mock command
	command := dockertest.NewMockCommand()

	// Setup http client
	httpClient, err := r8HTTP.ProvideHTTPClient(command)
	require.NoError(t, err)

	client := NewClient(httpClient)
	ctx := context.Background()
	p := mpb.New(
		mpb.WithRefreshRate(180 * time.Millisecond),
	)
	err = client.UploadFile(ctx, "weights", "111", weightPath, p, "weights - "+weightPath)
	require.NoError(t, err)
}
