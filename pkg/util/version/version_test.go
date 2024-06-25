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
		{"1.0.0", "1.0.0", true},
		{"1.0.0+foo", "1.0.0", false},
		{"11.2", "11.2.0", true},
		{"1", "2", false},
		{"1", "0", false},
		{"1.1", "1", false},
		{"1.0.1", "1", false},
		{"1.1.0", "1", false},
	} {
		not := ""
		if tt.equal {
			not = "not "
		}
		require.Equal(t, tt.equal, Equal(tt.v1, tt.v2), "%s is %sequal to %s", tt.v1, not, tt.v2)
	}
}

func TestVersionGreater(t *testing.T) {
	for _, tt := range []struct {
		v1      string
		v2      string
		greater bool
	}{
		{"1", "1", false},
		{"1.0", "1", false},
		{"1", "1.0", false},
		{"1.0.0", "1", false},
		{"1.0.0", "1.0", false},
		{"11.2", "11.2.0", false},
		{"1", "2", false},
		{"1", "0", true},
		{"1.1", "1", true},
		{"1.0.1", "1", true},
		{"1.1.0", "1", true},
		{"1.0.0+foo", "1", false},
	} {
		not := ""
		if tt.greater {
			not = "not "
		}
		require.Equal(t, tt.greater, Greater(tt.v1, tt.v2), "%s is %sgreater than %s", tt.v1, not, tt.v2)
	}
}
