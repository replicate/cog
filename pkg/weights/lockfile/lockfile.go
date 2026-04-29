// Package lockfile defines the on-disk weights.lock format and operations
// on it: parsing, loading, canonical serialization, and entry-level
// equality checks.
//
// The lockfile is Cog's source-of-truth for imported weights. It captures
// the source (URI + fingerprint + include/exclude), the resulting content
// (setDigest, files, layers), and the assembled OCI manifest digest.
// Everything downstream — OCI manifests, the runtime /.cog/weights.json,
// registry state validation — is a projection of these fields.
package lockfile

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"sort"
	"time"

	"github.com/replicate/cog/pkg/model/weightsource"
)

// WeightsLockFilename is the default filename for the weights lock file.
const WeightsLockFilename = "weights.lock"

// Version is the current lockfile format version.
//
// It is an integer; monotonic bumps (1 → 2) signal schema changes.
// Pre-release "v1" string versions have no migration path.
const Version = 1

// WeightsLock is the parsed representation of a weights.lock file.
//
// The serialized form is stable and deterministic: Weights is kept in
// insertion order (matching cog.yaml), every entry's Files slice is
// sorted by path, and every entry's Layers slice is sorted by digest.
// Regenerating the lockfile from the same source produces byte-identical
// output, which is what makes weights.lock safe to check into git.
type WeightsLock struct {
	Version int `json:"version"`
	// EnvelopeFormat is the sha256 digest (with "sha256:" prefix)
	// identifying the packer configuration that produced — or, on
	// the next import, will produce — the recorded layer digests.
	//
	// Cog stamps the current envelope digest into the lockfile on
	// every rewrite. On a subsequent import a mismatch (including a
	// missing/empty value, treated as "no match") forces the builder
	// to recompute layer digests from the local content store
	// instead of trusting the cached entry. See
	// pkg/model/envelope.go for what feeds into the digest.
	//
	// Empty when the lockfile has never been written by a
	// version of cog that knows about this field — that empty
	// value compares unequal to any current envelope digest, which
	// is exactly the "force a recompute" behavior we want.
	EnvelopeFormat string            `json:"envelopeFormat"`
	Weights        []WeightLockEntry `json:"weights"`
}

// WeightLockEntry is one declared weight in the lockfile.
//
// The entry carries everything needed to reproduce the OCI artifacts:
//   - identity of the source (Source block)
//   - content-addressable identity of the file set (SetDigest)
//   - per-file index mapping each file to its layer (Files)
//   - intrinsic layer properties for the manifest (Layers)
//   - the assembled manifest digest (Digest)
//
// No annotations are stored here; OCI presentation annotations are derived
// at manifest-build time from the typed fields (name, target, setDigest,
// etc.).
type WeightLockEntry struct {
	// Name is the weight's logical name (e.g. "z-image-turbo").
	Name string `json:"name"`
	// Target is the container mount path for this weight.
	Target string `json:"target"`
	// Source records where the weight came from and how it was filtered.
	Source WeightLockSource `json:"source"`
	// Digest is the sha256 digest of the assembled OCI manifest.
	Digest string `json:"digest"`
	// SetDigest is the weight set digest (spec §2.4): a content-addressable
	// identifier for the file set, independent of packing strategy.
	SetDigest string `json:"setDigest"`
	// Size is the total uncompressed size of all files in bytes (sum of
	// layer sizeUncompressed).
	Size int64 `json:"size"`
	// SizeCompressed is the total compressed layer size in bytes (sum of
	// layer size) — the bytes the registry stores.
	SizeCompressed int64 `json:"sizeCompressed"`
	// Files is the per-file index, sorted by path. Each entry records the
	// file's size, content digest, and which layer contains it.
	Files []WeightLockFile `json:"files"`
	// Layers is the set of packed tar layers, sorted by digest. Layer
	// emission order from the packer is not guaranteed stable (future
	// concurrency) — sorting produces deterministic output.
	Layers []WeightLockLayer `json:"layers"`
}

// WeightLockSource records provenance for a WeightLockEntry.
//
// An import is a pure function of (source URI, source fingerprint,
// include/exclude). Recording all four inputs plus the import timestamp
// makes the lockfile self-contained: given these fields and the source at
// Fingerprint, you can deterministically reproduce the Files/Layers that
// the entry describes.
type WeightLockSource struct {
	// URI is the normalized source URI (e.g. file://./weights,
	// hf://org/model, s3://bucket/prefix/).
	URI string `json:"uri"`
	// Fingerprint is the source's version identity at import time.
	// Scheme-prefixed (sha256:, commit:, etag:, …).
	Fingerprint weightsource.Fingerprint `json:"fingerprint"`
	// Include is the sorted list of glob-style include patterns applied
	// to the source. Sorted because order is not semantically meaningful
	// (the patterns are a set, not a sequence) and canonicalizing here
	// keeps the lockfile stable across reorderings in cog.yaml. Empty
	// patterns are serialized as [] so the shape is stable.
	Include []string `json:"include"`
	// Exclude is the sorted list of exclude patterns, same shape as Include.
	Exclude []string `json:"exclude"`
	// ImportedAt is the wall-clock time of the import that produced this
	// entry. It is informational only — it never participates in
	// equality checks (see EntriesEqual).
	ImportedAt time.Time `json:"importedAt"`
}

