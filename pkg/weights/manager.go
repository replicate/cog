// Package weights orchestrates managed-weight operations: populating
// the local content-addressed store from a registry (Pull) and
// assembling per-invocation mount dirs (Prepare).
//
// The Manager is the single entry point. CLI commands construct one
// and call it; no CLI surface constructs stores, fetches layers, or
// walks tars directly.
package weights

import (
	"context"
	"errors"
	"fmt"

	v1 "github.com/google/go-containerregistry/pkg/v1"

	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/registry"
	"github.com/replicate/cog/pkg/weights/store"
)

// imageFetcher is the subset of registry.Client that Pull uses. The
// full registry.Client satisfies it; narrowing here makes the
// dependency explicit and lets tests mock a single method instead of
// the whole fat interface.
type imageFetcher interface {
	GetImage(ctx context.Context, ref string, platform *registry.Platform) (v1.Image, error)
}

// Manager orchestrates managed-weight operations against a local
// content-addressed store and a remote OCI registry.
type Manager struct {
	store      store.Store
	registry   imageFetcher
	repo       string
	lock       *model.WeightsLock
	projectDir string
}

// ManagerOptions is the argument struct for NewManager.
//
// Store and Registry are always required. Lock, Repo, and ProjectDir
// are required only if the model has weights — a Manager constructed
// with a nil or empty Lock is a valid no-op, so callers don't need to
// branch on "does cog.yaml declare weights?" before constructing one.
type ManagerOptions struct {
	Store      store.Store
	Registry   registry.Client
	Repo       string
	Lock       *model.WeightsLock
	ProjectDir string
}

func NewManager(opts ManagerOptions) (*Manager, error) {
	if opts.Store == nil {
		return nil, errors.New("weights manager: store is required")
	}
	if opts.Registry == nil {
		return nil, errors.New("weights manager: registry is required")
	}
	// Repo/Lock/ProjectDir are only required when the model actually
	// has weights. Validated lazily in Pull and Prepare.
	if opts.Lock != nil && len(opts.Lock.Weights) > 0 && opts.Repo == "" {
		return nil, errors.New("weights manager: repo is required when lock has weights")
	}
	return &Manager{
		store:      opts.Store,
		registry:   opts.Registry,
		repo:       opts.Repo,
		lock:       opts.Lock,
		projectDir: opts.ProjectDir,
	}, nil
}

// ProjectDir returns the project directory configured on the Manager.
// Primarily useful for tests and for CLI code that wants to log where
// mounts will live.
func (m *Manager) ProjectDir() string { return m.projectDir }

// selectEntries returns the lockfile entries matching names, in name
// order. Empty names means every entry in lockfile order. Unknown
// names are reported in a single error so the user sees all typos in
// one shot.
func (m *Manager) selectEntries(names []string) ([]*model.WeightLockEntry, error) {
	if m.lock == nil {
		if len(names) > 0 {
			return nil, fmt.Errorf("unknown weight(s): %v (model has no weights)", names)
		}
		return nil, nil
	}
	if len(names) == 0 {
		out := make([]*model.WeightLockEntry, len(m.lock.Weights))
		for i := range m.lock.Weights {
			out[i] = &m.lock.Weights[i]
		}
		return out, nil
	}

	out := make([]*model.WeightLockEntry, 0, len(names))
	var missing []string
	for _, n := range names {
		entry := m.lock.FindWeight(n)
		if entry == nil {
			missing = append(missing, n)
			continue
		}
		out = append(out, entry)
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("unknown weight(s): %v", missing)
	}
	return out, nil
}
