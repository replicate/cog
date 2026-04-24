package weights

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/registry"
	"github.com/replicate/cog/pkg/registry/registrytest"
	"github.com/replicate/cog/pkg/weights/lockfile"
	"github.com/replicate/cog/pkg/weights/store"
)

// ---------------------------------------------------------------------------
// Test fixtures: in-memory layers + registry stub.
// ---------------------------------------------------------------------------

// rawTarLayer implements v1.Layer over a fixed byte slice of uncompressed
// tar data. Digest/DiffID are computed from the bytes so LayerByDigest
// lookups resolve correctly.
type rawTarLayer struct {
	bytes []byte
	hash  v1.Hash
}

func newRawTarLayer(data []byte) *rawTarLayer {
	sum := sha256.Sum256(data)
	return &rawTarLayer{
		bytes: data,
		hash:  v1.Hash{Algorithm: "sha256", Hex: hex.EncodeToString(sum[:])},
	}
}

func (l *rawTarLayer) Digest() (v1.Hash, error) { return l.hash, nil }
func (l *rawTarLayer) DiffID() (v1.Hash, error) { return l.hash, nil }
func (l *rawTarLayer) Size() (int64, error)     { return int64(len(l.bytes)), nil }
func (l *rawTarLayer) MediaType() (types.MediaType, error) {
	return types.OCILayer, nil // uncompressed tar
}

func (l *rawTarLayer) Compressed() (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(l.bytes)), nil
}

func (l *rawTarLayer) Uncompressed() (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(l.bytes)), nil
}

// truncatedLayer wraps a rawTarLayer but its Uncompressed reader
// returns the first `cutoff` bytes of the tar and then surfaces a
// read error. Simulates a flaky network / truncated blob mid-stream.
type truncatedLayer struct {
	*rawTarLayer
	cutoff int
}

func (l *truncatedLayer) Uncompressed() (io.ReadCloser, error) {
	head := l.bytes
	if l.cutoff < len(head) {
		head = head[:l.cutoff]
	}
	return io.NopCloser(io.MultiReader(
		bytes.NewReader(head),
		errReader{err: errors.New("simulated blob truncation")},
	)), nil
}

// errReader always returns the configured error on Read.
type errReader struct{ err error }

func (r errReader) Read(_ []byte) (int, error) { return 0, r.err }

// buildLayer returns (tarBytes, []WeightLockFile describing its content).
// Each file's Layer field is filled with layerDigest once known.
func buildLayer(t *testing.T, files map[string][]byte) ([]byte, []lockfile.WeightLockFile) {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	lockFiles := make([]lockfile.WeightLockFile, 0, len(files))

	// Stable iteration order so digests are deterministic across runs.
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	// Simple insertion-order stability is enough for tests.
	// Emit directory headers first (mirrors the real packer's behavior
	// so we exercise the "skip non-regular entries" branch in pullLayer).
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Typeflag: tar.TypeDir,
		Name:     "./",
	}))
	for _, p := range paths {
		data := files[p]
		require.NoError(t, tw.WriteHeader(&tar.Header{
			Typeflag: tar.TypeReg,
			Name:     p,
			Size:     int64(len(data)),
		}))
		_, err := tw.Write(data)
		require.NoError(t, err)

		sum := sha256.Sum256(data)
		lockFiles = append(lockFiles, lockfile.WeightLockFile{
			Path:   p,
			Size:   int64(len(data)),
			Digest: "sha256:" + hex.EncodeToString(sum[:]),
		})
	}
	require.NoError(t, tw.Close())
	return buf.Bytes(), lockFiles
}

