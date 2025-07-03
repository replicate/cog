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
		console.Debug("using docker host from DOCKER_HOST")

		return host, nil
	}

	// 2) try to get a host from the docker context. Use DOCKER_CONTEXT if set, otherwise check the current context
	if host, contextName, err := dockerHostFromContext(os.Getenv("DOCKER_CONTEXT")); err != nil {
		console.Debugf("could not find docker host from context %q: %v", contextName, err)

		// if DOCKER_CONTEXT was explicitly set, return an error since the user probably expects that context to be used
		if os.Getenv("DOCKER_CONTEXT") != "" {
			return "", err
		}
	} else if host != "" {
		console.Debugf("using docker host from context %q", contextName)

		return host, nil
	}

	console.Debug("using system default docker host")

	// 3) if we couldn't get a host from env or context, fallback to the system default
	return defaultDockerHost, nil
}

func dockerHostFromContext(contextName string) (string, string, error) {
	if contextName == "" {
		cf, err := dconfig.Load(dconfig.Dir())
		if err != nil {
			return "", "", fmt.Errorf("error loading docker config: %w", err)
		}
		contextName = cf.CurrentContext
	}

	typeGetter := func() any { return &dctxdocker.EndpointMeta{} }
	storeConfig := dctxstore.NewConfig(typeGetter, dctxstore.EndpointTypeGetter(dctxdocker.DockerEndpoint, typeGetter))

	store := dctxstore.New(dconfig.ContextStoreDir(), storeConfig)
	meta, err := store.GetMetadata(contextName)
	if err != nil {
		return "", contextName, fmt.Errorf("error getting metadata for context %q: %w", contextName, err)
	}

	endpoint, ok := meta.Endpoints[dctxdocker.DockerEndpoint]
	if !ok {
		return "", contextName, fmt.Errorf("no docker endpoints found for context %q", contextName)
	}

	dockerEPMeta, ok := endpoint.(dctxdocker.EndpointMeta)
	if !ok {
		return "", contextName, fmt.Errorf("invalid context config: %v", endpoint)
	}

	if dockerEPMeta.Host == "" {
		return "", contextName, fmt.Errorf("no host found for context %q", contextName)
	}

	return dockerEPMeta.Host, contextName, nil
}
