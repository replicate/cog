package docker

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/replicate/cog/pkg/config"

	"github.com/replicate/cog/pkg/util"
	"github.com/replicate/cog/pkg/util/console"
)

func Build(dir, dockerfile, imageName string, secrets []string, noCache bool, progressOutput string, epoch int64) error {
	var args []string

	args = append(args,
		"buildx", "build",
	)

	if util.IsAppleSiliconMac(runtime.GOOS, runtime.GOARCH) {
		// Fixes "WARNING: The requested image's platform (linux/amd64) does not match the detected host platform (linux/arm64/v8) and no specific platform was requested"
		args = append(args, "--platform", "linux/amd64", "--load")
	}

	for _, secret := range secrets {
		args = append(args, "--secret", secret)
	}

	if noCache {
		args = append(args, "--no-cache")
	}

	// Base Images are special, we force timestamp rewriting to epoch. This requires some consideration on the output
	// format. It's generally safe to override to --output type=docker,rewrite-timestamp=true as the use of `--load` is
	// equivalent to `--output type=docker`
	if epoch >= 0 {
		args = append(args,
			"--build-arg", fmt.Sprintf("SOURCE_DATE_EPOCH=%d", epoch),
			"--output", "type=docker,rewrite-timestamp=true")
		console.Infof("Forcing timestamp rewriting to epoch %d", epoch)

	}

	if config.BuildXCachePath != "" {
		args = append(
			args,
			"--cache-from", "type=local,src="+config.BuildXCachePath,
			"--cache-to", "type=local,dest="+config.BuildXCachePath,
		)
	} else {
		args = append(args, "--cache-to", "type=inline")
	}

	args = append(args,
		"--file", "-",
		"--tag", imageName,
		"--progress", progressOutput,
		".",
	)

	cmd := exec.Command("docker", args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stderr // redirect stdout to stderr - build output is all messaging
	cmd.Stderr = os.Stderr
	cmd.Stdin = strings.NewReader(dockerfile)

	console.Debug("$ " + strings.Join(cmd.Args, " "))
	return cmd.Run()
}

func BuildAddLabelsAndSchemaToImage(image string, labels map[string]string, bundledSchemaFile string, bundledSchemaPy string) error {
	var args []string

	args = append(args,
		"buildx", "build",
	)

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
	cmd := exec.Command("docker", args...)

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
