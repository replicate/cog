package docker

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/replicate/cog/pkg/util"
	"github.com/replicate/cog/pkg/util/console"
)

func Build(dir, dockerfile, imageName string, secrets []string, noCache bool, progressOutput string) error {
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

	args = append(args,
		"--file", "-",
		"--cache-to", "type=inline",
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

func BuildAddLabelsToImage(image string, labels map[string]string) error {
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

	dockerfile := "FROM " + image
	cmd.Stdin = strings.NewReader(dockerfile)

	console.Debug("$ " + strings.Join(cmd.Args, " "))

	if combinedOutput, err := cmd.CombinedOutput(); err != nil {
		console.Info(string(combinedOutput))
		return err
	}
	return nil
}
