package dockerfile

import (
	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/registry"
)

func NewGenerator(config *config.Config, dir string, configFilename string, command command.Command, client registry.Client, requiresCog bool) (Generator, error) {
	return NewStandardGenerator(config, dir, configFilename, command, client, requiresCog)
}
