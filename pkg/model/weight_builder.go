package model

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/types"

	"github.com/replicate/cog/pkg/model/weightsource"
	"github.com/replicate/cog/pkg/weights/lockfile"
	"github.com/replicate/cog/pkg/weights/store"
)

// WeightBuilder is the weight factory: given a WeightSpec (source URI +
// target), it ingresses the source files into the local
// content-addressed store, plans tar layers, derives layer digests
// (cache-fast or recompute), assembles the v1 OCI manifest, and
// returns a WeightArtifact carrying the layer descriptors and
// manifest digest.
//
// `cog weights import` ≡ `cog weights import + cog weights pull` —
// the build path leaves the local store warm so subsequent `cog
// predict` invocations can hardlink-assemble without a separate pull.
//
// The builder is offline: it never talks to a registry. The manifest
// digest it writes into the artifact descriptor is a sha256 of the
// serialized manifest bytes.
type WeightBuilder struct {
	source   *Source
	store    store.Store
	lockPath string
}

// NewWeightBuilder creates a WeightBuilder.
//
// st is the local content-addressed weight store. lockPath is where
// weights.lock is read/written.
func NewWeightBuilder(source *Source, st store.Store, lockPath string) *WeightBuilder {
	return &WeightBuilder{source: source, store: st, lockPath: lockPath}
}

