package docker

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/replicate/cog/pkg/util/console"
)

type Port struct {
	HostPort      int
	ContainerPort int
}

type Volume struct {
	Source      string
	Destination string
}

type RunOptions struct {
	Args    []string
	Env     []string
	GPUs    string
	Image   string
	Ports   []Port
	Volumes []Volume
	Workdir string
}

func generateDockerArgs(options RunOptions) []string {
	// Use verbose options for clarity
	dockerArgs := []string{
		"run",
		"--interactive",
		"--rm",
		"--shm-size", "8G", // https://github.com/pytorch/pytorch/issues/2244
		// TODO: relative to pwd and cog.yaml
	}
	if options.GPUs != "" {
		dockerArgs = append(dockerArgs, "--gpus", options.GPUs)
	}
	for _, port := range options.Ports {
		dockerArgs = append(dockerArgs, "--publish", fmt.Sprintf("%d:%d", port.HostPort, port.ContainerPort))
	}
	for _, volume := range options.Volumes {
		// This needs escaping if we want to support commas in filenames
		// https://github.com/moby/moby/issues/8604
		dockerArgs = append(dockerArgs, "--mount", "type=bind,source="+volume.Source+",destination="+volume.Destination)
	}
	if options.Workdir != "" {
		dockerArgs = append(dockerArgs, "--workdir", options.Workdir)
	}
	if isatty.IsTerminal(os.Stdin.Fd()) {
		dockerArgs = append(dockerArgs, "--tty")
	}
	return dockerArgs
}

func Run(options RunOptions) error {
	dockerArgs := generateDockerArgs(options)
	dockerArgs = append(dockerArgs, options.Image)
	dockerArgs = append(dockerArgs, options.Args...)

	cmd := exec.Command("docker", dockerArgs...)
	cmd.Env = os.Environ()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	console.Debug("$ " + strings.Join(cmd.Args, " "))

	return cmd.Run()
}

func RunDaemon(options RunOptions) (string, error) {
	dockerArgs := generateDockerArgs(options)
	dockerArgs = append(dockerArgs, "--detach")
	dockerArgs = append(dockerArgs, options.Image)
	dockerArgs = append(dockerArgs, options.Args...)

	cmd := exec.Command("docker", dockerArgs...)
	cmd.Env = os.Environ()
	// TODO: display errors more elegantly?
	cmd.Stderr = os.Stderr

	console.Debug("$ " + strings.Join(cmd.Args, " "))

	containerID, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(containerID)), nil
}
