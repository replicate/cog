package cli

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDebugCommandIsVisible(t *testing.T) {
	root, err := NewRootCommand()
	require.NoError(t, err)

	debugCmd, _, err := root.Find([]string{"debug"})
	require.NoError(t, err)
	require.False(t, debugCmd.Hidden)
}
