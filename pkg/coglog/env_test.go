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

func TestDisabledFromEnvironment(t *testing.T) {
	t.Setenv(CoglogDisableEnvVarName, "true")
	disabled, err := DisableFromEnvironment()
	require.NoError(t, err)
	require.True(t, disabled)
}
