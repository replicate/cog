package docker

import (
	"os"
	"os/exec"
)

func ImageRm(name string) error {
	cmd := exec.Command("docker", "image", "rm", name)
	cmd.Env = os.Environ()
	cmd.Stderr = os.Stderr

	_, err := cmd.Output()
	return err
}
