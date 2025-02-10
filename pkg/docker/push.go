package docker

import (
	"context"

	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/http"
	"github.com/replicate/cog/pkg/monobeam"
	"github.com/replicate/cog/pkg/web"
)

func Push(image string, fast bool, projectDir string, command command.Command) error {
	if fast {
		client, err := http.ProvideHTTPClient(command)
		if err != nil {
			return err
		}
		webClient := web.NewClient(command, client)
		monobeamClient := monobeam.NewClient(client)
		return FastPush(context.Background(), image, projectDir, command, webClient, monobeamClient)
	}
	return StandardPush(image, command)
}
