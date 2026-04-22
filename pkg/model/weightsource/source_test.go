package weightsource

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFor(t *testing.T) {
	// Prepare a real directory so file:// constructors succeed.
	projectDir := t.TempDir()
	weightsDir := filepath.Join(projectDir, "weights")
	require.NoError(t, os.MkdirAll(weightsDir, 0o755))

	absDir := t.TempDir()

	tests := []struct {
		name        string
		uri         string
		projectDir  string
		wantFile    bool
		wantErrSubs string
	}{
		{"file scheme", "file://" + absDir, "", true, ""},
		{"file scheme relative", "file://./weights", projectDir, true, ""},
		{"bare absolute path", absDir, "", true, ""},
		{"bare relative path", "./weights", projectDir, true, ""},
		{"bare no prefix", "weights", projectDir, true, ""},
		{"hf scheme rejected", "hf://org/repo", "", false, "unsupported weight source scheme"},
		{"s3 scheme rejected", "s3://bucket/key", "", false, "unsupported"},
		{"http scheme rejected", "http://example.com/x", "", false, "unsupported"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, err := For(tc.uri, tc.projectDir)
			if !tc.wantFile {
				assert.ErrorContains(t, err, tc.wantErrSubs)
				return
			}
			require.NoError(t, err)
			_, ok := s.(*FileSource)
			assert.True(t, ok, "expected *FileSource, got %T", s)
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
