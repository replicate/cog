package cogpack

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/cogpack/core"
	"github.com/replicate/cog/pkg/config"
)

func TestIt(t *testing.T) {
	sourceInfo, err := core.NewSourceInfo("testdata/string-project", &config.Config{})
	require.NoError(t, err)
	fmt.Println(sourceInfo)
}
