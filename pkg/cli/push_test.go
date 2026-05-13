package cli

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/model"
)

// Refs are full 64-char digests so the assertions exercise the
// "copy-pasteable, never truncated" contract.
const (
	testRepo        = "registry.example.com/acct/resnet-50"
	testImageRef    = testRepo + "@sha256:f3c67c0000000000000000000000000000000000000000000000000000000000"
	testModelDigest = "sha256:abc1230000000000000000000000000000000000000000000000000000000000"
	testWeightRef1  = testRepo + "@sha256:d2daaf0000000000000000000000000000000000000000000000000000000000"
	testWeightRef2  = testRepo + "@sha256:e4f5a60000000000000000000000000000000000000000000000000000000000"
)

func TestFormatPushResult_FormatBundle_NoWeights(t *testing.T) {
	img := &model.ImageArtifact{Reference: testImageRef}
	m := &model.Model{
		Format: model.FormatBundle,
		Ref: &model.ResolvedRef{
			Registry: "registry.example.com",
			Repo:     "acct/resnet-50",
			Digest:   testModelDigest,
		},
		Image:     img,
		Artifacts: []model.Artifact{img},
	}

	out := formatPushResult(m)

	assert.Contains(t, out, "model")
	assert.Contains(t, out, testRepo+"@"+testModelDigest)
	assert.Contains(t, out, "└─ image")
	assert.Contains(t, out, testImageRef)
	// Only one child — should be └─, never ├─.
	assert.NotContains(t, out, "├─")
	// Refs must always be full digests — no truncation.
	assert.NotContains(t, out, "...")
	for line := range strings.SplitSeq(out, "\n") {
		if strings.Contains(line, "model") || strings.Contains(line, "image") {
			assert.Contains(t, line, "@sha256:", "ref must be digest-pinned: %q", line)
		}
	}
	// Caller adds separators; the formatter must not.
	assert.False(t, strings.HasPrefix(out, "\n"), "output should not start with a blank line")
	assert.False(t, strings.HasSuffix(out, "\n"), "output should not end with a trailing newline")
}

func TestFormatPushResult_FormatBundle_SingleWeight(t *testing.T) {
	img := &model.ImageArtifact{Reference: testImageRef}
	m := &model.Model{
		Format: model.FormatBundle,
		Ref: &model.ResolvedRef{
			Registry: "registry.example.com",
			Repo:     "acct/resnet-50",
			Digest:   testModelDigest,
		},
		Image:     img,
		Artifacts: []model.Artifact{img},
		Weights: []model.Weight{
			{Name: "resnet50", Reference: testWeightRef1},
		},
	}

	out := formatPushResult(m)

	assert.Contains(t, out, "model")
	assert.Contains(t, out, "├─ image")
	assert.Contains(t, out, "└─ weight")
	assert.Contains(t, out, "resnet50")
	assert.Contains(t, out, testWeightRef1)
	// Image is the not-last child, so it should be ├─ not └─.
	assert.NotContains(t, out, "└─ image")
}

func TestFormatPushResult_FormatBundle_MultipleWeights(t *testing.T) {
	img := &model.ImageArtifact{Reference: testImageRef}
	m := &model.Model{
		Format: model.FormatBundle,
		Ref: &model.ResolvedRef{
			Registry: "registry.example.com",
			Repo:     "flux",
			Digest:   testModelDigest,
		},
		Image:     img,
		Artifacts: []model.Artifact{img},
		Weights: []model.Weight{
			{Name: "transformer", Reference: testWeightRef1},
			{Name: "text-encoder", Reference: testWeightRef2},
		},
	}

	out := formatPushResult(m)

	assert.Contains(t, out, "├─ image")
	assert.Contains(t, out, "├─ weight")
	assert.Contains(t, out, "└─ weight")
	assert.Contains(t, out, "transformer")
	assert.Contains(t, out, "text-encoder")
	assert.Contains(t, out, testWeightRef1)
	assert.Contains(t, out, testWeightRef2)

	// Weight names should be column-aligned: find the two weight
	// lines and assert their refs start at the same column.
	var weightLines []string
	for line := range strings.SplitSeq(out, "\n") {
		if strings.Contains(line, "weight") && strings.Contains(line, "@sha256:") {
			weightLines = append(weightLines, line)
		}
	}
	require.Len(t, weightLines, 2, "expected two weight lines, got: %v", weightLines)
	assert.Equal(t,
		strings.Index(weightLines[0], "@sha256:"),
		strings.Index(weightLines[1], "@sha256:"),
		"weight refs should be column-aligned across rows; got:\n%s\n%s",
		weightLines[0], weightLines[1],
	)
}

