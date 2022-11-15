package image

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/dockerfile"
	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/util/console"
)

// Build a Cog model from a config
//
// This is separated out from docker.Build(), so that can be as close as possible to the behavior of 'docker build'.
func Build(cfg *config.Config, dir, imageName string, progressOutput string) (string, error) {
	console.Infof("Building Docker image from environment in cog.yaml as %s...", imageName)

	// Use a unique temporary ID for builds so there isn't a race condition where two builds could happen in parallel with the same image name,
	// which may cause generating OpenAPI schema or adding labels to collide.
	tempImageName := fmt.Sprintf("cog-build-%s", uuid.New().String())

	generator, err := dockerfile.NewGenerator(cfg, dir)
	if err != nil {
		return "", fmt.Errorf("Error creating Dockerfile generator: %w", err)
	}
	defer func() {
		if err := generator.Cleanup(); err != nil {
			console.Warnf("Error cleaning up Dockerfile generator: %s", err)
		}
	}()

	dockerfileContents, err := generator.Generate()
	if err != nil {
		return "", fmt.Errorf("Failed to generate Dockerfile: %w", err)
	}

	if err := docker.Build(dir, dockerfileContents, tempImageName, progressOutput); err != nil {
		return "", fmt.Errorf("Failed to build Docker image: %w", err)
	}

	console.Info("Adding labels to image...")
	schema, err := GenerateOpenAPISchema(tempImageName, cfg.Build.GPU)
	if err != nil {
		return "", fmt.Errorf("Failed to get type signature: %w", err)
	}
	configJSON, err := json.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("Failed to convert config to JSON: %w", err)
	}
	// We used to set the cog_version and config labels in Dockerfile, because we didn't require running the
	// built image to get those. But, the escaping of JSON inside a label inside a Dockerfile was gnarly, and
	// doesn't seem to be a problem here, so do it here instead.
	labels := map[string]string{
		global.LabelNamespace + "version": global.Version,
		global.LabelNamespace + "config":  string(bytes.TrimSpace(configJSON)),
		// Backwards compatibility. Remove for 1.0.
		"org.cogmodel.deprecated":  "The org.cogmodel labels are deprecated. Use run.cog.",
		"org.cogmodel.cog_version": global.Version,
		"org.cogmodel.config":      string(bytes.TrimSpace(configJSON)),
	}

	// OpenAPI schema is not set if there is no predictor.
	if len((*schema).(map[string]interface{})) != 0 {
		schemaJSON, err := json.Marshal(schema)
		if err != nil {
			return "", fmt.Errorf("Failed to convert type signature to JSON: %w", err)
		}
		labels[global.LabelNamespace+"openapi_schema"] = string(schemaJSON)
		labels["org.cogmodel.openapi_schema"] = string(schemaJSON)
	}

	if err := docker.BuildAddLabelsToImage(tempImageName, labels); err != nil {
		return "", fmt.Errorf("Failed to add labels to image: %w", err)
	}

	image, err := docker.ImageInspect(tempImageName)
	if err != nil {
		return "", fmt.Errorf("Failed to inspect image: %w", err)
	}

	if err := docker.Tag(tempImageName, imageName); err != nil {
		return "", fmt.Errorf("Failed to tag image: %w", err)
	}

	// We've just tagged the image with a different name, so this removes the temporary tag, not the actual image
	if err := docker.ImageRm(tempImageName); err != nil {
		return "", fmt.Errorf("Failed to remove temporary image: %w", err)
	}

	return image.ID, nil
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
