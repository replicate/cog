package docker

import (
	"fmt"
	"os"

	dconfig "github.com/docker/cli/cli/config"
	dctxdocker "github.com/docker/cli/cli/context/docker"
	dctxstore "github.com/docker/cli/cli/context/store"

	"github.com/replicate/cog/pkg/util/console"
)

// determineDockerHost returns the host to use for the docker client.
// It first checks the DOCKER_HOST environment variable, then the docker context, and finally the system default.
func determineDockerHost() (string, error) {
	// 1) if DOCKER_HOST is set, use it
	if host := os.Getenv("DOCKER_HOST"); host != "" {
		return host, nil
	}

	// 2) try to get a host from the docker context. Use DOCKER_CONTEXT if set, otherwise check the current context
	if host, err := dockerHostFromContext(os.Getenv("DOCKER_CONTEXT")); err != nil {
		console.Warnf("error finding docker host from context: %v", err)

		// if DOCKER_CONTEXT was explicitly set, return an error since the user probably expects that context to be used
		if os.Getenv("DOCKER_CONTEXT") != "" {
			return "", err
		}
	} else if host != "" {
		return host, nil
	}

	// 3) if we couldn't get a host from env or context, fallback to the system default
	return defaultDockerHost, nil
}

func dockerHostFromContext(contextName string) (string, error) {
	if contextName == "" {
		cf, err := dconfig.Load(dconfig.Dir())
		if err != nil {
			return "", err
		}
		contextName = cf.CurrentContext
	}

	typeGetter := func() any { return &dctxdocker.EndpointMeta{} }
	storeConfig := dctxstore.NewConfig(typeGetter, dctxstore.EndpointTypeGetter(dctxdocker.DockerEndpoint, typeGetter))

	store := dctxstore.New(dconfig.ContextStoreDir(), storeConfig)
	meta, err := store.GetMetadata(contextName)
	if err != nil {
		return "", err
	}

	endpoint, ok := meta.Endpoints[dctxdocker.DockerEndpoint]
	if !ok {
		return "", fmt.Errorf("no docker endpoints found for context %s", contextName)
	}

	dockerEPMeta, ok := endpoint.(dctxdocker.EndpointMeta)
	if !ok {
		return "", fmt.Errorf("invalid context config: %v", endpoint)
	}

	if dockerEPMeta.Host == "" {
		return "", fmt.Errorf("no host found for context %s", contextName)
	}

	return dockerEPMeta.Host, nil
}
