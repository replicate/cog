package weights

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/model/weightsource"
	"github.com/replicate/cog/pkg/weights/lockfile"
)

// CheckDrift loads the lockfile from projectDir and compares it against
// the config's weight declarations. It returns a user-facing error if
// any drift is detected, telling the user to run "cog weights import".
//
// Returns nil when weights is empty, when config and lockfile agree,
// or when the lockfile is missing and there are no config weights.
func CheckDrift(projectDir string, weights []config.WeightSource) error {
	if len(weights) == 0 {
		return nil
	}

	lockPath := filepath.Join(projectDir, lockfile.WeightsLockFilename)
	lock, err := lockfile.LoadWeightsLock(lockPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("load %s: %w", lockfile.WeightsLockFilename, err)
		}
		lock = nil // missing lockfile → all weights are pending
	}

	configWeights, err := toConfigWeights(weights)
	if err != nil {
		return err
	}

	results := lockfile.CheckDrift(lock, configWeights)
	if len(results) == 0 {
		return nil
	}

	return formatDriftError(results)
}

// toConfigWeights converts config weight declarations to
// lockfile.ConfigWeight values, normalizing URIs and sorting
// include/exclude patterns so the comparison matches the lockfile's
// canonical form.
func toConfigWeights(ws []config.WeightSource) ([]lockfile.ConfigWeight, error) {
	cws := make([]lockfile.ConfigWeight, 0, len(ws))
	for _, w := range ws {
		uri, err := weightsource.NormalizeURI(w.SourceURI())
		if err != nil {
			return nil, fmt.Errorf("weight %q: %w", w.Name, err)
		}
		cw := lockfile.ConfigWeight{
			Name:   w.Name,
			Target: w.Target,
			URI:    uri,
		}
		if w.Source != nil {
			cw.Include = sortedCopy(w.Source.Include)
			cw.Exclude = sortedCopy(w.Source.Exclude)
		}
		cws = append(cws, cw)
	}
	return cws, nil
}

// sortedCopy returns a sorted copy of s, or nil if s is nil.
func sortedCopy(s []string) []string {
	if s == nil {
		return nil
	}
	out := slices.Clone(s)
	slices.Sort(out)
	return out
}

// formatDriftError builds a user-facing error from drift results.
func formatDriftError(results []lockfile.DriftResult) error {
	var b strings.Builder
	b.WriteString("weights.lock is out of sync with cog.yaml:\n")
	for _, r := range results {
		switch r.Kind {
		case lockfile.DriftPending:
			fmt.Fprintf(&b, "  - %q: not imported yet\n", r.Name)
		case lockfile.DriftOrphaned:
			fmt.Fprintf(&b, "  - %q: removed from cog.yaml but still in lockfile\n", r.Name)
		case lockfile.DriftConfigChanged:
			fmt.Fprintf(&b, "  - %q: config changed (%s)\n", r.Name, r.Details)
		}
	}
	b.WriteString("Run 'cog weights import' to update.")
	return errors.New(b.String())
}
