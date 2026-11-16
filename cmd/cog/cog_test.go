package main

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/cli"
)

func TestExitCode(t *testing.T) {
	require.Equal(t, 2, exitCode(cli.ErrScanFindings))
	require.Equal(t, 1, exitCode(errors.New("incomplete scan")))
}