// WeightLockFile is a single file in a WeightLockEntry's Files index.
//
// This mirrors the config blob entry shape (spec §2.3) so the config blob
// can be projected directly from Files without a second walk of the
// source directory.
type WeightLockFile struct {
	// Path is the file path relative to the weight source directory,
	// with forward slashes regardless of host OS.
	Path string `json:"path"`
	// Size is the file's uncompressed size in bytes.
	Size int64 `json:"size"`
	// Digest is the sha256 content digest of the file (hex-encoded with
	// the "sha256:" prefix).
	Digest string `json:"digest"`
	// Layer is the digest of the layer containing this file.
	Layer string `json:"layer"`
}

// DirhashParts implements weightsource.Dirhashable so WeightLockFile
// slices can be passed directly to weightsource.DirHash.
func (f WeightLockFile) DirhashParts() weightsource.DirhashPart {
	return weightsource.DirhashPart{Path: f.Path, Digest: f.Digest}
}

// WeightLockLayer is an intrinsic description of a single packed tar layer.
//
// Only intrinsic properties live here — digest, mediaType, compressed size
// (Size), uncompressed size (SizeUncompressed). Layer content type
// ("bundle" vs "file") is not stored; it is derivable from Files (one
// file referencing the layer = single-file layer, many = bundle).
// Annotations are an OCI presentation detail and never stored in the
// lockfile.
type WeightLockLayer struct {
	// Digest is the sha256 digest of the layer blob.
	Digest string `json:"digest"`
	// MediaType is the OCI layer media type
	// (application/vnd.oci.image.layer.v1.tar or .tar+gzip).
	MediaType string `json:"mediaType"`
	// Size is the size of the layer blob in bytes (the bytes the
	// registry stores, post-compression for gzip layers).
	Size int64 `json:"size"`
	// SizeUncompressed is the sum of regular-file bytes in the layer,
	// matching the definition used for run.cog.weight.size.uncompressed
	// on index descriptors.
	SizeUncompressed int64 `json:"sizeUncompressed"`
}

// ParseWeightsLock parses a weights.lock JSON document and rejects
// anything that is not a supported lockfile version.
func ParseWeightsLock(data []byte) (*WeightsLock, error) {
	var lock WeightsLock
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, fmt.Errorf("parse weights.lock: %w", err)
	}
	if lock.Version != Version {
		return nil, fmt.Errorf("unsupported weights.lock version %d (want %d)",
			lock.Version, Version)
	}
	return &lock, nil
}

// LoadWeightsLock loads a weights.lock file from disk.
func LoadWeightsLock(path string) (*WeightsLock, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read weights.lock: %w", err)
	}
	return ParseWeightsLock(data)
}

// Save writes the weights.lock to disk in canonical JSON form.
//
// Save is deterministic: for any given WeightsLock value, repeated calls
// produce byte-identical output. It sorts each entry's Files by path and
// Layers by digest before serializing, normalizes empty Include/Exclude
// slices to [] (never omitted), and emits standard two-space indent.
func (wl *WeightsLock) Save(path string) error {
	data, err := wl.Marshal()
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil { //nolint:gosec // lockfile is checked into the repo
		return fmt.Errorf("write weights.lock: %w", err)
	}
	return nil
}

// Marshal serializes the lockfile to canonical JSON bytes. It applies the
// sort + normalization rules described on Save. Marshal mutates the
// receiver's entries in place (sorting their Files and Layers); this is
// safe because the sort order is the canonical order.
func (wl *WeightsLock) Marshal() ([]byte, error) {
	if wl.Version == 0 {
		wl.Version = Version
	}
	for i := range wl.Weights {
		canonicalize(&wl.Weights[i])
	}
	data, err := json.MarshalIndent(wl, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal weights.lock: %w", err)
	}
	return data, nil
}

// ComputeSetDigest returns the weight set digest (spec §2.4): the dirhash
// of the entry's file set. ComputeSetDigest canonicalizes the entry
// in place before hashing, so Files order at call time does not affect
// the result.
func (e *WeightLockEntry) ComputeSetDigest() string {
	canonicalize(e)
	return weightsource.DirHash(e.Files)
}

// RuntimeWeightsManifest is the in-image /.cog/weights.json file that
// signals managed weights to coglet. It is a minimal projection of the
// lockfile: only the fields coglet needs to know which weights to expect
// and where (spec §3.3).
type RuntimeWeightsManifest struct {
	Weights []RuntimeWeightEntry `json:"weights"`
}

// RuntimeWeightEntry is one weight in the runtime manifest. Three fields
// per entry: name, target, and the content-addressable set digest.
type RuntimeWeightEntry struct {
	Name      string `json:"name"`
	Target    string `json:"target"`
	SetDigest string `json:"setDigest"`
}

