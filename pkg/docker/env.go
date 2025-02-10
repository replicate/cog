package docker

import "os"

const DockerCommandEnvVarName = "R8_DOCKER_COMMAND"

func DockerCommandFromEnvironment() string {
	command := os.Getenv(DockerCommandEnvVarName)
	if command == "" {
		command = "docker"
	}
	return command
}
