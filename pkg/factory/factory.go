package factory

import (
	"github.com/replicate/cog/pkg/docker/command"
)

type Factory struct {
	dockerProvider command.Command
}

func NewFactory(dockerProvider command.Command) *Factory {
	return &Factory{dockerProvider: dockerProvider}
}
