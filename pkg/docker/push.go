package docker

import (
	"context"
	"net/http"
	"time"

	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/util/console"
	"github.com/replicate/cog/pkg/web"
)

type BuildInfo struct {
	BuildTime time.Duration
	BuildID   string
}

func Push(ctx context.Context, image string, projectDir string, command command.Command, buildInfo BuildInfo, client *http.Client) error {
	webClient := web.NewClient(command, client)

	if err := webClient.PostPushStart(ctx, buildInfo.BuildID, buildInfo.BuildTime); err != nil {
		console.Warnf("Failed to send build timings to server: %v", err)
	}

	return StandardPush(ctx, image, command)
}
