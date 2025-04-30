package coglog

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHostFromEnvironment(t *testing.T) {
	const testHost = "coglog"
	t.Setenv(CoglogHostEnvVarName, testHost)
	require.Equal(t, HostFromEnvironment(), testHost)
}
