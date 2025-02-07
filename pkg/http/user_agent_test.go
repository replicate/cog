package http

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUserAgent(t *testing.T) {
	require.True(t, strings.HasPrefix(UserAgent(), "Cog/"))
}
