package docker

import (
	"github.com/replicate/cog/pkg/logger"
)

type ImageBuilder interface {
	BuildAndPush(dir string, dockerfilePath string, name string, logWriter logger.Logger) (fullImageTag string, err error)
}
