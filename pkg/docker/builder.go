package docker

import (
	"github.com/replicate/cog/pkg/logger"
)

type ImageBuilder interface {
	Build(dir string, dockerfileContents string, name string, logWriter logger.Logger) (tag string, err error)
	Push(tag string, logWriter logger.Logger) error
}
