package docker

import (
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

	"github.com/docker/cli/cli/config"
	"github.com/docker/cli/cli/config/configfile"
	"github.com/docker/cli/cli/config/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"

	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/util"
	"github.com/replicate/cog/pkg/util/console"
	"github.com/replicate/cog/pkg/util/slices"
)

var (
	commandsRequiringPlatform = []string{"build", "run"}
)

type DockerCommand struct{}

func NewDockerCommand() *DockerCommand {
	return &DockerCommand{}
}

func (c *DockerCommand) Pull(ctx context.Context, image string) error {
	console.Debugf("=== DockerCommand.Pull %s", image)

	return c.exec(ctx, os.Stderr, "pull", image, "--platform", "linux/amd64")
}

func (c *DockerCommand) Push(ctx context.Context, image string) error {
	console.Debugf("=== DockerCommand.Push %s", image)

	return c.exec(ctx, os.Stderr, "push", image)
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
		"--volume",
		tmpDir + ":/buildtmp",
		image,
		"/opt/r8/monobase/tar.sh",
		"/buildtmp/" + tarFile,
		"/",
		folder,
	}
	if err := c.exec(ctx, os.Stderr, args...); err != nil {
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
		"--volume",
		tmpDir + ":/buildtmp",
		"r8.im/monobase:latest",
		"/opt/r8/monobase/apt.sh",
		"/buildtmp/" + aptTarFile,
	}
	args = append(args, packages...)
	if err := c.exec(ctx, os.Stderr, args...); err != nil {
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
	output, err := c.execCaptured(ctx, args...)
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

	return c.exec(ctx, w, args...)
}

func (c *DockerCommand) ContainerInspect(ctx context.Context, id string) (*container.InspectResponse, error) {
	console.Debugf("=== DockerCommand.ContainerInspect %s", id)

	args := []string{
		"container",
		"inspect",
		id,
	}

	output, err := c.execCaptured(ctx, args...)
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

	if err := c.exec(ctx, os.Stderr, "container", "stop", "--time", "3", containerID); err != nil {
		if strings.Contains(err.Error(), "No such container") {
			err = &command.NotFoundError{Object: "container", Ref: containerID}
		}
		return fmt.Errorf("failed to stop container %q: %w", containerID, err)
	}

	return nil
}

func (c *DockerCommand) exec(ctx context.Context, w io.Writer, args ...string) error {
	if slices.ContainsString(commandsRequiringPlatform, args[0]) && util.IsAppleSiliconMac(runtime.GOOS, runtime.GOARCH) {
		args = append(args, "--platform", "linux/amd64")
	}
	dockerCmd := DockerCommandFromEnvironment()
	cmd := exec.CommandContext(ctx, dockerCmd, args...)

	// the ring buffer captures the last N bytes written to `w` so we have some context to return in an error
	errbuf := util.NewRingBufferWriter(w, 1024)
	cmd.Stdout = errbuf
	cmd.Stderr = errbuf

	console.Debug("$ " + strings.Join(cmd.Args, " "))
	err := cmd.Run()
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return err
		}
		return fmt.Errorf("command failed: %s: %w", errbuf.String(), err)
	}
	return nil
}

func (c *DockerCommand) execCaptured(ctx context.Context, args ...string) (string, error) {
	var out strings.Builder
	err := c.exec(ctx, &out, args...)
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
