package model

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/model/weightsource"
)

// sampleEntry returns a fully-populated WeightLockEntry for tests.
func sampleEntry() WeightLockEntry {
	return WeightLockEntry{
		Name:   "z-image-turbo",
		Target: "/src/weights",
		Source: WeightLockSource{
			URI:         "file://./weights",
			Fingerprint: weightsource.Fingerprint("sha256:def456"),
			Include:     []string{},
			Exclude:     []string{},
			ImportedAt:  time.Date(2026, 4, 16, 17, 27, 7, 0, time.UTC),
		},
		Digest:         "sha256:abc123",
		SetDigest:      "sha256:def456",
		Size:           1500,
		SizeCompressed: 1200,
		Files: []WeightLockFile{
			{Path: "a.json", Size: 100, Digest: "sha256:f01", Layer: "sha256:aaa"},
			{Path: "b.bin", Size: 1400, Digest: "sha256:f02", Layer: "sha256:bbb"},
		},
		Layers: []WeightLockLayer{
			{Digest: "sha256:aaa", MediaType: mediaTypeOCILayerTarGzip, Size: 110, SizeUncompressed: 100},
			{Digest: "sha256:bbb", MediaType: mediaTypeOCILayerTar, Size: 1400, SizeUncompressed: 1400},
		},
	}
}

func TestWeightsLock_ParseValid(t *testing.T) {
	data := `{
		"version": 1,
		"weights": [
			{
				"name": "z-image-turbo",
				"target": "/src/weights",
				"source": {
					"uri": "file://./weights",
					"fingerprint": "sha256:def456",
					"include": [],
					"exclude": [],
					"importedAt": "2026-04-16T17:27:07Z"
				},
				"digest": "sha256:abc123",
				"setDigest": "sha256:def456",
				"size": 1500,
				"sizeCompressed": 1200,
				"files": [
					{"path": "a.json", "size": 100, "digest": "sha256:f01", "layer": "sha256:aaa"}
				],
				"layers": [
					{"digest": "sha256:aaa", "mediaType": "application/vnd.oci.image.layer.v1.tar+gzip", "size": 110, "sizeUncompressed": 100}
				]
			}
		]
	}`

	lock, err := ParseWeightsLock([]byte(data))
	require.NoError(t, err)
	assert.Equal(t, weightsLockVersion, lock.Version)
	require.Len(t, lock.Weights, 1)

	w := lock.Weights[0]
	assert.Equal(t, "z-image-turbo", w.Name)
	assert.Equal(t, "/src/weights", w.Target)
	assert.Equal(t, "sha256:abc123", w.Digest)
	assert.Equal(t, "sha256:def456", w.SetDigest)
	assert.Equal(t, int64(1500), w.Size)
	assert.Equal(t, int64(1200), w.SizeCompressed)

	assert.Equal(t, "file://./weights", w.Source.URI)
	assert.Equal(t, weightsource.Fingerprint("sha256:def456"), w.Source.Fingerprint)

	require.Len(t, w.Files, 1)
	assert.Equal(t, "a.json", w.Files[0].Path)
	assert.Equal(t, int64(100), w.Files[0].Size)
	assert.Equal(t, "sha256:aaa", w.Files[0].Layer)

	require.Len(t, w.Layers, 1)
	assert.Equal(t, "sha256:aaa", w.Layers[0].Digest)
	assert.Equal(t, mediaTypeOCILayerTarGzip, w.Layers[0].MediaType)
	assert.Equal(t, int64(110), w.Layers[0].Size)
	assert.Equal(t, int64(100), w.Layers[0].SizeUncompressed)
}

func TestWeightsLock_RejectsUnknownVersion(t *testing.T) {
	// The pre-release lockfile used version "v1" (string). The v1
	// schema uses integer 1; anything else is rejected.
	data := `{"version": "v1", "weights": []}`
	_, err := ParseWeightsLock([]byte(data))
	require.Error(t, err)

	data = `{"version": 2, "weights": []}`
	_, err = ParseWeightsLock([]byte(data))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported weights.lock version")
}

func TestWeightsLock_LoadFromFile(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "weights.lock")
	content := `{"version": 1, "weights": []}`
	require.NoError(t, os.WriteFile(lockPath, []byte(content), 0o644))

	lock, err := LoadWeightsLock(lockPath)
	require.NoError(t, err)
	assert.Equal(t, weightsLockVersion, lock.Version)
	assert.Empty(t, lock.Weights)
}

