package lockfile

import (
	"fmt"
	"slices"
	"strings"
)

// DriftKind classifies how a weight declaration has drifted from the
// lockfile.
type DriftKind string

const (
	// DriftOrphaned means the lockfile has an entry with no matching
	// config declaration — the weight was removed from cog.yaml.
	DriftOrphaned DriftKind = "orphaned"
	// DriftPending means config declares a weight that has no lockfile
	// entry — the weight has never been imported.
	DriftPending DriftKind = "pending"
	// DriftConfigChanged means both config and lockfile have the weight
	// but a user-intent field (URI, target, include, exclude) differs.
	DriftConfigChanged DriftKind = "config-changed"
)

// DriftResult describes a single config-vs-lockfile mismatch.
type DriftResult struct {
	Name    string
	Kind    DriftKind
	Details string // human-readable detail, e.g. "target: /old → /new"
}

// ConfigWeight is the lockfile package's view of a weight declaration
// from cog.yaml. It carries only the user-intent fields that affect
// whether a lockfile entry is stale. Callers must normalize URI and
// sort Include/Exclude before constructing a ConfigWeight — CheckDrift
// does byte-exact comparison.
type ConfigWeight struct {
	Name    string
	URI     string
	Target  string
	Include []string
	Exclude []string
}

// CheckDrift compares config declarations against lockfile entries and
// returns every mismatch. The result is empty when config and lockfile
// agree. A nil lock is treated as an empty lockfile (every config
// weight is "pending"). The function is pure: no I/O, no network.
func CheckDrift(lock *WeightsLock, configWeights []ConfigWeight) []DriftResult {
	lockByName := make(map[string]*WeightLockEntry)
	if lock != nil {
		for i := range lock.Weights {
			lockByName[lock.Weights[i].Name] = &lock.Weights[i]
		}
	}

	configNames := make(map[string]bool, len(configWeights))
	var results []DriftResult

	// Check each config weight against the lockfile.
	for _, cw := range configWeights {
		configNames[cw.Name] = true
		le := lockByName[cw.Name]

		if le == nil {
			results = append(results, DriftResult{
				Name: cw.Name,
				Kind: DriftPending,
			})
		} else if details := configChanged(cw, le); details != "" {
			results = append(results, DriftResult{
				Name:    cw.Name,
				Kind:    DriftConfigChanged,
				Details: details,
			})
		}
	}

	// Check for orphaned lockfile entries.
	if lock != nil {
		for _, le := range lock.Weights {
			if !configNames[le.Name] {
				results = append(results, DriftResult{
					Name: le.Name,
					Kind: DriftOrphaned,
				})
			}
		}
	}

	return results
}

// configChanged returns a human-readable diff string listing every
// user-intent field that differs between config and lockfile. Returns
// "" when they match.
func configChanged(cw ConfigWeight, le *WeightLockEntry) string {
	var diffs []string
	if cw.URI != le.Source.URI {
		diffs = append(diffs, fmt.Sprintf("uri: %s → %s", le.Source.URI, cw.URI))
	}
	if cw.Target != le.Target {
		diffs = append(diffs, fmt.Sprintf("target: %s → %s", le.Target, cw.Target))
	}
	if !slices.Equal(cw.Include, le.Source.Include) {
		diffs = append(diffs, fmt.Sprintf("include: %v → %v", le.Source.Include, cw.Include))
	}
	if !slices.Equal(cw.Exclude, le.Source.Exclude) {
		diffs = append(diffs, fmt.Sprintf("exclude: %v → %v", le.Source.Exclude, cw.Exclude))
	}
	return strings.Join(diffs, "; ")
}
