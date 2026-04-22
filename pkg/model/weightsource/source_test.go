package weightsource

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFor(t *testing.T) {
	tests := []struct {
		name        string
		uri         string
		wantType    string // type name or "" for error
		wantErrSubs string
	}{
		{"file scheme", "file:///abs/path", "FileSource", ""},
		{"file scheme relative", "file://./weights", "FileSource", ""},
		{"bare absolute path", "/abs/path", "FileSource", ""},
		{"bare relative path", "./weights", "FileSource", ""},
		{"bare no prefix", "weights", "FileSource", ""},
		{"hf scheme rejected", "hf://org/repo", "", "unsupported weight source scheme"},
		{"s3 scheme rejected", "s3://bucket/key", "", "unsupported"},
		{"http scheme rejected", "http://example.com/x", "", "unsupported"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, err := For(tc.uri)
			if tc.wantType == "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrSubs)
				return
			}
			require.NoError(t, err)
			_, ok := s.(FileSource)
			assert.True(t, ok, "expected FileSource, got %T", s)
		})
	}
}

func TestSchemeOf(t *testing.T) {
	tests := []struct {
		uri  string
		want string
	}{
		{"file:///abs", "file"},
		{"hf://org/repo", "hf"},
		{"s3://bucket/key", "s3"},
		{"/abs", ""},
		{"./rel", ""},
		{"bare", ""},
		{"", ""},
	}
	for _, tc := range tests {
		t.Run(tc.uri, func(t *testing.T) {
			assert.Equal(t, tc.want, schemeOf(tc.uri))
		})
	}
}
