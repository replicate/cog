package schemagen

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveBinary_RejectsHTTP(t *testing.T) {
	t.Setenv(EnvVar, "http://evil.example.com/cog-schema-gen")

	_, err := ResolveBinary()
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be a local file path")
}

func TestResolveBinary_RejectsHTTPS(t *testing.T) {
	t.Setenv(EnvVar, "https://example.com/cog-schema-gen")

	_, err := ResolveBinary()
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be a local file path")
}
