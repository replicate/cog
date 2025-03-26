package docker

import (
	"context"
	"time"

	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/http"
	"github.com/replicate/cog/pkg/monobeam"
	"github.com/replicate/cog/pkg/util/console"
	"github.com/replicate/cog/pkg/web"
)

type BuildInfo struct {
	BuildTime time.Duration
	BuildID   string
}

func Push(image string, fast bool, projectDir string, command command.Command, buildInfo BuildInfo) error {
	ctx := context.Background()
	client, err := http.ProvideHTTPClient(command)
	if err != nil {
		return err
	}
	webClient := web.NewClient(command, client)

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
	return StandardPush(image, command)
}
