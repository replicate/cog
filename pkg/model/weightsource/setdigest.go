package weightsource

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/replicate/cog/pkg/util"
)

// computeInventory walks dir and produces an Inventory: per-file
// path/size/digest plus the source fingerprint. For file:// sources the
// fingerprint is the dirhash of the file set (spec §2.4) — the same
// formula used for the weight set digest.
//
// The .cog state directory is skipped to match the packer's behavior.
// Symlinks and non-regular files are skipped — same reason.
func computeInventory(ctx context.Context, dir string) (Inventory, error) {
	var files []InventoryFile

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

		h, err := util.SHA256HashFile(path)
		if err != nil {
			return fmt.Errorf("hash %s: %w", rel, err)
		}
		files = append(files, InventoryFile{
			Path:   rel,
			Size:   info.Size(),
			Digest: "sha256:" + h,
		})
		return nil
	})
	if err != nil {
		return Inventory{}, err
	}

	sortInventoryFiles(files)

	return Inventory{
		Files:       files,
		Fingerprint: Fingerprint(DirHash(files)),
	}, nil
}
