package docker

import (
	"context"

	"github.com/replicate/cog/pkg/docker/command"
)

func StandardPush(ctx context.Context, image string, command command.Command) error {
	return command.Push(ctx, image)
}
