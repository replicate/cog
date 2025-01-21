package docker

func StandardPush(image string, command Command) error {
	return command.Push(image)
}
