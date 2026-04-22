package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// NewWeightLockEntry assembles a WeightLockEntry from a source description,
// the packed file index, and the set of packed layers produced by Pack.
//
// The caller owns sort order — canonicalizeEntry sorts Files by path and
// Layers by digest at Save time, so NewWeightLockEntry accepts whatever
// order the packer happened to emit.
func NewWeightLockEntry(
	name, target string,
	manifestDigest, setDigest string,
	source WeightLockSource,
	files []PackedFile,
	layers []LayerResult,
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
		Digest:         manifestDigest,
		SetDigest:      setDigest,
		Size:           totalSize,
		SizeCompressed: totalCompressed,
		Files:          lockFiles,
		Layers:         lockLayers,
	}
	canonicalizeEntry(&entry)
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

// ComputeWeightSetDigest computes the weight set digest per spec §2.4:
//
//	sha256(join(sort(entries), "\n"))
//
// where each entry is "<hex-sha256>  <path>" (two spaces, matching
// sha256sum format). files must be sorted by path; the caller is
// responsible for ordering (BuildWeightConfigBlob sorts before calling).
//
// SYNC: weightsource.computeInventory computes the same digest from a
// raw directory walk (producing an Inventory alongside). Changes to the
// formula must update both.
func ComputeWeightSetDigest(files []PackedFile) string {
	entries := make([]string, len(files))
	for i, f := range files {
		_, hexStr, _ := strings.Cut(f.Digest, ":")
		entries[i] = hexStr + "  " + f.Path
	}
	payload := strings.Join(entries, "\n")
	sum := sha256.Sum256([]byte(payload))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// BuildWeightConfigBlob builds the serialized config blob JSON (§2.3) and
// computes the weight set digest (§2.4).
func BuildWeightConfigBlob(name, target string, files []PackedFile) (configJSON []byte, setDigest string, err error) {
	if len(files) == 0 {
		return nil, "", fmt.Errorf("no files for config blob")
	}

	// Sort by path for deterministic output (§2.3: "files array MUST be
	// sorted by path lexicographically").
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })

	setDigest = ComputeWeightSetDigest(files)

	cfg := WeightConfigBlob{
		Name:      name,
		Target:    target,
		SetDigest: setDigest,
		Files:     make([]WeightConfigFile, len(files)),
	}
	for i, f := range files {
		cfg.Files[i] = WeightConfigFile{
			Path:   f.Path,
			Layer:  f.LayerDigest,
			Size:   f.Size,
			Digest: f.Digest,
		}
	}

	configJSON, err = json.Marshal(cfg)
	if err != nil {
		return nil, "", fmt.Errorf("marshal config blob: %w", err)
	}
	return configJSON, setDigest, nil
}
