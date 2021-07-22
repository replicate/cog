package docker

import (
	"os"
	"os/exec"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/replicate/cog/pkg/util/console"
)

func Run(dir, image string, args []string) error {
	// TODO(bfirsh): ports
	ports := []string{}

	dockerArgs := []string{
		"run",
		"--interactive",
		"--rm",
		"--shm-size", "8G", // https://github.com/pytorch/pytorch/issues/2244
		// This needs escaping if we want to support commas in filenames
		// https://github.com/moby/moby/issues/8604
		"--mount", "type=bind,source=" + dir + ",destination=/src",
		// TODO: relative to pwd and cog.yaml
		"--workdir=/src",
	}
	for _, port := range ports {
		dockerArgs = append(dockerArgs, "-p", port+":"+port)
	}
	if isatty.IsTerminal(os.Stdin.Fd()) {
		dockerArgs = append(dockerArgs, "--tty")
	}
	dockerArgs = append(dockerArgs, image)
	dockerArgs = append(dockerArgs, args...)

	cmd := exec.Command("docker", dockerArgs...)
	cmd.Env = os.Environ()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	console.Debug("$ " + strings.Join(cmd.Args, " "))

	return cmd.Run()
}
