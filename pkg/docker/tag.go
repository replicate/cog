package docker

import (
	"os"
	"os/exec"
)

func Tag(source, target string) error {
	cmd := exec.Command("docker", "image", "tag", source, target)
	cmd.Env = os.Environ()
	cmd.Stderr = os.Stderr

	_, err := cmd.Output()
	return err
}
