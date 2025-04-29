package docker

import (
	"context"
	"os"
	"os/exec"
)

func Stop(ctx context.Context, id string) error {
	cmd := exec.CommandContext(ctx, "docker", "container", "stop", "--time", "3", id)
	cmd.Env = os.Environ()
	cmd.Stderr = os.Stderr

	_, err := cmd.Output()
	return err
}
