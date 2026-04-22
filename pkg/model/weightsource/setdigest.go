package weightsource

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/replicate/cog/pkg/util"
)

// computeInventory walks dir and produces an Inventory: per-file
// path/size/digest plus the source fingerprint (sha256 of the sorted
// file set, spec §2.4).
//
// The set digest formula matches model.ComputeWeightSetDigest (which
// computes the same digest from a []PackedFile slice produced by the
// packer). Changes to the formula must update both.
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

	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })

	// Set-digest input lines: "<hex>  <path>" (two spaces, matching
	// coreutils sha256sum output). InventoryFile.Digest carries the
	// "sha256:" prefix; strip it here to match the on-disk format the
	// packer also reproduces in model.ComputeWeightSetDigest.
	entries := make([]string, len(files))
	for i, f := range files {
		entries[i] = strings.TrimPrefix(f.Digest, "sha256:") + "  " + f.Path
	}
	sum := sha256.Sum256([]byte(strings.Join(entries, "\n")))
	return Inventory{
		Files:       files,
		Fingerprint: Fingerprint("sha256:" + hex.EncodeToString(sum[:])),
	}, nil
}
