package docker

import (
	"context"

	"github.com/replicate/cog/pkg/logger"
)

type ImageBuilder interface {
	Build(ctx context.Context, dir string, dockerfileContents string, name string, useGPU bool, logWriter logger.Logger) (tag string, err error)
	Push(ctx context.Context, tag string, logWriter logger.Logger) error
}
