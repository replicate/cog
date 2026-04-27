package weights

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/registry"
	"github.com/replicate/cog/pkg/weights/lockfile"
	"github.com/replicate/cog/pkg/weights/store"
)

// NewFromSource constructs a Manager from a model.Source and an
// already-parsed repository string. Callers (typically CLI commands)
// are responsible for parsing their `--image` flag / `cog.yaml image:`
// value into a bare repo before calling.
//
// repo may be empty for models that declare no weights — the returned
// Manager is a valid no-op in that case, so CLI callers can construct
// one unconditionally and let Pull/Prepare decide if there's anything
// to do.
//
// Missing lockfiles error out with an actionable message pointing at
// `cog weights import` when the model actually has weights.
func NewFromSource(src *model.Source, repo string) (*Manager, error) {
	fileStore, err := store.OpenDefault()
	if err != nil {
		return nil, fmt.Errorf("open weights cache: %w", err)
	}

	var lock *lockfile.WeightsLock
	if len(src.Config.Weights) > 0 {
		if repo == "" {
			return nil, errors.New("cog.yaml declares weights but no repository was resolved; set 'image:' in cog.yaml or pass --image")
		}
		lockPath := filepath.Join(src.ProjectDir, lockfile.WeightsLockFilename)
		loaded, err := lockfile.LoadWeightsLock(lockPath)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil, fmt.Errorf("%s not found (run 'cog weights import' first)", lockfile.WeightsLockFilename)
			}
			return nil, fmt.Errorf("load %s: %w", lockfile.WeightsLockFilename, err)
		}
		lock = loaded
	}

	return NewManager(ManagerOptions{
		Store:      fileStore,
		Registry:   registry.NewRegistryClient(),
		Repo:       repo,
		Lock:       lock,
		ProjectDir: src.ProjectDir,
	})
}
