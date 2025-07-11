package dockerfile

import (
	"net/http"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/registry"
)

func NewGenerator(config *config.Config, dir string, buildFast bool, command command.Command, localImage bool, client registry.Client, requiresCog bool) (Generator, error) {
	if buildFast {
		matrix, err := NewMonobaseMatrix(http.DefaultClient)
		if err != nil {
			return nil, err
		}
		return NewFastGenerator(config, dir, command, matrix, localImage)
	}
	return NewStandardGenerator(config, dir, command, client, requiresCog)
}
