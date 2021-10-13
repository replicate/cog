package docker

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestImageExistsFalse(t *testing.T) {
	exists, err := ImageExists("thisimagedoesnotexist")
	require.NoError(t, err)
	require.False(t, exists)
}
