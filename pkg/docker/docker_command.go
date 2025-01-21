package docker

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/docker/cli/cli/config"
	"github.com/docker/cli/cli/config/configfile"

	"github.com/replicate/cog/pkg/util/console"
)

type DockerCommand struct{}

func NewDockerCommand() *DockerCommand {
	return &DockerCommand{}
}

func (c *DockerCommand) Push(image string) error {
	return c.exec("push", image)
}

func (c *DockerCommand) LoadLoginToken(registryHost string) (string, error) {
	conf := config.LoadDefaultConfigFile(os.Stderr)
	credsStore := conf.CredentialsStore
	if credsStore == "" {
		return loadAuthFromConfig(conf, registryHost)
	}
	return loadAuthFromCredentialsStore(credsStore, registryHost)
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
	err := c.exec("run", args...)
	if err != nil {
		return "", err
	}
	return filepath.Join(tmpDir, tarFile), nil
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

func loadAuthFromConfig(conf *configfile.ConfigFile, registryHost string) (string, error) {
	return conf.AuthConfigs[registryHost].Password, nil
}

func loadAuthFromCredentialsStore(credsStore string, registryHost string) (string, error) {
	var out strings.Builder
	binary := DockerCredentialBinary(credsStore)
	cmd := exec.Command(binary, "get")
	cmd.Env = os.Environ()
	cmd.Stdout = &out
	cmd.Stderr = &out
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return "", err
	}
	defer stdin.Close()
	console.Debug("$ " + strings.Join(cmd.Args, " "))
	err = cmd.Start()
	if err != nil {
		return "", err
	}
	_, err = io.WriteString(stdin, registryHost)
	if err != nil {
		return "", err
	}
	err = stdin.Close()
	if err != nil {
		return "", err
	}
	err = cmd.Wait()
	if err != nil {
		return "", fmt.Errorf("exec wait error: %w", err)
	}

	var config CredentialHelperInput
	err = json.Unmarshal([]byte(out.String()), &config)
	if err != nil {
		return "", err
	}

	return config.Secret, nil
}

func DockerCredentialBinary(credsStore string) string {
	return "docker-credential-" + credsStore
}
