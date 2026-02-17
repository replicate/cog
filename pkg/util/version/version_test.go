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

func TestVersionStripModifier(t *testing.T) {
	version := "2.3.1"
	versionWithModifier := version + "+cu118"
	versionWithoutModifier := StripModifier(versionWithModifier)
	require.Equal(t, versionWithoutModifier, version)
}

func TestVersionMatches(t *testing.T) {
	version := "2.3"
	matchVersion := "2.3.2"
	require.True(t, Matches(version, matchVersion))
}

func TestVersionMatchesModifier(t *testing.T) {
	version := "2.3"
	matchVersion := "2.3.2+cu118"
	require.True(t, Matches(version, matchVersion))
}

func TestGreaterThanOrEqualToWithInvalidPatch(t *testing.T) {
	leftVersion := "1.1.0b2"
	rightVersion := "1.1.0b2"
	require.True(t, GreaterOrEqual(leftVersion, rightVersion))
}
