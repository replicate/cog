package docker

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"github.com/mattn/go-isatty"

	"github.com/replicate/cog/pkg/util"
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
	Args     []string
	Env      []string
	GPUs     string
	Image    string
	Ports    []Port
	Volumes  []Volume
	Workdir  string
	Platform string
}

// used for generating arguments, with a few options not exposed by public API
type internalRunOptions struct {
	RunOptions
	Detach      bool
	Interactive bool
	TTY         bool
}

var ErrMissingDeviceDriver = errors.New("Docker is missing required device driver")

func generateDockerArgs(options internalRunOptions) []string {
	// Use verbose options for clarity
	dockerArgs := []string{
		"run",
		"--rm",
		"--shm-size", "6G",
		// https://github.com/pytorch/pytorch/issues/2244
		// https://github.com/replicate/cog/issues/1293
		// TODO: relative to pwd and cog.yaml
	}

	if options.Detach {
		dockerArgs = append(dockerArgs, "--detach")
	}
	for _, env := range options.Env {
		dockerArgs = append(dockerArgs, "--env", env)
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
	if options.Platform != "" {
		dockerArgs = append(dockerArgs, "--platform", options.Platform)
	}
	dockerArgs = append(dockerArgs, options.Image)
	dockerArgs = append(dockerArgs, options.Args...)
	return dockerArgs
}

func generateEnv(options internalRunOptions) []string {
	env := os.Environ()
	if util.IsAppleSiliconMac(runtime.GOOS, runtime.GOARCH) {
		// Fixes "WARNING: The requested image's platform (linux/amd64) does not match the detected host platform (linux/arm64/v8) and no specific platform was requested"
		env = append(env, "DOCKER_DEFAULT_PLATFORM=linux/amd64")
	}

	return env
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
	stderrCopy := new(bytes.Buffer)
	stderrMultiWriter := io.MultiWriter(stderr, stderrCopy)

	dockerArgs := generateDockerArgs(internalOptions)
	cmd := exec.Command("docker", dockerArgs...)
	cmd.Env = generateEnv(internalOptions)
	cmd.Stdout = stdout
	cmd.Stdin = stdin
	cmd.Stderr = stderrMultiWriter
	console.Debug("$ " + strings.Join(cmd.Args, " "))

	err := cmd.Run()
	if err != nil {
		stderrString := stderrCopy.String()
		if strings.Contains(stderrString, "could not select device driver") || strings.Contains(stderrString, "nvidia-container-cli: initialization error") {
			return ErrMissingDeviceDriver
		}
		return err
	}
	return nil
}

func RunDaemon(options RunOptions, stderr io.Writer) (string, error) {
	internalOptions := internalRunOptions{RunOptions: options}
	internalOptions.Detach = true

	stderrCopy := new(bytes.Buffer)
	stderrMultiWriter := io.MultiWriter(stderr, stderrCopy)

	dockerArgs := generateDockerArgs(internalOptions)
	cmd := exec.Command("docker", dockerArgs...)
	cmd.Env = generateEnv(internalOptions)
	cmd.Stderr = stderrMultiWriter

	console.Debug("$ " + strings.Join(cmd.Args, " "))

	containerID, err := cmd.Output()

	stderrString := stderrCopy.String()
	if strings.Contains(stderrString, "could not select device driver") || strings.Contains(stderrString, "nvidia-container-cli: initialization error") {
		return "", ErrMissingDeviceDriver
	}

	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(containerID)), nil
}

func GetPort(containerID string, containerPort int) (int, error) {
	cmd := exec.Command("docker", "port", containerID, fmt.Sprintf("%d", containerPort)) //#nosec G204
	cmd.Env = os.Environ()
	cmd.Stderr = os.Stderr

	output, err := cmd.Output()
	if err != nil {
		return 0, err
	}

	lines := []string{}
	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanner.Err() != nil {
		return 0, err
	}

	for _, line := range lines {
		if !strings.HasPrefix(line, "0.0.0.0:") {
			continue
		}

		_, portString, err := net.SplitHostPort(strings.TrimSpace(line))
		if err != nil {
			return 0, err
		}

		port, err := strconv.Atoi(portString)
		if err != nil {
			return 0, err
		}

		return port, nil
	}

	return 0, fmt.Errorf("did not find port bound to 0.0.0.0 in `docker port` output")

}
