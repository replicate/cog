package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/model/modeltest"
)

func TestResolveWeightRepo(t *testing.T) {
	const bundleModel = "registry.example.com/user/model"

	tests := []struct {
		name         string
		configImage  string
		configModel  string
		hasWeights   bool
		envModel     string // COG_MODEL full-ref override
		envRepo      string // COG_MODEL_REPO override
		wantRepo     string
		wantErrMatch string
	}{
		{
			name:       "no weights returns empty repo",
			hasWeights: false,
			wantRepo:   "",
		},
		{
			name:        "model in config resolves to repository",
			configModel: bundleModel,
			hasWeights:  true,
			wantRepo:    bundleModel,
		},
		{
			name:        "COG_MODEL_REPO overrides config model",
			configModel: bundleModel,
			hasWeights:  true,
			envRepo:     "user/other",
			wantRepo:    "registry.example.com/user/other",
		},
		{
			name:       "COG_MODEL full-ref wins with no config model",
			hasWeights: true,
			envModel:   "registry.example.com/team/other:v1",
			wantRepo:   "registry.example.com/team/other",
		},
		{
			name:         "image with weights is rejected and names the config file",
			configImage:  bundleModel,
			hasWeights:   true,
			wantErrMatch: "weight commands require 'model' in test-cog.yaml, not 'image'",
		},
		{
			name:         "no model ref names the config file",
			hasWeights:   true,
			wantErrMatch: "set 'model' in test-cog.yaml",
		},
		{
			name:         "invalid COG_MODEL is wrapped, not swallowed",
			hasWeights:   true,
			envModel:     "::not a ref::",
			wantErrMatch: "resolving model ref",
		},
		{
			name:         "invalid COG_MODEL_REPO names the offending env var",
			configModel:  bundleModel,
			hasWeights:   true,
			envRepo:      "user/model:v1", // colon is illegal in a bare repo
			wantErrMatch: "COG_MODEL_REPO",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			modeltest.ClearEnv(t)
			if tt.envModel != "" {
				t.Setenv(model.EnvModel, tt.envModel)
			}
			if tt.envRepo != "" {
				t.Setenv(model.EnvModelRepo, tt.envRepo)
			}

			cfg := &config.Config{
				Image: tt.configImage,
				Model: tt.configModel,
			}
			if tt.hasWeights {
				// Single placeholder weight — content is irrelevant;
				// resolveWeightRepo only checks len(Weights) > 0.
				cfg.Weights = []config.WeightSource{{Name: "w", Target: "/w"}}
			}
			src := model.NewSourceFromConfig(cfg, t.TempDir())

			got, err := resolveWeightRepo(src, "test-cog.yaml")
			if tt.wantErrMatch != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErrMatch)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantRepo, got)
		})
	}
}
