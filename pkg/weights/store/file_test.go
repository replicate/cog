package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func digestOf(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func newStore(t *testing.T) *FileStore {
	t.Helper()
	s, err := NewFileStore(t.TempDir())
	require.NoError(t, err)
	return s
}

func TestNewFileStore_EmptyRootRejected(t *testing.T) {
	t.Parallel()
	_, err := NewFileStore("")
	require.Error(t, err)
}

func TestFile_RoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newStore(t)
	data := []byte("hello weights")
	d := digestOf(data)

	ok, err := s.Exists(ctx, d)
	require.NoError(t, err)
	require.False(t, ok)

	require.NoError(t, s.PutFile(ctx, d, int64(len(data)), bytes.NewReader(data)))

	ok, err = s.Exists(ctx, d)
	require.NoError(t, err)
	require.True(t, ok)

	p, err := s.Path(ctx, d)
	require.NoError(t, err)
	onDisk, err := os.ReadFile(p) //nolint:gosec // test-owned path
	require.NoError(t, err)
	require.Equal(t, data, onDisk)

	// Path is under the expected layout: <root>/files/sha256/<ab>/<full-hex>.
	hexStr := strings.TrimPrefix(d, "sha256:")
	require.Equal(t, filepath.Join(s.Root(), "files", "sha256", hexStr[:2], hexStr), p)
}

func TestFile_PutFile_SizeMismatch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newStore(t)
	data := []byte("hello weights")
	d := digestOf(data)

	// Claim the file is larger than it really is.
	err := s.PutFile(ctx, d, int64(len(data))+100, bytes.NewReader(data))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "size mismatch")

	// File must not be stored.
	ok, err := s.Exists(ctx, d)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestFile_PutFile_DigestMismatch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newStore(t)
	data := []byte("real content")
	wrong := digestOf([]byte("something else"))

	err := s.PutFile(ctx, wrong, int64(len(data)), bytes.NewReader(data))
	require.Error(t, err)
	require.Contains(t, err.Error(), "digest mismatch")

	ok, err := s.Exists(ctx, wrong)
	require.NoError(t, err)
	require.False(t, ok)

	// No stray temp files in the prefix dir.
	hexStr := strings.TrimPrefix(wrong, "sha256:")
	prefix := filepath.Join(s.Root(), "files", "sha256", hexStr[:2])
	if entries, err := os.ReadDir(prefix); err == nil {
		for _, e := range entries {
			assert.NotContains(t, e.Name(), "put-", "stray temp file left behind: %s", e.Name())
		}
	}
}

func TestFile_PutFile_Idempotent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newStore(t)
	data := []byte("idempotent bytes")
	d := digestOf(data)

	require.NoError(t, s.PutFile(ctx, d, int64(len(data)), bytes.NewReader(data)))

	// Second Put must succeed AND drain the reader — Pull relies on
	// the tar stream staying in sync.
	r := &countingReader{Reader: bytes.NewReader(data)}
	require.NoError(t, s.PutFile(ctx, d, int64(len(data)), r))
	require.Equal(t, len(data), r.n)
}

func TestFile_Path_NotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newStore(t)
	_, err := s.Path(ctx, digestOf([]byte("absent")))
	require.Error(t, err)
	require.ErrorIs(t, err, fs.ErrNotExist)
}

func TestFile_Delete_Idempotent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newStore(t)
	data := []byte("to delete")
	d := digestOf(data)

	// Delete on empty is fine.
	require.NoError(t, s.Delete(ctx, d))

	require.NoError(t, s.PutFile(ctx, d, int64(len(data)), bytes.NewReader(data)))
	require.NoError(t, s.Delete(ctx, d))

	ok, err := s.Exists(ctx, d)
	require.NoError(t, err)
	require.False(t, ok)

	require.NoError(t, s.Delete(ctx, d))
}

func TestFile_List(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newStore(t)

	want := map[string]int64{}
	for _, b := range [][]byte{[]byte("alpha"), []byte("beta"), []byte("gamma")} {
		d := digestOf(b)
		want[d] = int64(len(b))
		require.NoError(t, s.PutFile(ctx, d, int64(len(b)), bytes.NewReader(b)))
	}

	got := map[string]int64{}
	for fi, err := range s.List(ctx) {
		require.NoError(t, err)
		got[fi.Digest] = fi.Size
	}
	require.Equal(t, want, got)
}

