package docker

import (
	"context"
	"os/exec"
	"strings"

	"github.com/replicate/cog/pkg/util/console"
)

func ManifestInspect(ctx context.Context, image string) error {
	cmd := exec.CommandContext(ctx, "docker", "manifest", "inspect", image)
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out

	console.Debug("$ " + strings.Join(cmd.Args, " "))
	err := cmd.Run()

	if err != nil {
		output := out.String()
		if strings.Contains(output, "no such manifest") || strings.Contains(output, "manifest unknown") || strings.Contains(output, "not found") {
			return nil
		}
		return err
	}
	return nil
}
