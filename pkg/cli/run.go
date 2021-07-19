package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/dockerfile"
	"github.com/replicate/cog/pkg/util/console"
	"github.com/spf13/cobra"
)

func newRunCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run <command> [arg...]",
		Short: "Run a command inside a Docker environment",
		RunE:  run,
		Args:  cobra.MinimumNArgs(1),
	}

	flags := cmd.Flags()
	// Flags after first argment are considered args and passed to command
	flags.SetInterspersed(false)

	return cmd
}

func run(cmd *cobra.Command, args []string) error {
	// TODO: support multiple run architectures, or automatically select arch based on host
	arch := "cpu"

	cfg, projectDir, err := config.GetConfig(projectDirFlag)
	if err != nil {
		return err
	}

	// TODO: better image management so we don't eat up disk space
	image := config.BaseDockerImageName(projectDir)

	// FIXME: refactor to share with predict
	console.Info("Building Docker image from environment in cog.yaml...")

	generator := dockerfile.NewGenerator(cfg, arch, projectDir)
	defer func() {
		if err := generator.Cleanup(); err != nil {
			console.Warnf("Error cleaning up Dockerfile generator: %s", err)
		}
	}()
	dockerfileContents, err := generator.GenerateBase()
	if err != nil {
		return fmt.Errorf("Failed to generate Dockerfile for %s: %w", arch, err)
	}

	if err := docker.Build(projectDir, dockerfileContents, image); err != nil {
		return fmt.Errorf("Failed to build Docker image: %w", err)
	}

	// TODO(bfirsh): ports
	ports := []string{}

	console.Infof("Running '%s' in Docker with the current directory mounted as a volume...", strings.Join(args, " "))

	dockerArgs := []string{
		"run",
		"--interactive",
		"--rm",
		"--shm-size", "8G", // https://github.com/pytorch/pytorch/issues/2244
		// TODO: escape
		"--volume", projectDir + ":/code",
	}
	for _, port := range ports {
		dockerArgs = append(dockerArgs, "-p", port+":"+port)
	}
	if isatty.IsTerminal(os.Stdin.Fd()) {
		dockerArgs = append(dockerArgs, "--tty")
	}
	dockerArgs = append(dockerArgs, image)
	dockerArgs = append(dockerArgs, args...)

	dockerCmd := exec.Command("docker", dockerArgs...)
	dockerCmd.Env = os.Environ()
	dockerCmd.Stdout = os.Stdout
	dockerCmd.Stderr = os.Stderr
	dockerCmd.Stdin = os.Stdin

	return dockerCmd.Run()
}
