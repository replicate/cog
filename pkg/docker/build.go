package docker

import (
	"os"
	"os/exec"
	"strings"

	"github.com/replicate/cog/pkg/util/console"
)

func Build(dir, dockerfile, imageName string) error {
	cmd := exec.Command(
		"docker", "build", ".",
		"-f", "-",
		"--build-arg", "BUILDKIT_INLINE_CACHE=1",
		"-t", imageName,
	)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = strings.NewReader(dockerfile)
	cmd.Env = append(os.Environ(), "DOCKER_BUILDKIT=1")

	console.Debug("$ " + strings.Join(cmd.Args, " "))
	return cmd.Run()
}
