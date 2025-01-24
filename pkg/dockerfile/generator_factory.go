package dockerfile

import (
	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker"
)

func NewGenerator(config *config.Config, dir string, buildFast bool, command docker.Command) (Generator, error) {
	if buildFast {
		return NewFastGenerator(config, dir, command)
	}
	return NewStandardGenerator(config, dir, command)
}
