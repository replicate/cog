// Package weightsource is the pluggable source layer for weight imports.
//
// A Source is a stateful provider bound at construction time to a specific
// URI. It exposes two capabilities: Inventory lists the files the source
// offers (with sizes, per-file digests, and a source-identity Fingerprint),
// and Open streams one file's bytes. The packer drives the import one file
// at a time so that sources larger than local disk can be imported without
// full materialization.
//
// Today file:// is the only implementation.
package weightsource

import (
	"fmt"
	"strings"
)

// Fingerprint is a source's version identity, carrying its algorithm (or
// source-native identifier type) as a scheme prefix.
//
// Examples:
//
//	sha256:<hex>            — content hash (file:// sources)
//	commit:<sha>            — git commit (hf:// repos pinned to a commit)
//	etag:<value>            — HTTP ETag (http:// sources)
//	md5:<hex>               — MD5 hash (s3:// objects)
//	timestamp:<rfc3339>     — last-modified timestamp (fallback for systems
//	                           that expose nothing stronger)
//
// The prefix makes two fingerprints from different sources unambiguously
// unequal even when the opaque values happen to collide. The empty string
// is not a valid Fingerprint — callers that want to express "no fingerprint
// known" should use a separate sentinel.
type Fingerprint string

// Scheme returns the fingerprint's algorithm or identifier prefix (the
// part before the first colon). Returns "" if the fingerprint is malformed
// (no colon).
func (f Fingerprint) Scheme() string {
	scheme, _, ok := strings.Cut(string(f), ":")
	if !ok {
		return ""
	}
	return scheme
}

// Value returns the fingerprint's opaque value (the part after the first
// colon). Returns "" if the fingerprint is malformed.
func (f Fingerprint) Value() string {
	_, value, ok := strings.Cut(string(f), ":")
	if !ok {
		return ""
	}
	return value
}

// String returns the fingerprint in its canonical "<scheme>:<value>" form.
func (f Fingerprint) String() string { return string(f) }

// IsZero reports whether f is the zero value (empty string). Use this to
// distinguish "no fingerprint" from a real fingerprint whose scheme or
// value happens to be empty.
func (f Fingerprint) IsZero() bool { return f == "" }

// ParseFingerprint validates a fingerprint string and returns it as a
// Fingerprint. It rejects empty strings, values missing the scheme
// separator, and scheme-only strings with no value.
func ParseFingerprint(s string) (Fingerprint, error) {
	if s == "" {
		return "", fmt.Errorf("fingerprint is empty")
	}
	scheme, value, ok := strings.Cut(s, ":")
	if !ok {
		return "", fmt.Errorf("fingerprint %q missing scheme separator (want <scheme>:<value>)", s)
	}
	if scheme == "" {
		return "", fmt.Errorf("fingerprint %q has empty scheme", s)
	}
	if value == "" {
		return "", fmt.Errorf("fingerprint %q has empty value", s)
	}
	return Fingerprint(s), nil
}
