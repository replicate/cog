package docker

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/docker/cli/cli/config"
	"github.com/docker/cli/cli/config/configfile"
	"github.com/docker/cli/cli/config/types"

	"github.com/replicate/cog/pkg/util/console"
)

type credentialHelperInput struct {
	Username  string
	Secret    string
	ServerURL string
}

func SaveLoginToken(registryHost string, username string, token string) error {
	conf := config.LoadDefaultConfigFile(os.Stderr)
	credsStore := conf.CredentialsStore
	if credsStore == "" {
		return saveAuthToConfig(conf, registryHost, username, token)
	}
	return saveAuthToCredentialsStore(credsStore, registryHost, username, token)
}

func saveAuthToConfig(conf *configfile.ConfigFile, registryHost string, username string, token string) error {
	// conf.Save() will base64 encode username and password
	conf.AuthConfigs[registryHost] = types.AuthConfig{
		Username: username,
		Password: token,
	}
	if err := conf.Save(); err != nil {
		return fmt.Errorf("Failed to save Docker config.json: %w", err)
	}
	return nil
}

func saveAuthToCredentialsStore(credsStore string, registryHost string, username string, token string) error {
	binary := "docker-credential-" + credsStore
	input := credentialHelperInput{
		Username:  username,
		Secret:    token,
		ServerURL: registryHost,
	}
	cmd := exec.Command(binary, "store")
	cmd.Env = os.Environ()
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("Failed to connect stdin to %s: %w", binary, err)
	}
	console.Debug("$ " + strings.Join(cmd.Args, " "))
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("Failed to start %s: %w", binary, err)
	}
	if err := json.NewEncoder(stdin).Encode(input); err != nil {
		return fmt.Errorf("Failed to write to %s: %w", binary, err)
	}
	if err := stdin.Close(); err != nil {
		return fmt.Errorf("Failed to close stdin to %s: %w", binary, err)
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("Failed to run %s: %w", binary, err)
	}
	return nil
}
