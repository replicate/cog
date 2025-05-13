package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/creack/pty"
	"github.com/docker/cli/cli/config"
	"github.com/docker/cli/cli/config/configfile"
	"github.com/docker/cli/cli/config/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/mattn/go-isatty"

	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/util"
	"github.com/replicate/cog/pkg/util/console"

	cogconfig "github.com/replicate/cog/pkg/config"
)

type DockerCommand struct{}

func NewDockerCommand() *DockerCommand {
	return &DockerCommand{}
}

func (c *DockerCommand) Pull(ctx context.Context, image string, force bool) (*image.InspectResponse, error) {
	console.Debugf("=== DockerCommand.Pull %s force:%t", image, force)

	if !force {
		inspect, err := c.Inspect(ctx, image)
		if err == nil {
			return inspect, nil
		} else if !command.IsNotFoundError(err) {
			// Log a warning if inspect fails for any reason other than not found.
			// It's likely that pull will fail as well, but it's better to return that error
			// so the caller can handle it appropriately than to fail silently here.
			console.Warnf("failed to inspect image before pulling %q: %s", image, err)
		}
	}

	args := []string{
		"pull",
		image,
		// force image to linux/amd64 to match production
		"--platform", "linux/amd64",
	}

	err := c.exec(ctx, nil, nil, nil, "", args)
	if err != nil {
		// A "not found" error message will be different depending on what flavor of engine and
		// registry version we're hitting. This checks for both docker and OCI lingo.
		if strings.Contains(err.Error(), "manifest unknown") || strings.Contains(err.Error(), "failed to resolve reference") {
			return nil, &command.NotFoundError{Object: "manifest", Ref: image}
		}
		return nil, fmt.Errorf("failed to pull image %q: %w", image, err)
	}

	// pull succeeded, inspect the image again and return
	inspect, err := c.Inspect(ctx, image)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect image after pulling %q: %w", image, err)
	}
	return inspect, nil
}

func (c *DockerCommand) Push(ctx context.Context, image string) error {
	console.Debugf("=== DockerCommand.Push %s", image)

	return c.exec(ctx, nil, nil, nil, "", []string{"push", image})
}

func (c *DockerCommand) LoadUserInformation(ctx context.Context, registryHost string) (*command.UserInfo, error) {
	console.Debugf("=== DockerCommand.LoadUserInformation %s", registryHost)

	conf := config.LoadDefaultConfigFile(os.Stderr)
	credsStore := conf.CredentialsStore
	if credsStore == "" {
		authConf, err := loadAuthFromConfig(conf, registryHost)
		if err != nil {
			return nil, err
		}
		return &command.UserInfo{
			Token:    authConf.Password,
			Username: authConf.Username,
		}, nil
	}
	credsHelper, err := loadAuthFromCredentialsStore(ctx, credsStore, registryHost)
	if err != nil {
		return nil, err
	}
	return &command.UserInfo{
		Token:    credsHelper.Secret,
		Username: credsHelper.Username,
	}, nil
}

func (c *DockerCommand) CreateTarFile(ctx context.Context, image string, tmpDir string, tarFile string, folder string) (string, error) {
	console.Debugf("=== DockerCommand.CreateTarFile %s %s %s %s", image, tmpDir, tarFile, folder)

	args := []string{
		"run",
		"--rm",
		// force platform to linux/amd64 so darwin/arm64 outputs work in prod
		"--platform", "linux/amd64",
		"--volume",
		tmpDir + ":/buildtmp",
		image,
		"/opt/r8/monobase/tar.sh",
		"/buildtmp/" + tarFile,
		"/",
		folder,
	}
	if err := c.exec(ctx, nil, nil, nil, "", args); err != nil {
		return "", err
	}
	return filepath.Join(tmpDir, tarFile), nil
}

