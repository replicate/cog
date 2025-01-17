package docker

func Push(image string, fast bool, projectDir string) error {
	if fast {
		return FastPush(image, projectDir)
	}
	return StandardPush(image)
}
