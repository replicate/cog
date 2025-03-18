package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSplitPinnedPythonRequirement(t *testing.T) {
	testCases := []struct {
		input                  string
		expectedName           string
		expectedVersion        string
		expectedFindLinks      []string
		expectedExtraIndexURLs []string
		expectedError          bool
	}{
		{"package1==1.0.0", "package1", "1.0.0", nil, nil, false},
		{"package1==1.0.0+alpha", "package1", "1.0.0+alpha", nil, nil, false},
		{"--find-links=link1 --find-links=link2 package3==3.0.0", "package3", "3.0.0", []string{"link1", "link2"}, nil, false},
		{"package4==4.0.0 --extra-index-url=url1 --extra-index-url=url2", "package4", "4.0.0", nil, []string{"url1", "url2"}, false},
		{"-f link1 --find-links=link2 package5==5.0.0 --extra-index-url=url1 --extra-index-url=url2", "package5", "5.0.0", []string{"link1", "link2"}, []string{"url1", "url2"}, false},
		{"package6 --find-links=link1 --find-links=link2 --extra-index-url=url1 --extra-index-url=url2", "", "", nil, nil, true},
		{"invalid package", "", "", nil, nil, true},
		{"package8==", "", "", nil, nil, true},
		{"==8.0.0", "", "", nil, nil, true},
	}

	for _, tc := range testCases {
		name, version, findLinks, extraIndexURLs, err := SplitPinnedPythonRequirement(tc.input)

		if tc.expectedError {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
			require.Equal(t, tc.expectedName, name, "input: "+tc.input)
			require.Equal(t, tc.expectedVersion, version, "input: "+tc.input)
			require.Equal(t, tc.expectedFindLinks, findLinks, "input: "+tc.input)
			require.Equal(t, tc.expectedExtraIndexURLs, extraIndexURLs, "input: "+tc.input)
		}
	}
}
