package compat

import (
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/stretchr/testify/require"
)

func TestParseVersion(t *testing.T) {
	v, err := semver.NewVersion("22.04")
	require.NoError(t, err)
	require.Equal(t, v.String(), "22.4.0")
}
