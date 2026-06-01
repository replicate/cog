package cli

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTrainCommandIsDeprecated(t *testing.T) {
	cmd := newTrainCommand()
	require.NotEmpty(t, cmd.Deprecated, "train command should have a deprecation message")
	require.Contains(t, cmd.Deprecated, "will be removed in a future version")
}
