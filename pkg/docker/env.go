package docker

import "os"

const DockerCommandEnvVarName = "R8_DOCKER_COMMAND"

func DockerCommandFromEnvironment() string {
	scheme := os.Getenv(DockerCommandEnvVarName)
	if scheme == "" {
		scheme = "docker"
	}
	return scheme
}
