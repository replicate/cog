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

// computeDirSetDigest walks dir and computes the weight set digest per
// spec §2.4:
//
//	sha256(join(sort(entries), "\n"))  where entry = "<hex-sha256>  <path>"
//
// SYNC: model.ComputeWeightSetDigest computes the same digest from a
// []PackedFile slice. Changes to the formula must update both.
//
// The .cog state directory is skipped to match the packer's behavior.
// Symlinks and non-regular files are skipped — same reason.
func computeDirSetDigest(ctx context.Context, dir string) (string, error) {
	type fileDigest struct {
		relPath string
		digest  string
	}
	var files []fileDigest

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

		h, err := util.SHA256HashFile(path)
		if err != nil {
			return fmt.Errorf("hash %s: %w", rel, err)
		}
		files = append(files, fileDigest{relPath: rel, digest: h})
		return nil
	})
	if err != nil {
		return "", err
	}

	sort.Slice(files, func(i, j int) bool { return files[i].relPath < files[j].relPath })

	entries := make([]string, len(files))
	for i, f := range files {
		entries[i] = f.digest + "  " + f.relPath
	}
	sum := sha256.Sum256([]byte(strings.Join(entries, "\n")))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
