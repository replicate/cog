package docker

import (
	"bytes"
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

func (c *DockerCommand) Pull(image string) error {
	_, err := c.exec("pull", false, image, "--platform", "linux/amd64")
	return err
}

func (c *DockerCommand) Push(image string) error {
	_, err := c.exec("push", false, image)
	return err
}

func (c *DockerCommand) LoadUserInformation(registryHost string) (*command.UserInfo, error) {
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
	credsHelper, err := loadAuthFromCredentialsStore(credsStore, registryHost)
	if err != nil {
		return nil, err
	}
	return &command.UserInfo{
		Token:    credsHelper.Secret,
		Username: credsHelper.Username,
	}, nil
}

func (c *DockerCommand) CreateTarFile(image string, tmpDir string, tarFile string, folder string) (string, error) {
	args := []string{
		"--rm",
		"--volume",
		tmpDir + ":/buildtmp",
		image,
		"/opt/r8/monobase/tar.sh",
		"/buildtmp/" + tarFile,
		"/",
		folder,
	}
	_, err := c.exec("run", false, args...)
	if err != nil {
		return "", err
	}
	return filepath.Join(tmpDir, tarFile), nil
}

func (c *DockerCommand) CreateAptTarFile(tmpDir string, aptTarFile string, packages ...string) (string, error) {
	// This uses a hardcoded monobase image to produce an apt tar file.
	// The reason being that this apt tar file is created outside the docker file, and it is created by
	// running the apt.sh script on the monobase with the packages we intend to install, which produces
	// a tar file that can be untarred into a docker build to achieve the equivalent of an apt-get install.
	args := []string{
		"--rm",
		"--volume",
		tmpDir + ":/buildtmp",
		"r8.im/monobase:latest",
		"/opt/r8/monobase/apt.sh",
		"/buildtmp/" + aptTarFile,
	}
	args = append(args, packages...)
	_, err := c.exec("run", false, args...)
	if err != nil {
		return "", err
	}

	return aptTarFile, nil
}

func (c *DockerCommand) Inspect(image string) (*command.Manifest, error) {
	args := []string{
		"inspect",
		image,
	}
	manifestData, err := c.exec("image", true, args...)
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

func (c *DockerCommand) exec(name string, capture bool, args ...string) (string, error) {
	cmdArgs := []string{name}
	if slices.ContainsString(commandsRequiringPlatform, name) && util.IsAppleSiliconMac(runtime.GOOS, runtime.GOARCH) {
		cmdArgs = append(cmdArgs, "--platform", "linux/amd64")
	}
	cmdArgs = append(cmdArgs, args...)
	dockerCmd := DockerCommandFromEnvironment()
	cmd := exec.Command(dockerCmd, cmdArgs...)
	var out strings.Builder
	if !capture {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	} else {
		cmd.Stdout = &out
		cmd.Stderr = &out
	}

	console.Debug("$ " + strings.Join(cmd.Args, " "))
	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("%s: %w", out.String(), err)
	}
	return out.String(), nil
}

func loadAuthFromConfig(conf *configfile.ConfigFile, registryHost string) (types.AuthConfig, error) {
	return conf.AuthConfigs[registryHost], nil
}

func loadAuthFromCredentialsStore(credsStore string, registryHost string) (*CredentialHelperInput, error) {
	var out strings.Builder
	binary := DockerCredentialBinary(credsStore)
	cmd := exec.Command(binary, "get")
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
