package docker

import (
	"context"
	"os"
	"strconv"

	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/util/console"
)

func NewClient(ctx context.Context, opts ...Option) (command.Command, error) {
	enabled, _ := strconv.ParseBool(os.Getenv("COG_DOCKER_SDK_CLIENT"))
	if enabled {
		console.Debugf("Docker client: sdk")
		panic("not implemented yet")
		// return NewAPIClient(ctx, opts...)
	}

	console.Debugf("Docker client: cli")
	return NewDockerCommand(), nil
}
