package cli

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/model/modeltest"
	"github.com/replicate/cog/pkg/provider"
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

func TestResolvePushDestination(t *testing.T) {
	const digestTarget = "registry.example.com/user/model@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	tests := []struct {
		name         string
		configImage  string
		configModel  string
		env          map[string]string
		args         []string
		wantFormat   model.Format
		wantTarget   string
		wantTargetRE string
		wantRef      bool
		wantErr      string
	}{
		{
			name:        "tagged positional bundle target replaces full environment override",
			configModel: "registry.example.com/config/model",
			env:         map[string]string{model.EnvModel: "invalid target"},
			args:        []string{"registry.example.com/target/model:v2"},
			wantFormat:  model.FormatBundle,
			wantTarget:  "registry.example.com/target/model:v2",
			wantRef:     true,
		},
		{
			name:        "positional bundle target replaces partial environment overrides",
			configModel: "registry.example.com/config/model",
			env: map[string]string{
				model.EnvModelRegistry: "invalid/registry",
				model.EnvModelRepo:     "invalid:repo",
				model.EnvModelTag:      "invalid tag",
			},
			args:       []string{"registry.example.com/target/model:v3"},
			wantFormat: model.FormatBundle,
			wantTarget: "registry.example.com/target/model:v3",
			wantRef:    true,
		},
		{
			name:         "untagged positional bundle target gets timestamp tag",
			configModel:  "registry.example.com/config/model",
			args:         []string{"registry.example.com/target/model"},
			wantFormat:   model.FormatBundle,
			wantTargetRE: `^registry\.example\.com/target/model:[0-9]{8}T[0-9]{6}Z$`,
			wantRef:      true,
		},
		{
			name:        "tagged positional image target replaces model environment",
			configImage: "registry.example.com/config/image",
			env:         map[string]string{model.EnvModel: "registry.example.com/env/model:v1"},
			args:        []string{"registry.example.com/target/image:v2"},
			wantFormat:  model.FormatImage,
			wantTarget:  "registry.example.com/target/image:v2",
		},
		{
			name:        "untagged positional image target stays untagged",
			configImage: "registry.example.com/config/image",
			args:        []string{"registry.example.com/target/image"},
			wantFormat:  model.FormatImage,
			wantTarget:  "registry.example.com/target/image",
		},
		{
			name:        "existing configured image behavior",
			configImage: "registry.example.com/config/image",
			wantFormat:  model.FormatImage,
			wantTarget:  "registry.example.com/config/image",
		},
		{
			name:         "existing configured bundle behavior",
			configModel:  "registry.example.com/config/model",
			wantFormat:   model.FormatBundle,
			wantRef:      true,
			wantTargetRE: `^registry\.example\.com/config/model:[0-9]{8}T[0-9]{6}Z$`,
		},
		{
			name:        "positional image digest rejected",
			configImage: "registry.example.com/config/image",
			args:        []string{digestTarget},
			wantErr:     "digest-pinned",
		},
		{
			name:        "positional bundle digest rejected",
			configModel: "registry.example.com/config/model",
			args:        []string{digestTarget},
			wantErr:     "digest-pinned",
		},
		{
			name:        "environment bundle digest rejected",
			configModel: "registry.example.com/config/model",
			env:         map[string]string{model.EnvModel: digestTarget},
			wantErr:     "digest-pinned",
		},
		{
			name:        "invalid environment still fails without positional target",
			configModel: "registry.example.com/config/model",
			env:         map[string]string{model.EnvModelTag: "cog-reserved"},
			wantErr:     "reserved prefix",
		},
		{
			name:        "image and model environment conflict remains without positional target",
			configImage: "registry.example.com/config/image",
			env:         map[string]string{model.EnvModelRepo: "acct/model"},
			wantErr:     "'image' in cog.yaml cannot be combined with COG_MODEL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			modeltest.ClearEnv(t)
			for key, value := range tt.env {
				t.Setenv(key, value)
			}

			destination, err := resolvePushDestination(tt.configImage, tt.configModel, tt.args)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, destination)
			assert.Equal(t, tt.wantFormat, destination.format)
			if tt.wantTarget != "" {
				assert.Equal(t, tt.wantTarget, destination.target)
			}
			if tt.wantTargetRE != "" {
				assert.Regexp(t, regexp.MustCompile(tt.wantTargetRE), destination.target)
			}
			if tt.wantRef {
				assert.NotNil(t, destination.modelRef)
			} else {
				assert.Nil(t, destination.modelRef)
			}
		})
	}
}

func TestMarshalPushOutput(t *testing.T) {
	bundle := func(weights []model.Weight) *model.Model {
		img := &model.ImageArtifact{Reference: testImageRef}
		return &model.Model{
			Format: model.FormatBundle,
			Ref: &model.ResolvedRef{
				Registry: "registry.example.com",
				Repo:     "acct/resnet-50",
				Digest:   testModelDigest,
			},
			Image:     img,
			Artifacts: []model.Artifact{img},
			Weights:   weights,
		}
	}

	tests := []struct {
		name        string
		model       *model.Model
		wantJSON    string
		wantWeights []PushWeightOutput
	}{
		{
			name: "ordinary image",
			model: func() *model.Model {
				img := &model.ImageArtifact{Reference: testImageRef}
				return &model.Model{Format: model.FormatImage, Image: img, Artifacts: []model.Artifact{img}}
			}(),
			wantJSON: `{"version":1,"image":"` + testImageRef + `"}`,
		},
		{
			name:     "bundle without weights omits weights",
			model:    bundle(nil),
			wantJSON: `{"version":1,"model":"` + testRepo + `@` + testModelDigest + `","image":"` + testImageRef + `"}`,
		},
		{
			name: "bundle with one weight",
			model: bundle([]model.Weight{
				{Name: "transformer", Reference: testWeightRef1},
			}),
			wantWeights: []PushWeightOutput{
				{Name: "transformer", Reference: testWeightRef1},
			},
		},
		{
			name: "bundle preserves multiple weight order",
			model: bundle([]model.Weight{
				{Name: "transformer", Reference: testWeightRef1},
				{Name: "text-encoder", Reference: testWeightRef2},
			}),
			wantWeights: []PushWeightOutput{
				{Name: "transformer", Reference: testWeightRef1},
				{Name: "text-encoder", Reference: testWeightRef2},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := marshalPushOutput(tt.model)
			require.NoError(t, err)

			if tt.wantJSON != "" {
				assert.JSONEq(t, tt.wantJSON, string(data))
			}
			var got PushOutput
			require.NoError(t, json.Unmarshal(data, &got))
			assert.Equal(t, 1, got.Version)
			assert.Equal(t, tt.wantWeights, got.Weights)
			assert.NotContains(t, string(data), "\n", "output must be one JSON document on one line")
		})
	}
}

