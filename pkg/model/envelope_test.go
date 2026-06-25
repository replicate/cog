package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// envelopeFormatChangeMessage explains why a snapshot mismatch
// matters and how to resolve it. The envelope digest is stamped into
// weights.lock; a mismatch on next `cog weights import` forces a
// recompute pass and rewrites the lockfile. Touching this snapshot is
// meaningful by design.
//
// When this assertion fails:
//
//  1. Intentional packer parameter change (defaultBundleFileMax,
//     incompressibleExts, gzip level)? Update the snapshot. Expect
//     lockfile churn on the next import — that's the whole point.
//
//  2. Changed the *bytes* the packer writes (tar header format, file
//     ordering, directory-entry emission, compressor framing) without
//     a parameter change? Bump envelopeRevision (see the SYNC block
//     on that constant) and update the snapshot.
//
//  3. Reverting an intentional change? Revert the snapshot too.
//
//  4. Don't know why this is failing? Probably touched envelope or
//     envelopeFromOptions accidentally — field shape, JSON tags,
//     field order. Fix that, don't update the snapshot.
const envelopeFormatChangeMessage = "envelope digest changed: see envelopeFormatChangeMessage in envelope_test.go for context and resolution steps"

// defaultEnvelopeFormatDigest is the snapshot digest of the
// zero-value packOptions envelope under envelopeRevision = 1.
//
// Update this only after reading envelopeFormatChangeMessage above.
const defaultEnvelopeFormatDigest = "sha256:ce2d53f8dd962ace393450e0abadbe227304897be87753a503b61f9c8525726e"

func TestEnvelopeFormat_DefaultIsStable(t *testing.T) {
	got, err := computeEnvelopeFormat(envelopeFromOptions(packOptions{}))
	require.NoError(t, err)
	assert.Equal(t, defaultEnvelopeFormatDigest, got, envelopeFormatChangeMessage)
}

func TestEnvelopeFormat_NonDefaults(t *testing.T) {
	// Snapshot table for non-default envelopes. Each row freezes the
	// digest under one explicit packOptions tweak. If a row breaks,
	// see envelopeFormatChangeMessage for resolution.
	cases := []struct {
		name   string
		opts   packOptions
		digest string
	}{
		{
			name:   "custom bundle file max",
			opts:   packOptions{BundleFileMax: 32 * 1024 * 1024},
			digest: "sha256:42f28ed027f791b53cbd282663de73971c32d3cad9cbb64de6504cadf42b248f",
		},
		{
			name:   "custom bundle size max",
			opts:   packOptions{BundleSizeMax: 128 * 1024 * 1024},
			digest: "sha256:3e009edfdfc3c4371ed17572723c4f19d7646a1282646fe008fcb95c955c1547",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := computeEnvelopeFormat(envelopeFromOptions(tc.opts))
			require.NoError(t, err)
			assert.Equal(t, tc.digest, got, envelopeFormatChangeMessage)
		})
	}
}

func TestEnvelopeFormat_Deterministic(t *testing.T) {
	// Two computations of the same envelope must produce the same
	// digest. Catches accidental nondeterminism (map iteration order
	// leaking into the JSON, time stamps, etc.).
	env := envelopeFromOptions(packOptions{})
	a, err := computeEnvelopeFormat(env)
	require.NoError(t, err)
	b, err := computeEnvelopeFormat(env)
	require.NoError(t, err)
	assert.Equal(t, a, b, "computeEnvelopeFormat must be deterministic")
}

func TestEnvelopeFormat_RevisionBumpChangesDigest(t *testing.T) {
	// Sanity check that bumping Revision changes the digest. Without
	// this, the SYNC block on envelopeRevision would be a comment
	// pointing at machinery that doesn't actually work.
	env := envelopeFromOptions(packOptions{})
	bumped := env
	bumped.Revision = env.Revision + 1

	a, err := computeEnvelopeFormat(env)
	require.NoError(t, err)
	b, err := computeEnvelopeFormat(bumped)
	require.NoError(t, err)
	assert.NotEqual(t, a, b,
		"bumping envelopeRevision must produce a different digest")
}

func TestEnvelopeFormat_FieldsCaptured(t *testing.T) {
	// Each field in envelope must contribute to the digest — otherwise
	// adding a field to track a new packer input is a silent no-op.
	// Mutate one field at a time and assert the digest changes.
	base := envelopeFromOptions(packOptions{})
	baseDigest, err := computeEnvelopeFormat(base)
	require.NoError(t, err)

	mutations := []struct {
		name string
		fn   func(*envelope)
	}{
		{"BundleFileMax", func(e *envelope) { e.BundleFileMax++ }},
		{"BundleSizeMax", func(e *envelope) { e.BundleSizeMax++ }},
		{"GzipLevelBundle", func(e *envelope) { e.GzipLevelBundle++ }},
		{"GzipLevelLarge", func(e *envelope) { e.GzipLevelLarge++ }},
		{"IncompressibleExts", func(e *envelope) { e.IncompressibleExts = append(e.IncompressibleExts, ".new") }},
		{"MediaTypeCompressed", func(e *envelope) { e.MediaTypeCompressed = "x/different" }},
		{"MediaTypeRaw", func(e *envelope) { e.MediaTypeRaw = "x/different" }},
		{"Revision", func(e *envelope) { e.Revision++ }},
	}
	for _, m := range mutations {
		t.Run(m.name, func(t *testing.T) {
			mutated := base
			// Clone slice so the mutation doesn't bleed into base.
			mutated.IncompressibleExts = append([]string(nil), base.IncompressibleExts...)
			m.fn(&mutated)
			got, err := computeEnvelopeFormat(mutated)
			require.NoError(t, err)
			assert.NotEqual(t, baseDigest, got,
				"mutating %s must change the digest; if not, the field isn't actually part of the envelope", m.name)
		})
	}
}
