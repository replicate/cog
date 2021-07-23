package docker

import (
	"os"
	"os/exec"
)

func Stop(id string) error {
	cmd := exec.Command("docker", "container", "stop", "--time", "3", id)
	cmd.Env = os.Environ()
	cmd.Stderr = os.Stderr

	_, err := cmd.Output()
	return err
}
