package dockerfile

import (
	"github.com/replicate/cog/pkg/config"
)

func NewGenerator(config *config.Config, dir string, buildFast bool) (Generator, error) {
	if buildFast {
		return NewFastGenerator(config, dir)
	}
	return NewStandardGenerator(config, dir)
}
