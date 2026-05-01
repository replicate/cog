// Package store defines a narrow, content-addressed interface for
// storing individual weight files on the local machine.
//
// The store knows only digests. Filenames, layer membership, and
// registry URIs are Manager-level concerns. Keeping the surface small
// is what makes the store swappable — a future containerd-backed store
// can drop in behind the same interface.
//
// Digests are "sha256:<hex>"; v1 implementations may reject other
// algorithms. Missing digests surface as errors wrapping fs.ErrNotExist.
package store

import (
	"context"
	"io"
	"iter"
)

// Store is a content-addressed store of individual weight files.
//
// Every method takes a context; implementations SHOULD honor cancellation
// where it makes sense. Missing digests on Path surface as errors
// wrapping fs.ErrNotExist.
type Store interface {
	// Exists reports whether a file with the given digest is in the store.
	// A nil error with false is the ordinary "not present" result.
	Exists(ctx context.Context, digest string) (bool, error)

	// PutFile stores r under expectedDigest, hash-verifying as it streams.
	// A digest mismatch leaves the store unchanged.
	//
	// PutFile is idempotent: if the digest is already present, the
	// reader is drained to io.Discard and nil is returned. This lets
	// callers loop over tar entries without branching on Exists first.
	//
	// size is advisory.
	PutFile(ctx context.Context, expectedDigest string, size int64, r io.Reader) error

	// Path returns an on-disk path for the file, suitable for hardlinking.
	// The file MUST be treated as read-only. Not every backend can
	// satisfy this; such backends return an error.
	Path(ctx context.Context, digest string) (string, error)

	// List iterates every file in the store. Walk errors surface as a
	// final (zero, err) pair before the iterator terminates.
	List(ctx context.Context) iter.Seq2[FileInfo, error]

	// Delete removes the file at digest. Missing digests are not an error.
	Delete(ctx context.Context, digest string) error
}

// FileInfo describes one stored file.
type FileInfo struct {
	Digest string
	Size   int64
}
