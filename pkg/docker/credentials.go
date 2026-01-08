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
	"github.com/replicate/cog/pkg/global"
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
		// Try loading auth for the requested host
		auth, err := tryLoadAuthForHost(ctx, conf, host)
		if err == nil && auth != nil {
			out[host] = *auth
			continue
		}

		// FALLBACK: If requesting alternate registry and no auth found,
		// try reusing r8.im credentials
		if host != global.DefaultReplicateRegistryHost {
			auth, err := tryLoadAuthForHost(ctx, conf, global.DefaultReplicateRegistryHost)
			if err == nil && auth != nil {
				// Reuse credentials for the alternate registry
				auth.ServerAddress = host // Update to new host
				out[host] = *auth
				console.Infof("Using existing %s credentials for %s", global.DefaultReplicateRegistryHost, host)
				continue
			}
		}
	}

	return out, nil
}

func tryLoadAuthForHost(ctx context.Context, conf *configfile.ConfigFile, host string) (*registry.AuthConfig, error) {
	// Try credentials store first (e.g., osxkeychain, pass)
	if conf.CredentialsStore != "" {
		credsHelper, err := loadAuthFromCredentialsStore(ctx, conf.CredentialsStore, host)
		if err == nil {
			return &registry.AuthConfig{
				Username:      credsHelper.Username,
				Password:      credsHelper.Secret,
				ServerAddress: host,
			}, nil
		}
	}

	// Fallback to config file
	if auth, ok := conf.AuthConfigs[host]; ok {
		return &registry.AuthConfig{
			Username:      auth.Username,
			Password:      auth.Password,
			Auth:          auth.Auth,
			Email:         auth.Email,
			ServerAddress: host,
			IdentityToken: auth.IdentityToken,
			RegistryToken: auth.RegistryToken,
		}, nil
	}

	return nil, fmt.Errorf("no credentials found for %s", host)
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
		output := strings.TrimSpace(out.String())
		if output != "" {
			return nil, fmt.Errorf("failed to get credentials for %q: %s", registryHost, output)
		}
		return nil, fmt.Errorf("failed to get credentials for %q: %w", registryHost, err)
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
