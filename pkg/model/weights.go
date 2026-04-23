package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// newWeightLockEntry assembles a WeightLockEntry from a source description,
// the packed file index, and the set of packed layers produced by pack.
//
// The set digest (spec §2.4) is computed from the file index. The manifest
// digest is left empty — it is filled in by the caller after
// buildWeightManifestV1 assembles the manifest from this entry.
//
// The caller owns sort order — canonicalizeEntry sorts Files by path and
// Layers by digest at Save time, so newWeightLockEntry accepts whatever
// order the packer happened to emit.
func newWeightLockEntry(
	name, target string,
	source WeightLockSource,
	files []packedFile,
	layers []packedLayer,
) WeightLockEntry {
	lockFiles := make([]WeightLockFile, len(files))
	for i, f := range files {
		lockFiles[i] = WeightLockFile{
			Path:   f.Path,
			Size:   f.Size,
			Digest: f.Digest,
			Layer:  f.LayerDigest,
		}
	}

	lockLayers := make([]WeightLockLayer, len(layers))
	var totalSize, totalCompressed int64
	for i, l := range layers {
		lockLayers[i] = WeightLockLayer{
			Digest:           l.Digest.String(),
			MediaType:        string(l.MediaType),
			Size:             l.Size,
			SizeUncompressed: l.UncompressedSize,
		}
		totalSize += l.UncompressedSize
		totalCompressed += l.Size
	}

	entry := WeightLockEntry{
		Name:           name,
		Target:         target,
		Source:         source,
		Size:           totalSize,
		SizeCompressed: totalCompressed,
		Files:          lockFiles,
		Layers:         lockLayers,
	}
	canonicalizeEntry(&entry)

	// Compute set digest from the canonical (sorted) file index.
	entry.SetDigest = computeWeightSetDigest(entry.Files)

	return entry
}

// WeightConfigBlob is the JSON structure for the config blob (§2.3).
type WeightConfigBlob struct {
	Name      string             `json:"name"`
	Target    string             `json:"target"`
	SetDigest string             `json:"setDigest"`
	Files     []WeightConfigFile `json:"files"`
}

// WeightConfigFile is one entry in the config blob's files array.
type WeightConfigFile struct {
	Path   string `json:"path"`
	Layer  string `json:"layer"`
	Size   int64  `json:"size"`
	Digest string `json:"digest"`
}

// computeWeightSetDigest computes the weight set digest per spec §2.4:
//
//	sha256(join(sort(entries), "\n"))
//
// where each entry is "<hex-sha256>  <path>" (two spaces, matching
// sha256sum format). files must be sorted by path; the caller is
// responsible for ordering (buildWeightConfigBlob sorts before calling).
//
// SYNC: weightsource.computeInventory computes the same digest from a
// raw directory walk (producing an Inventory alongside). Changes to the
// formula must update both.
func computeWeightSetDigest(files []WeightLockFile) string {
	entries := make([]string, len(files))
	for i, f := range files {
		_, hexStr, _ := strings.Cut(f.Digest, ":")
		entries[i] = hexStr + "  " + f.Path
	}
	payload := strings.Join(entries, "\n")
	sum := sha256.Sum256([]byte(payload))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// buildWeightConfigBlob builds the serialized config blob JSON (§2.3).
// The setDigest and file index come from the lockfile entry — the
// lockfile is the single source of truth for these values.
func buildWeightConfigBlob(name, target, setDigest string, files []WeightLockFile) ([]byte, error) {
	if len(files) == 0 {
		return nil, fmt.Errorf("no files for config blob")
	}

	// Sort by path for deterministic output (§2.3: "files array MUST be
	// sorted by path lexicographically"). Clone to avoid mutating the
	// caller's slice.
	sorted := slices.Clone(files)
	slices.SortFunc(sorted, func(a, b WeightLockFile) int {
		return strings.Compare(a.Path, b.Path)
	})

	cfg := WeightConfigBlob{
		Name:      name,
		Target:    target,
		SetDigest: setDigest,
		Files:     make([]WeightConfigFile, len(sorted)),
	}
	for i, f := range sorted {
		cfg.Files[i] = WeightConfigFile{
			Path:   f.Path,
			Layer:  f.Layer,
			Size:   f.Size,
			Digest: f.Digest,
		}
	}

	configJSON, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshal config blob: %w", err)
	}
	return configJSON, nil
}
