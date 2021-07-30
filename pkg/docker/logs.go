package docker

import (
	"io"
	"os"
	"os/exec"
)

func ContainerLogsFollow(containerID string, out io.Writer) error {
	cmd := exec.Command("docker", "container", "logs", "--follow", containerID)
	cmd.Env = os.Environ()
	cmd.Stdout = out
	cmd.Stderr = out
	return cmd.Run()
}
