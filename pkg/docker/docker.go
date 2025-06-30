package docker

import (
	"context"
	"strconv"

	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/util"
	"github.com/replicate/cog/pkg/util/console"
)

func NewClient(ctx context.Context, opts ...Option) (command.Command, error) {
	enabled := util.GetEnvOrDefault("COG_DOCKER_SDK_CLIENT", true, strconv.ParseBool)
	if enabled {
		console.Debugf("Docker client: sdk")
		return NewAPIClient(ctx, opts...)
	}

	console.Debugf("Docker client: cli")
	return NewDockerCommand(), nil
}
