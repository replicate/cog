package path

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTrimExt(t *testing.T) {
	path := TrimExt("/mydir/myoutput.bmp")
	require.Equal(t, path, "/mydir/myoutput")
}

func TestIsExtInteger(t *testing.T) {
	require.True(t, IsExtInteger(".0"))
}
