package docker

import (
	"os"
	"os/exec"

	"github.com/replicate/cog/pkg/logger"
)

func Exists(tag string, logWriter logger.Logger) bool {
	cmd := exec.Command("docker", "inspect", "--type=image", tag)
	cmd.Env = os.Environ()
	// TODO(andreas): error handling
	//nolint
	pipeToWithDockerChecks(cmd.StderrPipe, logWriter)

	err := cmd.Run()
	return err == nil
}
