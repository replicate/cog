package docker

import "github.com/docker/docker/api/types/registry"

type clientOptions struct {
	authConfigs map[string]registry.AuthConfig
	host        string
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
