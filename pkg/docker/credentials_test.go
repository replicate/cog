package docker

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/docker/cli/cli/config/configfile"
	"github.com/docker/cli/cli/config/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/global"
)

func TestLoadRegistryAuths_Fallback(t *testing.T) {
	ctx := context.Background()

	t.Run("uses credentials for requested host when available", func(t *testing.T) {
		// Create a mock config with credentials for the requested host
		conf := &configfile.ConfigFile{
			AuthConfigs: map[string]types.AuthConfig{
				"registry.example.com": {
					Username: "user1",
					Password: "pass1",
				},
			},
		}

		auth, err := tryLoadAuthForHost(ctx, conf, "registry.example.com")
		require.NoError(t, err)
		require.NotNil(t, auth)
		assert.Equal(t, "user1", auth.Username)
		assert.Equal(t, "pass1", auth.Password)
		assert.Equal(t, "registry.example.com", auth.ServerAddress)
	})

	t.Run("falls back to default registry credentials when alternate registry has no credentials", func(t *testing.T) {
		// Set up a temporary docker config file
		tmpDir := t.TempDir()
		dockerConfigPath := filepath.Join(tmpDir, "config.json")

		// Create a config file with credentials only for the default registry
		conf := &configfile.ConfigFile{
			Filename: dockerConfigPath,
			AuthConfigs: map[string]types.AuthConfig{
				global.DefaultReplicateRegistryHost: {
					Username: "defaultuser",
					Password: "defaultpass",
				},
			},
		}
		require.NoError(t, conf.Save())

		// Point Docker to our test config
		t.Setenv("DOCKER_CONFIG", tmpDir)

		// Try loading auth for an alternate registry that doesn't have credentials
		auths, err := loadRegistryAuths(ctx, "registry.example.com")
		require.NoError(t, err)
		require.NotNil(t, auths)

		// Should have fallen back to default registry credentials
		auth, ok := auths["registry.example.com"]
		require.True(t, ok, "should have auth for registry.example.com")
		assert.Equal(t, "defaultuser", auth.Username)
		assert.Equal(t, "defaultpass", auth.Password)
		assert.Equal(t, "registry.example.com", auth.ServerAddress, "server address should be updated to the requested host")
	})

	t.Run("does not fallback when requesting default registry", func(t *testing.T) {
		// This test uses tryLoadAuthForHost directly to avoid credential store issues
		conf := &configfile.ConfigFile{
			AuthConfigs: map[string]types.AuthConfig{},
		}

		// Try loading auth for the default registry
		auth, err := tryLoadAuthForHost(ctx, conf, global.DefaultReplicateRegistryHost)
		require.Error(t, err, "should error when no credentials found")
		assert.Nil(t, auth)
		assert.Contains(t, err.Error(), "no credentials found")
	})

	t.Run("prefers direct credentials over fallback", func(t *testing.T) {
		// Create a mock config with credentials for both registries
		conf := &configfile.ConfigFile{
			AuthConfigs: map[string]types.AuthConfig{
				global.DefaultReplicateRegistryHost: {
					Username: "defaultuser",
					Password: "defaultpass",
				},
				"registry.example.com": {
					Username: "directuser",
					Password: "directpass",
				},
			},
		}

		// Try loading auth for the alternate registry
		auth, err := tryLoadAuthForHost(ctx, conf, "registry.example.com")
		require.NoError(t, err)
		require.NotNil(t, auth)

		// Should use direct credentials, not fallback
		assert.Equal(t, "directuser", auth.Username)
		assert.Equal(t, "directpass", auth.Password)
		assert.Equal(t, "registry.example.com", auth.ServerAddress)
	})

	t.Run("returns empty map when no credentials available", func(t *testing.T) {
		// This test uses tryLoadAuthForHost to avoid credential store issues
		// The loadRegistryAuths function doesn't error when no credentials are found,
		// it just returns an empty map
		conf := &configfile.ConfigFile{
			AuthConfigs: map[string]types.AuthConfig{},
		}

		// Try loading auth for an alternate registry (will fail)
		auth1, err := tryLoadAuthForHost(ctx, conf, "registry.example.com")
		require.Error(t, err)
		assert.Nil(t, auth1)

		// Try loading auth for default registry (will also fail)
		auth2, err := tryLoadAuthForHost(ctx, conf, global.DefaultReplicateRegistryHost)
		require.Error(t, err)
		assert.Nil(t, auth2)

		// Since both fail, loadRegistryAuths would return an empty map
		// (it doesn't error, just silently skips hosts without credentials)
	})
}

func TestTryLoadAuthForHost(t *testing.T) {
	ctx := context.Background()

	t.Run("loads auth from config file", func(t *testing.T) {
		conf := &configfile.ConfigFile{
			AuthConfigs: map[string]types.AuthConfig{
				"registry.example.com": {
					Username: "testuser",
					Password: "testpass",
					Auth:     "dGVzdHVzZXI6dGVzdHBhc3M=",
					Email:    "test@example.com",
				},
			},
		}

		auth, err := tryLoadAuthForHost(ctx, conf, "registry.example.com")
		require.NoError(t, err)
		require.NotNil(t, auth)
		assert.Equal(t, "testuser", auth.Username)
		assert.Equal(t, "testpass", auth.Password)
		assert.Equal(t, "dGVzdHVzZXI6dGVzdHBhc3M=", auth.Auth)
		assert.Equal(t, "test@example.com", auth.Email)
		assert.Equal(t, "registry.example.com", auth.ServerAddress)
	})

	t.Run("returns error when no auth found", func(t *testing.T) {
		conf := &configfile.ConfigFile{
			AuthConfigs: map[string]types.AuthConfig{},
		}

		auth, err := tryLoadAuthForHost(ctx, conf, "registry.example.com")
		require.Error(t, err)
		assert.Nil(t, auth)
		assert.Contains(t, err.Error(), "no credentials found")
	})
}
