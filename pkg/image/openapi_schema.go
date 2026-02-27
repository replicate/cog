package image

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/util/console"
)

// GenerateOpenAPISchema generates the OpenAPI schema by running the built Docker
// image with `python -m cog.command.openapi_schema`. This is the legacy path used
// for SDK versions < 0.17.0 where the schema must be generated at runtime via
// pydantic introspection rather than static analysis.
func GenerateOpenAPISchema(ctx context.Context, dockerClient command.Command, imageName string, enableGPU bool) (map[string]any, error) {
	console.Debugf("=== image.GenerateOpenAPISchema %s", imageName)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	gpus := ""
	if enableGPU {
		gpus = "all"
	}

	err := docker.RunWithIO(ctx, dockerClient, command.RunOptions{
		Image: imageName,
		Args: []string{
			"python", "-m", "cog.command.openapi_schema",
		},
		GPUs: gpus,
	}, nil, &stdout, &stderr)

	if enableGPU && err == docker.ErrMissingDeviceDriver {
		console.Debug(stdout.String())
		console.Debug(stderr.String())
		console.Debug("Missing device driver, re-trying without GPU")
		return GenerateOpenAPISchema(ctx, dockerClient, imageName, false)
	}

	if err != nil {
		console.Info(stdout.String())
		console.Info(stderr.String())
		return nil, err
	}

	var schema map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &schema); err != nil {
		console.Info(stdout.String())
		console.Info(stderr.String())
		return nil, err
	}

	return schema, nil
}
