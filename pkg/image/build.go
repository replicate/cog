package image

import (
	"encoding/json"
	"fmt"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/dockerfile"
	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/util/console"
)

// Build a Cog model from a config
//
// This is separated out from docker.Build(), so that can be as close as possible to the behavior of 'docker build'.
func Build(cfg *config.Config, dir, imageName string) error {
	console.Infof("Building Docker image from environment in cog.yaml as %s...", imageName)

	generator := dockerfile.NewGenerator(cfg, dir)
	defer func() {
		if err := generator.Cleanup(); err != nil {
			console.Warnf("Error cleaning up Dockerfile generator: %s", err)
		}
	}()

	dockerfileContents, err := generator.Generate()
	if err != nil {
		return fmt.Errorf("Failed to generate Dockerfile: %w", err)
	}

	if err := docker.Build(dir, dockerfileContents, imageName); err != nil {
		return fmt.Errorf("Failed to build Docker image: %w", err)
	}

	console.Info("Adding labels to image...")
	signature, err := GetTypeSignature(imageName)
	if err != nil {
		return fmt.Errorf("Failed to get type signature: %w", err)
	}
	signatureJSON, err := json.Marshal(signature)
	if err != nil {
		return fmt.Errorf("Failed to convert type signature to JSON: %w", err)
	}
	if err := docker.BuildAddLabelsToImage(imageName, map[string]string{
		global.LabelNamespace + "type_signature": string(signatureJSON),
	}); err != nil {
		return fmt.Errorf("Failed to add labels to image: %w", err)
	}
	return nil
}