// RuntimeManifest projects the lockfile into the minimal runtime manifest
// written to /.cog/weights.json (spec §3.3). The result contains only the
// fields coglet needs: name, target, and setDigest per weight.
func (wl *WeightsLock) RuntimeManifest() *RuntimeWeightsManifest {
	entries := make([]RuntimeWeightEntry, len(wl.Weights))
	for i, w := range wl.Weights {
		entries[i] = RuntimeWeightEntry{
			Name:      w.Name,
			Target:    w.Target,
			SetDigest: w.SetDigest,
		}
	}
	return &RuntimeWeightsManifest{Weights: entries}
}

// canonicalize applies the serialization rules to a single entry:
// Files sorted by path, Layers sorted by digest, nil Include/Exclude
// normalized to [] so the shape is stable. Include/Exclude ordering is
// already canonical — WeightSpec sorts at construction, and all writes
// to WeightLockSource flow through a WeightSpec.
func canonicalize(e *WeightLockEntry) {
	sort.Slice(e.Files, func(i, j int) bool { return e.Files[i].Path < e.Files[j].Path })
	sort.Slice(e.Layers, func(i, j int) bool { return e.Layers[i].Digest < e.Layers[j].Digest })
	if e.Source.Include == nil {
		e.Source.Include = []string{}
	}
	if e.Source.Exclude == nil {
		e.Source.Exclude = []string{}
	}
}

// FindWeight returns the lockfile entry with the given name, or nil if no
// such entry exists.
func (wl *WeightsLock) FindWeight(name string) *WeightLockEntry {
	for i := range wl.Weights {
		if wl.Weights[i].Name == name {
			return &wl.Weights[i]
		}
	}
	return nil
}

// Retain removes any entries whose Name is not in keep. The order of
// surviving entries is preserved. Retain is used after a full import
// pass to prune weights that were removed from cog.yaml.
func (wl *WeightsLock) Retain(keep []string) {
	set := make(map[string]bool, len(keep))
	for _, n := range keep {
		set[n] = true
	}
	kept := make([]WeightLockEntry, 0, len(keep))
	for _, e := range wl.Weights {
		if set[e.Name] {
			kept = append(kept, e)
		}
	}
	wl.Weights = kept
}

// PruneLockfile removes lockfile entries whose names are not in keep.
// It is a no-op when the lockfile does not exist or when nothing would
// change, avoiding unnecessary file rewrites (which churn git diffs).
func PruneLockfile(lockPath string, keep []string) error {
	lock, err := LoadWeightsLock(lockPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	before := len(lock.Weights)
	lock.Retain(keep)
	if len(lock.Weights) == before {
		return nil // nothing pruned
	}

	return lock.Save(lockPath)
}

// Upsert inserts or replaces the entry with the matching Name. It leaves
// all other entries in place and untouched.
func (wl *WeightsLock) Upsert(entry WeightLockEntry) {
	for i := range wl.Weights {
		if wl.Weights[i].Name == entry.Name {
			wl.Weights[i] = entry
			return
		}
	}
	wl.Weights = append(wl.Weights, entry)
}

// EntriesEqual reports whether two entries are identical in both content
// and source. ImportedAt is intentionally excluded — it is a consequence
// of an import being written, not an input to the equality check.
//
// A lockfile entry is safe to leave unchanged only when both a and b are
// non-nil and every field (besides ImportedAt) agrees.
func EntriesEqual(a, b *WeightLockEntry) bool {
	return entriesContentEqual(a, b) && entriesSourceEqual(a, b)
}

// entriesContentEqual reports whether two entries describe identical
// on-registry content: same manifest digest, same set digest, same total
// sizes, same file index, same layer descriptors.
func entriesContentEqual(a, b *WeightLockEntry) bool {
	if a == nil || b == nil {
		return false
	}
	if a.Name != b.Name || a.Target != b.Target ||
		a.Digest != b.Digest || a.SetDigest != b.SetDigest ||
		a.Size != b.Size || a.SizeCompressed != b.SizeCompressed {
		return false
	}
	if len(a.Files) != len(b.Files) {
		return false
	}
	for i := range a.Files {
		if a.Files[i] != b.Files[i] {
			return false
		}
	}
	if len(a.Layers) != len(b.Layers) {
		return false
	}
	for i := range a.Layers {
		if a.Layers[i] != b.Layers[i] {
			return false
		}
	}
	return true
}

// entriesSourceEqual reports whether two entries have identical source
// metadata: same URI, same fingerprint, same include/exclude patterns.
// ImportedAt is intentionally excluded.
func entriesSourceEqual(a, b *WeightLockEntry) bool {
	if a == nil || b == nil {
		return false
	}
	if a.Source.URI != b.Source.URI || a.Source.Fingerprint != b.Source.Fingerprint {
		return false
	}
	if !slices.Equal(a.Source.Include, b.Source.Include) {
		return false
	}
	if !slices.Equal(a.Source.Exclude, b.Source.Exclude) {
		return false
	}
	return true
}
