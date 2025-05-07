package config

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
)

// PythonRequirement represents a single line of a Python requirements.txt-style file.
type PythonRequirement struct {
	Name           string
	Version        string
	FindLinks      []string
	ExtraIndexURLs []string
}

// NameAndVersion returns a string representation of the Python requirement. Note that find links
// and extra index URLs are not included in the string representation.
func (p PythonRequirement) NameAndVersion() string {
	if p.Name == "" {
		return ""
	}

	fields := []string{p.Name}
	if p.Version != "" {
		fields = append(fields, "==", p.Version)
	}
	return strings.Join(fields, "")
}

type PythonRequirements []PythonRequirement

// RequirementsFileContent returns a string representation of all the Python requirements. --find-links and --extra-index-url entries
// will be prepended to the requirements.
func (p PythonRequirements) RequirementsFileContent() string {
	findLinks := make(map[string]struct{})
	extraIndexURLs := make(map[string]struct{})

	lines := make([]string, 0)
	for _, req := range p {
		for _, findLink := range req.FindLinks {
			findLinks[findLink] = struct{}{}
		}
		for _, extraIndexURL := range req.ExtraIndexURLs {
			extraIndexURLs[extraIndexURL] = struct{}{}
		}
	}

	// First, emit the --find-links lines. Sort for stability.
	sortedFindLinks := slices.Sorted(maps.Keys(findLinks))
	for _, findLink := range sortedFindLinks {
		lines = append(lines, "--find-links "+findLink)
	}

	// Now, emit the --extra-index-url lines
	sortedExtraIndexURLs := slices.Sorted(maps.Keys(extraIndexURLs))
	for _, extraIndexURL := range sortedExtraIndexURLs {
		lines = append(lines, "--extra-index-url "+extraIndexURL)
	}

	for _, req := range p {
		lines = append(lines, req.NameAndVersion())
	}
	return strings.Join(lines, "\n")
}

func ParseRequirements(packages []string) (PythonRequirements, error) {
	reqs := make(PythonRequirements, 0, len(packages))
	for _, pkg := range packages {
		if req, err := SplitPinnedPythonRequirement(pkg); err != nil {
			return nil, fmt.Errorf("failed to parse requirements for %s: %w", pkg, err)
		} else {
			reqs = append(reqs, req)
		}
	}
	return reqs, nil
}

// SplitPinnedPythonRequirement returns the name, version, findLinks, and extraIndexURLs from a requirements.txt line
// in the form name==version [--find-links=<findLink>] [-f <findLink>] [--extra-index-url=<extraIndexURL>]
func SplitPinnedPythonRequirement(requirement string) (req PythonRequirement, err error) {
	pinnedPackageRe := regexp.MustCompile(`(?:([a-zA-Z0-9\-_]+)==([^ ]+)|--find-links=([^\s]+)|-f\s+([^\s]+)|--extra-index-url=([^\s]+))`)

	matches := pinnedPackageRe.FindAllStringSubmatch(requirement, -1)
	if matches == nil {
		return req, fmt.Errorf("Package %s is not in the expected format", requirement)
	}

	nameFound := false
	versionFound := false

	for _, match := range matches {
		if match[1] != "" {
			req.Name = match[1]
			nameFound = true
		}

		if match[2] != "" {
			req.Version = match[2]
			versionFound = true
		}

		if match[3] != "" {
			req.FindLinks = append(req.FindLinks, match[3])
		}

		if match[4] != "" {
			req.FindLinks = append(req.FindLinks, match[4])
		}

		if match[5] != "" {
			req.ExtraIndexURLs = append(req.ExtraIndexURLs, match[5])
		}
	}

	if !nameFound || !versionFound {
		return PythonRequirement{}, fmt.Errorf("Package name or version is missing in %s", requirement)
	}
	return
}