func (c *DockerCommand) CreateAptTarFile(ctx context.Context, tmpDir string, aptTarFile string, packages ...string) (string, error) {
	console.Debugf("=== DockerCommand.CreateAptTarFile %s %s", aptTarFile, packages)

	// This uses a hardcoded monobase image to produce an apt tar file.
	// The reason being that this apt tar file is created outside the docker file, and it is created by
	// running the apt.sh script on the monobase with the packages we intend to install, which produces
	// a tar file that can be untarred into a docker build to achieve the equivalent of an apt-get install.
	args := []string{
		"run",
		"--rm",
		// force platform to linux/amd64 so darwin/arm64 outputs work in prod
		"--platform", "linux/amd64",
		"--volume",
		tmpDir + ":/buildtmp",
		"r8.im/monobase:latest",
		"/opt/r8/monobase/apt.sh",
		"/buildtmp/" + aptTarFile,
	}
	args = append(args, packages...)
	if err := c.exec(ctx, nil, nil, nil, "", args); err != nil {
		return "", err
	}

	return aptTarFile, nil
}

func (c *DockerCommand) Inspect(ctx context.Context, ref string) (*image.InspectResponse, error) {
	console.Debugf("=== DockerCommand.Inspect %s", ref)
	args := []string{
		"image",
		"inspect",
		ref,
	}
	output, err := c.execCaptured(ctx, nil, "", args)
	if err != nil {
		if strings.Contains(err.Error(), "No such image") {
			return nil, &command.NotFoundError{Object: "image", Ref: ref}
		}
		return nil, err
	}

	console.Debugf("=== DockerCommand.Inspect %s", output)

	var resp []image.InspectResponse
	if err := json.Unmarshal([]byte(output), &resp); err != nil {
		return nil, fmt.Errorf("error unmarshaling inspect response: %w", err)
	}

	// There may be some Docker versions where a missing image
	// doesn't return exit code 1, but progresses to output an
	// empty list.
	if len(resp) == 0 {
		return nil, &command.NotFoundError{Ref: ref}
	}
	// inspect returns a list of manifests but we only care about the first
	return &resp[0], nil
}

