package dockerfile

import (
	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/registry"
	"github.com/replicate/cog/pkg/util/console"
)

func NewGenerator(config *config.Config, dir string, buildFast bool, command command.Command, localImage bool, client registry.Client, requiresCog bool) (Generator, error) {
	if buildFast {
		console.Warnf("--fast flag is deprecated and has no effect")
	}
	return NewStandardGenerator(config, dir, command, client, requiresCog)
}
