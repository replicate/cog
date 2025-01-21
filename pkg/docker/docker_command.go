package docker

import (
	"os"
	"os/exec"
	"strings"

	"github.com/replicate/cog/pkg/util/console"
)

type DockerCommand struct{}

func NewDockerCommand() *DockerCommand {
	return &DockerCommand{}
}

func (c *DockerCommand) Push(image string) error {
	return c.exec("push", image)
}

func (c *DockerCommand) exec(name string, args ...string) error {
	cmdArgs := []string{name}
	cmdArgs = append(cmdArgs, args...)
	cmd := exec.Command("docker", cmdArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	console.Debug("$ " + strings.Join(cmd.Args, " "))
	return cmd.Run()
}
