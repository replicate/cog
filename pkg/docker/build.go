package docker

import (
	"context"
	"fmt"

	"github.com/replicate/cog/pkg/docker/command"
)

// BuildAddLabelsAndSchemaToImage builds a new image based on the provided image with the labels and schema file added to it.
func BuildAddLabelsAndSchemaToImage(ctx context.Context, dockerClient command.Command, image string, labels map[string]string, bundledSchemaFile string) error {
	dockerfile := "FROM " + image + "\n"
	dockerfile += "COPY " + bundledSchemaFile + " .cog\n"

	buildOpts := command.ImageBuildOptions{
		DockerfileContents: dockerfile,
		ImageName:          image,
		Labels:             labels,
	}

	if err := dockerClient.ImageBuild(ctx, buildOpts); err != nil {
		return fmt.Errorf("Failed to add labels and schema to image: %w", err)
	}
	return nil
}
