package docker

import (
	"context"
	"net/http"
	"time"

	"github.com/replicate/cog/pkg/api"
	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/monobeam"
	"github.com/replicate/cog/pkg/util/console"
	"github.com/replicate/cog/pkg/web"
)

type BuildInfo struct {
	BuildTime time.Duration
	BuildID   string
	Pipeline  bool
}

func Push(ctx context.Context, image string, fast bool, projectDir string, command command.Command, buildInfo BuildInfo, client *http.Client, cfg *config.Config) error {
	webClient := web.NewClient(command, client)

	if buildInfo.Pipeline {
		apiClient := api.NewClient(command, client, webClient)
		return PipelinePush(ctx, image, projectDir, apiClient, client, cfg)
	}

	if err := webClient.PostPushStart(ctx, buildInfo.BuildID, buildInfo.BuildTime); err != nil {
		console.Warnf("Failed to send build timings to server: %v", err)
	}

	if fast {
		monobeamClient := monobeam.NewClient(client)
		if err := monobeamClient.PostPreUpload(ctx); err != nil {
			// The pre upload is not required, just helpful. If it fails, log and continue
			console.Debugf("Failed to POST pre_upload: %v", err)
		}
		return FastPush(ctx, image, projectDir, command, webClient, monobeamClient)
	}
	return StandardPush(ctx, image, command)
}
