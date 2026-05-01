package weightsource

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
)

// DirhashPart is the atomic input to DirHash: the pair of fields that
// uniquely identify a file's contribution to the dirhash. Path is the
// relative path (forward slashes) and Digest is the file's sha256 content
// digest in "sha256:<hex>" form.
type DirhashPart struct {
	Path   string
	Digest string
}

// String returns the canonical identity of a single file: "path\x00digest".
// This is the primitive that any code comparing files across layers,
// plans, or lockfile entries should use. DirHash composes over this
// (sorted, then hashed); layer keys join these (preserving individual
// file identity so two files with identical content but different paths
// remain distinguishable).
func (p DirhashPart) String() string {
	return p.Path + "\x00" + p.Digest
}

// Dirhashable is implemented by types that can participate in DirHash.
// Both weightsource.InventoryFile and lockfile.WeightLockFile implement
// it, letting the two call sites share one digest implementation.
type Dirhashable interface {
	DirhashParts() DirhashPart
}

// DirHash computes a content-addressable digest of a file set per spec §2.4:
//
//	sha256(join(sort("<hex>  <path>"), "\n"))
//
// where each line is the file's sha256 hex digest and relative path joined
// by two spaces (matching sha256sum output). DirHash sorts the lines
// itself, so the caller's input order does not affect the result.
//
// The result is the "sha256:<hex>" form. This formula computes the weight
// set digest stored in weights.lock (WeightLockEntry.SetDigest), and is
// also used by file:// sources specifically as their Fingerprint —
// content-addressable stores happen to match their fingerprint to their
// dirhash. Other schemes (hf://, s3://, http://) use scheme-native
// identifiers (commit SHA, ETag, etc.) for their Fingerprint instead.
func DirHash[F Dirhashable](files []F) string {
	lines := make([]string, len(files))
	for i, f := range files {
		p := f.DirhashParts()
		_, hexStr, _ := strings.Cut(p.Digest, ":")
		lines[i] = hexStr + "  " + p.Path
	}
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return "sha256:" + hex.EncodeToString(sum[:])
}
