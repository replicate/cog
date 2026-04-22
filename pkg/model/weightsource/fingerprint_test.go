package weightsource

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
			assert.Equal(t, tc.want, tc.fp.Value())
		})
	}
}

func TestFingerprint_IsZero(t *testing.T) {
	assert.True(t, Fingerprint("").IsZero())
	assert.False(t, Fingerprint("sha256:abc").IsZero())
}

func TestFingerprint_String(t *testing.T) {
	assert.Equal(t, "sha256:abc", Fingerprint("sha256:abc").String())
}

func TestParseFingerprint(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		fp, err := ParseFingerprint("sha256:abc123")
		require.NoError(t, err)
		assert.Equal(t, Fingerprint("sha256:abc123"), fp)
	})

	t.Run("preserves colons in value", func(t *testing.T) {
		fp, err := ParseFingerprint("timestamp:2026-04-17T12:00:00Z")
		require.NoError(t, err)
		assert.Equal(t, "timestamp", fp.Scheme())
		assert.Equal(t, "2026-04-17T12:00:00Z", fp.Value())
	})

	errorCases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", "empty"},
		{"no separator", "bare", "missing scheme separator"},
		{"empty scheme", ":abc", "empty scheme"},
		{"empty value", "sha256:", "empty value"},
	}
	for _, tc := range errorCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseFingerprint(tc.in)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}