func TestMarshalPushOutput_RejectsIncompleteReferences(t *testing.T) {
	tests := []struct {
		name    string
		model   *model.Model
		wantErr string
	}{
		{name: "nil model", wantErr: "without a model result"},
		{
			name:    "missing image",
			model:   &model.Model{Format: model.FormatImage},
			wantErr: "no image artifact",
		},
		{
			name: "tagged image",
			model: func() *model.Model {
				img := &model.ImageArtifact{Reference: testRepo + ":latest"}
				return &model.Model{Format: model.FormatImage, Image: img, Artifacts: []model.Artifact{img}}
			}(),
			wantErr: "not digest-pinned",
		},
		{
			name: "bundle without model ref",
			model: func() *model.Model {
				img := &model.ImageArtifact{Reference: testImageRef}
				return &model.Model{Format: model.FormatBundle, Image: img, Artifacts: []model.Artifact{img}}
			}(),
			wantErr: "no model reference",
		},
		{
			name: "weight without digest ref",
			model: func() *model.Model {
				img := &model.ImageArtifact{Reference: testImageRef}
				return &model.Model{
					Format:    model.FormatBundle,
					Ref:       &model.ResolvedRef{Registry: "registry.example.com", Repo: "acct/resnet-50", Digest: testModelDigest},
					Image:     img,
					Artifacts: []model.Artifact{img},
					Weights:   []model.Weight{{Name: "transformer", Reference: testRepo + ":cog-weight.transformer"}},
				}
			}(),
			wantErr: `weight "transformer" reference`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := marshalPushOutput(tt.model)
			require.ErrorContains(t, err, tt.wantErr)
			assert.Nil(t, data)
		})
	}
}

func TestNewPushCommand_JSONFlag(t *testing.T) {
	cmd := newPushCommand()
	flag := cmd.Flags().Lookup("json")
	require.NotNil(t, flag)
	assert.Equal(t, "false", flag.DefValue)
}

type pushTestProvider struct {
	postPush func(pushErr error) error
}

func (p *pushTestProvider) Name() string { return "test" }

func (p *pushTestProvider) MatchesRegistry(string) bool { return true }

func (p *pushTestProvider) Login(context.Context, provider.LoginOptions) error { return nil }

func (p *pushTestProvider) PostPush(_ context.Context, _ provider.PushOptions, pushErr error) error {
	return p.postPush(pushErr)
}

func TestCompletePush_JSONOutputWaitsForFullSuccess(t *testing.T) {
	validImage := func() *model.Model {
		img := &model.ImageArtifact{Reference: testImageRef}
		return &model.Model{Format: model.FormatImage, Image: img, Artifacts: []model.Artifact{img}}
	}

	tests := []struct {
		name            string
		pushed          *model.Model
		pushErr         error
		providerErr     error
		wantErr         string
		wantProviderErr string
		wantOutput      bool
	}{
		{
			name:       "success",
			pushed:     validImage(),
			wantOutput: true,
		},
		{
			name:            "push failure",
			pushErr:         errors.New("registry rejected push"),
			wantErr:         "registry rejected push",
			wantProviderErr: "registry rejected push",
		},
		{
			name:        "provider failure",
			pushed:      validImage(),
			providerErr: errors.New("provider post-processing failed"),
			wantErr:     "provider post-processing failed",
		},
		{
			name:            "digest validation failure",
			pushed:          &model.Model{Format: model.FormatImage},
			wantErr:         "no image artifact",
			wantProviderErr: "no image artifact",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			providerCalled := false
			providerDone := false
			var providerPushErr error
			p := &pushTestProvider{postPush: func(pushErr error) error {
				providerCalled = true
				providerPushErr = pushErr
				providerDone = true
				return tt.providerErr
			}}
			var outputs []string

			err := completePush(
				context.Background(),
				p,
				provider.PushOptions{},
				tt.pushed,
				tt.pushErr,
				true,
				func(output string) {
					assert.True(t, providerDone, "JSON must be emitted after provider post-processing")
					outputs = append(outputs, output)
				},
			)

			assert.True(t, providerCalled)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			if tt.wantProviderErr != "" {
				require.ErrorContains(t, providerPushErr, tt.wantProviderErr)
			} else {
				assert.NoError(t, providerPushErr)
			}
			if tt.wantOutput {
				require.Len(t, outputs, 1)
				assert.JSONEq(t, `{"version":1,"image":"`+testImageRef+`"}`, outputs[0])
			} else {
				assert.Empty(t, outputs)
			}
		})
	}
}

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
