package dockerfile

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCogletVersionFromEnvironment(t *testing.T) {
	const cogletVersion = "coglet==0.1.0-alpha17"
	t.Setenv(CogletVersionEnvVarName, cogletVersion)
	require.Equal(t, CogletVersionFromEnvironment(), cogletVersion)
}

func TestMonobaseMatrixHostFromEnvironment(t *testing.T) {
	const monobaseMatrixHost = "localhost"
	t.Setenv(MonobaseMatrixHostVarName, monobaseMatrixHost)
	require.Equal(t, MonobaseMatrixHostFromEnvironment(), monobaseMatrixHost)
}

func TestMonobaseMatrixSchemeFromEnvironment(t *testing.T) {
	const monobaseMatrixScheme = "http"
	t.Setenv(MonobaseMatrixSchemeVarName, monobaseMatrixScheme)
	require.Equal(t, MonobaseMatrixSchemeFromEnvironment(), monobaseMatrixScheme)
}
