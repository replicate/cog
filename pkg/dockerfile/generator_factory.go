package dockerfile

import (
	"github.com/replicate/cog/pkg/config"
)

func NewGenerator(config *config.Config, dir string) (Generator, error) {
	return NewStandardGenerator(config, dir)
}
