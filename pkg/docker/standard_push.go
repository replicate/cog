package docker

import "github.com/replicate/cog/pkg/docker/command"

func StandardPush(image string, command command.Command) error {
	return command.Push(image)
}
