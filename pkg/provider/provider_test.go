package provider

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestErrUseDockerLogin(t *testing.T) {
	require.NotNil(t, ErrUseDockerLogin)
	require.Equal(t, "use docker login", ErrUseDockerLogin.Error())
}
