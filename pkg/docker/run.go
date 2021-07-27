package docker

import (
	"fmt"
	"io"
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

// used for generating arguments, with a few options not exposed by public API
type internalRunOptions struct {
	RunOptions
	Detach      bool
	Interactive bool
	TTY         bool
}

func generateDockerArgs(options internalRunOptions) []string {
	// Use verbose options for clarity
	dockerArgs := []string{
		"run",
		"--rm",
		"--shm-size", "8G", // https://github.com/pytorch/pytorch/issues/2244
		// TODO: relative to pwd and cog.yaml
	}

	if options.Detach {
		dockerArgs = append(dockerArgs, "--detach")
	}
	if options.GPUs != "" {
		dockerArgs = append(dockerArgs, "--gpus", options.GPUs)
	}
	if options.Interactive {
		dockerArgs = append(dockerArgs, "--interactive")
	}
	for _, port := range options.Ports {
		dockerArgs = append(dockerArgs, "--publish", fmt.Sprintf("%d:%d", port.HostPort, port.ContainerPort))
	}
	if options.TTY {
		dockerArgs = append(dockerArgs, "--tty")
	}
	for _, volume := range options.Volumes {
		// This needs escaping if we want to support commas in filenames
		// https://github.com/moby/moby/issues/8604
		dockerArgs = append(dockerArgs, "--mount", "type=bind,source="+volume.Source+",destination="+volume.Destination)
	}
	if options.Workdir != "" {
		dockerArgs = append(dockerArgs, "--workdir", options.Workdir)
	}
	dockerArgs = append(dockerArgs, options.Image)
	dockerArgs = append(dockerArgs, options.Args...)
	return dockerArgs
}

func Run(options RunOptions) error {
	return RunWithIO(options, os.Stdin, os.Stdout, os.Stderr)
}

func RunWithIO(options RunOptions, stdin io.Reader, stdout, stderr io.Writer) error {
	internalOptions := internalRunOptions{RunOptions: options}
	if stdin != nil {
		internalOptions.Interactive = true
		if f, ok := stdin.(*os.File); ok {
			internalOptions.TTY = isatty.IsTerminal(f.Fd())
		}
	}
	dockerArgs := generateDockerArgs(internalOptions)
	cmd := exec.Command("docker", dockerArgs...)
	cmd.Env = os.Environ()
	cmd.Stdout = stdout
	cmd.Stdin = stdin
	cmd.Stderr = stderr
	console.Debug("$ " + strings.Join(cmd.Args, " "))

	return cmd.Run()
}

func RunDaemon(options RunOptions) (string, error) {
	internalOptions := internalRunOptions{RunOptions: options}
	internalOptions.Detach = true

	dockerArgs := generateDockerArgs(internalOptions)
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
