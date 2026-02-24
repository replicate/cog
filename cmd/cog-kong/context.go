package main

import (
	"context"

	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/provider"
	"github.com/replicate/cog/pkg/provider/setup"
	"github.com/replicate/cog/pkg/registry"
)

// provideDockerClient creates a Docker client, binding to the command.Command interface.
// Registered as a singleton provider so all commands share one connection.
func provideDockerClient(ctx context.Context) (command.Command, error) {
	return docker.NewClient(ctx)
}

// provideRegistryClient creates a registry client, binding to the registry.Client interface.
func provideRegistryClient() registry.Client {
	return registry.NewRegistryClient()
}

// provideProviderRegistry creates a provider registry with all built-in providers.
// This replaces the setup.Init() global side-effect pattern used in the cobra CLI.
func provideProviderRegistry() *provider.Registry {
	return setup.NewRegistry()
}