// buildWeightImage returns a v1.Image + the manifest digest + the final
// lockfile entry for a weight whose layers contain the given per-layer
// file maps.
func buildWeightImage(t *testing.T, name, target string, layerFiles []map[string][]byte) (v1.Image, *lockfile.WeightLockEntry) {
	t.Helper()

	img := empty.Image
	img = mutate.MediaType(img, types.OCIManifestSchema1)

	var allFiles []lockfile.WeightLockFile
	var lockLayers []lockfile.WeightLockLayer

	for _, files := range layerFiles {
		tarBytes, fs := buildLayer(t, files)
		layer := newRawTarLayer(tarBytes)
		digest, err := layer.Digest()
		require.NoError(t, err)
		size, _ := layer.Size()

		// Attach layer digest to each lock file.
		for i := range fs {
			fs[i].Layer = digest.String()
		}
		allFiles = append(allFiles, fs...)
		lockLayers = append(lockLayers, lockfile.WeightLockLayer{
			Digest:           digest.String(),
			MediaType:        string(types.OCILayer),
			Size:             size,
			SizeUncompressed: size,
		})

		img, err = mutate.Append(img, mutate.Addendum{Layer: layer})
		require.NoError(t, err)
	}

	manifestDigest, err := img.Digest()
	require.NoError(t, err)

	return img, &lockfile.WeightLockEntry{
		Name:   name,
		Target: target,
		Digest: manifestDigest.String(),
		Files:  allFiles,
		Layers: lockLayers,
	}
}

// stubRegistry composes MockRegistryClient and overrides GetImage to
// return real in-memory v1.Image values (which the mock returns nil
// for). Tests that want to assert Pull doesn't touch the registry set
// getImageErr.
type stubRegistry struct {
	*registrytest.MockRegistryClient
	images      map[string]v1.Image
	getImageErr error
}

func newStubRegistry() *stubRegistry {
	return &stubRegistry{
		MockRegistryClient: registrytest.NewMockRegistryClient(),
		images:             map[string]v1.Image{},
	}
}

func (s *stubRegistry) put(ref string, img v1.Image) { s.images[ref] = img }

func (s *stubRegistry) GetImage(_ context.Context, ref string, _ *registry.Platform) (v1.Image, error) {
	if s.getImageErr != nil {
		return nil, s.getImageErr
	}
	img, ok := s.images[ref]
	if !ok {
		return nil, fmt.Errorf("stub registry: no image at %s", ref)
	}
	return img, nil
}

const testRepo = "example.com/me/model"

func newTestManager(t *testing.T, reg registry.Client, lock *lockfile.WeightsLock) (*Manager, store.Store) {
	t.Helper()
	fs, err := store.NewFileStore(t.TempDir())
	require.NoError(t, err)
	mgr, err := NewManager(ManagerOptions{
		Store:      fs,
		Registry:   reg,
		Repo:       testRepo,
		Lock:       lock,
		ProjectDir: t.TempDir(),
	})
	require.NoError(t, err)
	return mgr, fs
}

func TestManager_Pull_HappyPath(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	reg := newStubRegistry()

	img, entry := buildWeightImage(t, "m1", "/src/weights", []map[string][]byte{
		{"a.txt": []byte("alpha bytes"), "b.txt": []byte("bravo bytes")},
		{"c.bin": []byte("charlie bytes")},
	})
	reg.put(testRepo+"@"+entry.Digest, img)

	lock := &lockfile.WeightsLock{Version: 1, Weights: []lockfile.WeightLockEntry{*entry}}
	mgr, fs := newTestManager(t, reg, lock)

	results, err := mgr.Pull(ctx, nil, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "m1", results[0].Name)
	assert.False(t, results[0].FullyCached)
	assert.Equal(t, 3, results[0].FilesFetched)
	assert.Equal(t, 2, results[0].LayersFetched)

	// Every file is now in the store.
	for _, f := range entry.Files {
		ok, err := fs.Exists(ctx, f.Digest)
		require.NoError(t, err)
		require.True(t, ok, "file %s should be cached", f.Path)
	}
}

