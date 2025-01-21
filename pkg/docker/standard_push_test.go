package docker

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStandardPush(t *testing.T) {
	command := NewMockCommand()
	PushError = nil
	err := StandardPush("test", command)
	require.NoError(t, err)
}
