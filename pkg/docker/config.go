package docker

import (
	"fmt"
	"os"

	"github.com/docker/cli/cli/config"
)

const CredentialHelperName = "cog"

func SetCogCredentialHelperForHost(host string) error {
	conf := config.LoadDefaultConfigFile(os.Stderr)
	conf.CredentialHelpers[host] = CredentialHelperName
	if err := conf.Save(); err != nil {
		return fmt.Errorf("Failed to save Docker config: %w", err)
	}
	return nil
}
