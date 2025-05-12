package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/docker/go-connections/nat"

	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/util"
	"github.com/replicate/cog/pkg/util/console"
	"github.com/replicate/cog/pkg/weights"
)

// type Port struct {
// 	HostPort      int
// 	ContainerPort int
// }

// type Volume struct {
// 	Source      string
// 	Destination string
// }

// type RunOptions struct {
// 	Args     []string
// 	Env      []string
// 	GPUs     string
// 	Image    string
// 	Ports    []Port
// 	Volumes  []Volume
// 	Workdir  string
// 	Platform string
// }

// used for generating arguments, with a few options not exposed by public API
type internalRunOptions struct {
	command.RunOptions
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

func Run(ctx context.Context, dockerClient command.Command, options command.RunOptions) error {
	return RunWithIO(ctx, dockerClient, options, os.Stdin, os.Stdout, os.Stderr)
}

func RunWithIO(ctx context.Context, dockerClient command.Command, options command.RunOptions, stdin io.Reader, stdout, stderr io.Writer) error {
	return dockerClient.Run(ctx, options)
	// internalOptions := internalRunOptions{RunOptions: options}
	// if stdin != nil {
	// 	internalOptions.Interactive = true
	// 	if f, ok := stdin.(*os.File); ok {
	// 		internalOptions.TTY = isatty.IsTerminal(f.Fd())
	// 	}
	// }
	// stderrCopy := new(bytes.Buffer)
	// stderrMultiWriter := io.MultiWriter(stderr, stderrCopy)

	// dockerArgs := generateDockerArgs(internalOptions)
	// cmd := exec.CommandContext(ctx, "docker", dockerArgs...)
	// cmd.Env = generateEnv(internalOptions)
	// cmd.Stdout = stdout
	// cmd.Stdin = stdin
	// cmd.Stderr = stderrMultiWriter
	// console.Debug("$ " + strings.Join(cmd.Args, " "))

	// err := cmd.Run()
	// if err != nil {
	// 	stderrString := stderrCopy.String()
	// 	if strings.Contains(stderrString, "could not select device driver") || strings.Contains(stderrString, "nvidia-container-cli: initialization error") {
	// 		return ErrMissingDeviceDriver
	// 	}
	// 	return err
	// }
	// return nil
}

func RunDaemon(ctx context.Context, dockerClient command.Command, options command.RunOptions, stderr io.Writer) (string, error) {
	options.Detach = true
	var stdout bytes.Buffer
	options.Stdout = &stdout

	if err := dockerClient.Run(ctx, options); err != nil {
		return "", fmt.Errorf("failed to run container: %w", err)
	}

	containerID := strings.TrimSpace(stdout.String())

	return containerID, nil

	// internalOptions := internalRunOptions{RunOptions: options}
	// internalOptions.Detach = true

	// stderrCopy := new(bytes.Buffer)
	// stderrMultiWriter := io.MultiWriter(stderr, stderrCopy)

	// dockerArgs := generateDockerArgs(internalOptions)
	// cmd := exec.CommandContext(ctx, "docker", dockerArgs...)
	// cmd.Env = generateEnv(internalOptions)
	// cmd.Stderr = stderrMultiWriter

	// console.Debug("$ " + strings.Join(cmd.Args, " "))

	// containerID, err := cmd.Output()

	// stderrString := stderrCopy.String()
	// if strings.Contains(stderrString, "could not select device driver") || strings.Contains(stderrString, "nvidia-container-cli: initialization error") {
	// 	return "", ErrMissingDeviceDriver
	// }

	// if err != nil {
	// 	return "", err
	// }

	// return strings.TrimSpace(string(containerID)), nil
}

func GetHostPortForContainer(ctx context.Context, dockerCommand command.Command, containerID string, containerPort int) (int, error) {
	console.Debugf("=== DockerCommand.GetPort %s/%d", containerID, containerPort)

	inspect, err := dockerCommand.ContainerInspect(ctx, containerID)
	if err != nil {
		return 0, fmt.Errorf("failed to inspect container %q: %w", containerID, err)
	}

	if inspect.ContainerJSONBase == nil || inspect.State == nil || !inspect.State.Running {
		return 0, fmt.Errorf("container %s is not running", containerID)
	}

	targetPort, err := nat.NewPort("tcp", strconv.Itoa(containerPort))
	if err != nil {
		return 0, fmt.Errorf("failed to create target port: %w", err)
	}

	if inspect.NetworkSettings == nil || inspect.NetworkSettings.Ports == nil {
		return 0, fmt.Errorf("container %s does not have expected network configuration", containerID)
	}

	for _, portBinding := range inspect.NetworkSettings.Ports[targetPort] {
		// TODO[md]: this should not be hardcoded since docker may be bound to a different address
		if portBinding.HostIP != "0.0.0.0" {
			continue
		}
		hostPort, err := nat.ParsePort(portBinding.HostPort)
		if err != nil {
			return 0, fmt.Errorf("failed to parse host port: %w", err)
		}
		return hostPort, nil
	}

	return 0, fmt.Errorf("container %s does not have a port bound to 0.0.0.0", containerID)
}

func FillInWeightsManifestVolumes(ctx context.Context, dockerCommand command.Command, runOptions command.RunOptions) (command.RunOptions, error) {
	// Check if the image has a weights manifest
	manifest, err := dockerCommand.Inspect(ctx, runOptions.Image)
	if err != nil {
		return runOptions, err
	}
	weightsManifest, ok := manifest.Config.Labels[command.CogWeightsManifestLabelKey]
	if ok {
		var weightsPaths []weights.WeightManifest
		err = json.Unmarshal([]byte(weightsManifest), &weightsPaths)
		if err != nil {
			return runOptions, err
		}
		for _, weightPath := range weightsPaths {
			runOptions.Volumes = append(runOptions.Volumes, command.Volume{
				Source:      weightPath.Source,
				Destination: "/src/" + weightPath.Destination,
			})
		}
	}

	return runOptions, nil
}
