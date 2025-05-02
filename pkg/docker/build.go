package docker

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"github.com/replicate/cog/pkg/util"
	"github.com/replicate/cog/pkg/util/console"
)

func BuildAddLabelsAndSchemaToImage(ctx context.Context, image string, labels map[string]string, bundledSchemaFile string, bundledSchemaPy string) error {
	args := []string{
		"buildx", "build",
		// disable provenance attestations since we don't want them cluttering the registry
		"--provenance", "false",
	}

	if util.IsAppleSiliconMac(runtime.GOOS, runtime.GOARCH) {
		// Fixes "WARNING: The requested image's platform (linux/amd64) does not match the detected host platform (linux/arm64/v8) and no specific platform was requested"
		args = append(args, "--platform", "linux/amd64", "--load")
	}

	args = append(args,
		"--file", "-",
		"--tag", image,
	)
	for k, v := range labels {
		// Unlike in Dockerfiles, the value here does not need quoting -- Docker merely
		// splits on the first '=' in the argument and the rest is the label value.
		args = append(args, "--label", fmt.Sprintf(`%s=%s`, k, v))
	}
	// We're not using context, but Docker requires we pass a context
	args = append(args, ".")
	cmd := exec.CommandContext(ctx, "docker", args...)

	dockerfile := "FROM " + image + "\n"
	dockerfile += "COPY " + bundledSchemaFile + " .cog\n"
	cmd.Stdin = strings.NewReader(dockerfile)

	console.Debug("$ " + strings.Join(cmd.Args, " "))

	if combinedOutput, err := cmd.CombinedOutput(); err != nil {
		console.Info(string(combinedOutput))
		return err
	}
	return nil
}
