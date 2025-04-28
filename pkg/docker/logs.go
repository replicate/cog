package docker

import (
	"context"
	"io"
	"os"
	"os/exec"
)

func ContainerLogsFollow(ctx context.Context, containerID string, out io.Writer) error {
	cmd := exec.CommandContext(ctx, "docker", "container", "logs", "--follow", containerID)
	cmd.Env = os.Environ()
	cmd.Stdout = out
	cmd.Stderr = out
	return cmd.Run()
}
