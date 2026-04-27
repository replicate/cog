package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
)

// envelopeRevision counts byte-level packer changes the envelope
// struct can't capture on its own.
//
// SYNC: bump when the bytes a packer writes for a given (inventory,
// parameters) tuple change in a way the struct fields below don't
// reflect. Examples:
//
//   - tar header format flips (FormatPAX → FormatGNU) or changes to
//     deterministic header fields (mode, uid/gid, mtime, typeflag).
//   - file ordering inside a layer (today: small-file bundles sort
//     by path; large-file layers contain a single file).
//   - directory-entry insertion rules (today: every parent of every
//     packed file gets a deterministic dir header before the files).
//   - compressor framing changes (different gzip impl, header tweaks).
//   - default flips on existing parameters where the field stays the
//     same shape but the meaning of the value changes.
//
// Pure parameter changes — defaultBundleFileMax, incompressibleExts,
// gzip levels — are captured automatically by envelopeFromOptions and
// do NOT require a revision bump. Bias toward bumping when in doubt:
// missed bumps mean silent lockfile drift; unnecessary bumps mean
// one round of lockfile churn on next `cog weights import`.
const envelopeRevision = 1

// envelope captures every input that determines the packer's byte
// output for a given inventory: thresholds, gzip levels,
// incompressible extensions, layer media types, and envelopeRevision.
// Equal envelopes ⇒ byte-identical layer blobs for the same inventory.
//
// JSON tags are on-disk identifiers; renaming a field requires
// updating the snapshot digests in envelope_test.go.
type envelope struct {
	BundleFileMax       int64    `json:"bundleFileMax"`
	BundleSizeMax       int64    `json:"bundleSizeMax"`
	GzipLevelBundle     int      `json:"gzipLevelBundle"`
	GzipLevelLarge      int      `json:"gzipLevelLarge"`
	IncompressibleExts  []string `json:"incompressibleExts"` // sorted ascending
	MediaTypeCompressed string   `json:"mediaTypeCompressed"`
	MediaTypeRaw        string   `json:"mediaTypeRaw"`
	Revision            int      `json:"revision"`
}

// envelopeFromOptions builds the envelope describing the current
// packer behavior under opts. Every field reads the live value at
// the call site (defaults via packOptions methods, package-level
// constants for gzip levels and media types, the live
// incompressibleExts map) so a parameter change propagates to the
// digest without anyone having to remember to update the envelope.
//
// TODO: gzip levels and incompressibleExts live on package-level
// state in packer.go rather than on packOptions. Behavior is correct
// but the separation is muddled — revisit when packer grows
// configurable gzip.
func envelopeFromOptions(opts packOptions) envelope {
	exts := make([]string, 0, len(incompressibleExts))
	for ext := range incompressibleExts {
		exts = append(exts, ext)
	}
	slices.Sort(exts)

	return envelope{
		BundleFileMax:       opts.bundleFileMax(),
		BundleSizeMax:       opts.bundleSizeMax(),
		GzipLevelBundle:     gzipLevelBundle,
		GzipLevelLarge:      gzipLevelLarge,
		IncompressibleExts:  exts,
		MediaTypeCompressed: mediaTypeOCILayerTarGzip,
		MediaTypeRaw:        mediaTypeOCILayerTar,
		Revision:            envelopeRevision,
	}
}

// computeEnvelopeFormat returns the canonical sha256 digest of env
// (with "sha256:" prefix). Determinism rests on encoding/json's
// stable struct-field ordering and on IncompressibleExts being
// sorted at construction time.
func computeEnvelopeFormat(env envelope) (string, error) {
	data, err := json.Marshal(env)
	if err != nil {
		return "", fmt.Errorf("marshal envelope: %w", err)
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