func TestWeightsLock_Save_SetsMissingVersion(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "weights.lock")

	lock := &WeightsLock{
		Weights: []WeightLockEntry{sampleEntry()},
	}
	require.NoError(t, lock.Save(lockPath))
	assert.Equal(t, weightsLockVersion, lock.Version, "Save fills in the missing version")

	loaded, err := LoadWeightsLock(lockPath)
	require.NoError(t, err)
	assert.Equal(t, weightsLockVersion, loaded.Version)
	require.Len(t, loaded.Weights, 1)
	assert.Equal(t, "z-image-turbo", loaded.Weights[0].Name)
}

func TestWeightsLock_Save_Deterministic(t *testing.T) {
	dir := t.TempDir()
	path1 := filepath.Join(dir, "a.lock")
	path2 := filepath.Join(dir, "b.lock")

	lock1 := &WeightsLock{Version: weightsLockVersion, Weights: []WeightLockEntry{sampleEntry()}}
	lock2 := &WeightsLock{Version: weightsLockVersion, Weights: []WeightLockEntry{sampleEntry()}}

	require.NoError(t, lock1.Save(path1))
	require.NoError(t, lock2.Save(path2))

	d1, err := os.ReadFile(path1)
	require.NoError(t, err)
	d2, err := os.ReadFile(path2)
	require.NoError(t, err)
	assert.Equal(t, d1, d2, "saving the same lockfile twice must be byte-identical")
}

func TestWeightsLock_Marshal_SortsFilesByPath(t *testing.T) {
	lock := &WeightsLock{
		Version: weightsLockVersion,
		Weights: []WeightLockEntry{
			{
				Name: "w",
				Files: []WeightLockFile{
					{Path: "z.txt", Size: 1, Digest: "sha256:z", Layer: "sha256:a"},
					{Path: "a.txt", Size: 1, Digest: "sha256:a", Layer: "sha256:a"},
					{Path: "m.txt", Size: 1, Digest: "sha256:m", Layer: "sha256:a"},
				},
				Layers: []WeightLockLayer{
					{Digest: "sha256:a", MediaType: mediaTypeOCILayerTar, Size: 1, SizeUncompressed: 1},
				},
			},
		},
	}
	_, err := lock.Marshal()
	require.NoError(t, err)

	got := []string{lock.Weights[0].Files[0].Path, lock.Weights[0].Files[1].Path, lock.Weights[0].Files[2].Path}
	assert.Equal(t, []string{"a.txt", "m.txt", "z.txt"}, got, "Marshal sorts files by path")
}

func TestWeightsLock_Marshal_SortsLayersByDigest(t *testing.T) {
	lock := &WeightsLock{
		Version: weightsLockVersion,
		Weights: []WeightLockEntry{
			{
				Name: "w",
				Layers: []WeightLockLayer{
					{Digest: "sha256:zzz", MediaType: mediaTypeOCILayerTar, Size: 1, SizeUncompressed: 1},
					{Digest: "sha256:aaa", MediaType: mediaTypeOCILayerTar, Size: 1, SizeUncompressed: 1},
					{Digest: "sha256:mmm", MediaType: mediaTypeOCILayerTar, Size: 1, SizeUncompressed: 1},
				},
			},
		},
	}
	_, err := lock.Marshal()
	require.NoError(t, err)

	got := []string{lock.Weights[0].Layers[0].Digest, lock.Weights[0].Layers[1].Digest, lock.Weights[0].Layers[2].Digest}
	assert.Equal(t, []string{"sha256:aaa", "sha256:mmm", "sha256:zzz"}, got, "Marshal sorts layers by digest")
}

func TestWeightsLock_Marshal_NormalizesEmptyPatterns(t *testing.T) {
	// Source.Include and Source.Exclude should serialize as [] (never
	// omitted) when empty or nil, so the schema shape is stable.
	lock := &WeightsLock{
		Version: weightsLockVersion,
		Weights: []WeightLockEntry{
			{Name: "w", Source: WeightLockSource{URI: "file://./x"}},
		},
	}
	data, err := lock.Marshal()
	require.NoError(t, err)
	assert.Contains(t, string(data), `"include": []`)
	assert.Contains(t, string(data), `"exclude": []`)
}

