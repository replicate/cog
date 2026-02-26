package schemagen

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveBinary_RejectsHTTP(t *testing.T) {
	t.Setenv(EnvVar, "http://evil.example.com/cog-schema-gen")

	_, err := ResolveBinary()
	require.Error(t, err)
	require.Contains(t, err.Error(), "HTTPS required")
}
