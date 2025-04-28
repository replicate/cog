package docker

import (
	"context"
	"os"
	"os/exec"
	"strings"

	"github.com/replicate/cog/pkg/util/console"
)

func Pull(ctx context.Context, image string) error {
	cmd := exec.CommandContext(ctx, "docker", "pull", image)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	console.Debug("$ " + strings.Join(cmd.Args, " "))
	return cmd.Run()
}
