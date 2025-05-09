package image

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/util/console"
)

// GenerateOpenAPISchema by running the image and executing Cog
// This will be run as part of the build process then added as a label to the image. It can be retrieved more efficiently with the label by using GetOpenAPISchema
func GenerateOpenAPISchema(ctx context.Context, dockerClient command.Command, imageName string, enableGPU bool) (map[string]any, error) {
	console.Debugf("=== image.GenerateOpenAPISchema %s", imageName)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	// FIXME(bfirsh): we could detect this by reading the config label on the image
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
		// Exit code was 0, but JSON was not returned.
		// This is verbose, but print so anything that gets printed in Python bubbles up here.
		console.Info(stdout.String())
		console.Info(stderr.String())
		return nil, err
	}
	return schema, nil
}

// // TODO[md]: this is unused, remove it
// func GetOpenAPISchema(ctx context.Context, dockerClient command.Command, imageName string) (*openapi3.T, error) {
// 	manifest, err := dockerClient.Inspect(ctx, imageName)
// 	if err != nil {
// 		return nil, fmt.Errorf("Failed to inspect %s: %w", imageName, err)
// 	}
// 	schemaString := manifest.Config.Labels[command.CogOpenAPISchemaLabelKey]
// 	if schemaString == "" {
// 		// Deprecated. Remove for 1.0.
// 		schemaString = manifest.Config.Labels["org.cogmodel.openapi_schema"]
// 	}
// 	if schemaString == "" {
// 		return nil, fmt.Errorf("Image %s does not appear to be a Cog model", imageName)
// 	}
// 	return openapi3.NewLoader().LoadFromData([]byte(schemaString))
// }
