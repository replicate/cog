package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/docker/cli/cli/config"
	"github.com/docker/cli/cli/config/configfile"
	"github.com/docker/cli/cli/config/types"
	"github.com/docker/docker/api/types/registry"

	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/util/console"
)

func loadUserInformation(ctx context.Context, registryHost string) (*command.UserInfo, error) {
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

func loadAuthFromConfig(conf *configfile.ConfigFile, registryHost string) (types.AuthConfig, error) {
	return conf.AuthConfigs[registryHost], nil
}

func loadRegistryAuths(ctx context.Context, registryHosts ...string) (map[string]registry.AuthConfig, error) {
	conf := config.LoadDefaultConfigFile(os.Stderr)

	out := make(map[string]registry.AuthConfig)

	for _, host := range registryHosts {
		console.Debugf("=== loadRegistryAuths %s", host)
		// check the credentials store first if set
		if conf.CredentialsStore != "" {
			console.Debugf("=== loadRegistryAuths %s: credentials store set", host)
			credsHelper, err := loadAuthFromCredentialsStore(ctx, conf.CredentialsStore, host)
			if err != nil {
				console.Debugf("=== loadRegistryAuths %s: error loading credentials store: %s", host, err)
				return nil, err
			}
			console.Debugf("=== loadRegistryAuths %s: credentials store loaded", host)
			out[host] = registry.AuthConfig{
				Username:      credsHelper.Username,
				Password:      credsHelper.Secret,
				ServerAddress: host,
			}
			continue
		}

		// next, check if the auth config exists in the config file
		if auth, ok := conf.AuthConfigs[host]; ok {
			console.Debugf("=== loadRegistryAuths %s: auth config found in config file", host)
			out[host] = registry.AuthConfig{
				Username:      auth.Username,
				Password:      auth.Password,
				Auth:          auth.Auth,
				Email:         auth.Email,
				ServerAddress: host,
				IdentityToken: auth.IdentityToken,
				RegistryToken: auth.RegistryToken,
			}
			continue
		}

		console.Debugf("=== loadRegistryAuths %s: no auth config found", host)
	}

	return out, nil
}

func loadAuthFromCredentialsStore(ctx context.Context, credsStore string, registryHost string) (*CredentialHelperInput, error) {
	var out strings.Builder
	binary := dockerCredentialBinary(credsStore)
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

func dockerCredentialBinary(credsStore string) string {
	return "docker-credential-" + credsStore
}
