package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/replicate/cog/pkg/weights"
)

func TestPullProgressID(t *testing.T) {
	const digest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	first := weights.PullEvent{Weight: "model", FilePath: "encoder/config.json", FileDigest: digest}
	otherPath := weights.PullEvent{Weight: "model", FilePath: "decoder/config.json", FileDigest: digest}
	otherWeight := weights.PullEvent{Weight: "other", FilePath: first.FilePath, FileDigest: digest}

	id := pullProgressID(first)
	assert.Len(t, id, 21)
	assert.NotEqual(t, id, pullProgressID(otherPath))
	assert.NotEqual(t, id, pullProgressID(otherWeight))
}
