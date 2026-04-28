package weightsource

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"runtime"

	"golang.org/x/sync/errgroup"

	"github.com/replicate/cog/pkg/util"
)

// fileEntry holds the metadata collected during the walk phase, before
// hashing. Separating walk from hash lets us parallelize the expensive
// SHA-256 computation.
type fileEntry struct {
	absPath string
	rel     string
	size    int64
}

// computeInventory walks dir and produces an Inventory: per-file
// path/size/digest plus the source fingerprint. For file:// sources the
// fingerprint is the dirhash of the file set (spec §2.4) — the same
// formula used for the weight set digest.
//
// The walk phase collects file metadata (fast, sequential). The hash
// phase computes SHA-256 digests concurrently, bounded by GOMAXPROCS.
//
// The .cog state directory is skipped to match the packer's behavior.
// Symlinks and non-regular files are skipped — same reason.
func computeInventory(ctx context.Context, dir string) (Inventory, error) {
	// Phase 1: walk to collect paths and sizes (metadata only, fast).
	var entries []fileEntry
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ".cog" {
			return filepath.SkipDir
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}

		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return fmt.Errorf("rel path for %s: %w", path, err)
		}
		rel = filepath.ToSlash(rel)

		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("stat %s: %w", rel, err)
		}

		entries = append(entries, fileEntry{absPath: path, rel: rel, size: info.Size()})
		return nil
	})
	if err != nil {
		return Inventory{}, err
	}

	// Phase 2: hash files concurrently.
	files := make([]InventoryFile, len(entries))
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(runtime.GOMAXPROCS(0))

	for i, e := range entries {
		g.Go(func() error {
			if err := ctx.Err(); err != nil {
				return err
			}
			h, err := util.SHA256HashFile(e.absPath)
			if err != nil {
				return fmt.Errorf("hash %s: %w", e.rel, err)
			}
			files[i] = InventoryFile{
				Path:   e.rel,
				Size:   e.size,
				Digest: "sha256:" + h,
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return Inventory{}, err
	}

	sortInventoryFiles(files)

	return Inventory{
		Files:       files,
		Fingerprint: Fingerprint(DirHash(files)),
	}, nil
}
