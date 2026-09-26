package docker

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDisplayPushOutput(t *testing.T) {
	const digest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	output := strings.NewReader(
		`{"status":"Pushing layers"}` + "\n" +
			`{"aux":{"Tag":"v1","Digest":"` + digest + `","Size":1234}}` + "\n",
	)
	var stderr bytes.Buffer

	result, err := displayPushOutput(output, &stderr, 0, false, "registry.example.com/user/model:v1")

	require.NoError(t, err)
	assert.Equal(t, digest, result.Digest)
	assert.Equal(t, int64(1234), result.Size)
	assert.Contains(t, stderr.String(), "Pushing layers")
	assert.NotContains(t, stderr.String(), digest, "the machine-readable push result shouldn't be rendered as progress")
}

func TestDisplayPushOutput_ContainerdStatus(t *testing.T) {
	const digest = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	output := strings.NewReader(`{"status":"v1: digest: ` + digest + ` size: 5678"}` + "\n")

	result, err := displayPushOutput(output, &bytes.Buffer{}, 0, false, "registry.example.com/user/model:v1")

	require.NoError(t, err)
	assert.Equal(t, digest, result.Digest)
	assert.Equal(t, int64(5678), result.Size)
}
