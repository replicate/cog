package dockerfile

import (
	"net/http"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker/command"
)

func NewGenerator(config *config.Config, dir string, buildFast bool, command command.Command) (Generator, error) {
	if buildFast {
		matrix, err := NewMonobaseMatrix(http.DefaultClient)
		if err != nil {
			return nil, err
		}
		return NewFastGenerator(config, dir, command, matrix)
	}
	return NewStandardGenerator(config, dir, command)
}
