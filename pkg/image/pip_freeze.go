package image

import (
	"bytes"
	"context"

	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/util/console"
)

// GeneratePipFreeze by running a pip freeze on the image.
// This will be run as part of the build process then added as a label to the image.
func GeneratePipFreeze(ctx context.Context, dockerClient command.Command, imageName string) (string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	args := []string{"python", "-m", "pip", "freeze"}
	err := docker.RunWithIO(ctx, dockerClient, command.RunOptions{
		Image: imageName,
		Args:  args,
	}, nil, &stdout, &stderr)

	if err != nil {
		console.Info(stdout.String())
		console.Info(stderr.String())
		return "", err
	}

	return stdout.String(), nil
}
