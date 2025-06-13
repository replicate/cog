package path

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTrimExt(t *testing.T) {
	path := TrimExt("/mydir/myoutput.bmp")
	require.Equal(t, path, "/mydir/myoutput")
}
