package doctor

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDockerCheck_RunsWithoutError(t *testing.T) {
	ctx := &CheckContext{ProjectDir: t.TempDir()}
	check := &DockerCheck{}
	// We don't assert findings — Docker may or may not be available in the test environment.
	// We just verify the check doesn't panic or return an error.
	_, err := check.Check(ctx)
	require.NoError(t, err)
}

func TestDockerCheck_FixReturnsNoAutoFix(t *testing.T) {
	check := &DockerCheck{}
	err := check.Fix(nil, nil)
	require.ErrorIs(t, err, ErrNoAutoFix)
}
