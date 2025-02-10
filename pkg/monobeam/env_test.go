package monobeam

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHostFromEnvironment(t *testing.T) {
	const testHost = "monobeam"
	t.Setenv(MonobeamHostEnvVarName, testHost)
	require.Equal(t, HostFromEnvironment(), testHost)
}
