package env

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSchemeFromEnvironment(t *testing.T) {
	const testScheme = "myscheme"
	t.Setenv(SchemeEnvVarName, "myscheme")
	require.Equal(t, SchemeFromEnvironment(), testScheme)
}

func TestMonobeamHostFromEnvironment(t *testing.T) {
	const testHost = "monobeam"
	t.Setenv(MonobeamHostEnvVarName, testHost)
	require.Equal(t, MonobeamHostFromEnvironment(), testHost)
}

func TestWebHostFromEnvironment(t *testing.T) {
	const testHost = "web"
	t.Setenv(WebHostEnvVarName, testHost)
	require.Equal(t, WebHostFromEnvironment(), testHost)
}
