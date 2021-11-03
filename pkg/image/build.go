package image

import (
	"bytes"
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
func Build(cfg *config.Config, dir, imageName string, progressOutput string) error {
	console.Infof("Building Docker image from environment in cog.yaml as %s...", imageName)

	generator, err := dockerfile.NewGenerator(cfg, dir)
	if err != nil {
		return fmt.Errorf("Error creating Dockerfile generator: %w", err)
	}
	defer func() {
		if err := generator.Cleanup(); err != nil {
			console.Warnf("Error cleaning up Dockerfile generator: %s", err)
		}
	}()

	dockerfileContents, err := generator.Generate()
	if err != nil {
		return fmt.Errorf("Failed to generate Dockerfile: %w", err)
	}

	if err := docker.Build(dir, dockerfileContents, imageName, progressOutput); err != nil {
		return fmt.Errorf("Failed to build Docker image: %w", err)
	}

	console.Info("Adding labels to image...")
	signature, err := GetTypeSignature(imageName, cfg.Build.GPU)
	if err != nil {
		return fmt.Errorf("Failed to get type signature: %w", err)
	}
	signatureJSON, err := json.Marshal(signature)
	if err != nil {
		return fmt.Errorf("Failed to convert type signature to JSON: %w", err)
	}
	configJSON, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("Failed to convert config to JSON: %w", err)
	}
	// We used to set the cog_version and config labels in Dockerfile, because we didn't require running the
	// built image to get those. But, the escaping of JSON inside a label inside a Dockerfile was gnarly, and
	// doesn't seem to be a problem here, so do it here instead.
	labels := map[string]string{
		global.LabelNamespace + "cog_version":    global.Version,
		global.LabelNamespace + "config":         string(bytes.TrimSpace(configJSON)),
		global.LabelNamespace + "type_signature": string(signatureJSON),
	}
	if err := docker.BuildAddLabelsToImage(imageName, labels); err != nil {
		return fmt.Errorf("Failed to add labels to image: %w", err)
	}
	return nil
}

func BuildBase(cfg *config.Config, dir string, progressOutput string) (string, error) {
	// TODO: better image management so we don't eat up disk space
	// https://github.com/replicate/cog/issues/80
	imageName := config.BaseDockerImageName(dir)

	console.Info("Building Docker image from environment in cog.yaml...")
	generator, err := dockerfile.NewGenerator(cfg, dir)
	if err != nil {
		return "", fmt.Errorf("Error creating Dockerfile generator: %w", err)
	}
	defer func() {
		if err := generator.Cleanup(); err != nil {
			console.Warnf("Error cleaning up Dockerfile generator: %s", err)
		}
	}()
	dockerfileContents, err := generator.GenerateBase()
	if err != nil {
		return "", fmt.Errorf("Failed to generate Dockerfile: %w", err)
	}
	if err := docker.Build(dir, dockerfileContents, imageName, progressOutput); err != nil {
		return "", fmt.Errorf("Failed to build Docker image: %w", err)
	}
	return imageName, nil
}