// Build runs the full import pipeline for one weight:
//
//  1. Inventory the source.
//  2. Ingress every file into the local store (skipping already-present
//     digests). Hash mismatches surface here.
//  3. Decide whether to trust the lockfile's recorded layer digests
//     (fast path) or recompute them by streaming from the store
//     (recompute path).
//  4. Assemble the OCI manifest.
//  5. Stamp the current envelope format into the lockfile and rewrite
//     it iff anything actually changed.
//
// The push path is independent of Build; the caller is responsible
// for handing the returned artifact to the pusher (which checks
// per-layer with BlobExists before uploading).
func (b *WeightBuilder) Build(ctx context.Context, spec ArtifactSpec) (Artifact, error) {
	ws, ok := spec.(*WeightSpec)
	if !ok {
		return nil, fmt.Errorf("weight builder: expected *WeightSpec, got %T", spec)
	}
	if b.store == nil {
		return nil, fmt.Errorf("weight builder: store is required")
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	projectDir := b.projectDir()

	src, err := weightsource.For(ws.URI, projectDir)
	if err != nil {
		return nil, err
	}

	// Step 1+2: inventory + ingress. Always. Import is a source read
	// by definition; the store gets warmed as a side effect, which is
	// the whole point of this code.
	inv, err := src.Inventory(ctx)
	if err != nil {
		return nil, fmt.Errorf("inventory weight %q: %w", ws.Name(), err)
	}

	if err := ingressFromInventory(ctx, src, b.store, inv); err != nil {
		return nil, fmt.Errorf("populate store for weight %q: %w", ws.Name(), err)
	}

	// Step 3: decide fast-path vs recompute.
	lock, err := loadLockfileOrEmpty(b.lockPath)
	if err != nil {
		return nil, err
	}

	currentEnvelope, err := computeEnvelopeFormat(envelopeFromOptions(packOptions{}))
	if err != nil {
		return nil, fmt.Errorf("compute envelope format: %w", err)
	}

	existing := lock.FindWeight(ws.Name())
	pkr := newPacker(nil)
	plan := pkr.planLayers(inv)
	if len(plan.Layers) == 0 {
		return nil, fmt.Errorf("weight %q: inventory is empty", ws.Name())
	}

	var layers []packedLayer
	if canFastPath(lock, currentEnvelope, existing, ws, inv) {
		layers, err = layersFromLockfile(existing, plan)
		if err != nil {
			// Lockfile and freshly-planned layers disagree on
			// shape. Treat as a cache miss: recompute. This can
			// happen if the user edited weights.lock by hand.
			layers = nil
		}
	}
	if layers == nil {
		layers, err = pkr.computeLayerDigests(ctx, b.store, plan)
		if err != nil {
			return nil, fmt.Errorf("compute layer digests for weight %q: %w", ws.Name(), err)
		}
	}

	entry := newWeightLockEntry(
		ws.Name(), ws.Target,
		lockfile.WeightLockSource{
			URI:         ws.URI,
			Fingerprint: inv.Fingerprint,
			Include:     ws.Include,
			Exclude:     ws.Exclude,
			ImportedAt:  time.Now().UTC(),
		},
		packedFilesFromPlan(layers),
		layers,
	)

	// buildWeightArtifact populates entry.Digest (the manifest
	// digest), which EntriesEqual needs to compare meaningfully.
	artifact, err := buildWeightArtifact(&entry, layers, b.store)
	if err != nil {
		return nil, fmt.Errorf("weight %q: %w", ws.Name(), err)
	}

	// Preserve the original ImportedAt on a content-equal rewrite so
	// a format-bump-only rewrite doesn't churn the timestamp.
	// EntriesEqual ignores ImportedAt by design, so this comparison
	// answers "would the lockfile diff be only the timestamp?"
	entryEqualsExisting := lockfile.EntriesEqual(existing, &entry)
	if entryEqualsExisting {
		entry.Source.ImportedAt = existing.Source.ImportedAt
	}

	// Step 5: stamp envelope + rewrite iff anything changed.
	formatChanged := lock.EnvelopeFormat != currentEnvelope
	lock.EnvelopeFormat = currentEnvelope
	if formatChanged || !entryEqualsExisting {
		lock.Upsert(entry)
		if err := lock.Save(b.lockPath); err != nil {
			return nil, fmt.Errorf("update lockfile: %w", err)
		}
	}

	return artifact, nil
}

// projectDir returns the builder's project directory, or "" if the
// builder was constructed without a Source.
func (b *WeightBuilder) projectDir() string {
	if b.source == nil {
		return ""
	}
	return b.source.ProjectDir
}

// canFastPath reports whether the recorded lockfile entry can be
// trusted as-is for this build, allowing us to skip the
// digest-recomputation pass.
//
// Every input that determines layer bytes must agree:
//
//   - The recorded EnvelopeFormat matches the current packer config.
//     A miss here means cog itself produces different bytes for the
//     same inventory than the version that wrote the lockfile did.
//   - An entry with this name exists.
//   - The user-intent fields (target, URI, include/exclude) match.
//   - The source's fingerprint matches what the lockfile recorded.
//     For file:// the fingerprint is the dirhash of the file set,
//     so matching fingerprint ⇒ matching files. For hf:// it's the
//     commit SHA, so matching fingerprint ⇒ same canonical files.
//
// Anything missing pushes us onto the recompute path. Recompute is
// cheap because the store is already warm (local I/O + sha256 +
// gzip) — the cost is local CPU, not network or source-side I/O.
func canFastPath(
	lock *lockfile.WeightsLock,
	currentEnvelope string,
	existing *lockfile.WeightLockEntry,
	ws *WeightSpec,
	inv weightsource.Inventory,
) bool {
	if lock.EnvelopeFormat != currentEnvelope {
		return false
	}
	if existing == nil {
		return false
	}
	if existing.Target != ws.Target ||
		existing.Source.URI != ws.URI ||
		!slices.Equal(existing.Source.Include, ws.Include) ||
		!slices.Equal(existing.Source.Exclude, ws.Exclude) {
		return false
	}
	if existing.Source.Fingerprint != inv.Fingerprint {
		return false
	}
	return true
}

// layersFromLockfile reconstructs []packedLayer from a lockfile entry,
// pairing each recorded layer with the corresponding layerPlan from
// the freshly-planned layout. The plan is what reproduces layer bytes
// during push, so the planning result must be available even on the
// fast path.
//
// Returns an error if the lockfile and plan disagree on how many
// layers there are or which files they contain — a strong signal
// that the lockfile is out of sync, which makes the fast path
// unsafe.
func layersFromLockfile(entry *lockfile.WeightLockEntry, pl plan) ([]packedLayer, error) {
	if len(entry.Layers) != len(pl.Layers) {
		return nil, fmt.Errorf("layer count mismatch: lockfile has %d, plan has %d", len(entry.Layers), len(pl.Layers))
	}

	// Index plan layers by their content signature so we can match
	// them to lockfile layers regardless of ordering. Signature is
	// the sorted file digests within the layer; that's what
	// determines the tar bytes.
	planByKey := make(map[string]layerPlan, len(pl.Layers))
	for _, lp := range pl.Layers {
		planByKey[layerKey(lp)] = lp
	}

	out := make([]packedLayer, 0, len(entry.Layers))
	for _, lk := range entry.Layers {
		// Find the plan layer whose files match this locked layer.
		// We compare by file digests (the file→layer mapping in
		// entry.Files tells us which files belong to this layer).
		want := lockedLayerKey(entry, lk.Digest)
		lp, ok := planByKey[want]
		if !ok {
			return nil, fmt.Errorf("locked layer %s has no matching plan layer", lk.Digest)
		}
		hash, err := v1.NewHash(lk.Digest)
		if err != nil {
			return nil, fmt.Errorf("parse locked layer digest %s: %w", lk.Digest, err)
		}
		out = append(out, packedLayer{
			Plan:             lp,
			Digest:           hash,
			Size:             lk.Size,
			UncompressedSize: lk.SizeUncompressed,
			MediaType:        types.MediaType(lk.MediaType),
		})
	}
	return out, nil
}

// loadLockfileOrEmpty loads the lockfile at path. A missing file is not
// an error — it yields a fresh empty lockfile.
func loadLockfileOrEmpty(path string) (*lockfile.WeightsLock, error) {
	lock, err := lockfile.LoadWeightsLock(path)
	if err == nil {
		return lock, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return &lockfile.WeightsLock{Version: lockfile.Version}, nil
	}
	return nil, err
}

// layerKey returns a content signature for a layerPlan: the joined
// file digests in tar-emission order. Two planLayers with identical
// keys produce identical tar bytes and therefore identical layer
// digests (modulo envelope-format concerns, which the caller handles
// separately).
func layerKey(lp layerPlan) string {
	digests := make([]string, len(lp.Files))
	for i, f := range lp.Files {
		digests[i] = f.Digest
	}
	return strings.Join(digests, "\n")
}

// lockedLayerKey returns the layerKey for a recorded layer in entry,
// reconstructed by collecting the file digests of every entry.Files
// member that points at the given layerDigest.
//
// Result is sorted by inventory path so it matches a planLayer whose
// Files are in the packer's emission order (small-file bundles are
// path-sorted; single-file layers carry one file). For multi-file
// bundles, both this function and planLayers sort by path; for
// single-file layers, both have one entry. Either way the keys match
// when the underlying file set matches.
func lockedLayerKey(entry *lockfile.WeightLockEntry, layerDigest string) string {
	type fd struct {
		path   string
		digest string
	}
	var fs []fd
	for _, f := range entry.Files {
		if f.Layer == layerDigest {
			fs = append(fs, fd{path: f.Path, digest: f.Digest})
		}
	}
	slices.SortFunc(fs, func(a, b fd) int { return strings.Compare(a.path, b.path) })
	digests := make([]string, len(fs))
	for i, f := range fs {
		digests[i] = f.digest
	}
	return strings.Join(digests, "\n")
}