func TestManager_Pull_AllCached(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	reg := newStubRegistry()

	img, entry := buildWeightImage(t, "m1", "/w", []map[string][]byte{
		{"x": []byte("data")},
	})
	reg.put(testRepo+"@"+entry.Digest, img)

	lock := &lockfile.WeightsLock{Version: 1, Weights: []lockfile.WeightLockEntry{*entry}}
	mgr, fs := newTestManager(t, reg, lock)

	// Pre-populate the store.
	for _, f := range entry.Files {
		require.NoError(t, fs.PutFile(ctx, f.Digest, f.Size, bytes.NewReader([]byte("data"))))
	}

	// Make the registry explode if touched — a fully-cached pull must
	// not call it.
	reg.getImageErr = errors.New("registry should not be touched for cached pull")

	results, err := mgr.Pull(ctx, nil, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.True(t, results[0].FullyCached)
	assert.Equal(t, 0, results[0].FilesFetched)
}

func TestManager_Pull_Idempotent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	reg := newStubRegistry()

	img, entry := buildWeightImage(t, "m1", "/w", []map[string][]byte{
		{"x": []byte("one"), "y": []byte("two")},
	})
	reg.put(testRepo+"@"+entry.Digest, img)

	lock := &lockfile.WeightsLock{Version: 1, Weights: []lockfile.WeightLockEntry{*entry}}
	mgr, _ := newTestManager(t, reg, lock)

	// First pull populates.
	results, err := mgr.Pull(ctx, nil, nil)
	require.NoError(t, err)
	assert.False(t, results[0].FullyCached)

	// Second pull is a no-op.
	results, err = mgr.Pull(ctx, nil, nil)
	require.NoError(t, err)
	assert.True(t, results[0].FullyCached)
}

