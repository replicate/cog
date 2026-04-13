package resolver

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveWithSDKWheelDoesNotSetPatchVersion(t *testing.T) {
	result, err := Resolve("/tmp/cog", "", "", "", "/tmp/cog-1.2.3.whl", nil)
	require.NoError(t, err)

	assert.Equal(t, "/tmp/cog", result.CogBinary)
	assert.Equal(t, "/tmp/cog-1.2.3.whl", result.SDKWheel)
	assert.Equal(t, "", result.SDKPatchVersion)
	assert.Equal(t, "wheel:cog-1.2.3.whl", result.SDKVersion)
}

func TestResolveWithSDKWheelAndExplicitSDKVersionSetsPatchVersion(t *testing.T) {
	result, err := Resolve("/tmp/cog", "", "", "0.17.0", "/tmp/cog-1.2.3.whl", nil)
	require.NoError(t, err)

	assert.Equal(t, "/tmp/cog-1.2.3.whl", result.SDKWheel)
	assert.Equal(t, "0.17.0", result.SDKPatchVersion)
	assert.Equal(t, "0.17.0", result.SDKVersion)
}

func TestParseChecksum(t *testing.T) {
	content := "abc123  cog_Darwin_arm64\nfeedbeef *other_file\n"
	checksum, err := parseChecksum(content, "cog_Darwin_arm64")
	require.NoError(t, err)
	assert.Equal(t, "abc123", checksum)
}

func TestParseChecksumNotFound(t *testing.T) {
	_, err := parseChecksum("abc123  some_other_file\n", "cog_Linux_x86_64")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "checksum for cog_Linux_x86_64 not found")
}

func TestChecksumMismatchError(t *testing.T) {
	err := &checksumMismatchError{Asset: "cog_Darwin_arm64", Expected: "aaa", Actual: "bbb"}
	assert.Contains(t, err.Error(), "checksum mismatch")
	assert.Contains(t, err.Error(), "cog_Darwin_arm64")
}

func TestFileSHA256(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "file.bin")
	require.NoError(t, os.WriteFile(path, []byte("hello"), 0o644))

	got, err := fileSHA256(path)
	require.NoError(t, err)

	expectedHash := sha256.Sum256([]byte("hello"))
	assert.Equal(t, hex.EncodeToString(expectedHash[:]), got)
}
