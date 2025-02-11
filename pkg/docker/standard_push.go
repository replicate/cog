package docker

import (
	"strings"

	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/util"
)

func StandardPush(image string, command command.Command) error {
	err := command.Push(image)
	if err != nil && strings.Contains(err.Error(), "NAME_UNKNOWN") {
		return util.WrapError(err, "Bad response from registry: 404")
	}
	return err
}
