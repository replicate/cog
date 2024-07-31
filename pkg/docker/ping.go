package docker

import (
	"context"
	"fmt"
	"time"

	"github.com/docker/docker/client"
)

func Ping(ctx context.Context, duration time.Duration) error {
	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		return fmt.Errorf("failed to create docker client: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()

	_, err = cli.Ping(ctx)
	if err != nil {
		return fmt.Errorf("failed to ping docker, please try restarting the docker daemon: %w", err)
	}

	return nil
}
