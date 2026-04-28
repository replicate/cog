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
		wantType    string // "file", "hf", or "" for expected error
		wantErrSubs string
	}{
		{"file scheme", "file://" + absDir, "", "file", ""},
		{"file scheme relative", "file://./weights", projectDir, "file", ""},
		{"bare absolute path", absDir, "", "file", ""},
		{"bare relative path", "./weights", projectDir, "file", ""},
		{"bare no prefix", "weights", projectDir, "file", ""},
		{"hf scheme", "hf://org/repo", "", "hf", ""},
		{"huggingface scheme", "huggingface://org/repo", "", "hf", ""},
		{"s3 scheme rejected", "s3://bucket/key", "", "", "unsupported"},
		{"http scheme rejected", "http://example.com/x", "", "", "unsupported"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, err := For(tc.uri, tc.projectDir)
			if tc.wantType == "" {
				assert.ErrorContains(t, err, tc.wantErrSubs)
				return
			}
			require.NoError(t, err)
			switch tc.wantType {
			case "file":
				_, ok := s.(*FileSource)
				assert.True(t, ok, "expected *FileSource, got %T", s)
			case "hf":
				_, ok := s.(*HFSource)
				assert.True(t, ok, "expected *HFSource, got %T", s)
			}
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
		{"huggingface://org/repo", "huggingface"},
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
