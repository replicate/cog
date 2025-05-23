package image

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/util/console"
)

func GenerateModelDependencies(ctx context.Context, dockerClient command.Command, imageName string, cfg *config.Config) (string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	stubComponents := strings.Split(cfg.Predict, ":")

	args := []string{"python", "-m", "cog.command.call_graph", filepath.Join("/src", stubComponents[0])}
	err := docker.RunWithIO(ctx, dockerClient, command.RunOptions{
		Image: imageName,
		Args:  args,
	}, nil, &stdout, &stderr)

	if err != nil {
		console.Info(stdout.String())
		console.Info(stderr.String())
		return "", err
	}
	dependencies := strings.Split(strings.TrimSpace(stdout.String()), ",")
	jsonBytes, err := json.Marshal(dependencies)
	if err != nil {
		return "", err
	}

	return string(jsonBytes), nil
}
