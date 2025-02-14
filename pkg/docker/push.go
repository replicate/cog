package docker

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/http"
	"github.com/replicate/cog/pkg/monobeam"
	"github.com/replicate/cog/pkg/util/console"
	"github.com/replicate/cog/pkg/web"
)

func Push(image string, fast bool, projectDir string, command command.Command, buildTime time.Duration) error {
	ctx := context.Background()
	client, err := http.ProvideHTTPClient(command)
	if err != nil {
		return err
	}
	webClient := web.NewClient(command, client)

	// For the timing flow, on error we will just log and continue since
	// this is just a loss of push timing information
	imageID := ""
	if fast {
		// Generate 256 bit random hash (4x64 bits) to use as an upload ID
		for i := 0; i < 4; i++ {
			// Ignoring the linter warning about math/rand/v2 not being cryptographically secure
			// because this just needs to be a "unique enough" ID for a cache between when the
			// push starts and ends, which should only be ~a week max.
			imageID = fmt.Sprintf("%s%x", imageID, rand.Int64()) //nolint:gosec
		}
	} else {
		imageMeta, err := command.Inspect(image)
		if err != nil {
			console.Warnf("Failed to inspect image: %v", err)
		}
		parts := strings.Split(imageMeta.ID, ":")
		if len(parts) != 2 {
			console.Warn("Image ID was not of the form sha:hash")
		}
		imageID = parts[1]
	}

	err = webClient.PostBuildStart(ctx, imageID, buildTime)
	if err != nil {
		console.Warnf("Failed to send build timings to server: %v", err)
	}

	if fast {
		monobeamClient := monobeam.NewClient(client)
		return FastPush(ctx, image, projectDir, command, webClient, monobeamClient, imageID)
	}
	return StandardPush(image, command)
}
