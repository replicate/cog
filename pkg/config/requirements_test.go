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
		t.Run(tc.input, func(t *testing.T) {
			expectedReq := PythonRequirement{
				Name:           tc.expectedName,
				Version:        tc.expectedVersion,
				FindLinks:      tc.expectedFindLinks,
				ExtraIndexURLs: tc.expectedExtraIndexURLs,
			}
			req, err := SplitPinnedPythonRequirement(tc.input)

			if tc.expectedError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, expectedReq, req)
			}
		})
	}
}

func TestPythonRequirementString(t *testing.T) {
	testCases := []struct {
		name     string
		req      PythonRequirement
		expected string
	}{
		{
			name: "basic package with version",
			req: PythonRequirement{
				Name:    "package1",
				Version: "1.0.0",
			},
			expected: "package1==1.0.0",
		},
		{
			name: "package with no version",
			req: PythonRequirement{
				Name: "package2",
			},
			expected: "package2",
		},
		{
			name: "empty requirement",
			req: PythonRequirement{
				Name: "",
			},
			expected: "",
		},
		{
			name: "package with find links and extra index URLs",
			req: PythonRequirement{
				Name:           "package3",
				Version:        "3.0.0",
				FindLinks:      []string{"link1", "link2"},
				ExtraIndexURLs: []string{"url1", "url2"},
			},
			expected: "package3==3.0.0",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.req.String()
			require.Equal(t, tc.expected, result)
		})
	}
}