// Full layout assertion: pin the exact rendering so a future
// alignment/format change is caught loudly. Uses short fixed digests
// so the golden output stays readable in the test file.
func TestFormatPushResult_FormatBundle_GoldenLayout(t *testing.T) {
	const (
		repo    = "registry.example.com/acct/flux"
		modelD  = "sha256:abc123"
		imageD  = "sha256:f3c67c"
		weightD = "sha256:d2daaf"
	)

	img := &model.ImageArtifact{Reference: repo + "@" + imageD}
	m := &model.Model{
		Format: model.FormatBundle,
		Ref: &model.ResolvedRef{
			Registry: "registry.example.com",
			Repo:     "acct/flux",
			Digest:   modelD,
		},
		Image:     img,
		Artifacts: []model.Artifact{img},
		Weights: []model.Weight{
			{Name: "transformer", Reference: repo + "@" + weightD},
		},
	}

	expected := strings.Join([]string{
		"  model   " + repo + "@" + modelD,
		"  ├─ image   " + repo + "@" + imageD,
		"  └─ weight  transformer  " + repo + "@" + weightD,
	}, "\n")

	assert.Equal(t, expected, formatPushResult(m))
}

func TestFormatPushResult_FormatImage(t *testing.T) {
	img := &model.ImageArtifact{Reference: testImageRef}
	m := &model.Model{
		Format:    model.FormatImage,
		Image:     img,
		Artifacts: []model.Artifact{img},
	}

	out := formatPushResult(m)

	// FormatImage: no model/weight rows, no tree branches.
	assert.NotContains(t, out, "  model ")
	assert.NotContains(t, out, "├─")
	assert.NotContains(t, out, "└─")
	assert.NotContains(t, out, "weight")
	assert.Equal(t, "  image  "+testImageRef, out)
}

func TestFormatPushResult_NilModel(t *testing.T) {
	assert.Empty(t, formatPushResult(nil), "nil model should produce empty output")
}

// Defensive guards: these states should be unreachable post-push
// (Resolver.Push enriches Image.Reference and Model.Ref), but the
// function defends against them. Lock the contract so a future
// refactor that drops the guards has to update these tests.
func TestFormatPushResult_DefensiveGuards(t *testing.T) {
	t.Run("FormatImage with no image artifact", func(t *testing.T) {
		m := &model.Model{Format: model.FormatImage}
		assert.Empty(t, formatPushResult(m))
	})

	t.Run("FormatImage with empty image reference", func(t *testing.T) {
		img := &model.ImageArtifact{Reference: ""}
		m := &model.Model{
			Format:    model.FormatImage,
			Image:     img,
			Artifacts: []model.Artifact{img},
		}
		assert.Empty(t, formatPushResult(m))
	})

	t.Run("FormatBundle with nil Ref skips the model line but still shows children", func(t *testing.T) {
		img := &model.ImageArtifact{Reference: testImageRef}
		m := &model.Model{
			Format:    model.FormatBundle,
			Image:     img,
			Artifacts: []model.Artifact{img},
		}

		out := formatPushResult(m)
		assert.NotContains(t, out, "  model ", "nil Ref should suppress the model row")
		assert.Contains(t, out, "└─ image", "image child should still render")
	})

	t.Run("FormatBundle with weights but no image artifact", func(t *testing.T) {
		// Bundle without an image makes no sense in practice, but
		// the function defends against it: the single weight
		// becomes the last child.
		m := &model.Model{
			Format: model.FormatBundle,
			Ref: &model.ResolvedRef{
				Registry: "registry.example.com",
				Repo:     "acct/flux",
				Digest:   testModelDigest,
			},
			Weights: []model.Weight{
				{Name: "w1", Reference: testWeightRef1},
			},
		}

		out := formatPushResult(m)
		assert.Contains(t, out, "model")
		assert.Contains(t, out, "└─ weight")
		assert.NotContains(t, out, "├─", "single child should use └─, not ├─")
		assert.NotContains(t, out, "image")
	})
}
