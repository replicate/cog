package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRenderWeightDownloadLineUsesBarWhenItFits(t *testing.T) {
	line := renderWeightDownloadLine("large-model-custom-voice", "model.safetensors", 2_400_000_000, 3_800_000_000, 120, false)

	assert.LessOrEqual(t, progressVisibleWidth(line), 120)
	assert.Contains(t, line, "model.safetensors")
	assert.Contains(t, line, "█")
	assert.NotContains(t, line, "%")
}

func TestRenderWeightDownloadLineUsesPercentWhenBarDoesNotFit(t *testing.T) {
	line := renderWeightDownloadLine("large-model-custom-voice", "model.safetensors", 2_400_000_000, 3_800_000_000, 40, false)

	assert.LessOrEqual(t, progressVisibleWidth(line), 40)
	assert.Contains(t, line, "%")
	assert.NotContains(t, line, "█")
}

func TestRenderWeightDownloadLineNeverExceedsWidth(t *testing.T) {
	for _, width := range []int{32, 40, 60, 80, 100, 120} {
		line := renderWeightDownloadLine(
			"large-model-custom-voice-with-extra-long-name",
			"nested/path/to/model-with-a-very-long-name.safetensors",
			2_400_000_000,
			3_800_000_000,
			width,
			false,
		)

		assert.LessOrEqual(t, progressVisibleWidth(line), width, "line %q should fit width %d", line, width)
	}
}

func TestRenderWeightDownloadStatusLineShowsCheck(t *testing.T) {
	line := renderWeightDownloadStatusLine("large-model-tokenizer", "done", 80, false)

	assert.LessOrEqual(t, progressVisibleWidth(line), 80)
	assert.Contains(t, line, "✓ done")
}
