package dockerfile

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/docker/dockertest"
	r8HTTP "github.com/replicate/cog/pkg/http"
)

func TestMonobaseMatrixDefaultCUDA(t *testing.T) {
	// Setup mock command
	command := dockertest.NewMockCommand()

	// Setup http client
	httpClient, err := r8HTTP.ProvideHTTPClient(t.Context(), command)
	require.NoError(t, err)

	monobaseMatrix, err := NewMonobaseMatrix(httpClient)
	require.NoError(t, err)

	defaultCuda := monobaseMatrix.DefaultCUDAVersion("2.7.0")
	require.Equal(t, defaultCuda, "12.8")
}
