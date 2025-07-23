package docker

import (
	"github.com/docker/docker/api/types/registry"
	"github.com/docker/docker/client"
)

type clientOptions struct {
	authConfigs map[string]registry.AuthConfig
	host        string
	client      client.APIClient
}

type Option func(*clientOptions)

func WithAuthConfig(authConfig registry.AuthConfig) Option {
	return func(o *clientOptions) {
		o.authConfigs[authConfig.ServerAddress] = authConfig
	}
}

func WithHost(host string) Option {
	return func(o *clientOptions) {
		o.host = host
	}
}

func WithClient(client client.APIClient) Option {
	return func(o *clientOptions) {
		o.client = client
	}
}