func (c *DockerCommand) ImageExists(ctx context.Context, ref string) (bool, error) {
	console.Debugf("=== DockerCommand.ImageExists %s", ref)
	_, err := c.Inspect(ctx, ref)
	if err != nil {
		if command.IsNotFoundError(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (c *DockerCommand) ContainerLogs(ctx context.Context, containerID string, w io.Writer) error {
	console.Debugf("=== DockerCommand.ContainerLogs %s", containerID)

	args := []string{
		"container",
		"logs",
		containerID,
		"--follow",
	}

	return c.exec(ctx, nil, w, nil, "", args)
}

func (c *DockerCommand) ContainerInspect(ctx context.Context, id string) (*container.InspectResponse, error) {
	console.Debugf("=== DockerCommand.ContainerInspect %s", id)

	args := []string{
		"container",
		"inspect",
		id,
	}

	output, err := c.execCaptured(ctx, nil, "", args)
	if err != nil {
		if strings.Contains(err.Error(), "No such container") {
			return nil, &command.NotFoundError{Object: "container", Ref: id}
		}
		return nil, err
	}

	var resp []*container.InspectResponse
	if err = json.Unmarshal([]byte(output), &resp); err != nil {
		return nil, err
	}
	if len(resp) == 0 {
		return nil, &command.NotFoundError{Object: "container", Ref: id}
	}

	return resp[0], nil
}

func (c *DockerCommand) ContainerStop(ctx context.Context, containerID string) error {
	console.Debugf("=== DockerCommand.ContainerStop %s", containerID)

	args := []string{
		"container",
		"stop",
		"--timeout", "3",
		containerID,
	}

	if err := c.exec(ctx, nil, nil, nil, "", args); err != nil {
		if strings.Contains(err.Error(), "No such container") {
			err = &command.NotFoundError{Object: "container", Ref: containerID}
		}
		return fmt.Errorf("failed to stop container %q: %w", containerID, err)
	}

	return nil
}

func (c *DockerCommand) ImageBuild(ctx context.Context, options command.ImageBuildOptions) error {
	console.Debugf("=== DockerCommand.ImageBuild %s", options.ImageName)

	args := []string{
		"buildx", "build",
		// disable provenance attestations since we don't want them cluttering the registry
		"--provenance", "false",
		// Fixes "WARNING: The requested image's platform (linux/amd64) does not match the detected host platform (linux/arm64/v8) and no specific platform was requested"
		// We do this regardless of the host platform so windows/*. linux/arm64, etc work as well
		"--platform", "linux/amd64",
	}

	if util.IsAppleSiliconMac(runtime.GOOS, runtime.GOARCH) {
		args = append(args,
			// buildx doesn't load images by default, so we tell it to load here. _however_, the
			// --output type=docker,rewrite-timestamp=true flag also loads the image, this may not be necessary
			"--load",
		)
	}

	for _, secret := range options.Secrets {
		args = append(args, "--secret", secret)
	}

	if options.NoCache {
		args = append(args, "--no-cache")
	}

	for k, v := range options.Labels {
		// Unlike in Dockerfiles, the value here does not need quoting -- Docker merely
		// splits on the first '=' in the argument and the rest is the label value.
		args = append(args, "--label", fmt.Sprintf(`%s=%s`, k, v))
	}

	// Base Images are special, we force timestamp rewriting to epoch. This requires some consideration on the output
	// format. It's generally safe to override to --output type=docker,rewrite-timestamp=true as the use of `--load` is
	// equivalent to `--output type=docker`
	if options.Epoch != nil && *options.Epoch >= 0 {
		args = append(args,
			"--build-arg", fmt.Sprintf("SOURCE_DATE_EPOCH=%d", options.Epoch),
			"--output", "type=docker,rewrite-timestamp=true")
		console.Infof("Forcing timestamp rewriting to epoch %d", options.Epoch)
	}

	if cogconfig.BuildXCachePath != "" {
		args = append(
			args,
			"--cache-from", "type=local,src="+cogconfig.BuildXCachePath,
			"--cache-to", "type=local,dest="+cogconfig.BuildXCachePath,
		)
	} else {
		args = append(args, "--cache-to", "type=inline")
	}

	for name, dir := range options.BuildContexts {
		args = append(args, "--build-context", name+"="+dir)
	}

	if options.ProgressOutput != "" {
		args = append(args, "--progress", options.ProgressOutput)
	}

	// default to "." if a context dir is not provided
	if options.ContextDir == "" {
		options.ContextDir = "."
	}

	args = append(args,
		"--file", "-",
		"--tag", options.ImageName,
		options.ContextDir,
	)

	in := strings.NewReader(options.DockerfileContents)

	return c.exec(ctx, in, nil, nil, options.WorkingDir, args)
}

func (c *DockerCommand) ContainerStart(ctx context.Context, options command.RunOptions) (string, error) {
	console.Debugf("=== DockerCommand.ContainerStart %s %v", options.Image, options.Args)

	var out bytes.Buffer
	options.Stdout = &out
	options.Detach = true

	if err := c.containerRun(ctx, options); err != nil {
		return "", err
	}

	return strings.TrimSpace(out.String()), nil
}

func (c *DockerCommand) Run(ctx context.Context, options command.RunOptions) error {
	console.Debugf("=== DockerCommand.Run %s %v", options.Image, options.Args)
	if options.Stdout == nil {
		options.Stdout = os.Stdout
	}
	if options.Stderr == nil {
		options.Stderr = os.Stderr
	}

	return c.containerRun(ctx, options)
}

func (c *DockerCommand) containerRun(ctx context.Context, options command.RunOptions) error {
	console.Debugf("=== DockerCommand.containerRun %s", options.Image)

	var isInteractive, isTTY bool
	if options.Stdin != nil {
		isInteractive = true
		if f, ok := options.Stdin.(*os.File); ok {
			isTTY = isatty.IsTerminal(f.Fd())
		}
	}

	args := []string{
		"run",
		"--rm",
		// https://github.com/pytorch/pytorch/issues/2244
		// https://github.com/replicate/cog/issues/1293
		"--shm-size", "6G",
		// force platform to linux/amd64
		"--platform", "linux/amd64",
	}

	for _, env := range options.Env {
		args = append(args, "--env", env)
	}

	if options.Detach {
		args = append(args, "--detach")
	}

	if options.GPUs != "" {
		args = append(args, "--gpus", options.GPUs)
	}
	if isInteractive {
		args = append(args, "--interactive")
	}
	for _, port := range options.Ports {
		args = append(args, "--publish", fmt.Sprintf("%d:%d", port.HostPort, port.ContainerPort))
	}
	if isTTY {
		args = append(args, "--tty")
	}
	for _, volume := range options.Volumes {
		// This needs escaping if we want to support commas in filenames
		// https://github.com/moby/moby/issues/8604
		args = append(args, "--mount", "type=bind,source="+volume.Source+",destination="+volume.Destination)
	}
	if options.Workdir != "" {
		args = append(args, "--workdir", options.Workdir)
	}

	args = append(args, options.Image)
	args = append(args, options.Args...)

	err := c.exec(ctx, options.Stdin, options.Stdout, options.Stderr, "", args)
	if err != nil {
		if strings.Contains(err.Error(), "could not select device driver") || strings.Contains(err.Error(), "nvidia-container-cli: initialization error") {
			return ErrMissingDeviceDriver
		}
		return err
	}
	return nil
}

func (c *DockerCommand) exec(ctx context.Context, in io.Reader, outw, errw io.Writer, dir string, args []string) error {
	if outw == nil {
		outw = os.Stderr
	}
	if errw == nil {
		errw = os.Stderr
	}

	dockerCmd := DockerCommandFromEnvironment()
	cmd := exec.CommandContext(ctx, dockerCmd, args...)
	if dir != "" {
		cmd.Dir = dir
	}

	// setup stderr buffer & writer to errw and buffer
	var stderrBuf bytes.Buffer

	// if errw is a TTY, use a pty for stderr output so that the child process will properly detect an interactive console
	if f, ok := errw.(*os.File); ok && console.IsTTY(f) {
		stderrpty, stderrtty, err := pty.Open()
		if err != nil {
			return fmt.Errorf("failed to open stderr pty: %w", err)
		}
		cmd.Stderr = stderrtty

		go func() {
			defer stderrpty.Close()
			defer stderrtty.Close()

			_, err = io.Copy(io.MultiWriter(
				errw,
				util.NewRingBufferWriter(&stderrBuf, 1024),
			), stderrpty)
			if err != nil {
				console.Errorf("failed to copy stderr pty to errw: %s", err)
			}
		}()
	} else {
		cmd.Stderr = io.MultiWriter(errw, util.NewRingBufferWriter(&stderrBuf, 1024))
	}

	// setup stdout pipe
	outpipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	// copy stdout to outw
	go func() {
		defer outpipe.Close()

		_, err = io.Copy(outw, outpipe)
		if err != nil {
			console.Errorf("failed to copy stdout to outw: %s", err)
		}
	}()

	if in != nil {
		cmd.Stdin = in
	}

	console.Debug("$ " + strings.Join(cmd.Args, " "))
	if err := cmd.Run(); err != nil {
		if errors.Is(err, context.Canceled) {
			return err
		}
		return fmt.Errorf("command failed: %s: %w", stderrBuf.String(), err)
	}
	return nil
}

func (c *DockerCommand) execCaptured(ctx context.Context, in io.Reader, dir string, args []string) (string, error) {
	var out strings.Builder
	err := c.exec(ctx, in, &out, nil, dir, args)
	if err != nil {
		return "", err
	}
	return out.String(), nil
}

func loadAuthFromConfig(conf *configfile.ConfigFile, registryHost string) (types.AuthConfig, error) {
	return conf.AuthConfigs[registryHost], nil
}

func loadAuthFromCredentialsStore(ctx context.Context, credsStore string, registryHost string) (*CredentialHelperInput, error) {
	var out strings.Builder
	binary := DockerCredentialBinary(credsStore)
	cmd := exec.CommandContext(ctx, binary, "get")
	cmd.Env = os.Environ()
	cmd.Stdout = &out
	cmd.Stderr = &out
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	defer stdin.Close()
	console.Debug("$ " + strings.Join(cmd.Args, " "))
	err = cmd.Start()
	if err != nil {
		return nil, err
	}
	_, err = io.WriteString(stdin, registryHost)
	if err != nil {
		return nil, err
	}
	err = stdin.Close()
	if err != nil {
		return nil, err
	}
	err = cmd.Wait()
	if err != nil {
		return nil, fmt.Errorf("exec wait error: %w", err)
	}

	var config CredentialHelperInput
	err = json.Unmarshal([]byte(out.String()), &config)
	if err != nil {
		return nil, err
	}

	return &config, nil
}

func DockerCredentialBinary(credsStore string) string {
	return "docker-credential-" + credsStore
}
