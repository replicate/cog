package docker

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestImageInspectDoesNotExist(t *testing.T) {
	_, err := ImageInspect("thisimagedoesnotexist")
	require.ErrorIs(t, err, ErrNoSuchImage)
}
