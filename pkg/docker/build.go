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

func Build(dir, dockerfile, imageName, progressOutput string) error {
	var cmd *exec.Cmd
	if util.IsM1Mac(runtime.GOOS, runtime.GOARCH) {
		cmd = m1BuildxCommand(imageName, progressOutput)
	} else {
		cmd = buildKitCommand(imageName, progressOutput)
	}
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = strings.NewReader(dockerfile)

	console.Debug("$ " + strings.Join(cmd.Args, " "))
	return cmd.Run()
}

func BuildAddLabelsToImage(image string, labels map[string]string) error {
	dockerfile := "FROM " + image
	args := []string{
		"build",
		"-f", "-",
		"-t", image,
	}
	for k, v := range labels {
		// Unlike in Dockerfiles, the value here does not need quoting -- Docker merely
		// splits on the first '=' in the argument and the rest is the label value.
		args = append(args, "--label", fmt.Sprintf(`%s=%s`, k, v))
	}
	// We're not using context, but Docker requires we pass a context
	args = append(args, ".")
	cmd := exec.Command("docker", args...)
	cmd.Stdin = strings.NewReader(dockerfile)

	console.Debug("$ " + strings.Join(cmd.Args, " "))

	if combinedOutput, err := cmd.CombinedOutput(); err != nil {
		console.Info(string(combinedOutput))
		return err
	}
	return nil
}

func buildKitCommand(imageName, progressOutput string) *exec.Cmd {
	cmd := exec.Command(
		"docker", "build", ".",
		"-f", "-",
		"--build-arg", "BUILDKIT_INLINE_CACHE=1",
		"-t", imageName,
		"--progress", progressOutput,
	)
	cmd.Env = append(os.Environ(), "DOCKER_BUILDKIT=1")
	return cmd
}

func m1BuildxCommand(imageName, progressOutput string) *exec.Cmd {
	cmd := exec.Command(
		"docker", "buildx", "build", ".",
		"-f", "-",
		"--build-arg", "BUILDKIT_INLINE_CACHE=1",
		"-t", imageName,
		"--platform", "linux/amd64",
		"--progress", progressOutput,
	)
	cmd.Env = append(os.Environ(), "DOCKER_BUILDKIT=1")
	return cmd
}
