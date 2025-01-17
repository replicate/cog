package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
)

func GenerateRequirements(tmpDir string, config *Config) (string, error) {
	// Deduplicate packages between the requirements.txt and the python packages directive.
	packageNames := make(map[string]string)

	// Read the python packages configuration.
	for _, requirement := range config.Build.PythonPackages {
		packageName, err := PackageName(requirement)
		if err != nil {
			return "", err
		}
		packageNames[packageName] = requirement
	}

	// Read the python requirements.
	if config.Build.PythonRequirements != "" {
		fh, err := os.Open(config.Build.PythonRequirements)
		if err != nil {
			return "", err
		}
		scanner := bufio.NewScanner(fh)
		for scanner.Scan() {
			requirement := scanner.Text()
			packageName, err := PackageName(requirement)
			if err != nil {
				return "", err
			}
			packageNames[packageName] = requirement
		}
	}

	// Workaround for fastapi in base image not working with pydantic 2
	// https://github.com/replicate/cog/issues/2112
	if _, ok := packageNames["fastapi"]; !ok {
		packageNames["fastapi"] = ">0.100.0,<0.111.0"
	}

	// If we don't have any packages skip further processing
	if len(packageNames) == 0 {
		return "", nil
	}

	// Sort the package names by alphabetical order.
	keys := make([]string, 0, len(packageNames))
	for k := range packageNames {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Render the expected contents
	requirementsContent := ""
	for _, k := range keys {
		requirementsContent += packageNames[k] + "\n"
	}

	// Check against the old requirements contents
	requirementsFile := filepath.Join(tmpDir, "requirements.txt")
	_, err := os.Stat(requirementsFile)
	if !errors.Is(err, os.ErrNotExist) {
		bytes, err := os.ReadFile(requirementsFile)
		if err != nil {
			return "", err
		}
		oldRequirementsContents := string(bytes)
		if oldRequirementsContents == requirementsFile {
			return requirementsFile, nil
		}
	}

	// Write out a new requirements file
	err = os.WriteFile(requirementsFile, []byte(requirementsContent), 0o644)
	if err != nil {
		return "", err
	}
	return requirementsFile, nil
}

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
