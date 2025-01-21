package docker

func Push(image string, fast bool, projectDir string, command Command) error {
	if fast {
		return FastPush(image, projectDir, command)
	}
	return StandardPush(image, command)
}
