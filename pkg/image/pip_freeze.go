package image

import (
	"bytes"

	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/util/console"
)

// GeneratePipFreeze by running a pip freeze on the image.
// This will be run as part of the build process then added as a label to the image.
func GeneratePipFreeze(imageName string) (string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	err := docker.RunWithIO(docker.RunOptions{
		Image: imageName,
		Args: []string{
			"python", "-m", "pip", "freeze",
		},
	}, nil, &stdout, &stderr)

	if err != nil {
		console.Info(stdout.String())
		console.Info(stderr.String())
		return "", err
	}

	return stdout.String(), nil
}
