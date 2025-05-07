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
		expectedEnvAndHash     string
		expectedError          bool
	}{
		{"package1==1.0.0", "package1", "1.0.0", nil, nil, "", false},
		{"package1==1.0.0+alpha", "package1", "1.0.0+alpha", nil, nil, "", false},
		{"--find-links=link1 --find-links=link2 package3==3.0.0", "package3", "3.0.0", []string{"link1", "link2"}, nil, "", false},
		{"package4==4.0.0 --extra-index-url=url1 --extra-index-url=url2", "package4", "4.0.0", nil, []string{"url1", "url2"}, "", false},
		{"-f link1 --find-links=link2 package5==5.0.0 --extra-index-url=url1 --extra-index-url=url2", "package5", "5.0.0", []string{"link1", "link2"}, []string{"url1", "url2"}, "", false},
		{"package6 --find-links=link1 --find-links=link2 --extra-index-url=url1 --extra-index-url=url2", "", "", nil, nil, "", true},
		{"invalid package", "", "", nil, nil, "", true},
		{"package8==", "", "", nil, nil, "", true},
		{"==8.0.0", "", "", nil, nil, "", true},
		{"package9==1.0.0 ; python_version >= '3.8'", "package9", "1.0.0", nil, nil, "python_version >= '3.8'", false},
		{"package10==2.0.0 ; sys_platform == 'win32' and python_version < '3.9'", "package10", "2.0.0", nil, nil, "sys_platform == 'win32' and python_version < '3.9'", false},
		{"package11==3.0.0 --find-links=link1 ; extra == 'gpu'", "package11", "3.0.0", []string{"link1"}, nil, "extra == 'gpu'", false},
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			expectedReq := PythonRequirement{
				Name:               tc.expectedName,
				Version:            tc.expectedVersion,
				FindLinks:          tc.expectedFindLinks,
				ExtraIndexURLs:     tc.expectedExtraIndexURLs,
				EnvironmentAndHash: tc.expectedEnvAndHash,
				Literal:            tc.input,
				ParsedFieldsValid:  !tc.expectedError,
			}
			req := SplitPinnedPythonRequirement(tc.input)

			if tc.expectedError {
				require.False(t, req.ParsedFieldsValid)
			} else {
				require.True(t, req.ParsedFieldsValid)
				require.Equal(t, expectedReq, req)
			}
		})
	}
}

func TestPythonRequirementNameAndVersion(t *testing.T) {
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
		{
			name: "package with environment marker",
			req: PythonRequirement{
				Name:               "package4",
				Version:            "4.0.0",
				EnvironmentAndHash: "python_version >= '3.8'",
			},
			expected: "package4==4.0.0 ; python_version >= '3.8'",
		},
		{
			name: "package with environment marker and no version",
			req: PythonRequirement{
				Name:               "package5",
				EnvironmentAndHash: "sys_platform == 'win32'",
			},
			expected: "package5 ; sys_platform == 'win32'",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.req.RequirementLine()
			require.Equal(t, tc.expected, result)
		})
	}
}

func TestPythonRequirementsRequirementsFileContent(t *testing.T) {
	testCases := []struct {
		name     string
		reqs     PythonRequirements
		expected string
	}{
		{
			name:     "empty requirements",
			reqs:     PythonRequirements{},
			expected: "",
		},
		{
			name: "single package without find links or extra index urls",
			reqs: PythonRequirements{
				{
					Name:    "package1",
					Version: "1.0.0",
				},
			},
			expected: "package1==1.0.0",
		},
		{
			name: "multiple packages with find links",
			reqs: PythonRequirements{
				{
					Name:      "package1",
					Version:   "1.0.0",
					FindLinks: []string{"link1"},
				},
				{
					Name:      "package2",
					Version:   "2.0.0",
					FindLinks: []string{"link2"},
				},
			},
			expected: `--find-links link1
--find-links link2
package1==1.0.0
package2==2.0.0`,
		},
		{
			name: "multiple packages with extra index urls",
			reqs: PythonRequirements{
				{
					Name:           "package1",
					Version:        "1.0.0",
					ExtraIndexURLs: []string{"url1"},
				},
				{
					Name:           "package2",
					Version:        "2.0.0",
					ExtraIndexURLs: []string{"url2"},
				},
			},
			expected: `--extra-index-url url1
--extra-index-url url2
package1==1.0.0
package2==2.0.0`,
		},
		{
			name: "multiple packages with both find links and extra index urls",
			reqs: PythonRequirements{
				{
					Name:           "package1",
					Version:        "1.0.0",
					FindLinks:      []string{"link1"},
					ExtraIndexURLs: []string{"url1"},
				},
				{
					Name:           "package2",
					Version:        "2.0.0",
					FindLinks:      []string{"link2"},
					ExtraIndexURLs: []string{"url2"},
				},
			},
			expected: `--find-links link1
--find-links link2
--extra-index-url url1
--extra-index-url url2
package1==1.0.0
package2==2.0.0`,
		},
		{
			name: "duplicate find links and extra index urls",
			reqs: PythonRequirements{
				{
					Name:           "package1",
					Version:        "1.0.0",
					FindLinks:      []string{"link1", "link1"},
					ExtraIndexURLs: []string{"url1", "url1"},
				},
				{
					Name:           "package2",
					Version:        "2.0.0",
					FindLinks:      []string{"link1"},
					ExtraIndexURLs: []string{"url1"},
				},
			},
			expected: `--find-links link1
--extra-index-url url1
package1==1.0.0
package2==2.0.0`,
		},
		{
			name: "packages without versions",
			reqs: PythonRequirements{
				{
					Name: "package1",
				},
				{
					Name: "package2",
				},
			},
			expected: `package1
package2`,
		},
		{
			name: "packages with environment markers",
			reqs: PythonRequirements{
				{
					Name:               "package1",
					Version:            "1.0.0",
					EnvironmentAndHash: "python_version >= '3.8'",
				},
				{
					Name:               "package2",
					Version:            "2.0.0",
					EnvironmentAndHash: "sys_platform == 'win32'",
				},
			},
			expected: `package1==1.0.0 ; python_version >= '3.8'
package2==2.0.0 ; sys_platform == 'win32'`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.reqs.RequirementsFileContent()
			require.Equal(t, tc.expected, result)
		})
	}
}
