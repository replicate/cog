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
func GeneratePipFreeze(ctx context.Context, dockerClient command.Command, imageName string, fastFlag bool) (string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	args := []string{"python", "-m", "pip", "freeze"}
	var env []string
	// Fast-push builds with monobase has 3 disjoint venvs, base, cog & user
	// Freeze user layer only
	if fastFlag {
		args = []string{"uv", "pip", "freeze"}
		env = []string{"VIRTUAL_ENV=/root/.venv"}
	}
	err := docker.RunWithIO(ctx, dockerClient, command.RunOptions{
		Image: imageName,
		Args:  args,
		Env:   env,
	}, nil, &stdout, &stderr)

	if err != nil {
		console.Info(stdout.String())
		console.Info(stderr.String())
		return "", err
	}

	return stdout.String(), nil
}