func TestWeightsLock_Upsert(t *testing.T) {
	t.Run("replaces existing entry", func(t *testing.T) {
		lock := &WeightsLock{
			Version: weightsLockVersion,
			Weights: []WeightLockEntry{
				{Name: "a", Target: "/a", Digest: "sha256:aaa"},
				{Name: "b", Target: "/b", Digest: "sha256:bbb"},
			},
		}

		lock.Upsert(WeightLockEntry{Name: "a", Target: "/a-new", Digest: "sha256:aaa2"})
		require.Len(t, lock.Weights, 2, "upsert replaces in place, does not append")

		got := lock.FindWeight("a")
		require.NotNil(t, got)
		assert.Equal(t, "/a-new", got.Target)
		assert.Equal(t, "sha256:aaa2", got.Digest)

		b := lock.FindWeight("b")
		require.NotNil(t, b)
		assert.Equal(t, "sha256:bbb", b.Digest)
	})

	t.Run("appends new entry", func(t *testing.T) {
		lock := &WeightsLock{Version: weightsLockVersion}
		lock.Upsert(WeightLockEntry{Name: "a", Target: "/a", Digest: "sha256:aaa"})
		lock.Upsert(WeightLockEntry{Name: "b", Target: "/b", Digest: "sha256:bbb"})

		require.Len(t, lock.Weights, 2)
		assert.Equal(t, "a", lock.Weights[0].Name)
		assert.Equal(t, "b", lock.Weights[1].Name)
	})
}

func TestWeightsLock_RoundTrip(t *testing.T) {
	original := &WeightsLock{
		Version: weightsLockVersion,
		Weights: []WeightLockEntry{sampleEntry()},
	}
	data, err := original.Marshal()
	require.NoError(t, err)

	var decoded WeightsLock
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, original.Version, decoded.Version)
	require.Len(t, decoded.Weights, 1)
	assert.Equal(t, original.Weights[0].Source.Fingerprint, decoded.Weights[0].Source.Fingerprint)
	assert.Equal(t, original.Weights[0].Files, decoded.Weights[0].Files)
	assert.Equal(t, original.Weights[0].Layers, decoded.Weights[0].Layers)
}

func TestLockEntriesContentEqual(t *testing.T) {
	a := sampleEntry()
	b := sampleEntry()
	assert.True(t, lockEntriesContentEqual(&a, &b), "identical entries are content-equal")

	c := sampleEntry()
	c.Digest = "sha256:different"
	assert.False(t, lockEntriesContentEqual(&a, &c), "differing manifest digest breaks equality")

	d := sampleEntry()
	d.SetDigest = "sha256:different"
	assert.False(t, lockEntriesContentEqual(&a, &d), "differing set digest breaks equality")

	e := sampleEntry()
	e.Files[0].Digest = "sha256:tampered"
	assert.False(t, lockEntriesContentEqual(&a, &e), "differing file digest breaks equality")

	f := sampleEntry()
	f.Size = 99999
	assert.False(t, lockEntriesContentEqual(&a, &f), "differing size breaks equality")
}

func TestLockEntriesSourceEqual(t *testing.T) {
	a := sampleEntry()
	b := sampleEntry()
	// Different importedAt must still be source-equal.
	b.Source.ImportedAt = a.Source.ImportedAt.Add(1 * time.Hour)
	assert.True(t, lockEntriesSourceEqual(&a, &b), "importedAt must not affect source equality")

	c := sampleEntry()
	c.Source.URI = "file://./different"
	assert.False(t, lockEntriesSourceEqual(&a, &c), "differing URI breaks source equality")

	d := sampleEntry()
	d.Source.Fingerprint = "sha256:different"
	assert.False(t, lockEntriesSourceEqual(&a, &d), "differing fingerprint breaks source equality")

	e := sampleEntry()
	e.Source.Include = []string{"*.safetensors"}
	assert.False(t, lockEntriesSourceEqual(&a, &e), "differing include patterns break source equality")

	f := sampleEntry()
	f.Source.Exclude = []string{"README*"}
	assert.False(t, lockEntriesSourceEqual(&a, &f), "differing exclude patterns break source equality")
}

func TestLockEntriesEqual_RequiresBothContentAndSource(t *testing.T) {
	a := sampleEntry()
	b := sampleEntry()
	assert.True(t, lockEntriesEqual(&a, &b))

	// Same content, different source — not equal.
	c := sampleEntry()
	c.Source.URI = "file://./other"
	assert.False(t, lockEntriesEqual(&a, &c))

	// Same source, different content — not equal.
	d := sampleEntry()
	d.Digest = "sha256:different"
	assert.False(t, lockEntriesEqual(&a, &d))
}
