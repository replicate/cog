package docker

import (
	"context"
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

func SaveLoginToken(ctx context.Context, registryHost string, username string, token string) error {
	conf := config.LoadDefaultConfigFile(os.Stderr)
	credsStore := conf.CredentialsStore
	if credsStore == "" {
		return saveAuthToConfig(conf, registryHost, username, token)
	}
	return saveAuthToCredentialsStore(ctx, credsStore, registryHost, username, token)
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

func saveAuthToCredentialsStore(ctx context.Context, credsStore string, registryHost string, username string, token string) error {
	binary := dockerCredentialBinary(credsStore)
	input := CredentialHelperInput{
		Username:  username,
		Secret:    token,
		ServerURL: registryHost,
	}
	cmd := exec.CommandContext(ctx, binary, "store") //nolint:gosec // G702: binary is from Docker config, not user input
	cmd.Env = os.Environ()
	var stderr strings.Builder
	cmd.Stderr = &stderr
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
		output := strings.TrimSpace(stderr.String())
		if strings.Contains(strings.ToLower(output), "pass not initialized") {
			return fmt.Errorf("failed to store Docker credentials because `pass` is not initialized; create a GPG key with `gpg --generate-key`, initialize the password store with `pass init <gpg-id>`, then run `cog login` again")
		}
		if output != "" {
			return fmt.Errorf("Failed to run %s: %s", binary, output)
		}
		return fmt.Errorf("Failed to run %s: %w", binary, err)
	}
	return nil
}
