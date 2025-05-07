package config

import (
	"maps"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/replicate/cog/pkg/util/console"
)

// pinnedPackageRe is the regular expression used to identify and extract fields from a requirements line.
var pinnedPackageRe = regexp.MustCompile(`(?:([a-zA-Z0-9\-_]+)==([^ ]+)|--find-links=([^\s]+)|-f\s+([^\s]+)|--extra-index-url=([^\s]+))`)

// PythonRequirement represents a single line of a Python requirements.txt-style file. It's not meant to power a
// full-fledged parser - just the bits that we care about when we generate new requirements files. In particular, we
// actually only parse packages pinned to an exact version with `==`.
type PythonRequirement struct {
	// Name is the simple name of the requirement, eg `torch`
	Name string

	// Version is the version of the python package this requirement is pinned to.
	Version string

	// EnvironmentAndHash contains any extra information after the ; in the requirements line. Typically this is
	// any hashes, runtime environment constraints, and so on.
	EnvironmentAndHash string

	// FindLinks is a list of URLs or directories to search for packages when resolving Python dependencies.
	FindLinks []string

	// ExtraIndexURLs represents additional package index URLs to use when resolving Python dependencies.
	ExtraIndexURLs []string

	// Literal is the string value that this PythonRequirement was originally parsed from, if any. This may be
	// empty.
	Literal string

	// ParsedFieldsValid indicates whether the Name, Version etc. fields are valid and can be read from. If this is
	// false, then the Literal field should be used.
	ParsedFieldsValid bool

	// order is used internally to make sure that requirements files we emit resemble the input as closely as
	// possible, by maintaining order.
	order int
}

// RequirementLine returns a string representation of the Python requirement. Note that find links
// and extra index URLs are not included in the string representation.
func (p PythonRequirement) RequirementLine() string {
	if !p.ParsedFieldsValid {
		return p.Literal
	}

	if p.Name == "" {
		return ""
	}

	fields := []string{p.Name}
	if p.Version != "" {
		fields = append(fields, "==", p.Version)
	}

	if p.EnvironmentAndHash != "" {
		fields = append(fields, " ; ", p.EnvironmentAndHash)
	}

	return strings.Join(fields, "")
}

// PythonRequirements is a collection of PythonRequirement lines. This alias is a convenience to allow us to write
// a method RequirementsFileContent to generate an actual requirements file from this collection.
type PythonRequirements []PythonRequirement

// RequirementsFileContent returns a string representation of all the Python requirements. --find-links and --extra-index-url entries
// will be prepended to the requirements. Note that the ordering of the generated file depends on the `order` attributes
// of the content, not the actual ordering of the PythonRequirements collection.
func (p PythonRequirements) RequirementsFileContent() string {
	findLinks := make(map[string]struct{})
	extraIndexURLs := make(map[string]struct{})
	lines := make([]string, 0)

	// First, extract any find-links or extra-index-url lines from requirements we were able to parse
	for _, req := range p {
		if !req.ParsedFieldsValid {
			continue
		}

		for _, findLink := range req.FindLinks {
			if len(findLink) > 0 {
				findLinks[findLink] = struct{}{}
			}
		}
		for _, extraIndexURL := range req.ExtraIndexURLs {
			if len(extraIndexURL) > 0 {
				extraIndexURLs[extraIndexURL] = struct{}{}
			}
		}
	}

	// Emit the --find-links lines. Sort for stability.
	sortedFindLinks := slices.Sorted(maps.Keys(findLinks))
	for _, findLink := range sortedFindLinks {
		lines = append(lines, "--find-links "+findLink)
	}

	// Emit the --extra-index-url lines
	sortedExtraIndexURLs := slices.Sorted(maps.Keys(extraIndexURLs))
	for _, extraIndexURL := range sortedExtraIndexURLs {
		lines = append(lines, "--extra-index-url "+extraIndexURL)
	}

	// Sort by the ordering key to preserve the user-supplied order
	sort.Slice(p, func(i, j int) bool {
		return p[i].order < p[j].order
	})

	for _, req := range p {
		lines = append(lines, req.RequirementLine())
	}
	return strings.Join(lines, "\n")
}

// ParseRequirements will attempt to parse all the packages specified in `packages`. Any requirements that can't
// be parsed will simply be passed through as literals.
func ParseRequirements(packages []string, orderStart int) PythonRequirements {
	reqs := make(PythonRequirements, 0, len(packages))
	for i, pkg := range packages {
		// We actually don't care at this point if the requirement parsed OK - we're happy just to pass the literal
		// through
		req := SplitPinnedPythonRequirement(pkg)
		if !req.ParsedFieldsValid {
			console.Debugf("pass-through unparseable requirement - this is usually ok: %s", pkg)
		}

		// Store an ordering key so that we can preserve order after deduplication
		req.order = i + orderStart
		reqs = append(reqs, req)
	}
	return reqs
}

// SplitPinnedPythonRequirement returns the name, version, findLinks, and extraIndexURLs from a requirements.txt line
// in the form name==version [--find-links=<findLink>] [-f <findLink>] [--extra-index-url=<extraIndexURL>]. If the
// requirement could not be parsed, then the returned PythonRequirement will have the `Parsed` field set to false.
// Either way, the `Literal` field will contain the original line.
func SplitPinnedPythonRequirement(requirement string) (req PythonRequirement) {
	req.Literal = requirement

	// Split out anything after the semicolon - this can contain things like runtime platform constraints, hashes,
	// etc. We don't care what is actually in this, but we do need to preserve it.
	parts := strings.Split(requirement, ";")
	requirementAndVersion := strings.TrimSpace(parts[0])
	if len(parts) > 1 {
		// Split the environment and hash section into parts and filter out --hash entries. It's not ideal to omit
		// the hashes, but we do not yet track the hashes of auto-generated requirements. Since the presence of even
		// one hash enables `--require-hashes` mode, we remove them to avoid breaking existing builds. In due course,
		// we may be able to remove this.
		envParts := strings.Fields(parts[1])
		var filteredParts []string
		for _, part := range envParts {
			if !strings.HasPrefix(part, "--hash=") {
				filteredParts = append(filteredParts, part)
			}
		}
		req.EnvironmentAndHash = strings.TrimSpace(strings.Join(filteredParts, " "))
	}

	matches := pinnedPackageRe.FindAllStringSubmatch(requirementAndVersion, -1)
	if matches == nil {
		return
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
		return
	}

	req.ParsedFieldsValid = true
	return
}
