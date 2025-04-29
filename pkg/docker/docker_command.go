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

	"github.com/docker/cli/cli/config"
	"github.com/docker/cli/cli/config/configfile"
	"github.com/docker/cli/cli/config/types"

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
	return c.exec(ctx, os.Stderr, "pull", image, "--platform", "linux/amd64")
}

func (c *DockerCommand) Push(ctx context.Context, image string) error {
	return c.exec(ctx, os.Stderr, "push", image)
}

func (c *DockerCommand) LoadUserInformation(ctx context.Context, registryHost string) (*command.UserInfo, error) {
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

func (c *DockerCommand) Inspect(ctx context.Context, image string) (*command.Manifest, error) {
	args := []string{
		"image",
		"inspect",
		image,
	}
	manifestData, err := c.execCaptured(ctx, args...)
	if err != nil {
		return nil, err
	}

	decoder := json.NewDecoder(bytes.NewReader(([]byte(manifestData))))
	var manifests []command.Manifest
	err = decoder.Decode(&manifests)
	if err != nil {
		return nil, err
	}

	if len(manifests) == 0 {
		return nil, errors.New("Failed to decode result of docker inspect")
	}
	return &manifests[0], nil // Docker inspect returns us a list of manifests
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
