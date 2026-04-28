package model

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/replicate/cog/pkg/weights/lockfile"
)

// newWeightLockEntry assembles a lockfile.WeightLockEntry from a source
// description, the packed file index, and the set of packed layers
// produced by pack.
//
// The set digest (spec §2.4) is computed from the canonical file index.
// The manifest digest is left empty — it is filled in by the caller after
// buildWeightManifestV1 assembles the manifest from this entry.
func newWeightLockEntry(
	name, target string,
	source lockfile.WeightLockSource,
	files []packedFile,
	layers []packedLayer,
) lockfile.WeightLockEntry {
	lockFiles := make([]lockfile.WeightLockFile, len(files))
	for i, f := range files {
		lockFiles[i] = lockfile.WeightLockFile{
			Path:   f.Path,
			Size:   f.Size,
			Digest: f.Digest,
			Layer:  f.LayerDigest,
		}
	}

	lockLayers := make([]lockfile.WeightLockLayer, len(layers))
	var totalSize, totalCompressed int64
	for i, l := range layers {
		lockLayers[i] = lockfile.WeightLockLayer{
			Digest:           l.Digest.String(),
			MediaType:        string(l.MediaType),
			Size:             l.Size,
			SizeUncompressed: l.UncompressedSize,
		}
		totalSize += l.UncompressedSize
		totalCompressed += l.Size
	}

	entry := lockfile.WeightLockEntry{
		Name:           name,
		Target:         target,
		Source:         source,
		Size:           totalSize,
		SizeCompressed: totalCompressed,
		Files:          lockFiles,
		Layers:         lockLayers,
	}
	entry.SetDigest = entry.ComputeSetDigest()
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

// buildWeightConfigBlob builds the serialized config blob JSON (§2.3).
// The setDigest and file index come from the lockfile entry — the
// lockfile is the single source of truth for these values.
func buildWeightConfigBlob(name, target, setDigest string, files []lockfile.WeightLockFile) ([]byte, error) {
	if len(files) == 0 {
		return nil, fmt.Errorf("no files for config blob")
	}

	// Sort by path for deterministic output (§2.3: "files array MUST be
	// sorted by path lexicographically"). Clone to avoid mutating the
	// caller's slice.
	sorted := slices.Clone(files)
	slices.SortFunc(sorted, func(a, b lockfile.WeightLockFile) int {
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
