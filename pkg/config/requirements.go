package config

import (
	"fmt"
	"regexp"
)

// SplitPinnedPythonRequirement returns the name, version, findLinks, and extraIndexURLs from a requirements.txt line
// in the form name==version [--find-links=<findLink>] [-f <findLink>] [--extra-index-url=<extraIndexURL>]
func SplitPinnedPythonRequirement(requirement string) (name string, version string, findLinks []string, extraIndexURLs []string, err error) {
	pinnedPackageRe := regexp.MustCompile(`(?:([a-zA-Z0-9\-_]+)==([^ ]+)|--find-links=([^\s]+)|-f\s+([^\s]+)|--extra-index-url=([^\s]+))`)

	matches := pinnedPackageRe.FindAllStringSubmatch(requirement, -1)
	if matches == nil {
		return "", "", nil, nil, fmt.Errorf("Package %s is not in the expected format", requirement)
	}

	nameFound := false
	versionFound := false

	for _, match := range matches {
		if match[1] != "" {
			name = match[1]
			nameFound = true
		}

		if match[2] != "" {
			version = match[2]
			versionFound = true
		}

		if match[3] != "" {
			findLinks = append(findLinks, match[3])
		}

		if match[4] != "" {
			findLinks = append(findLinks, match[4])
		}

		if match[5] != "" {
			extraIndexURLs = append(extraIndexURLs, match[5])
		}
	}

	if !nameFound || !versionFound {
		return "", "", nil, nil, fmt.Errorf("Package name or version is missing in %s", requirement)
	}

	return name, version, findLinks, extraIndexURLs, nil
}

func PackageName(pipRequirement string) (string, error) {
	name, _, _, _, err := SplitPinnedPythonRequirement(pipRequirement)
	return name, err
}
