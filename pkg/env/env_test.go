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
