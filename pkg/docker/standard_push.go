package docker

import (
	"strings"

	"github.com/pkg/errors"

	"github.com/replicate/cog/pkg/docker/command"
)

func StandardPush(image string, command command.Command) error {
	err := command.Push(image)
	if err != nil && strings.Contains(err.Error(), "NAME_UNKNOWN") {
		return errors.Wrap(err, "Bad response from registry: 404")
	}
	return err
}
