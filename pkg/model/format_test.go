package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOCIIndexEnabled_Default(t *testing.T) {
	t.Setenv("COG_OCI_INDEX", "")
	require.False(t, OCIIndexEnabled())
}

func TestOCIIndexEnabled_Enabled(t *testing.T) {
	t.Setenv("COG_OCI_INDEX", "1")
	require.True(t, OCIIndexEnabled())
}

func TestOCIIndexEnabled_OtherValue(t *testing.T) {
	t.Setenv("COG_OCI_INDEX", "0")
	require.False(t, OCIIndexEnabled())
}
