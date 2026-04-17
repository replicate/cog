package model

import "maps"

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
	// Layers are the descriptors of the tar layers making up this weight,
	// in manifest order.
	Layers []WeightLockLayer `json:"layers"`
}

// NewWeightLockEntry builds a WeightLockEntry from a set of packed tar
// layers. Annotations are cloned so later mutations on the LayerResult do
// not bleed into the entry.
func NewWeightLockEntry(name, target, manifestDigest string, layers []LayerResult) WeightLockEntry {
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
		Name:   name,
		Target: target,
		Digest: manifestDigest,
		Layers: lockLayers,
	}
}
