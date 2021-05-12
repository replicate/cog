package docker

import (
	"fmt"
	"os/exec"

	"github.com/replicate/cog/pkg/util/console"

	"github.com/replicate/cog/pkg/logger"
)

func Pull(tag string, logWriter logger.Logger) error {
	console.Debugf("Downloading image %s...", tag)
	cmd := exec.Command("docker", "pull", tag)

	stderrDone, err := pipeToWithDockerChecks(cmd.StderrPipe, logWriter)
	if err != nil {
		return err
	}

	err = cmd.Run()
	<-stderrDone
	if err != nil {
		return fmt.Errorf("Failed to pull image: %w", err)
	}

	return err
}
