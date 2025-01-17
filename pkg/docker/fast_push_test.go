package docker

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFastPush(t *testing.T) {
	dir := t.TempDir()
	err := FastPush("test", dir)
	require.NoError(t, err)
}
