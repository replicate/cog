package version

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVersionEqual(t *testing.T) {
	for _, tt := range []struct {
		v1    string
		v2    string
		equal bool
	}{
		{"1", "1", true},
		{"1.0", "1", true},
		{"1", "1.0", true},
		{"1.0.0", "1", true},
		{"1.0.0", "1.0", true},
		{"11.2", "11.2.0", true},
		{"1", "2", false},
		{"1", "0", false},
		{"1.1", "1", false},
		{"1.0.1", "1", false},
		{"1.1.0", "1", false},
	} {
		require.Equal(t, tt.equal, Equal(tt.v1, tt.v2))
	}
}
