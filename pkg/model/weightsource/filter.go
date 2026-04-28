package weightsource

import (
	"fmt"
	"strings"

	ignore "github.com/sabhiram/go-gitignore"
)

// FilterInventory applies include/exclude glob patterns to an inventory's
// file list and returns a new inventory with only the matching files.
// The returned inventory shares the original's Fingerprint (which is the
// upstream version identity, not affected by filtering).
//
// Semantics:
//   - If include is non-empty, a file must match at least one include pattern.
//   - If a file matches any exclude pattern, it is excluded (even if it also
//     matches an include pattern — exclude wins).
//   - If both lists are empty/nil, all files pass through unchanged.
//
// Pattern matching uses gitignore-style globs via go-gitignore: bare patterns
// float across directories ("*.bin" matches any depth), path-shaped patterns
// anchor ("onnx/*.bin" matches direct children of onnx/), and "**" matches
// any number of path segments.
//
// Returns an error if the filter yields zero files — an empty weight set is
// almost always a mistake and should surface immediately.
func FilterInventory(inv Inventory, include, exclude []string) (Inventory, error) {
	if len(include) == 0 && len(exclude) == 0 {
		return inv, nil
	}

	var includeMatcher *ignore.GitIgnore
	if len(include) > 0 {
		includeMatcher = ignore.CompileIgnoreLines(include...)
	}

	var excludeMatcher *ignore.GitIgnore
	if len(exclude) > 0 {
		excludeMatcher = ignore.CompileIgnoreLines(exclude...)
	}

	filtered := make([]InventoryFile, 0, len(inv.Files))
	for _, f := range inv.Files {
		if !fileIncluded(f.Path, includeMatcher, excludeMatcher) {
			continue
		}
		filtered = append(filtered, f)
	}

	if len(filtered) == 0 {
		return Inventory{}, &ZeroSurvivorsError{
			InventorySize: len(inv.Files),
			Include:       include,
			Exclude:       exclude,
		}
	}

	return Inventory{
		Files:       filtered,
		Fingerprint: inv.Fingerprint,
	}, nil
}

// fileIncluded reports whether a file path passes the include/exclude filter.
func fileIncluded(path string, includeMatcher, excludeMatcher *ignore.GitIgnore) bool {
	if excludeMatcher != nil && excludeMatcher.MatchesPath(path) {
		return false
	}
	if includeMatcher != nil {
		return includeMatcher.MatchesPath(path)
	}
	return true
}

// ZeroSurvivorsError is returned when include/exclude filtering removes all
// files from an inventory.
type ZeroSurvivorsError struct {
	InventorySize int
	Include       []string
	Exclude       []string
}

func (e *ZeroSurvivorsError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "include/exclude patterns matched zero files out of %d in the source", e.InventorySize)
	if len(e.Include) > 0 {
		fmt.Fprintf(&b, "\n  include: %s", formatPatterns(e.Include))
	}
	if len(e.Exclude) > 0 {
		fmt.Fprintf(&b, "\n  exclude: %s", formatPatterns(e.Exclude))
	}
	b.WriteString("\n  check your patterns — did you mean a different glob?")
	return b.String()
}

func formatPatterns(patterns []string) string {
	quoted := make([]string, len(patterns))
	for i, p := range patterns {
		quoted[i] = fmt.Sprintf("%q", p)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}