func TestFile_List_EmptyStore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newStore(t)
	count := 0
	for _, err := range s.List(ctx) {
		require.NoError(t, err)
		count++
	}
	require.Equal(t, 0, count)
}

func TestFile_List_SkipsStrayTempFiles(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newStore(t)

	data := []byte("real file")
	d := digestOf(data)
	require.NoError(t, s.PutFile(ctx, d, int64(len(data)), bytes.NewReader(data)))

	// Drop a stray temp-file-like entry alongside the real blob.
	hexStr := strings.TrimPrefix(d, "sha256:")
	prefix := filepath.Join(s.Root(), "files", "sha256", hexStr[:2])
	require.NoError(t, os.WriteFile(filepath.Join(prefix, "put-stray"), []byte("trash"), 0o644)) //nolint:gosec // test

	count := 0
	for fi, err := range s.List(ctx) {
		require.NoError(t, err)
		require.Equal(t, d, fi.Digest)
		count++
	}
	require.Equal(t, 1, count)
}

func TestFile_ConcurrentPutSameDigest(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newStore(t)
	data := []byte("racy bytes")
	d := digestOf(data)

	const goroutines = 8
	var wg sync.WaitGroup
	errs := make([]error, goroutines)
	wg.Add(goroutines)
	for i := range goroutines {
		go func(i int) {
			defer wg.Done()
			errs[i] = s.PutFile(ctx, d, int64(len(data)), bytes.NewReader(data))
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		assert.NoError(t, err, "goroutine %d", i)
	}

	p, err := s.Path(ctx, d)
	require.NoError(t, err)
	got, err := os.ReadFile(p) //nolint:gosec // test-owned path
	require.NoError(t, err)
	require.Equal(t, data, got)
}

func TestFile_InterruptedWriteLeavesNoFinalFile(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newStore(t)
	data := []byte("interrupted")
	d := digestOf(data)

	err := s.PutFile(ctx, d, int64(len(data)), &failingReader{after: 4, data: data})
	require.Error(t, err)

	ok, err := s.Exists(ctx, d)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestFile_PutFile_ContextCanceled(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	data := []byte("bytes that will never finish writing")
	d := digestOf(data)

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel the context via the reader — the reader blocks its
	// second Read until the context is observed as canceled, so the
	// next ctxReader.Read guarantees ctx.Err() is set.
	reader := &gatedReader{data: data, cancel: cancel}

	err := s.PutFile(ctx, d, int64(len(data)), reader)
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)

	// The store must not expose a partial file.
	ok, err := s.Exists(context.Background(), d)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestFile_InvalidDigestRejected(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newStore(t)

	for _, bad := range []string{
		"",
		"no-colon",
		"md5:" + strings.Repeat("0", 32),
		"sha256:",
		"sha256:tooShort",
		"sha256:" + strings.Repeat("Z", 64), // uppercase
	} {
		_, err := s.Exists(ctx, bad)
		require.Error(t, err, "digest %q must be rejected", bad)
	}
}

// countingReader counts bytes read.
type countingReader struct {
	io.Reader
	n int
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.Reader.Read(p)
	c.n += n
	return n, err
}

// gatedReader emits one byte per Read and cancels its context after
// the first byte. The next Read is preceded by ctxReader's ctx.Err()
// check, which deterministically observes the canceled context.
type gatedReader struct {
	data   []byte
	off    int
	cancel context.CancelFunc
}

func (g *gatedReader) Read(p []byte) (int, error) {
	if g.off >= len(g.data) {
		return 0, io.EOF
	}
	p[0] = g.data[g.off]
	g.off++
	if g.off == 1 {
		g.cancel()
	}
	return 1, nil
}

// failingReader returns data[:after] then an error.
type failingReader struct {
	after int
	off   int
	data  []byte
}

func (f *failingReader) Read(p []byte) (int, error) {
	remaining := f.after - f.off
	if remaining <= 0 {
		return 0, errors.New("simulated transport failure")
	}
	if remaining > len(p) {
		remaining = len(p)
	}
	n := copy(p, f.data[f.off:f.off+remaining])
	f.off += n
	return n, nil
}
