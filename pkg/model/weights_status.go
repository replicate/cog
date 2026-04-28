package model

import (
	"context"

	"golang.org/x/sync/errgroup"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/registry"
	"github.com/replicate/cog/pkg/weights/lockfile"
)

// WeightStatus describes the resolved status of a weight.
type WeightStatus string

const (
	WeightStatusReady      WeightStatus = "ready"
	WeightStatusIncomplete WeightStatus = "incomplete"
	WeightStatusStale      WeightStatus = "stale"
	WeightStatusPending    WeightStatus = "pending"
	WeightStatusOrphaned   WeightStatus = "orphaned"
)

// LayerStatus describes the registry presence of a single layer.
type LayerStatus string

const (
	LayerStatusReady   LayerStatus = "ready"
	LayerStatusMissing LayerStatus = "missing"
)

// WeightStatusResult is one weight's resolved status. The LockEntry pointer
// is nil for pending weights; non-nil for all other statuses. Layers is
// populated only for weights that had registry checks performed.
type WeightStatusResult struct {
	Name      string
	Target    string
	Status    WeightStatus
	LockEntry *lockfile.WeightLockEntry
	Layers    []LayerStatusResult
}

// LayerStatusResult is one layer's status in the registry.
type LayerStatusResult struct {
	Digest string
	Size   int64
	Status LayerStatus
}

// WeightsStatus is the computed status of all weights for a model.
// It is the return value of ComputeWeightsStatus and provides methods
// to inspect the results.
type WeightsStatus struct {
	results []WeightStatusResult
}

// ComputeWeightsStatus determines the status of every weight by matching
// config declarations against the lockfile and checking the registry for
// per-layer blob presence.
//
// Registry checks run concurrently, bounded by GetPushConcurrency().
// Per-weight registry errors are soft: the weight is marked "incomplete"
// and layers are marked "missing".
// Context cancellation is propagated via errgroup and returns an error.
func ComputeWeightsStatus(ctx context.Context, cfg *config.Config, lock *lockfile.WeightsLock, repo string, reg registry.Client) (*WeightsStatus, error) {
	lockByName := make(map[string]*lockfile.WeightLockEntry)
	if lock != nil {
		for i := range lock.Weights {
			lockByName[lock.Weights[i].Name] = &lock.Weights[i]
		}
	}

	configNames := make(map[string]bool, len(cfg.Weights))

	// First pass: config-declared weights. Determine local status
	// (pending, stale, or needs-registry-check).
	results := make([]WeightStatusResult, 0, len(cfg.Weights)+len(lockByName))
	var needRegistryCheck []int // indices into results

	for _, w := range cfg.Weights {
		configNames[w.Name] = true
		le := lockByName[w.Name]

		r := WeightStatusResult{
			Name:      w.Name,
			Target:    w.Target,
			LockEntry: le,
		}

		switch {
		case le == nil:
			r.Status = WeightStatusPending
		case isStale(w, le):
			r.Status = WeightStatusStale
		case len(le.Layers) > 0:
			// Config matches lockfile, has layers. Registry check needed.
			needRegistryCheck = append(needRegistryCheck, len(results))
		default:
			// No layers to check (edge case: lockfile entry with no layers).
			r.Status = WeightStatusReady
		}

		results = append(results, r)
	}

	// Orphaned: in lockfile but not in config.
	for i := range lockByName {
		if configNames[i] {
			continue
		}
		le := lockByName[i]
		results = append(results, WeightStatusResult{
			Name:      le.Name,
			Target:    le.Target,
			Status:    WeightStatusOrphaned,
			LockEntry: le,
		})
	}

	// Second pass: concurrent per-layer registry checks.
	if len(needRegistryCheck) > 0 {
		if err := checkRegistryLayers(ctx, results, needRegistryCheck, repo, reg); err != nil {
			return nil, err
		}
	}

	return &WeightsStatus{results: results}, nil
}

// statusCheckConcurrency is the concurrency limit for registry HEAD
// requests during status checks. These are lightweight operations,
// not bandwidth-saturating uploads.
const statusCheckConcurrency = 8

// checkRegistryLayers checks layer blob existence in the registry for each
// weight that needs verification. Each weight's layers are checked
// concurrently. The weight's status is set to "ready" if all layers exist,
// "incomplete" otherwise.
func checkRegistryLayers(ctx context.Context, results []WeightStatusResult, indices []int, repo string, reg registry.Client) error {
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(statusCheckConcurrency)

	for _, idx := range indices {
		r := &results[idx]
		g.Go(func() error {
			if err := ctx.Err(); err != nil {
				return err
			}
			return checkWeightLayers(ctx, r, repo, reg)
		})
	}

	return g.Wait()
}

// checkWeightLayers checks each layer of a single weight against the registry
// and populates the result's Layers and Status fields.
func checkWeightLayers(ctx context.Context, r *WeightStatusResult, repo string, reg registry.Client) error {
	le := r.LockEntry
	r.Layers = make([]LayerStatusResult, len(le.Layers))

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(statusCheckConcurrency)

	for i, layer := range le.Layers {
		lr := &r.Layers[i]
		lr.Digest = layer.Digest
		lr.Size = layer.Size

		g.Go(func() error {
			if err := ctx.Err(); err != nil {
				return err
			}

			exists, err := reg.BlobExists(ctx, repo, layer.Digest)
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				lr.Status = LayerStatusMissing
				return nil
			}

			if exists {
				lr.Status = LayerStatusReady
			} else {
				lr.Status = LayerStatusMissing
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return err
	}

	// Derive weight status from layer results after all goroutines complete.
	r.Status = WeightStatusReady
	for _, lr := range r.Layers {
		if lr.Status != LayerStatusReady {
			r.Status = WeightStatusIncomplete
			break
		}
	}
	return nil
}

// isStale reports whether a config declaration has drifted from its
// lockfile entry. An invalid config (URI that fails normalization) is
// treated as stale: the user asked for something we can't represent, so
// the safe answer is "out of sync".
//
// Source.Fingerprint and Source.ImportedAt are lockfile-side metadata,
// not user-declared inputs, and are excluded from the comparison.
func isStale(w config.WeightSource, le *lockfile.WeightLockEntry) bool {
	configSpec, err := WeightSpecFromConfig(w)
	if err != nil {
		return true
	}
	return !configSpec.Equal(WeightSpecFromLock(*le))
}

// Results returns all weight status results in order: config-declared
// weights first (preserving cog.yaml order), then orphaned lockfile
// entries.
func (ws *WeightsStatus) Results() []WeightStatusResult {
	return ws.results
}

// AllReady reports whether every weight is in the "ready" state.
// Returns true for empty weight lists.
func (ws *WeightsStatus) AllReady() bool {
	for _, r := range ws.results {
		if r.Status != WeightStatusReady {
			return false
		}
	}
	return true
}

// ByStatus returns all results with the given status.
func (ws *WeightsStatus) ByStatus(status WeightStatus) []WeightStatusResult {
	var out []WeightStatusResult
	for _, r := range ws.results {
		if r.Status == status {
			out = append(out, r)
		}
	}
	return out
}
