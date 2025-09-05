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