func TestManager_Pull_DigestMismatchInTar(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	reg := newStubRegistry()

	img, entry := buildWeightImage(t, "m1", "/w", []map[string][]byte{
		{"a": []byte("legitimate content")},
	})
	// Corrupt the lockfile's expected digest so the bytes from the
	// registry fail verification. The manifest digest stays valid
	// (that addresses the manifest, not the file content).
	entry.Files[0].Digest = "sha256:" + hex.EncodeToString(make([]byte, 32))
	reg.put(testRepo+"@"+entry.Digest, img)

	lock := &lockfile.WeightsLock{Version: 1, Weights: []lockfile.WeightLockEntry{*entry}}
	mgr, fs := newTestManager(t, reg, lock)

	_, err := mgr.Pull(ctx, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "digest mismatch")

	// Store is unchanged.
	ok, err := fs.Exists(ctx, entry.Files[0].Digest)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestManager_Pull_UnexpectedFileInTar(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	reg := newStubRegistry()

	// Build an image whose layer tar contains a file the lockfile
	// doesn't describe. Constructing this by hand: write "a" + "b"
	// into the tar, but only list "a" in the lockfile.
	tarBytes, lockFiles := buildLayer(t, map[string][]byte{
		"a": []byte("known"),
		"b": []byte("secret extra"),
	})
	layer := newRawTarLayer(tarBytes)
	layerDigest, _ := layer.Digest()
	layerSize, _ := layer.Size()

	img := empty.Image
	img = mutate.MediaType(img, types.OCIManifestSchema1)
	img, err := mutate.Append(img, mutate.Addendum{Layer: layer})
	require.NoError(t, err)
	manifestDigest, err := img.Digest()
	require.NoError(t, err)

	// Keep only the "a" entry in the lockfile.
	var keep lockfile.WeightLockFile
	for _, f := range lockFiles {
		if f.Path == "a" {
			keep = f
			keep.Layer = layerDigest.String()
		}
	}
	entry := lockfile.WeightLockEntry{
		Name:   "m1",
		Target: "/w",
		Digest: manifestDigest.String(),
		Files:  []lockfile.WeightLockFile{keep},
		Layers: []lockfile.WeightLockLayer{{
			Digest:           layerDigest.String(),
			MediaType:        string(types.OCILayer),
			Size:             layerSize,
			SizeUncompressed: layerSize,
		}},
	}

	reg.put(testRepo+"@"+entry.Digest, img)
	lock := &lockfile.WeightsLock{Version: 1, Weights: []lockfile.WeightLockEntry{entry}}
	mgr, _ := newTestManager(t, reg, lock)

	_, err = mgr.Pull(ctx, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not in lockfile")
}

func TestManager_Pull_LayerReadError(t *testing.T) {
	t.Parallel()
	// Simulate a flaky network: the layer blob read errors partway
	// through the tar stream. The pull must fail with the underlying
	// read error, and the partially-written file must not appear in
	// the store (PutFile's temp+rename atomicity).
	ctx := context.Background()
	reg := newStubRegistry()

	// Build a layer with one small file and one large enough that
	// cutting the tar part-way through the file body triggers a mid-
	// stream read error.
	small := []byte("small")
	big := bytes.Repeat([]byte("X"), 8192)
	tarBytes, lockFiles := buildLayer(t, map[string][]byte{
		"small": small,
		"big":   big,
	})

	// Wrap the layer so Uncompressed returns a truncated reader. The
	// cutoff sits inside the payload of the second file.
	rawLayer := newRawTarLayer(tarBytes)
	layer := &truncatedLayer{rawTarLayer: rawLayer, cutoff: 1024}
	layerDigest, _ := layer.Digest()
	layerSize, _ := layer.Size()

	for i := range lockFiles {
		lockFiles[i].Layer = layerDigest.String()
	}

	img := empty.Image
	img = mutate.MediaType(img, types.OCIManifestSchema1)
	img, err := mutate.Append(img, mutate.Addendum{Layer: layer})
	require.NoError(t, err)
	manifestDigest, err := img.Digest()
	require.NoError(t, err)

	entry := lockfile.WeightLockEntry{
		Name:   "m1",
		Target: "/w",
		Digest: manifestDigest.String(),
		Files:  lockFiles,
		Layers: []lockfile.WeightLockLayer{{
			Digest:           layerDigest.String(),
			MediaType:        string(types.OCILayer),
			Size:             layerSize,
			SizeUncompressed: layerSize,
		}},
	}
	reg.put(testRepo+"@"+entry.Digest, img)

	lock := &lockfile.WeightsLock{Version: 1, Weights: []lockfile.WeightLockEntry{entry}}
	mgr, fs := newTestManager(t, reg, lock)

	_, err = mgr.Pull(ctx, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "truncation")

	// The partially-received file must not be exposed in the store.
	// At most the fully-streamed file (whichever the tar emitted
	// first) may be present — the torn one never is.
	for _, f := range entry.Files {
		ok, err := fs.Exists(ctx, f.Digest)
		require.NoError(t, err)
		if ok {
			// If something landed, it must be the small file whose
			// payload fit entirely before cutoff. The digest test
			// below proves integrity.
			require.Equal(t, sha256Of(small), f.Digest,
				"only the fully-streamed small file may be present; torn files must not appear")
		}
	}
}

func TestManager_Pull_LayerMissingExpectedFile(t *testing.T) {
	t.Parallel()
	// The lockfile claims layer L contains files A and B. The layer
	// tar in the registry only has A. Pull must fail for the weight
	// with a "missing expected file" error — guarding against
	// registry/lockfile drift.
	ctx := context.Background()
	reg := newStubRegistry()

	// Construct a layer tar that only contains "a".
	tarBytes, lockFiles := buildLayer(t, map[string][]byte{
		"a": []byte("alpha content"),
	})
	layer := newRawTarLayer(tarBytes)
	layerDigest, _ := layer.Digest()
	layerSize, _ := layer.Size()

	img := empty.Image
	img = mutate.MediaType(img, types.OCIManifestSchema1)
	img, err := mutate.Append(img, mutate.Addendum{Layer: layer})
	require.NoError(t, err)
	manifestDigest, err := img.Digest()
	require.NoError(t, err)

	// Lockfile claims both "a" and "b" live in this layer — "b" is
	// fabricated to trigger the post-walk missing-file check.
	aFile := lockFiles[0]
	aFile.Layer = layerDigest.String()
	fakeB := lockfile.WeightLockFile{
		Path:   "b",
		Size:   5,
		Digest: sha256Of([]byte("bravo")),
		Layer:  layerDigest.String(),
	}

	entry := lockfile.WeightLockEntry{
		Name:   "m1",
		Target: "/w",
		Digest: manifestDigest.String(),
		Files:  []lockfile.WeightLockFile{aFile, fakeB},
		Layers: []lockfile.WeightLockLayer{{
			Digest:           layerDigest.String(),
			MediaType:        string(types.OCILayer),
			Size:             layerSize,
			SizeUncompressed: layerSize,
		}},
	}
	reg.put(testRepo+"@"+entry.Digest, img)

	lock := &lockfile.WeightsLock{Version: 1, Weights: []lockfile.WeightLockEntry{entry}}
	mgr, _ := newTestManager(t, reg, lock)

	_, err = mgr.Pull(ctx, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing expected file")
	assert.Contains(t, err.Error(), "b", "error should name the missing path")
}

func TestManager_Pull_NameFilter(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	reg := newStubRegistry()

	img1, e1 := buildWeightImage(t, "keep", "/k", []map[string][]byte{{"x": []byte("1")}})
	img2, e2 := buildWeightImage(t, "skip", "/s", []map[string][]byte{{"y": []byte("2")}})
	reg.put(testRepo+"@"+e1.Digest, img1)
	reg.put(testRepo+"@"+e2.Digest, img2)

	lock := &lockfile.WeightsLock{Version: 1, Weights: []lockfile.WeightLockEntry{*e1, *e2}}
	mgr, fs := newTestManager(t, reg, lock)

	results, err := mgr.Pull(ctx, []string{"keep"}, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "keep", results[0].Name)

	// keep is cached; skip is not.
	ok, err := fs.Exists(ctx, e1.Files[0].Digest)
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = fs.Exists(ctx, e2.Files[0].Digest)
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestManager_Pull_UnknownName(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	reg := newStubRegistry()

	_, e1 := buildWeightImage(t, "known", "/k", []map[string][]byte{{"x": []byte("1")}})
	lock := &lockfile.WeightsLock{Version: 1, Weights: []lockfile.WeightLockEntry{*e1}}
	mgr, _ := newTestManager(t, reg, lock)

	_, err := mgr.Pull(ctx, []string{"known", "nope"}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nope")
}

func TestManager_Pull_EmitsEvents(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	reg := newStubRegistry()

	img, entry := buildWeightImage(t, "m1", "/src/weights", []map[string][]byte{
		{"a.txt": []byte("alpha"), "b.txt": []byte("bravo")},
	})
	reg.put(testRepo+"@"+entry.Digest, img)

	lock := &lockfile.WeightsLock{Version: 1, Weights: []lockfile.WeightLockEntry{*entry}}
	mgr, _ := newTestManager(t, reg, lock)

	var events []PullEvent
	_, err := mgr.Pull(ctx, nil, func(e PullEvent) { events = append(events, e) })
	require.NoError(t, err)

	// Expected sequence for a single weight with one layer of two
	// files: WeightStart, LayerStart, FileStored x2, LayerDone,
	// WeightDone.
	kinds := make([]PullEventKind, len(events))
	for i, e := range events {
		kinds[i] = e.Kind
	}
	require.Equal(t, []PullEventKind{
		PullEventWeightStart,
		PullEventLayerStart,
		PullEventFileStored,
		PullEventFileStored,
		PullEventLayerDone,
		PullEventWeightDone,
	}, kinds)

	// WeightStart carries the manifest reference and file counts.
	start := events[0]
	assert.Equal(t, "m1", start.Weight)
	assert.Equal(t, "/src/weights", start.Target)
	assert.Equal(t, 2, start.TotalFiles)
	assert.Equal(t, 2, start.MissingFiles)
	assert.Equal(t, testRepo+"@"+entry.Digest, start.ManifestRef)

	// FileStored events carry path + digest.
	for _, e := range events[2:4] {
		assert.NotEmpty(t, e.FilePath)
		assert.NotEmpty(t, e.FileDigest)
	}
}

func TestManager_Pull_EmitsFullyCachedEvent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	reg := newStubRegistry()

	img, entry := buildWeightImage(t, "m1", "/w", []map[string][]byte{
		{"x": []byte("data")},
	})
	reg.put(testRepo+"@"+entry.Digest, img)

	lock := &lockfile.WeightsLock{Version: 1, Weights: []lockfile.WeightLockEntry{*entry}}
	mgr, fs := newTestManager(t, reg, lock)

	// Pre-populate.
	for _, f := range entry.Files {
		require.NoError(t, fs.PutFile(ctx, f.Digest, f.Size, bytes.NewReader([]byte("data"))))
	}

	var events []PullEvent
	_, err := mgr.Pull(ctx, nil, func(e PullEvent) { events = append(events, e) })
	require.NoError(t, err)

	// Fully-cached weights emit exactly WeightStart + WeightDone.
	require.Len(t, events, 2)
	assert.Equal(t, PullEventWeightStart, events[0].Kind)
	assert.Equal(t, 0, events[0].MissingFiles)
	assert.Empty(t, events[0].ManifestRef, "fully-cached weight should not set manifest ref")
	assert.Equal(t, PullEventWeightDone, events[1].Kind)
	assert.True(t, events[1].FullyCached)
}

func TestNewManager_RequiresStore(t *testing.T) {
	t.Parallel()
	_, err := NewManager(ManagerOptions{
		Registry: newStubRegistry(),
		Repo:     "r",
		Lock:     &lockfile.WeightsLock{},
	})
	require.Error(t, err)
}

func TestNewManager_RequiresRegistry(t *testing.T) {
	t.Parallel()
	fs, err := store.NewFileStore(t.TempDir())
	require.NoError(t, err)
	_, err = NewManager(ManagerOptions{
		Store: fs,
		Repo:  "r",
		Lock:  &lockfile.WeightsLock{},
	})
	require.Error(t, err)
}

func TestNewManager_RequiresRepoWhenLockHasWeights(t *testing.T) {
	t.Parallel()
	fs, err := store.NewFileStore(t.TempDir())
	require.NoError(t, err)
	// Lock with at least one entry → Repo is now required.
	_, err = NewManager(ManagerOptions{
		Store:    fs,
		Registry: newStubRegistry(),
		Lock: &lockfile.WeightsLock{
			Version: 1,
			Weights: []lockfile.WeightLockEntry{{Name: "w", Target: "/t"}},
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "repo")
}

func TestNewManager_NilLockIsNoop(t *testing.T) {
	t.Parallel()
	fs, err := store.NewFileStore(t.TempDir())
	require.NoError(t, err)
	// No Lock, no Repo: weights-less model. Manager still constructs.
	mgr, err := NewManager(ManagerOptions{
		Store:    fs,
		Registry: newStubRegistry(),
	})
	require.NoError(t, err)

	// Pull is a no-op.
	results, err := mgr.Pull(context.Background(), nil, nil)
	require.NoError(t, err)
	assert.Empty(t, results)

	// Prepare is a no-op.
	mounts, err := mgr.Prepare(context.Background())
	require.NoError(t, err)
	assert.Empty(t, mounts.Specs)
	require.NoError(t, mounts.Release())
}
