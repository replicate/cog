package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"sort"
	"strings"
)

// WeightLockLayer describes a single packed layer inside a WeightLockEntry.
// The shape matches spec §3.6 (/.cog/weights.json) so this struct can be
// written verbatim into the model image at build time.
type WeightLockLayer struct {
	// Digest is the sha256 digest of the layer blob on disk.
	Digest string `json:"digest"`
	// Size is the size of the layer blob in bytes.
	Size int64 `json:"size"`
	// MediaType is the OCI layer media type
	// (application/vnd.oci.image.layer.v1.tar or .tar+gzip).
	MediaType string `json:"mediaType"`
	// Annotations are the descriptor annotations attached to this layer in
	// the manifest (e.g. run.cog.weight.content, run.cog.weight.file).
	Annotations map[string]string `json:"annotations,omitempty"`
}

// WeightLockEntry describes a single weight in a weights lockfile.
// The manifest digest is the unit of identity — there is no per-file entry.
type WeightLockEntry struct {
	// Name is the weight's logical name (e.g. "z-image-turbo").
	Name string `json:"name"`
	// Target is the container mount path for this weight.
	Target string `json:"target"`
	// Digest is the sha256 digest of the assembled OCI manifest.
	Digest string `json:"digest"`
	// SetDigest is the weight set digest (§2.4): a content-addressable
	// identifier for the file set, independent of packing strategy.
	SetDigest string `json:"setDigest"`
	// Layers are the descriptors of the tar layers making up this weight,
	// in manifest order.
	Layers []WeightLockLayer `json:"layers"`
}

// NewWeightLockEntry builds a WeightLockEntry from a set of packed tar
// layers. Annotations are cloned so later mutations on the LayerResult do
// not bleed into the entry.
func NewWeightLockEntry(name, target, manifestDigest, setDigest string, layers []LayerResult) WeightLockEntry {
	lockLayers := make([]WeightLockLayer, len(layers))
	for i, l := range layers {
		lockLayers[i] = WeightLockLayer{
			Digest:      l.Digest.String(),
			Size:        l.Size,
			MediaType:   string(l.MediaType),
			Annotations: maps.Clone(l.Annotations),
		}
	}
	return WeightLockEntry{
		Name:      name,
		Target:    target,
		Digest:    manifestDigest,
		SetDigest: setDigest,
		Layers:    lockLayers,
	}
}

// WeightConfigBlob is the JSON structure for the config blob (§2.3).
type WeightConfigBlob struct {
	Name      string              `json:"name"`
	Target    string              `json:"target"`
	SetDigest string              `json:"setDigest"`
	Files     []WeightConfigFile  `json:"files"`
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
// where each entry is "<hex-sha256>  <path>" (two spaces, matching sha256sum
// format). files must be sorted by path; the caller is responsible for
// ordering (BuildWeightConfigBlob sorts before calling).
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
