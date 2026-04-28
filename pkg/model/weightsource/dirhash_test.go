package weightsource

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDirHash_Deterministic(t *testing.T) {
	files := []InventoryFile{
		{Path: "a.txt", Digest: "sha256:aaa"},
		{Path: "b.txt", Digest: "sha256:bbb"},
	}
	d1 := DirHash(files)
	d2 := DirHash(files)
	require.Equal(t, d1, d2)
	assert.True(t, len(d1) > len("sha256:"), "digest must be non-trivial")
}

func TestDirHash_InputOrderIndependent(t *testing.T) {
	ordered := []InventoryFile{
		{Path: "a.txt", Digest: "sha256:aaa"},
		{Path: "b.txt", Digest: "sha256:bbb"},
	}
	reversed := []InventoryFile{
		{Path: "b.txt", Digest: "sha256:bbb"},
		{Path: "a.txt", Digest: "sha256:aaa"},
	}
	assert.Equal(t, DirHash(ordered), DirHash(reversed),
		"DirHash must sort internally — caller order must not matter")
}

func TestDirHash_DistinguishesContent(t *testing.T) {
	a := []InventoryFile{{Path: "f", Digest: "sha256:aaa"}}
	b := []InventoryFile{{Path: "f", Digest: "sha256:bbb"}}
	assert.NotEqual(t, DirHash(a), DirHash(b))
}

func TestDirHash_DistinguishesPath(t *testing.T) {
	a := []InventoryFile{{Path: "foo.txt", Digest: "sha256:aaa"}}
	b := []InventoryFile{{Path: "bar.txt", Digest: "sha256:aaa"}}
	assert.NotEqual(t, DirHash(a), DirHash(b))
}

func TestDirHash_EmptyInput(t *testing.T) {
	// Empty input hashes to sha256 of the empty string. Not a failure —
	// just documents the behavior.
	got := DirHash([]InventoryFile{})
	assert.Equal(t, "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", got)
}

// fakeFile lets us exercise DirHash with a hand-made Dirhashable type to
// confirm the generic constraint works for types outside this package.
type fakeFile struct {
	path   string
	digest string
}

func (f fakeFile) DirhashParts() DirhashPart {
	return DirhashPart{Path: f.path, Digest: f.digest}
}

func TestDirHash_ArbitraryType(t *testing.T) {
	// Confirm that DirHash is truly generic: any type implementing
	// Dirhashable should produce the same digest as an InventoryFile
	// carrying the same Path/Digest data.
	want := DirHash([]InventoryFile{
		{Path: "a.txt", Digest: "sha256:aaa"},
		{Path: "b.txt", Digest: "sha256:bbb"},
	})
	got := DirHash([]fakeFile{
		{path: "a.txt", digest: "sha256:aaa"},
		{path: "b.txt", digest: "sha256:bbb"},
	})
	assert.Equal(t, want, got)
}
