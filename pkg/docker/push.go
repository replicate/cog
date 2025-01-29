package docker

import "context"

func Push(image string, fast bool, projectDir string, command Command) error {
	if fast {
		return FastPush(context.Background(), image, projectDir, command)
	}
	return StandardPush(image, command)
}
