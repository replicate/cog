package cli

import (
	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/weights"
)

// newWeightManager resolves the repo (from imageOverride or
// src.Config.Image) and delegates construction to weights.NewFromSource.
// Repo resolution is CLI-input parsing, which is why it lives here and
// not in pkg/weights.
func newWeightManager(src *model.Source, imageOverride string) (*weights.Manager, error) {
	repo := ""
	if len(src.Config.Weights) > 0 {
		imageName := imageOverride
		if imageName == "" {
			imageName = src.Config.Image
		}
		if imageName != "" {
			parsed, err := parseRepoOnly(imageName)
			if err != nil {
				return nil, err
			}
			repo = parsed
		}
	}
	return weights.NewFromSource(src, repo)
}
