package docker

import "context"

func Push(image string, fast bool, projectDir string, command Command) error {
	if fast {
		return FastPush(image, projectDir, command, context.Background())
	}
	return StandardPush(image, command)
}
