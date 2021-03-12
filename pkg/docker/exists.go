package docker

import (
	"os/exec"

	log "github.com/sirupsen/logrus"
)

func Exists(tag string) bool {
	cmd := exec.Command("docker", "inspect", "--type=image", tag)

	pipeToWithDockerChecks(cmd.StderrPipe, log.Debug)

	err := cmd.Run()
	return err == nil
}
