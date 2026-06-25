package weightsource

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFingerprint_Scheme(t *testing.T) {
	tests := []struct {
		name string
		fp   Fingerprint
		want string
	}{
		{"sha256", Fingerprint("sha256:abc123"), "sha256"},
		{"commit", Fingerprint("commit:deadbeef"), "commit"},
		{"etag", Fingerprint("etag:W/\"abc\""), "etag"},
		{"timestamp with colons", Fingerprint("timestamp:2026-04-17T12:00:00Z"), "timestamp"},
		{"empty", Fingerprint(""), ""},
		{"no separator", Fingerprint("bare"), ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.fp.Scheme())
		})
	}
}

func TestFingerprint_Value(t *testing.T) {
	tests := []struct {
		name string
		fp   Fingerprint
		want string
	}{
		{"sha256", Fingerprint("sha256:abc123"), "abc123"},
		{"timestamp preserves inner colons", Fingerprint("timestamp:2026-04-17T12:00:00Z"), "2026-04-17T12:00:00Z"},
		{"empty", Fingerprint(""), ""},
		{"no separator", Fingerprint("bare"), ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.fp.value())
		})
	}
}

func TestFingerprint_IsZero(t *testing.T) {
	assert.True(t, Fingerprint("").isZero())
	assert.False(t, Fingerprint("sha256:abc").isZero())
}

func TestFingerprint_String(t *testing.T) {
	assert.Equal(t, "sha256:abc", Fingerprint("sha256:abc").String())
}
