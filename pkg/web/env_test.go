package web

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHostFromEnvironment(t *testing.T) {
	const testHost = "web"
	t.Setenv(WebHostEnvVarName, testHost)
	require.Equal(t, HostFromEnvironment(), testHost)
}
