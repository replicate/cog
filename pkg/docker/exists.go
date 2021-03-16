package docker

import (
	"os"
	"os/exec"

	log "github.com/sirupsen/logrus"
)

func Exists(tag string) bool {
	cmd := exec.Command("docker", "inspect", "--type=image", tag)
	cmd.Env = os.Environ()

	pipeToWithDockerChecks(cmd.StderrPipe, log.Debug)

	err := cmd.Run()
	return err == nil
}
