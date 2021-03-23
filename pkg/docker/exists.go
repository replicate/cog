package docker

import (
	"os"
	"os/exec"
)

func Exists(tag string, logWriter func(string)) bool {
	// TODO(andreas): error handling
	cmd := exec.Command("docker", "inspect", "--type=image", tag)
	cmd.Env = os.Environ()

	pipeToWithDockerChecks(cmd.StderrPipe, logWriter)

	err := cmd.Run()
	return err == nil
}
